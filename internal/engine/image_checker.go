package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
)

var infraServices = map[string]bool{
	"hive-nats":          true,
	"hive-traefik":       true,
	"hive-registry":      true,
	"hive-cadvisor":      true,
	"hive-engine":        true,
	"hive-manager":       true,
	"hive-node-exporter": true,
	"hive-prometheus":    true,
}

func isInfraService(name string) bool {
	if infraServices[name] {
		return true
	}
	return strings.HasPrefix(name, "hive-pg-")
}

type ImageChecker struct {
	sc       *swarm.Client
	db       *store.Store
	log      *zap.SugaredLogger
	interval time.Duration
	client   *http.Client
}

func NewImageChecker(sc *swarm.Client, db *store.Store, log *zap.SugaredLogger) *ImageChecker {
	return &ImageChecker{
		sc:       sc,
		db:       db,
		log:      log,
		interval: 4 * time.Hour,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (ic *ImageChecker) Run(ctx context.Context) {
	ic.log.Info("image update checker started")

	ic.cleanupInfraStatuses(ctx)
	ic.checkAll(ctx)

	ticker := time.NewTicker(ic.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ic.checkAll(ctx)
		}
	}
}

func (ic *ImageChecker) CheckOnce(ctx context.Context) error {
	ic.checkAll(ctx)
	return nil
}

func (ic *ImageChecker) cleanupInfraStatuses(ctx context.Context) {
	if ic.db == nil {
		return
	}
	names := make([]string, 0, len(infraServices))
	for name := range infraServices {
		names = append(names, name)
	}
	if err := ic.db.DeleteInfraServiceUpdateStatuses(ctx, names); err != nil {
		ic.log.Warnf("image checker: cleanup infra statuses: %v", err)
	} else {
		ic.log.Info("image checker: cleaned up infrastructure service update statuses")
	}
}

func (ic *ImageChecker) checkAll(ctx context.Context) {
	if ic.sc == nil {
		return
	}

	services, err := ic.sc.Docker().ServiceList(ctx, dockerswarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "hive.managed=true")),
	})
	if err != nil {
		ic.log.Warnf("image checker: list services: %v", err)
		return
	}

	updatesFound := 0
	for _, svc := range services {
		if isInfraService(svc.Spec.Name) {
			continue
		}

		image := svc.Spec.TaskTemplate.ContainerSpec.Image

		cleanImage, currentDigest := parseImageWithDigest(image)
		repo, tag := parseRepoTag(cleanImage)

		latestDigest, latestTag, err := ic.checkForUpdate(ctx, repo, tag)
		if err != nil {
			ic.log.Debugf("image checker: %s: %v", svc.Spec.Name, err)
			continue
		}

		hasUpdate := false
		if currentDigest != "" && latestDigest != "" && currentDigest != latestDigest {
			hasUpdate = true
		} else if latestTag != "" && latestTag != tag {
			hasUpdate = true
		}

		appID := svc.Spec.Labels["hive.app_id"]
		stackID := svc.Spec.Labels["hive.stack_id"]

		sus := &store.ServiceUpdateStatus{
			AppID:           appID,
			StackID:         stackID,
			ServiceName:     svc.Spec.Name,
			CurrentImage:    cleanImage,
			CurrentDigest:   currentDigest,
			LatestDigest:    latestDigest,
			LatestVersion:   latestTag,
			UpdateAvailable: hasUpdate,
		}

		if ic.db != nil {
			if err := ic.db.UpsertServiceUpdateStatus(ctx, sus); err != nil {
				ic.log.Warnf("image checker: persist %s: %v", svc.Spec.Name, err)
			}
		}

		if hasUpdate {
			updatesFound++
			ic.log.Infof("image update available: %s (%s -> %s)", svc.Spec.Name, tag, latestTag)

			data, _ := json.Marshal(map[string]any{
				"type": "service_update_available",
				"payload": map[string]any{
					"service_name":   svc.Spec.Name,
					"current_image":  cleanImage,
					"latest_version": latestTag,
				},
				"ts": time.Now().Unix(),
			})
			getUpdatesHub().broadcast(data)
		}
	}

	ic.log.Infof("image check complete: %d services checked, %d updates available", len(services), updatesFound)
}

func (ic *ImageChecker) checkForUpdate(ctx context.Context, repo, tag string) (digest string, latestTag string, err error) {
	registry, imagePath := splitRegistry(repo)

	switch {
	case registry == "" || registry == "docker.io" || registry == "index.docker.io":
		return ic.checkDockerHub(ctx, imagePath, tag)
	case registry == "ghcr.io":
		return ic.checkGHCR(ctx, imagePath, tag)
	default:
		return ic.checkGenericRegistry(ctx, registry, imagePath, tag)
	}
}

func (ic *ImageChecker) checkDockerHub(ctx context.Context, imagePath, tag string) (string, string, error) {
	if !strings.Contains(imagePath, "/") {
		imagePath = "library/" + imagePath
	}

	digestURL := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags/%s", imagePath, tag)
	req, _ := http.NewRequestWithContext(ctx, "GET", digestURL, nil)

	resp, err := ic.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("docker hub request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("docker hub status %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Digest      string `json:"digest"`
		LastUpdated string `json:"last_updated"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", err
	}

	if isSemverTag(tag) {
		latestTag, _ := ic.findLatestSemverTag(ctx, "docker.io", imagePath, tag)
		if latestTag != "" && latestTag != tag {
			return result.Digest, latestTag, nil
		}
	}

	return result.Digest, "", nil
}

func (ic *ImageChecker) checkGHCR(ctx context.Context, imagePath, tag string) (string, string, error) {
	tokenURL := fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull", imagePath)
	tokenReq, _ := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	tokenResp, err := ic.client.Do(tokenReq)
	if err != nil {
		return "", "", fmt.Errorf("ghcr token: %w", err)
	}
	defer tokenResp.Body.Close()

	var tokenResult struct {
		Token string `json:"token"`
	}
	tokenBody, _ := io.ReadAll(tokenResp.Body)
	if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
		return "", "", err
	}

	manifestURL := fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", imagePath, tag)
	req, _ := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	req.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json")

	resp, err := ic.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("ghcr manifest: %w", err)
	}
	defer resp.Body.Close()

	digest := resp.Header.Get("Docker-Content-Digest")

	if isSemverTag(tag) {
		latestTag, _ := ic.findLatestSemverTag(ctx, "ghcr.io", imagePath, tag)
		if latestTag != "" && latestTag != tag {
			return digest, latestTag, nil
		}
	}

	return digest, "", nil
}

func (ic *ImageChecker) checkGenericRegistry(ctx context.Context, registry, imagePath, tag string) (string, string, error) {
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", registry, imagePath, tag)
	req, _ := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.oci.image.index.v1+json")

	resp, err := ic.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("registry request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("registry status %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	return digest, "", nil
}

func (ic *ImageChecker) findLatestSemverTag(ctx context.Context, registry, imagePath, currentTag string) (string, error) {
	var tagsURL string
	var authHeader string

	switch registry {
	case "docker.io", "":
		tagsURL = fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100&ordering=-last_updated", imagePath)
	case "ghcr.io":
		tokenURL := fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull", imagePath)
		tokenReq, _ := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
		tokenResp, err := ic.client.Do(tokenReq)
		if err != nil {
			return "", err
		}
		defer tokenResp.Body.Close()
		var t struct {
			Token string `json:"token"`
		}
		b, _ := io.ReadAll(tokenResp.Body)
		_ = json.Unmarshal(b, &t)
		authHeader = "Bearer " + t.Token
		tagsURL = fmt.Sprintf("https://ghcr.io/v2/%s/tags/list", imagePath)
	default:
		tagsURL = fmt.Sprintf("https://%s/v2/%s/tags/list", registry, imagePath)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", tagsURL, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := ic.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	currentParts := parseSemver(currentTag)
	if currentParts == nil {
		return "", nil
	}

	var tags []string
	if registry == "docker.io" || registry == "" {
		var result struct {
			Results []struct {
				Name string `json:"name"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", err
		}
		for _, r := range result.Results {
			tags = append(tags, r.Name)
		}
	} else {
		var result struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", err
		}
		tags = result.Tags
	}

	var candidates []string
	currentPrefix := semverPrefix(currentTag)
	for _, t := range tags {
		if !isSemverTag(t) {
			continue
		}
		if currentPrefix != "" && !strings.HasPrefix(t, currentPrefix) {
			continue
		}
		parts := parseSemver(t)
		if parts != nil && compareSemver(parts, currentParts) > 0 {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		return "", nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		a := parseSemver(candidates[i])
		b := parseSemver(candidates[j])
		return compareSemver(a, b) > 0
	})

	return candidates[0], nil
}

// --- Helpers ---

func parseImageWithDigest(image string) (cleanImage, digest string) {
	if idx := strings.Index(image, "@"); idx >= 0 {
		return image[:idx], image[idx+1:]
	}
	return image, ""
}

func parseRepoTag(image string) (repo, tag string) {
	parts := splitImageTag(image)
	return parts[0], parts[1]
}

func splitRegistry(repo string) (registry, path string) {
	if strings.Contains(repo, ".") || strings.Contains(repo, ":") {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return "", repo
}

var semverRegex = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(.*)$`)

func isSemverTag(tag string) bool {
	return semverRegex.MatchString(tag) && tag != "latest"
}

func parseSemver(tag string) []int {
	m := semverRegex.FindStringSubmatch(tag)
	if m == nil {
		return nil
	}
	parts := make([]int, 3)
	parts[0], _ = strconv.Atoi(m[1])
	if m[2] != "" {
		parts[1], _ = strconv.Atoi(m[2])
	}
	if m[3] != "" {
		parts[2], _ = strconv.Atoi(m[3])
	}
	return parts
}

func compareSemver(a, b []int) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] - b[i]
		}
	}
	return 0
}

func semverPrefix(tag string) string {
	if strings.HasPrefix(tag, "v") {
		return "v"
	}
	return ""
}
