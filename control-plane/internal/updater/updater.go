// Package updater provides self-update capability for the Hive control-plane
// running inside Docker Swarm. It checks GitHub releases and updates Swarm
// services to newer images.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/version"
)

const (
	githubAPIURL = "https://api.github.com/repos/LukasParke/hive/releases/latest"
	ghcrBase     = "ghcr.io/lukasparke/hive"
)

// checkInterval is a var only so tests can shrink the polling loop; it is
// never mutated in production.
var checkInterval = 15 * time.Minute

// Status describes whether an update is available.
type Status struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LastCheckedAt   string `json:"lastCheckedAt,omitempty"`
}

// Updater checks for new releases and can trigger Swarm service updates.
type Updater struct {
	swarm      *swarmclient.Client
	httpClient *http.Client
	releaseURL string // GitHub releases endpoint; overridable in tests
	status     Status
}

// New creates an Updater.
func New(sw *swarmclient.Client) *Updater {
	return &Updater{
		swarm: sw,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		releaseURL: githubAPIURL,
		status: Status{
			CurrentVersion: version.Current,
		},
	}
}

// Status returns the current update status.
func (u *Updater) Status() Status {
	return u.status
}

// CheckNow fetches the latest release from GitHub and updates internal status.
func (u *Updater) CheckNow(ctx context.Context) error {
	latest, err := u.fetchLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}

	u.status.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
	u.status.LatestVersion = latest
	u.status.UpdateAvailable = version.Compare(latest, version.Current) > 0 && !version.IsDev()

	slog.Info("update check complete", "current", version.Current, "latest", latest, "available", u.status.UpdateAvailable)
	return nil
}

// Run starts a background loop that checks for updates periodically.
func (u *Updater) Run(ctx context.Context) {
	// Check immediately on startup
	if err := u.CheckNow(ctx); err != nil {
		slog.Warn("initial update check failed", "error", err)
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := u.CheckNow(ctx); err != nil {
				slog.Warn("update check failed", "error", err)
			}
		}
	}
}

// Update triggers a Swarm rolling update to the latest version.
// It updates both the control-plane and agent services.
func (u *Updater) Update(ctx context.Context) error {
	if u.status.LatestVersion == "" {
		if err := u.CheckNow(ctx); err != nil {
			return err
		}
	}
	if !u.status.UpdateAvailable {
		return fmt.Errorf("no update available (current=%s latest=%s)", version.Current, u.status.LatestVersion)
	}

	latest := u.status.LatestVersion

	// Update control-plane service
	if err := u.updateServiceByImagePrefix(ctx, fmt.Sprintf("%s/control-plane", ghcrBase), latest); err != nil {
		return fmt.Errorf("update control-plane: %w", err)
	}

	// Update agent service
	if err := u.updateServiceByImagePrefix(ctx, fmt.Sprintf("%s/agent", ghcrBase), latest); err != nil {
		return fmt.Errorf("update agent: %w", err)
	}

	slog.Info("swarm update triggered", "version", latest)
	return nil
}

// updateServiceByImagePrefix finds a Swarm service whose image starts with the
// given prefix and updates it to the new tag.
func (u *Updater) updateServiceByImagePrefix(ctx context.Context, imagePrefix, newTag string) error {
	services, err := u.swarm.ListServices(ctx)
	if err != nil {
		return err
	}

	for _, svc := range services {
		if svc.Spec.TaskTemplate.ContainerSpec == nil {
			continue
		}
		img := svc.Spec.TaskTemplate.ContainerSpec.Image
		if !strings.HasPrefix(img, imagePrefix) {
			continue
		}

		// Construct new image ref preserving digest if present
		newImage := fmt.Sprintf("%s:%s", imagePrefix, newTag)
		if at := strings.Index(img, "@sha256:"); at >= 0 {
			newImage = fmt.Sprintf("%s:%s%s", imagePrefix, newTag, img[at:])
		}

		svc.Spec.TaskTemplate.ContainerSpec.Image = newImage

		version := svc.Version.Index

		if err := u.swarm.UpdateService(ctx, svc.ID, version, svc.Spec); err != nil {
			return fmt.Errorf("update service %s: %w", svc.Spec.Name, err)
		}

		slog.Info("updated swarm service", "service", svc.Spec.Name, "image", newImage)
		return nil
	}

	return fmt.Errorf("no swarm service found with image prefix %s", imagePrefix)
}

// fetchLatestRelease queries the GitHub API for the latest release tag name.
func (u *Updater) fetchLatestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.releaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github API returned %d: %s", resp.StatusCode, string(body))
	}

	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}

	if rel.TagName == "" {
		return "", fmt.Errorf("latest release has no tag_name")
	}

	return rel.TagName, nil
}

// GetCurrentImageTag returns the image tag currently used by a service matching
// the given image prefix. This is useful for diagnostics.
func (u *Updater) GetCurrentImageTag(ctx context.Context, imagePrefix string) (string, error) {
	services, err := u.swarm.ListServices(ctx)
	if err != nil {
		return "", err
	}
	for _, svc := range services {
		if svc.Spec.TaskTemplate.ContainerSpec == nil {
			continue
		}
		img := svc.Spec.TaskTemplate.ContainerSpec.Image
		if strings.HasPrefix(img, imagePrefix) {
			// Extract tag from image ref
			if i := strings.LastIndex(img, ":"); i > 0 {
				return img[i+1:], nil
			}
			return "latest", nil
		}
	}
	return "", fmt.Errorf("no service found with image prefix %s", imagePrefix)
}
