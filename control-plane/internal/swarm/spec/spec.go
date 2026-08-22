// Package spec provides a fluent builder for Docker Swarm service specs.
//
// The builder maps one-to-one onto the moby swarm.ServiceSpec tree so callers
// can express desired state declaratively without hand-assembling nested
// structs. Every setter records the first error encountered (e.g. a failing
// network resolver or an invalid DNS address) and Build returns it alongside
// the assembled spec.
package spec

import (
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

// Builder assembles a swarm.ServiceSpec fluently.
type Builder struct {
	spec     swarm.ServiceSpec
	resolver func(name string) (string, error)
	err      error
}

// NewService starts a Builder for a service with the given name.
func NewService(name string) *Builder {
	return &Builder{spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: name},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{},
		},
	}}
}

// Build returns the assembled spec and the first error recorded by any setter.
func (b *Builder) Build() (swarm.ServiceSpec, error) {
	return b.spec, b.err
}

func (b *Builder) fail(err error) *Builder {
	if b.err == nil {
		b.err = err
	}
	return b
}

// Image sets the container image.
func (b *Builder) Image(image string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Image = image
	return b
}

// Env sets container environment variables as KEY=VALUE entries, sorted by key
// for deterministic specs.
func (b *Builder) Env(pairs map[string]string) *Builder {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+pairs[k])
	}
	b.spec.TaskTemplate.ContainerSpec.Env = env
	return b
}

// Command sets the container entrypoint (swarm "Command").
func (b *Builder) Command(entrypoint []string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Command = entrypoint
	return b
}

// Args sets the container command arguments (swarm "Args").
func (b *Builder) Args(args []string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Args = args
	return b
}

// User sets the user the container runs as.
func (b *Builder) User(user string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.User = user
	return b
}

// WorkingDir sets the working directory inside the container.
func (b *Builder) WorkingDir(dir string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Dir = dir
	return b
}

// Hostname sets the container hostname.
func (b *Builder) Hostname(hostname string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Hostname = hostname
	return b
}

// ServiceLabels sets labels on the service object itself.
func (b *Builder) ServiceLabels(labels map[string]string) *Builder {
	if len(labels) == 0 {
		return b
	}
	if b.spec.Labels == nil {
		b.spec.Labels = map[string]string{}
	}
	for k, v := range labels {
		b.spec.Labels[k] = v
	}
	return b
}

// ContainerLabels sets labels on the task containers.
func (b *Builder) ContainerLabels(labels map[string]string) *Builder {
	if len(labels) == 0 {
		return b
	}
	if b.spec.TaskTemplate.ContainerSpec.Labels == nil {
		b.spec.TaskTemplate.ContainerSpec.Labels = map[string]string{}
	}
	for k, v := range labels {
		b.spec.TaskTemplate.ContainerSpec.Labels[k] = v
	}
	return b
}

// Port describes one published service port.
type Port struct {
	Name      string
	Target    uint32
	Published uint32
	Protocol  string // "", "tcp", "udp", "sctp"
	Mode      string // "", "ingress", "host"
}

// Ports appends published ports to the endpoint spec.
func (b *Builder) Ports(ports ...Port) *Builder {
	if b.spec.EndpointSpec == nil {
		b.spec.EndpointSpec = &swarm.EndpointSpec{}
	}
	for _, p := range ports {
		b.spec.EndpointSpec.Ports = append(b.spec.EndpointSpec.Ports, swarm.PortConfig{
			Name:          p.Name,
			TargetPort:    p.Target,
			PublishedPort: p.Published,
			Protocol:      networkIPProtocol(p.Protocol),
			PublishMode:   portPublishMode(p.Mode),
		})
	}
	return b
}

func networkIPProtocol(proto string) network.IPProtocol {
	switch strings.ToLower(proto) {
	case "udp":
		return network.UDP
	case "sctp":
		return network.SCTP
	default:
		return network.TCP
	}
}

func portPublishMode(mode string) swarm.PortConfigPublishMode {
	if strings.EqualFold(mode, "host") {
		return swarm.PortConfigPublishModeHost
	}
	return swarm.PortConfigPublishModeIngress
}

// EndpointMode sets the endpoint resolution mode: "vip" (default) or "dnsrr".
func (b *Builder) EndpointMode(mode string) *Builder {
	if b.spec.EndpointSpec == nil {
		b.spec.EndpointSpec = &swarm.EndpointSpec{}
	}
	switch strings.ToLower(mode) {
	case "dnsrr":
		b.spec.EndpointSpec.Mode = swarm.ResolutionModeDNSRR
	default:
		b.spec.EndpointSpec.Mode = swarm.ResolutionModeVIP
	}
	return b
}

// NetworkResolver registers the function used to resolve logical network
// names into swarm network targets (IDs or names).
func (b *Builder) NetworkResolver(fn func(name string) (string, error)) *Builder {
	b.resolver = fn
	return b
}

// Networks attaches the named networks, resolved through the registered
// NetworkResolver.
func (b *Builder) Networks(names ...string) *Builder {
	for _, name := range names {
		target, err := b.resolveNetwork(name)
		if err != nil {
			return b.fail(fmt.Errorf("resolve network %q: %w", name, err))
		}
		b.spec.TaskTemplate.Networks = append(b.spec.TaskTemplate.Networks, swarm.NetworkAttachmentConfig{Target: target})
	}
	return b
}

func (b *Builder) resolveNetwork(name string) (string, error) {
	if b.resolver == nil {
		return name, nil
	}
	return b.resolver(name)
}

// Replicas configures the replicated service mode with n replicas.
func (b *Builder) Replicas(n uint64) *Builder {
	b.spec.Mode = swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &n}}
	return b
}

// Global configures the global service mode (one task per node).
func (b *Builder) Global() *Builder {
	b.spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
	return b
}

// ReplicatedJob configures the replicated job mode.
func (b *Builder) ReplicatedJob(maxConcurrent, totalCompletions uint64) *Builder {
	b.spec.Mode = swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{
		MaxConcurrent:    &maxConcurrent,
		TotalCompletions: &totalCompletions,
	}}
	return b
}

// GlobalJob configures the global job mode (one task per node until success).
func (b *Builder) GlobalJob() *Builder {
	b.spec.Mode = swarm.ServiceMode{GlobalJob: &swarm.GlobalJob{}}
	return b
}

// Limit describes hard resource limits.
type Limit struct {
	CPUs        float64 // cores, e.g. 1.5
	MemoryBytes int64
	Pids        int64
}

// Reservation describes soft resource reservations including GPUs and other
// generic (discrete) resources.
type Reservation struct {
	CPUs        float64
	MemoryBytes int64
	GPUs        int64 // discrete resource kind "gpu"
	Generic     []swarm.GenericResource
}

// Limits sets hard resource limits. Zero values are omitted.
func (b *Builder) Limits(l Limit) *Builder {
	limit := &swarm.Limit{}
	if l.CPUs > 0 {
		limit.NanoCPUs = int64(l.CPUs * 1e9)
	}
	if l.MemoryBytes > 0 {
		limit.MemoryBytes = l.MemoryBytes
	}
	if l.Pids > 0 {
		limit.Pids = l.Pids
	}
	b.spec.TaskTemplate.Resources = resourceRequirements(b.spec.TaskTemplate.Resources)
	b.spec.TaskTemplate.Resources.Limits = limit
	return b
}

// Reservations sets soft resource reservations; GPUs are expressed as a
// discrete generic resource of kind "gpu". Zero values are omitted.
func (b *Builder) Reservations(r Reservation) *Builder {
	res := &swarm.Resources{}
	if r.CPUs > 0 {
		res.NanoCPUs = int64(r.CPUs * 1e9)
	}
	if r.MemoryBytes > 0 {
		res.MemoryBytes = r.MemoryBytes
	}
	res.GenericResources = r.Generic
	if r.GPUs > 0 {
		res.GenericResources = append(res.GenericResources, swarm.GenericResource{
			DiscreteResourceSpec: &swarm.DiscreteGenericResource{Kind: "gpu", Value: r.GPUs},
		})
	}
	b.spec.TaskTemplate.Resources = resourceRequirements(b.spec.TaskTemplate.Resources)
	b.spec.TaskTemplate.Resources.Reservations = res
	return b
}

func resourceRequirements(existing *swarm.ResourceRequirements) *swarm.ResourceRequirements {
	if existing != nil {
		return existing
	}
	return &swarm.ResourceRequirements{}
}

// Placement describes scheduling constraints and preferences.
type Placement struct {
	Constraints        []string
	SpreadPreferences  []string // node label descriptors, e.g. "node.labels.zone"
	MaxReplicasPerNode uint64
}

// Placement sets task placement constraints, spread preferences and the
// per-node replica cap.
func (b *Builder) Placement(p Placement) *Builder {
	pl := &swarm.Placement{Constraints: p.Constraints}
	for _, spread := range p.SpreadPreferences {
		pl.Preferences = append(pl.Preferences, swarm.PlacementPreference{
			Spread: &swarm.SpreadOver{SpreadDescriptor: spread},
		})
	}
	pl.MaxReplicas = p.MaxReplicasPerNode
	b.spec.TaskTemplate.Placement = pl
	return b
}

// UpdateRollbackConfig describes update_config / rollback_config settings.
type UpdateRollbackConfig struct {
	Parallelism     uint64
	Delay           time.Duration
	FailureAction   string
	Monitor         time.Duration
	MaxFailureRatio float32
	Order           string
}

func (c UpdateRollbackConfig) toSwarm() *swarm.UpdateConfig {
	return &swarm.UpdateConfig{
		Parallelism:     c.Parallelism,
		Delay:           c.Delay,
		FailureAction:   swarm.FailureAction(c.FailureAction),
		Monitor:         c.Monitor,
		MaxFailureRatio: c.MaxFailureRatio,
		Order:           swarm.UpdateOrder(c.Order),
	}
}

// UpdateConfig sets rolling-update behavior.
func (b *Builder) UpdateConfig(c UpdateRollbackConfig) *Builder {
	b.spec.UpdateConfig = c.toSwarm()
	return b
}

// RollbackConfig sets rollback behavior for failed updates.
func (b *Builder) RollbackConfig(c UpdateRollbackConfig) *Builder {
	b.spec.RollbackConfig = c.toSwarm()
	return b
}

// RestartPolicy describes the task restart policy.
type RestartPolicy struct {
	Condition   string // "none", "on-failure", "any"
	Delay       *time.Duration
	MaxAttempts *uint64
	Window      *time.Duration
}

// RestartPolicy sets the task restart policy.
func (b *Builder) RestartPolicy(p RestartPolicy) *Builder {
	b.spec.TaskTemplate.RestartPolicy = &swarm.RestartPolicy{
		Condition:   swarm.RestartPolicyCondition(p.Condition),
		Delay:       p.Delay,
		MaxAttempts: p.MaxAttempts,
		Window:      p.Window,
	}
	return b
}

// LogDriver sets the task log driver and its options.
func (b *Builder) LogDriver(name string, options map[string]string) *Builder {
	if name == "" && len(options) == 0 {
		return b
	}
	b.spec.TaskTemplate.LogDriver = &swarm.Driver{Name: name, Options: options}
	return b
}

// Healthcheck describes the container health check.
type Healthcheck struct {
	Test          []string
	Interval      time.Duration
	Timeout       time.Duration
	StartPeriod   time.Duration
	StartInterval time.Duration
	Retries       int
	Disable       bool
}

// Healthcheck sets the container health check; Disable marks it ["NONE"].
func (b *Builder) Healthcheck(h Healthcheck) *Builder {
	hc := &container.HealthConfig{
		Test:          h.Test,
		Interval:      h.Interval,
		Timeout:       h.Timeout,
		StartPeriod:   h.StartPeriod,
		StartInterval: h.StartInterval,
	}
	if h.Retries > 0 {
		hc.Retries = h.Retries
	}
	if h.Disable {
		hc.Test = []string{"NONE"}
	}
	b.spec.TaskTemplate.ContainerSpec.Healthcheck = hc
	return b
}

// Mount describes one volume mount.
type Mount struct {
	Type            string // "bind", "volume", "tmpfs", "npipe", "cluster", "image"
	Source          string
	Target          string
	ReadOnly        bool
	BindPropagation string // e.g. "rprivate"
	VolumeNoCopy    bool
	TmpfsSizeBytes  int64
	TmpfsMode       uint32
}

// Mounts appends container mounts.
func (b *Builder) Mounts(mounts ...Mount) *Builder {
	for _, m := range mounts {
		out := mount.Mount{
			Type:     mountType(m.Type),
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		}
		if out.Type == mount.TypeBind && m.BindPropagation != "" {
			out.BindOptions = &mount.BindOptions{Propagation: mount.Propagation(m.BindPropagation)}
		}
		if out.Type == mount.TypeVolume && m.VolumeNoCopy {
			out.VolumeOptions = &mount.VolumeOptions{NoCopy: true}
		}
		if out.Type == mount.TypeTmpfs {
			opts := &mount.TmpfsOptions{}
			if m.TmpfsSizeBytes > 0 {
				opts.SizeBytes = m.TmpfsSizeBytes
			}
			if m.TmpfsMode > 0 {
				opts.Mode = os.FileMode(m.TmpfsMode)
			}
			out.TmpfsOptions = opts
		}
		b.spec.TaskTemplate.ContainerSpec.Mounts = append(b.spec.TaskTemplate.ContainerSpec.Mounts, out)
	}
	return b
}

func mountType(t string) mount.Type {
	switch strings.ToLower(t) {
	case "bind":
		return mount.TypeBind
	case "volume":
		return mount.TypeVolume
	case "tmpfs":
		return mount.TypeTmpfs
	case "npipe":
		return mount.TypeNamedPipe
	case "cluster":
		return mount.TypeCluster
	case "image":
		return mount.TypeImage
	default:
		return mount.Type("")
	}
}

// Hosts adds extra host-to-IP mappings in hosts-file format ("IP host").
func (b *Builder) Hosts(hostToIP map[string]string) *Builder {
	hosts := make([]string, 0, len(hostToIP))
	for host, ip := range hostToIP {
		hosts = append(hosts, ip+" "+host)
	}
	sort.Strings(hosts)
	b.spec.TaskTemplate.ContainerSpec.Hosts = append(b.spec.TaskTemplate.ContainerSpec.Hosts, hosts...)
	return b
}

// DNS sets DNS nameservers, search domains and options.
func (b *Builder) DNS(nameservers, search, options []string) *Builder {
	cfg := &swarm.DNSConfig{Search: search, Options: options}
	for _, ns := range nameservers {
		addr, err := netip.ParseAddr(strings.TrimSpace(ns))
		if err != nil {
			return b.fail(fmt.Errorf("parse DNS nameserver %q: %w", ns, err))
		}
		cfg.Nameservers = append(cfg.Nameservers, addr)
	}
	b.spec.TaskTemplate.ContainerSpec.DNSConfig = cfg
	return b
}

// Capabilities adds/drops Linux capabilities.
func (b *Builder) Capabilities(add, drop []string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.CapabilityAdd = add
	b.spec.TaskTemplate.ContainerSpec.CapabilityDrop = drop
	return b
}

// Ulimit describes one resource ulimit.
type Ulimit struct {
	Name string
	Soft int64
	Hard int64
}

// Ulimits appends ulimits.
func (b *Builder) Ulimits(ulimits ...Ulimit) *Builder {
	for _, u := range ulimits {
		b.spec.TaskTemplate.ContainerSpec.Ulimits = append(b.spec.TaskTemplate.ContainerSpec.Ulimits,
			&container.Ulimit{Name: u.Name, Soft: u.Soft, Hard: u.Hard})
	}
	return b
}

// Init sets whether the docker-init binary is used as PID 1.
func (b *Builder) Init(init bool) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Init = &init
	return b
}

// StopSignal sets the signal used to stop the container.
func (b *Builder) StopSignal(signal string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.StopSignal = signal
	return b
}

// StopGracePeriod sets how long to wait before force-killing the container.
func (b *Builder) StopGracePeriod(d time.Duration) *Builder {
	b.spec.TaskTemplate.ContainerSpec.StopGracePeriod = &d
	return b
}

// Sysctls sets container sysctls.
func (b *Builder) Sysctls(m map[string]string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Sysctls = m
	return b
}

// fileTarget normalizes a FileRef into (id, name, target, uid, gid, mode),
// applying defaults for missing values.
func fileTarget(ref FileRef, defaultMode uint32) (string, string, string, string, string, os.FileMode) {
	target := ref.Target
	if target == "" {
		target = ref.Name
	}
	uid := ref.UID
	if uid == "" {
		uid = "0"
	}
	gid := ref.GID
	if gid == "" {
		gid = "0"
	}
	mode := ref.Mode
	if mode == 0 {
		mode = defaultMode
	}
	return ref.ID, ref.Name, target, uid, gid, os.FileMode(mode)
}

// Groups adds supplementary group IDs (as strings) for the container process.
func (b *Builder) Groups(groups []string) *Builder {
	b.spec.TaskTemplate.ContainerSpec.Groups = groups
	return b
}

// TTY allocates a pseudo-terminal.
func (b *Builder) TTY(tty bool) *Builder {
	b.spec.TaskTemplate.ContainerSpec.TTY = tty
	return b
}

// OpenStdin keeps STDIN open.
func (b *Builder) OpenStdin(open bool) *Builder {
	b.spec.TaskTemplate.ContainerSpec.OpenStdin = open
	return b
}

// ReadOnly mounts the container root filesystem read-only.
func (b *Builder) ReadOnly(ro bool) *Builder {
	b.spec.TaskTemplate.ContainerSpec.ReadOnly = ro
	return b
}

// FileRef references a swarm secret or config attached to the task.
type FileRef struct {
	ID     string
	Name   string
	Target string
	UID    string
	GID    string
	Mode   uint32 // permission bits, e.g. 0o444
}

// Secrets appends secret references exposed at /run/secrets/<target>.
func (b *Builder) Secrets(refs ...FileRef) *Builder {
	for _, ref := range refs {
		id, name, target, uid, gid, mode := fileTarget(ref, 0o444)
		b.spec.TaskTemplate.ContainerSpec.Secrets = append(b.spec.TaskTemplate.ContainerSpec.Secrets,
			&swarm.SecretReference{
				File:       &swarm.SecretReferenceFileTarget{Name: target, UID: uid, GID: gid, Mode: mode},
				SecretID:   id,
				SecretName: name,
			})
	}
	return b
}

// Configs appends config references exposed at <target>.
func (b *Builder) Configs(refs ...FileRef) *Builder {
	for _, ref := range refs {
		id, name, target, uid, gid, mode := fileTarget(ref, 0o444)
		b.spec.TaskTemplate.ContainerSpec.Configs = append(b.spec.TaskTemplate.ContainerSpec.Configs,
			&swarm.ConfigReference{
				File:       &swarm.ConfigReferenceFileTarget{Name: target, UID: uid, GID: gid, Mode: mode},
				ConfigID:   id,
				ConfigName: name,
			})
	}
	return b
}

// Privileges sets security privileges: no-new-privileges and SELinux disable.
func (b *Builder) Privileges(noNewPrivileges, selinuxDisable bool) *Builder {
	p := &swarm.Privileges{NoNewPrivileges: noNewPrivileges}
	if selinuxDisable {
		p.SELinuxContext = &swarm.SELinuxContext{Disable: true}
	}
	b.spec.TaskTemplate.ContainerSpec.Privileges = p
	return b
}

// String renders the current service name (useful in diagnostics).
func (b *Builder) String() string {
	return b.spec.Name + ":" + strconv.Itoa(len(b.spec.TaskTemplate.ContainerSpec.Env))
}
