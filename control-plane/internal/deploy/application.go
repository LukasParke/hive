package deploy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/network"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	dockernet "github.com/moby/moby/api/types/network"
	dockerswarm "github.com/moby/moby/api/types/swarm"
)

// EnvVar is a single environment variable for an application.
type EnvVar struct {
	Key        string
	Value      string // empty for secrets
	IsSecret   bool
	SecretName string // Docker secret name, empty for non-secrets
}

// ApplicationSpec fully describes an application deployment.
type ApplicationSpec struct {
	AppID         string
	ServiceName   string
	Image         string
	ContainerPort int
	EnvVars       []EnvVar

	// ProjectSlug attaches the shared project overlay network
	// hive_project_{slug}. Empty (zero value) skips project network
	// attachment, preserving pre-existing behavior.
	ProjectSlug string

	// DomainLookup reports the domains routed to the application. When the
	// app has at least one domain, the shared hive_proxy overlay is attached
	// so Traefik can reach the service. Nil (default) never attaches
	// hive_proxy; callers (jobs handlers) inject a DB-backed lookup.
	DomainLookup func(ctx context.Context, appID string) ([]string, error)
}

// SwarmStack is the slice of the swarm client stack and application
// deployment need. *swarmclient.Client satisfies it; tests inject fakes.
type SwarmStack interface {
	ListServices(ctx context.Context) ([]dockerswarm.Service, error)
	CreateService(ctx context.Context, spec dockerswarm.ServiceSpec) (string, error)
	UpdateService(ctx context.Context, id string, version uint64, spec dockerswarm.ServiceSpec) error
	RemoveService(ctx context.Context, id string) error
	ListNetworks(ctx context.Context) ([]dockernet.Summary, error)
	CreateNetwork(ctx context.Context, name string) (string, error)
	ListSecrets(ctx context.Context) ([]dockerswarm.Secret, error)
	CreateSecret(ctx context.Context, spec dockerswarm.SecretSpec) (string, error)
	ListConfigs(ctx context.Context) ([]dockerswarm.Config, error)
	CreateConfig(ctx context.Context, spec dockerswarm.ConfigSpec) (string, error)
	UpdateConfig(ctx context.Context, id string, version uint64, spec dockerswarm.ConfigSpec) error
}

// Compile-time check that the concrete swarm client satisfies SwarmStack.
var _ SwarmStack = (*swarmclient.Client)(nil)

// Application creates or updates the swarm service for an application.
func Application(ctx context.Context, cli SwarmStack, spec ApplicationSpec) error {
	serviceName := normalizeServiceName(spec.ServiceName, spec.AppID)
	services, err := cli.ListServices(ctx)
	if err != nil {
		return err
	}

	var existing *dockerswarm.Service
	for i := range services {
		svc := services[i]
		if svc.Spec.Labels["hive.app.id"] == spec.AppID {
			existing = &svc
			break
		}
	}

	var envStrings []string
	var secretRefs []*dockerswarm.SecretReference
	for _, ev := range spec.EnvVars {
		if ev.IsSecret {
			secretRefs = append(secretRefs, &dockerswarm.SecretReference{
				File: &dockerswarm.SecretReferenceFileTarget{
					Name: ev.Key,
					UID:  "0",
					GID:  "0",
					Mode: 0o400,
				},
				SecretName: ev.SecretName,
			})
		} else {
			envStrings = append(envStrings, ev.Key+"="+ev.Value)
		}
	}

	serviceSpec := dockerswarm.ServiceSpec{
		Annotations: dockerswarm.Annotations{
			Name: serviceName,
			Labels: map[string]string{
				"hive.app.id":   spec.AppID,
				"hive.app.port": strconv.Itoa(spec.ContainerPort),
			},
		},
		TaskTemplate: dockerswarm.TaskSpec{
			ContainerSpec: &dockerswarm.ContainerSpec{
				Image:   spec.Image,
				Env:     envStrings,
				Secrets: secretRefs,
				Labels: map[string]string{
					"hive.app.id":   spec.AppID,
					"hive.app.port": strconv.Itoa(spec.ContainerPort),
				},
			},
			RestartPolicy: &dockerswarm.RestartPolicy{
				Condition:   dockerswarm.RestartPolicyConditionAny,
				Delay:       nil,
				MaxAttempts: nil,
			},
		},
		Mode: dockerswarm.ServiceMode{
			Replicated: &dockerswarm.ReplicatedService{Replicas: ptrUint64(1)},
		},
		UpdateConfig: &dockerswarm.UpdateConfig{
			Order:         "start-first",
			FailureAction: "rollback",
		},
		EndpointSpec: &dockerswarm.EndpointSpec{},
	}
	networkIDs, err := appNetworkIDs(ctx, cli, spec)
	if err != nil {
		return err
	}
	if len(networkIDs) > 0 {
		proxy.AttachNetworks(&serviceSpec, networkIDs...)
	}

	if existing == nil {
		_, err := cli.CreateService(ctx, serviceSpec)
		return err
	}

	serviceSpec.Name = existing.Spec.Name
	return cli.UpdateService(ctx, existing.ID, existing.Version.Index, serviceSpec)
}

// ProxyNetworkName is the shared overlay the reverse proxy attaches to.
const ProxyNetworkName = "hive_proxy"

// appNetworkNames decides which overlays an application service needs:
// the project network first (when a slug is set), then hive_proxy when at
// least one domain is routed to the app.
func appNetworkNames(spec ApplicationSpec, domains []string) []string {
	var names []string
	if slug := strings.TrimSpace(spec.ProjectSlug); slug != "" {
		names = append(names, network.ProjectNetworkName(slug))
	}
	if len(domains) > 0 {
		names = append(names, ProxyNetworkName)
	}
	return names
}

// appNetworkIDs ensures the required overlays exist (idempotently) and
// returns their swarm IDs.
func appNetworkIDs(ctx context.Context, cli SwarmStack, spec ApplicationSpec) ([]string, error) {
	var domains []string
	if spec.DomainLookup != nil {
		var err error
		domains, err = spec.DomainLookup(ctx, spec.AppID)
		if err != nil {
			return nil, fmt.Errorf("lookup domains for %s: %w", spec.AppID, err)
		}
	}
	manager := network.New(cli)
	var ids []string
	for _, name := range appNetworkNames(spec, domains) {
		id, err := manager.EnsureOverlay(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("ensure network %s: %w", name, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
func normalizeServiceName(base, appID string) string {
	cleaned := strings.Builder{}
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(base)) {
		if isServiceNameChar(r) {
			cleaned.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			cleaned.WriteByte('-')
			lastDash = true
		}
	}
	name := strings.Trim(cleaned.String(), "-")
	if name == "" {
		name = "app"
	}
	if !isServiceNameChar(rune(name[0])) {
		name = "app-" + name
	}
	shortID := strings.ToLower(strings.TrimSpace(appID))
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	if shortID != "" && !strings.HasSuffix(name, "-"+shortID) {
		maxBase := 63 - len(shortID) - 1
		if len(name) > maxBase {
			name = strings.Trim(name[:maxBase], "-")
			if name == "" {
				name = "app"
			}
		}
		name += "-" + shortID
	}
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	return name
}

func isServiceNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

func ptrUint64(v uint64) *uint64 { return &v }

// Emitter mirrors db.Fanout's Emit so deploy can NOTIFY subscribers
// (channel "deployment:{appID}") without depending on the db package.
type Emitter interface {
	Emit(ctx context.Context, channel, payload string) error
}

// Deps carries the collaborators the reusable deployment helpers need.
type Deps struct {
	Pool   *pgxpool.Pool
	Swarm  SwarmStack
	Fanout Emitter // optional; nil skips NOTIFY emission
}

// LoadEnvVars loads an application's environment variables in the form
// Application expects, mapping secrets to their docker secret names.
func LoadEnvVars(ctx context.Context, pool *pgxpool.Pool, appID string) ([]EnvVar, error) {
	rows, err := pool.Query(ctx, `
		select key, value, is_secret, secret_version
		from app_env_vars where application_id = $1::uuid order by key
	`, appID)
	if err != nil {
		return nil, fmt.Errorf("load env vars: %w", err)
	}
	defer rows.Close()

	var envVars []EnvVar
	for rows.Next() {
		var key string
		var value *string
		var isSecret bool
		var secretVersion int
		if err := rows.Scan(&key, &value, &isSecret, &secretVersion); err != nil {
			continue
		}
		ev := EnvVar{Key: key, IsSecret: isSecret}
		if isSecret {
			truncID := appID
			if len(truncID) > 12 {
				truncID = truncID[:12]
			}
			ev.SecretName = fmt.Sprintf("hive.%s.%s.v%d", truncID, key, secretVersion)
		} else if value != nil {
			ev.Value = *value
		}
		envVars = append(envVars, ev)
	}
	return envVars, rows.Err()
}

// RunDeployment loads env vars and creates/updates the swarm service for
// the given application spec.
func RunDeployment(ctx context.Context, deps Deps, spec ApplicationSpec) error {
	envVars, err := LoadEnvVars(ctx, deps.Pool, spec.AppID)
	if err != nil {
		return err
	}
	spec.EnvVars = envVars
	return Application(ctx, deps.Swarm, spec)
}

// ResolveAppService finds the swarm service carrying the hive.app.id label.
func ResolveAppService(ctx context.Context, cli SwarmStack, appID string) (string, error) {
	services, err := cli.ListServices(ctx)
	if err != nil {
		return "", fmt.Errorf("list services: %w", err)
	}
	for _, svc := range services {
		if svc.Spec.Labels["hive.app.id"] == appID {
			return svc.ID, nil
		}
	}
	return "", nil
}

// ApplyApplicationDomains applies routing labels for every domain attached
// to the application. Missing domains or a not-yet-created service are a
// no-op.
func ApplyApplicationDomains(ctx context.Context, deps Deps, appID string, containerPort int) error {
	rows, err := deps.Pool.Query(ctx, `
		select hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority
		from domains where application_id = $1::uuid`, appID)
	if err != nil {
		return fmt.Errorf("load domains: %w", err)
	}
	defer rows.Close()

	type domain struct {
		route proxy.Route
	}
	var domains []domain
	for rows.Next() {
		var d domain
		var priority *int
		if err := rows.Scan(&d.route.Host, &d.route.TLSEnabled, &d.route.RouteType,
			&d.route.PathPrefix, &d.route.StripPrefix, &priority); err == nil {
			if priority != nil {
				d.route.Priority = *priority
			}
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil
	}

	serviceID, err := ResolveAppService(ctx, deps.Swarm, appID)
	if err != nil || serviceID == "" {
		return err
	}
	domainManager := proxy.NewDomainManager(deps.Swarm)
	for _, d := range domains {
		_ = domainManager.ApplyDomain(ctx, serviceID, proxy.RouterNameFromHost(d.route.Host), d.route, containerPort)
	}
	return nil
}

// NotifyDeployment emits the deployment:{appID} NOTIFY used by realtime
// subscribers. Nil fanout is a no-op.
func NotifyDeployment(ctx context.Context, fanout Emitter, appID string) {
	if fanout == nil {
		return
	}
	_ = fanout.Emit(ctx, fmt.Sprintf("deployment:%s", appID), "deployed")
}
