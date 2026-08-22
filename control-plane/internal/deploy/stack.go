package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/luke/hive/control-plane/internal/swarm/spec"
	"github.com/moby/moby/api/types/swarm"
)

// NamespaceLabel marks every service (and container) of a stack with the
// stack name it belongs to, mirroring `docker stack deploy`.
const NamespaceLabel = "com.docker.stack.namespace"

// parseStack parses compose YAML into a compose-go project model. The
// project is named after the stack so normalized resources (overlay
// networks) follow the {stack}_{name} convention. When workingDir is
// non-empty, relative paths (secret/config files, env_file, bind mounts) are
// resolved against it.
func parseStack(ctx context.Context, composeYAML []byte, workingDir, projectName string) (*composetypes.Project, error) {
	environ := composetypes.Mapping{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		environ[k] = v
	}
	details := composetypes.ConfigDetails{
		WorkingDir:  workingDir,
		ConfigFiles: []composetypes.ConfigFile{{Content: composeYAML}},
		Environment: environ,
	}
	project, err := loader.LoadWithContext(ctx, details, func(o *loader.Options) {
		o.ResolvePaths = workingDir != ""
		o.SetProjectName(projectName, true)
	})
	if err != nil {
		return nil, fmt.Errorf("parse compose file: %w", err)
	}
	return project, nil
}

// stackNetworks maps each logical compose network to its swarm target name.
// Non-external networks are overlaid as {stack}_{net} (compose-go already
// normalizes explicit `name:` fields); external networks keep their own name
// and must already exist.
func stackNetworks(project *composetypes.Project, stackName string) (targets map[string]string, external map[string]bool) {
	targets = map[string]string{}
	external = map[string]bool{}
	for name, net := range project.Networks {
		if bool(net.External) {
			target := net.Name
			if target == "" {
				target = name
			}
			targets[name] = target
			external[name] = true
			continue
		}
		if net.Name != "" {
			targets[name] = net.Name
			continue
		}
		targets[name] = stackName + "_" + name
	}
	if _, ok := targets["default"]; !ok {
		targets["default"] = stackName + "_default"
	}
	return targets, external
}

// Stack deploys a compose file to the swarm as a stack.
func Stack(ctx context.Context, cli SwarmStack, stackName, composePath string) error {
	raw, err := os.ReadFile(composePath) //nolint:gosec // compose path is a server-staged temp file by design
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}
	workDir := filepath.Dir(composePath)
	project, err := parseStack(ctx, raw, workDir, stackName)
	if err != nil {
		return err
	}

	networkTargets, external := stackNetworks(project, stackName)
	if err := ensureStackNetworks(ctx, cli, networkTargets, external); err != nil {
		return err
	}
	secretIDs, err := ensureStackSecrets(ctx, cli, project, workDir)
	if err != nil {
		return err
	}
	configIDs, err := ensureStackConfigs(ctx, cli, project, workDir)
	if err != nil {
		return err
	}

	existingServices, err := cli.ListServices(ctx)
	if err != nil {
		return fmt.Errorf("list services: %w", err)
	}
	byName := map[string]swarm.Service{}
	for _, existing := range existingServices {
		byName[existing.Spec.Name] = existing
	}

	desired := map[string]struct{}{}
	for svcName, svc := range project.Services {
		name := stackName + "_" + svcName
		desired[name] = struct{}{}
		serviceSpec, err := serviceSpecFromCompose(stackName, svcName, svc, networkTargets, secretIDs, configIDs)
		if err != nil {
			return fmt.Errorf("translate service %s: %w", svcName, err)
		}
		if existing, ok := byName[name]; ok {
			if err := cli.UpdateService(ctx, existing.ID, existing.Version.Index, serviceSpec); err != nil {
				return fmt.Errorf("update service %s: %w", name, err)
			}
			continue
		}
		if _, err := cli.CreateService(ctx, serviceSpec); err != nil {
			return fmt.Errorf("create service %s: %w", name, err)
		}
	}

	for _, existing := range existingServices {
		if existing.Spec.Labels[NamespaceLabel] != stackName {
			continue
		}
		if _, ok := desired[existing.Spec.Name]; ok {
			continue
		}
		if err := cli.RemoveService(ctx, existing.ID); err != nil {
			return fmt.Errorf("remove service %s: %w", existing.Spec.Name, err)
		}
	}

	return nil
}

// StackDiff is the result of comparing a desired compose file against the
// live services of a stack.
type StackDiff struct {
	ToCreate []string `json:"to_create"`
	ToUpdate []string `json:"to_update"`
	ToRemove []string `json:"to_remove"`
}

// PreviewStackDeploy compares the desired compose file against the live
// services of the stack (matched by the stack namespace label) without
// mutating anything. stackName is the stack namespace, as known to the caller.
func PreviewStackDeploy(ctx context.Context, cli SwarmStack, stackName, composeYAML string) (*StackDiff, error) {
	project, err := parseStack(ctx, []byte(composeYAML), "", stackName)
	if err != nil {
		return nil, err
	}
	networkTargets, _ := stackNetworks(project, stackName)

	live, err := cli.ListServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	var desired []swarm.ServiceSpec
	for svcName, svc := range project.Services {
		serviceSpec, err := serviceSpecFromCompose(stackName, svcName, svc, networkTargets, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("translate service %s: %w", svcName, err)
		}
		desired = append(desired, serviceSpec)
	}
	diff := diffStack(stackName, desired, live)
	return &diff, nil
}

// diffStack is the pure core of PreviewStackDeploy: desired specs are matched
// against live services by name; live services carrying the stack namespace
// label but no longer present in the compose file are pruned.
func diffStack(stackName string, desired []swarm.ServiceSpec, live []swarm.Service) StackDiff {
	liveByLabel := map[string]swarm.Service{}
	for _, svc := range live {
		if svc.Spec.Labels[NamespaceLabel] == stackName {
			liveByLabel[svc.Spec.Name] = svc
		}
	}

	diff := StackDiff{}
	for _, want := range desired {
		existing, ok := liveByLabel[want.Name]
		if !ok {
			diff.ToCreate = append(diff.ToCreate, want.Name)
			continue
		}
		if !reflect.DeepEqual(want, existing.Spec) {
			diff.ToUpdate = append(diff.ToUpdate, want.Name)
		}
	}
	for name := range liveByLabel {
		desiredNames := map[string]bool{}
		for _, want := range desired {
			desiredNames[want.Name] = true
		}
		if !desiredNames[name] {
			diff.ToRemove = append(diff.ToRemove, name)
		}
	}
	sort.Strings(diff.ToCreate)
	sort.Strings(diff.ToUpdate)
	sort.Strings(diff.ToRemove)
	return diff
}

// serviceSpecFromCompose translates one compose service into a swarm
// ServiceSpec via the spec.Builder, mapping the full deploy key.
func serviceSpecFromCompose(
	stackName, svcName string,
	svc composetypes.ServiceConfig,
	networkTargets map[string]string,
	secretIDs, configIDs map[string]string,
) (swarm.ServiceSpec, error) {
	name := stackName + "_" + svcName
	b := spec.NewService(name)

	b.Image(svc.Image)

	env := map[string]string{}
	for k, v := range svc.Environment {
		if v != nil {
			env[k] = *v
		}
	}
	b.Env(env)

	b.Command(svc.Entrypoint).Args(svc.Command)
	b.User(svc.User).WorkingDir(svc.WorkingDir).Hostname(svc.Hostname)
	b.TTY(svc.Tty).OpenStdin(svc.StdinOpen).ReadOnly(svc.ReadOnly)
	if len(svc.GroupAdd) > 0 {
		b.Groups(svc.GroupAdd)
	}
	if len(svc.Sysctls) > 0 {
		b.Sysctls(svc.Sysctls)
	}

	// Compose service labels land on containers; deploy.labels land on the
	// service object. The stack namespace label goes on both, mirroring
	// `docker stack deploy`.
	containerLabels := map[string]string{NamespaceLabel: stackName}
	for k, v := range svc.Labels {
		containerLabels[k] = v
	}
	b.ContainerLabels(containerLabels)

	deploy := svc.Deploy
	serviceLabels := map[string]string{NamespaceLabel: stackName}
	if deploy != nil {
		for k, v := range deploy.Labels {
			serviceLabels[k] = v
		}
	}
	b.ServiceLabels(serviceLabels)

	for _, p := range svc.Ports {
		b.Ports(spec.Port{
			Name:      p.Name,
			Target:    p.Target,
			Published: parsePublishedPort(p.Published),
			Protocol:  p.Protocol,
			Mode:      p.Mode,
		})
	}
	if deploy != nil && deploy.EndpointMode != "" {
		b.EndpointMode(deploy.EndpointMode)
	}

	// Networks: resolve logical names through the stack overlay targets,
	// defaulting unknown names to {stack}_{name}.
	resolver := func(logical string) (string, error) {
		if target, ok := networkTargets[logical]; ok {
			return target, nil
		}
		return stackName + "_" + logical, nil
	}
	names := svc.NetworksByPriority()
	if len(names) == 0 {
		names = []string{"default"}
	}
	b.NetworkResolver(resolver).Networks(names...)

	if deploy != nil {
		applyDeploy(b, *deploy)
	}

	// Top-level restart is honored when no deploy.restart_policy is set.
	if deploy == nil || deploy.RestartPolicy == nil {
		if svc.Restart == "no" {
			b.RestartPolicy(spec.RestartPolicy{Condition: "none"})
		} else {
			b.RestartPolicy(spec.RestartPolicy{Condition: "any"})
		}
	}

	if svc.Logging != nil {
		b.LogDriver(svc.Logging.Driver, svc.Logging.Options)
	}
	if svc.HealthCheck != nil {
		b.Healthcheck(healthcheckFromCompose(*svc.HealthCheck))
	}
	if len(svc.Volumes) > 0 {
		mounts := make([]spec.Mount, 0, len(svc.Volumes))
		for _, v := range svc.Volumes {
			m := spec.Mount{
				Type:     v.Type,
				Source:   v.Source,
				Target:   v.Target,
				ReadOnly: v.ReadOnly,
			}
			if v.Bind != nil && v.Bind.Propagation != "" {
				m.BindPropagation = v.Bind.Propagation
			}
			if v.Volume != nil && v.Volume.NoCopy {
				m.VolumeNoCopy = true
			}
			if v.Tmpfs != nil {
				m.TmpfsSizeBytes = int64(v.Tmpfs.Size)
				m.TmpfsMode = v.Tmpfs.Mode
			}
			mounts = append(mounts, m)
		}
		b.Mounts(mounts...)
	}
	if len(svc.ExtraHosts) > 0 {
		hosts := map[string]string{}
		for host, ips := range svc.ExtraHosts {
			if len(ips) > 0 {
				hosts[host] = ips[0]
			}
		}
		b.Hosts(hosts)
	}
	if len(svc.DNS) > 0 || len(svc.DNSSearch) > 0 || len(svc.DNSOpts) > 0 {
		b.DNS(svc.DNS, svc.DNSSearch, svc.DNSOpts)
	}
	if len(svc.CapAdd) > 0 || len(svc.CapDrop) > 0 {
		b.Capabilities(svc.CapAdd, svc.CapDrop)
	}
	if len(svc.Ulimits) > 0 {
		for name, ulimit := range svc.Ulimits {
			if ulimit == nil {
				continue
			}
			soft, hard := int64(ulimit.Single), int64(ulimit.Single)
			if ulimit.Soft != 0 || ulimit.Hard != 0 {
				soft, hard = int64(ulimit.Soft), int64(ulimit.Hard)
			}
			b.Ulimits(spec.Ulimit{Name: name, Soft: soft, Hard: hard})
		}
	}
	if svc.Init != nil {
		b.Init(*svc.Init)
	}
	if svc.StopSignal != "" {
		b.StopSignal(svc.StopSignal)
	}
	if svc.StopGracePeriod != nil {
		b.StopGracePeriod(time.Duration(*svc.StopGracePeriod))
	}

	securityOpts := securityOptions(svc.SecurityOpt)
	if securityOpts.noNewPrivileges || securityOpts.selinuxDisable {
		b.Privileges(securityOpts.noNewPrivileges, securityOpts.selinuxDisable)
	}

	for _, ref := range svc.Secrets {
		b.Secrets(spec.FileRef{
			ID:     secretIDs[ref.Source],
			Name:   ref.Source,
			Target: ref.Target,
			UID:    ref.UID,
			GID:    ref.GID,
			Mode:   fileModeUint(ref.Mode),
		})
	}
	for _, ref := range svc.Configs {
		b.Configs(spec.FileRef{
			ID:     configIDs[ref.Source],
			Name:   ref.Source,
			Target: ref.Target,
			UID:    ref.UID,
			GID:    ref.GID,
			Mode:   fileModeUint(ref.Mode),
		})
	}

	return b.Build()
}

// applyDeploy maps the compose deploy key onto the builder.
func applyDeploy(b *spec.Builder, deploy composetypes.DeployConfig) {
	switch strings.ToLower(deploy.Mode) {
	case "global":
		b.Global()
	case "replicated-job":
		b.ReplicatedJob(0, 0)
	case "global-job":
		b.GlobalJob()
	default:
		replicas := uint64(1)
		if deploy.Replicas != nil && *deploy.Replicas >= 0 {
			replicas = uint64(*deploy.Replicas) //nolint:gosec // guarded positive above
		}
		b.Replicas(replicas)
	}

	if deploy.Resources.Limits != nil {
		limits := deploy.Resources.Limits
		b.Limits(spec.Limit{
			CPUs:        float64(limits.NanoCPUs),
			MemoryBytes: int64(limits.MemoryBytes),
			Pids:        limits.Pids,
		})
	}
	if res := deploy.Resources.Reservations; res != nil {
		reservation := spec.Reservation{
			CPUs:        float64(res.NanoCPUs),
			MemoryBytes: int64(res.MemoryBytes),
		}
		for _, gr := range res.GenericResources {
			if gr.DiscreteResourceSpec != nil {
				reservation.Generic = append(reservation.Generic, swarm.GenericResource{
					DiscreteResourceSpec: &swarm.DiscreteGenericResource{
						Kind:  gr.DiscreteResourceSpec.Kind,
						Value: gr.DiscreteResourceSpec.Value,
					},
				})
			}
		}
		for _, dev := range res.Devices {
			if !deviceRequestsGPU(dev) {
				continue
			}
			count := int64(dev.Count)
			if count == 0 {
				count = 1
			}
			if count < 0 {
				// "all": swarm cannot express unbounded counts; reserve one.
				count = 1
			}
			reservation.GPUs = count
			break
		}
		b.Reservations(reservation)
	}

	b.Placement(spec.Placement{
		Constraints:        deploy.Placement.Constraints,
		SpreadPreferences:  spreadPreferences(deploy.Placement.Preferences),
		MaxReplicasPerNode: deploy.Placement.MaxReplicas,
	})

	if deploy.UpdateConfig != nil {
		b.UpdateConfig(updateConfigFromCompose(*deploy.UpdateConfig))
	}
	if deploy.RollbackConfig != nil {
		b.RollbackConfig(updateConfigFromCompose(*deploy.RollbackConfig))
	}
	if deploy.RestartPolicy != nil {
		policy := spec.RestartPolicy{
			Condition:   deploy.RestartPolicy.Condition,
			MaxAttempts: deploy.RestartPolicy.MaxAttempts,
		}
		if deploy.RestartPolicy.Delay != nil {
			d := time.Duration(*deploy.RestartPolicy.Delay)
			policy.Delay = &d
		}
		if deploy.RestartPolicy.Window != nil {
			d := time.Duration(*deploy.RestartPolicy.Window)
			policy.Window = &d
		}
		b.RestartPolicy(policy)
	}
}

func deviceRequestsGPU(dev composetypes.DeviceRequest) bool {
	for _, cap := range dev.Capabilities {
		for _, c := range strings.Split(cap, ",") {
			if strings.EqualFold(strings.TrimSpace(c), "gpu") {
				return true
			}
		}
	}
	return false
}

func spreadPreferences(prefs []composetypes.PlacementPreferences) []string {
	out := make([]string, 0, len(prefs))
	for _, p := range prefs {
		if p.Spread != "" {
			out = append(out, p.Spread)
		}
	}
	return out
}

func updateConfigFromCompose(c composetypes.UpdateConfig) spec.UpdateRollbackConfig {
	return spec.UpdateRollbackConfig{
		Parallelism:     derefUint64(c.Parallelism),
		Delay:           time.Duration(c.Delay),
		FailureAction:   c.FailureAction,
		Monitor:         time.Duration(c.Monitor),
		MaxFailureRatio: c.MaxFailureRatio,
		Order:           c.Order,
	}
}

func healthcheckFromCompose(h composetypes.HealthCheckConfig) spec.Healthcheck {
	return spec.Healthcheck{
		Test:          h.Test,
		Interval:      time.Duration(derefDuration(h.Interval)),
		Timeout:       time.Duration(derefDuration(h.Timeout)),
		StartPeriod:   time.Duration(derefDuration(h.StartPeriod)),
		StartInterval: time.Duration(derefDuration(h.StartInterval)),
		Retries:       int(derefUint64(h.Retries)), //nolint:gosec // compose healthcheck retry count, small by spec
		Disable:       h.Disable,
	}
}

func derefUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefDuration(p *composetypes.Duration) composetypes.Duration {
	if p == nil {
		return 0
	}
	return *p
}

func parsePublishedPort(published string) uint32 {
	published = strings.SplitN(published, "-", 2)[0]
	port, err := strconv.ParseUint(strings.TrimSpace(published), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(port)
}

type serviceSecurityOptions struct {
	noNewPrivileges bool
	selinuxDisable  bool
}

func securityOptions(opts []string) serviceSecurityOptions {
	out := serviceSecurityOptions{}
	for _, opt := range opts {
		switch {
		case strings.EqualFold(opt, "no-new-privileges"):
			out.noNewPrivileges = true
		case strings.HasPrefix(strings.ToLower(opt), "label:disable"):
			out.selinuxDisable = true
		}
	}
	return out
}

func fileModeUint(mode *composetypes.FileMode) uint32 {
	if mode == nil {
		return 0
	}
	return uint32(*mode) //nolint:gosec // POSIX file mode, fits uint32 by definition
}

// ensureStackNetworks creates missing overlay networks for the stack.
// External networks are expected to exist and are never created.
func ensureStackNetworks(ctx context.Context, cli SwarmStack, targets map[string]string, external map[string]bool) error {
	existing, err := cli.ListNetworks(ctx)
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	byName := map[string]struct{}{}
	for _, n := range existing {
		byName[n.Name] = struct{}{}
	}
	seen := map[string]struct{}{}
	for logical, name := range targets {
		if external[logical] {
			continue
		}
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		if _, ok := byName[name]; ok {
			continue
		}
		if _, err := cli.CreateNetwork(ctx, name); err != nil {
			return fmt.Errorf("create network %s: %w", name, err)
		}
	}
	return nil
}

// ensureStackSecrets creates swarm secrets for non-external compose secrets
// (from inline content, environment or file) and returns a source-name → ID
// map for service references. External secrets must already exist. Existing
// secrets are reused: swarm secret data is immutable and unreadable, so
// content drift is detected at deploy time by the daemon on removal.
func ensureStackSecrets(ctx context.Context, cli SwarmStack, project *composetypes.Project, workDir string) (map[string]string, error) {
	if len(project.Secrets) == 0 {
		return nil, nil
	}
	existing, err := cli.ListSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	byName := map[string]string{}
	for _, s := range existing {
		if s.Spec.Name != "" {
			byName[s.Spec.Name] = s.ID
		}
	}

	ids := map[string]string{}
	for source, secret := range project.Secrets {
		name := secret.Name
		if name == "" {
			name = source
		}
		if id, ok := byName[name]; ok {
			ids[source] = id
			continue
		}
		spec := swarm.SecretSpec{
			Annotations: swarm.Annotations{Name: name, Labels: secret.Labels},
			Driver:      swarmDriver(secret.Driver, secret.DriverOpts),
		}
		if secret.TemplateDriver != "" {
			spec.Templating = &swarm.Driver{Name: secret.TemplateDriver}
		}
		data, err := fileObjectData(fileObjectSource{file: secret.File, content: secret.Content, environment: secret.Environment}, workDir)
		if err != nil {
			return nil, fmt.Errorf("load secret %q: %w", name, err)
		}
		spec.Data = data
		id, err := cli.CreateSecret(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("create secret %q: %w", name, err)
		}
		ids[source] = id
	}
	return ids, nil
}

// ensureStackConfigs creates or updates swarm configs for non-external
// compose configs and returns a source-name → ID map. Unlike secrets, config
// data is readable, so drift is repaired in place via UpdateConfig.
func ensureStackConfigs(ctx context.Context, cli SwarmStack, project *composetypes.Project, workDir string) (map[string]string, error) {
	if len(project.Configs) == 0 {
		return nil, nil
	}
	existing, err := cli.ListConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list configs: %w", err)
	}
	byName := map[string]swarm.Config{}
	for _, c := range existing {
		if c.Spec.Name != "" {
			byName[c.Spec.Name] = c
		}
	}

	ids := map[string]string{}
	for source, cfg := range project.Configs {
		name := cfg.Name
		if name == "" {
			name = source
		}
		if bool(cfg.External) {
			id, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("external config %q does not exist in the swarm", name)
			}
			ids[source] = id.ID
			continue
		}
		data, err := fileObjectData(fileObjectSource{file: cfg.File, content: cfg.Content, environment: cfg.Environment}, workDir)
		if err != nil {
			return nil, fmt.Errorf("load config %q: %w", name, err)
		}
		spec := swarm.ConfigSpec{
			Annotations: swarm.Annotations{Name: name, Labels: cfg.Labels},
			Data:        data,
		}
		if cfg.TemplateDriver != "" {
			spec.Templating = &swarm.Driver{Name: cfg.TemplateDriver}
		}
		if existing, ok := byName[name]; ok {
			if string(existing.Spec.Data) == string(data) {
				ids[source] = existing.ID
				continue
			}
			if err := cli.UpdateConfig(ctx, existing.ID, existing.Version.Index, spec); err != nil {
				return nil, fmt.Errorf("update config %q: %w", name, err)
			}
			ids[source] = existing.ID
			continue
		}
		id, err := cli.CreateConfig(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("create config %q: %w", name, err)
		}
		ids[source] = id
	}
	return ids, nil
}

type fileObjectSource struct {
	file        string
	content     string
	environment string
}

func fileObjectData(src fileObjectSource, workDir string) ([]byte, error) {
	switch {
	case src.file != "":
		path := src.file
		if workDir != "" && !filepath.IsAbs(path) {
			path = filepath.Join(workDir, path)
		}
		return os.ReadFile(path) //nolint:gosec // file source comes from the parsed compose spec by design
	case src.content != "":
		return []byte(src.content), nil
	case src.environment != "":
		value, ok := os.LookupEnv(src.environment)
		if !ok {
			return nil, fmt.Errorf("environment variable %q is not set", src.environment)
		}
		return []byte(value), nil
	default:
		return nil, fmt.Errorf("secret/config must set one of file, content or environment")
	}
}

func swarmDriver(driver string, opts map[string]string) *swarm.Driver {
	if driver == "" {
		return nil
	}
	return &swarm.Driver{Name: driver, Options: opts}
}
