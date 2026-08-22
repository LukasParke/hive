package spec

import (
	"errors"
	"testing"
	"time"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

// TestBuilderFieldMapping maps every ServiceSpec section the builder covers
// onto the exact moby swarm types it must produce.
func TestBuilderFieldMapping(t *testing.T) {
	delay := 5 * time.Second
	window := 90 * time.Second
	maxAttempts := uint64(3)
	parallelism := uint64(2)

	tests := []struct {
		name  string
		build func(*Builder) *Builder
		check func(*testing.T, swarm.ServiceSpec)
	}{
		{
			name:  "service name",
			build: func(b *Builder) *Builder { return b },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.Name != "svc" {
					t.Fatalf("Name = %q", s.Name)
				}
			},
		},
		{
			name:  "image",
			build: func(b *Builder) *Builder { return b.Image("nginx:1.27") },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if got := s.TaskTemplate.ContainerSpec.Image; got != "nginx:1.27" {
					t.Fatalf("Image = %q", got)
				}
			},
		},
		{
			name: "env sorted key=value",
			build: func(b *Builder) *Builder {
				return b.Env(map[string]string{"B": "2", "A": "1", "C_EMPTY": ""})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				want := []string{"A=1", "B=2", "C_EMPTY="}
				got := s.TaskTemplate.ContainerSpec.Env
				if len(got) != len(want) {
					t.Fatalf("Env = %v, want %v", got, want)
				}
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("Env[%d] = %q, want %q", i, got[i], want[i])
					}
				}
			},
		},
		{
			name: "command and args",
			build: func(b *Builder) *Builder {
				return b.Command([]string{"/entrypoint.sh"}).Args([]string{"serve", "--port", "80"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				cs := s.TaskTemplate.ContainerSpec
				if len(cs.Command) != 1 || cs.Command[0] != "/entrypoint.sh" {
					t.Fatalf("Command = %v", cs.Command)
				}
				if len(cs.Args) != 3 || cs.Args[0] != "serve" {
					t.Fatalf("Args = %v", cs.Args)
				}
			},
		},
		{
			name:  "user working dir hostname",
			build: func(b *Builder) *Builder { return b.User("1000:1000").WorkingDir("/app").Hostname("worker-1") },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				cs := s.TaskTemplate.ContainerSpec
				if cs.User != "1000:1000" || cs.Dir != "/app" || cs.Hostname != "worker-1" {
					t.Fatalf("User/Dir/Hostname = %q/%q/%q", cs.User, cs.Dir, cs.Hostname)
				}
			},
		},
		{
			name: "labels split between service and container",
			build: func(b *Builder) *Builder {
				return b.ServiceLabels(map[string]string{"svc": "1"}).
					ContainerLabels(map[string]string{"ctr": "2"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.Labels["svc"] != "1" {
					t.Fatalf("service Labels = %v", s.Labels)
				}
				if _, ok := s.Labels["ctr"]; ok {
					t.Fatalf("container label leaked into service labels: %v", s.Labels)
				}
				if s.TaskTemplate.ContainerSpec.Labels["ctr"] != "2" {
					t.Fatalf("container Labels = %v", s.TaskTemplate.ContainerSpec.Labels)
				}
				if _, ok := s.TaskTemplate.ContainerSpec.Labels["svc"]; ok {
					t.Fatalf("service label leaked into container labels: %v", s.TaskTemplate.ContainerSpec.Labels)
				}
			},
		},
		{
			name: "tcp ingress port default protocol",
			build: func(b *Builder) *Builder {
				return b.Ports(Port{Target: 80, Published: 8080})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				ports := s.EndpointSpec.Ports
				if len(ports) != 1 {
					t.Fatalf("ports = %v", ports)
				}
				p := ports[0]
				if p.TargetPort != 80 || p.PublishedPort != 8080 {
					t.Fatalf("port = %+v", p)
				}
				if p.Protocol != network.TCP || p.PublishMode != swarm.PortConfigPublishModeIngress {
					t.Fatalf("protocol/mode = %q/%q", p.Protocol, p.PublishMode)
				}
			},
		},
		{
			name: "udp host port",
			build: func(b *Builder) *Builder {
				return b.Ports(Port{Name: "dns", Target: 53, Published: 1053, Protocol: "udp", Mode: "host"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				p := s.EndpointSpec.Ports[0]
				if p.Protocol != network.UDP || p.PublishMode != swarm.PortConfigPublishModeHost {
					t.Fatalf("protocol/mode = %q/%q", p.Protocol, p.PublishMode)
				}
				if p.Name != "dns" {
					t.Fatalf("name = %q", p.Name)
				}
			},
		},
		{
			name:  "dnsrr endpoint mode",
			build: func(b *Builder) *Builder { return b.EndpointMode("dnsrr") },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.EndpointSpec.Mode != swarm.ResolutionModeDNSRR {
					t.Fatalf("mode = %q", s.EndpointSpec.Mode)
				}
			},
		},
		{
			name:  "vip endpoint mode explicit",
			build: func(b *Builder) *Builder { return b.EndpointMode("vip") },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.EndpointSpec.Mode != swarm.ResolutionModeVIP {
					t.Fatalf("mode = %q", s.EndpointSpec.Mode)
				}
			},
		},
		{
			name: "networks resolved by name to ID",
			build: func(b *Builder) *Builder {
				return b.NetworkResolver(func(name string) (string, error) {
					return map[string]string{"web": "net-id-web", "db": "net-id-db"}[name], nil
				}).Networks("web", "db")
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				nets := s.TaskTemplate.Networks
				if len(nets) != 2 || nets[0].Target != "net-id-web" || nets[1].Target != "net-id-db" {
					t.Fatalf("Networks = %+v", nets)
				}
			},
		},
		{
			name: "network without resolver keeps name",
			build: func(b *Builder) *Builder {
				return b.Networks("backdrop")
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if got := s.TaskTemplate.Networks[0].Target; got != "backdrop" {
					t.Fatalf("Target = %q", got)
				}
			},
		},
		{
			name:  "replicated mode",
			build: func(b *Builder) *Builder { return b.Replicas(4) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.Mode.Replicated == nil || s.Mode.Replicated.Replicas == nil || *s.Mode.Replicated.Replicas != 4 {
					t.Fatalf("Mode = %+v", s.Mode)
				}
			},
		},
		{
			name:  "global mode",
			build: func(b *Builder) *Builder { return b.Global() },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.Mode.Global == nil {
					t.Fatalf("Mode = %+v", s.Mode)
				}
			},
		},
		{
			name:  "replicated job mode",
			build: func(b *Builder) *Builder { return b.ReplicatedJob(2, 10) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				job := s.Mode.ReplicatedJob
				if job == nil || job.MaxConcurrent == nil || *job.MaxConcurrent != 2 ||
					job.TotalCompletions == nil || *job.TotalCompletions != 10 {
					t.Fatalf("Mode = %+v", s.Mode)
				}
			},
		},
		{
			name:  "global job mode",
			build: func(b *Builder) *Builder { return b.GlobalJob() },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.Mode.GlobalJob == nil {
					t.Fatalf("Mode = %+v", s.Mode)
				}
			},
		},
		{
			name: "resource limits",
			build: func(b *Builder) *Builder {
				return b.Limits(Limit{CPUs: 1.5, MemoryBytes: 256 << 20, Pids: 100})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				limits := s.TaskTemplate.Resources.Limits
				if limits == nil {
					t.Fatal("Limits is nil")
				}
				if limits.NanoCPUs != 1_500_000_000 || limits.MemoryBytes != 256<<20 || limits.Pids != 100 {
					t.Fatalf("Limits = %+v", limits)
				}
			},
		},
		{
			name: "reservations with GPUs as discrete generic resource",
			build: func(b *Builder) *Builder {
				return b.Reservations(Reservation{CPUs: 0.5, MemoryBytes: 128 << 20, GPUs: 2})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				res := s.TaskTemplate.Resources.Reservations
				if res == nil || res.NanoCPUs != 500_000_000 || res.MemoryBytes != 128<<20 {
					t.Fatalf("Reservations = %+v", res)
				}
				if len(res.GenericResources) != 1 {
					t.Fatalf("GenericResources = %+v", res.GenericResources)
				}
				gr := res.GenericResources[0]
				if gr.DiscreteResourceSpec == nil ||
					gr.DiscreteResourceSpec.Kind != "gpu" || gr.DiscreteResourceSpec.Value != 2 {
					t.Fatalf("GPU resource = %+v", gr)
				}
			},
		},
		{
			name: "generic resources passed through",
			build: func(b *Builder) *Builder {
				return b.Reservations(Reservation{Generic: []swarm.GenericResource{{
					NamedResourceSpec: &swarm.NamedGenericResource{Kind: "ssa", Value: "dev"},
				}}})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				gr := s.TaskTemplate.Resources.Reservations.GenericResources
				if len(gr) != 1 || gr[0].NamedResourceSpec == nil || gr[0].NamedResourceSpec.Kind != "ssa" {
					t.Fatalf("GenericResources = %+v", gr)
				}
			},
		},
		{
			name: "placement constraints preferences and max replicas",
			build: func(b *Builder) *Builder {
				return b.Placement(Placement{
					Constraints:        []string{"node.role==worker", "node.labels.gpu==true"},
					SpreadPreferences:  []string{"node.labels.zone"},
					MaxReplicasPerNode: 2,
				})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				pl := s.TaskTemplate.Placement
				if pl == nil {
					t.Fatal("Placement is nil")
				}
				if len(pl.Constraints) != 2 || pl.Constraints[0] != "node.role==worker" {
					t.Fatalf("Constraints = %v", pl.Constraints)
				}
				if len(pl.Preferences) != 1 || pl.Preferences[0].Spread == nil ||
					pl.Preferences[0].Spread.SpreadDescriptor != "node.labels.zone" {
					t.Fatalf("Preferences = %+v", pl.Preferences)
				}
				if pl.MaxReplicas != 2 {
					t.Fatalf("MaxReplicas = %d", pl.MaxReplicas)
				}
			},
		},
		{
			name: "update config",
			build: func(b *Builder) *Builder {
				return b.UpdateConfig(UpdateRollbackConfig{
					Parallelism:     parallelism,
					Delay:           delay,
					FailureAction:   "rollback",
					Monitor:         30 * time.Second,
					MaxFailureRatio: 0.25,
					Order:           "start-first",
				})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				uc := s.UpdateConfig
				if uc == nil {
					t.Fatal("UpdateConfig is nil")
				}
				if uc.Parallelism != 2 || uc.Delay != delay || uc.FailureAction != "rollback" ||
					uc.Monitor != 30*time.Second || uc.MaxFailureRatio != 0.25 || uc.Order != "start-first" {
					t.Fatalf("UpdateConfig = %+v", uc)
				}
			},
		},
		{
			name: "rollback config",
			build: func(b *Builder) *Builder {
				return b.RollbackConfig(UpdateRollbackConfig{Parallelism: 1, Order: "stop-first"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				rc := s.RollbackConfig
				if rc == nil || rc.Parallelism != 1 || rc.Order != "stop-first" {
					t.Fatalf("RollbackConfig = %+v", rc)
				}
			},
		},
		{
			name: "restart policy full",
			build: func(b *Builder) *Builder {
				return b.RestartPolicy(RestartPolicy{
					Condition:   "on-failure",
					Delay:       &delay,
					MaxAttempts: &maxAttempts,
					Window:      &window,
				})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				rp := s.TaskTemplate.RestartPolicy
				if rp == nil || rp.Condition != swarm.RestartPolicyConditionOnFailure ||
					rp.Delay == nil || *rp.Delay != delay ||
					rp.MaxAttempts == nil || *rp.MaxAttempts != 3 ||
					rp.Window == nil || *rp.Window != window {
					t.Fatalf("RestartPolicy = %+v", rp)
				}
			},
		},
		{
			name:  "log driver",
			build: func(b *Builder) *Builder { return b.LogDriver("json-file", map[string]string{"max-size": "10m"}) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				ld := s.TaskTemplate.LogDriver
				if ld == nil || ld.Name != "json-file" || ld.Options["max-size"] != "10m" {
					t.Fatalf("LogDriver = %+v", ld)
				}
			},
		},
		{
			name: "healthcheck",
			build: func(b *Builder) *Builder {
				return b.Healthcheck(Healthcheck{
					Test:          []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
					Interval:      10 * time.Second,
					Timeout:       3 * time.Second,
					StartPeriod:   15 * time.Second,
					StartInterval: 2 * time.Second,
					Retries:       5,
				})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				hc := s.TaskTemplate.ContainerSpec.Healthcheck
				if hc == nil {
					t.Fatal("Healthcheck is nil")
				}
				if len(hc.Test) != 2 || hc.Test[0] != "CMD-SHELL" {
					t.Fatalf("Test = %v", hc.Test)
				}
				if hc.Interval != 10*time.Second || hc.Timeout != 3*time.Second ||
					hc.StartPeriod != 15*time.Second || hc.StartInterval != 2*time.Second || hc.Retries != 5 {
					t.Fatalf("Healthcheck = %+v", hc)
				}
			},
		},
		{
			name:  "healthcheck disabled",
			build: func(b *Builder) *Builder { return b.Healthcheck(Healthcheck{Disable: true}) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				hc := s.TaskTemplate.ContainerSpec.Healthcheck
				if hc == nil || len(hc.Test) != 1 || hc.Test[0] != "NONE" {
					t.Fatalf("disabled Healthcheck = %+v", hc)
				}
			},
		},
		{
			name: "mounts bind volume tmpfs npipe",
			build: func(b *Builder) *Builder {
				return b.Mounts(
					Mount{Type: "bind", Source: "/srv/data", Target: "/data", ReadOnly: true, BindPropagation: "rslave"},
					Mount{Type: "volume", Source: "pgdata", Target: "/var/lib/postgresql/data", VolumeNoCopy: true},
					Mount{Type: "tmpfs", Target: "/run", TmpfsSizeBytes: 64 << 20, TmpfsMode: 0o700},
					Mount{Type: "npipe", Source: "\\\\.\\pipe\\docker_engine", Target: "\\\\.\\pipe\\docker_engine"},
				)
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				mounts := s.TaskTemplate.ContainerSpec.Mounts
				if len(mounts) != 4 {
					t.Fatalf("Mounts = %+v", mounts)
				}
				bind := mounts[0]
				if bind.Type != mount.TypeBind || !bind.ReadOnly || bind.Source != "/srv/data" ||
					bind.BindOptions == nil || bind.BindOptions.Propagation != mount.Propagation("rslave") {
					t.Fatalf("bind mount = %+v", bind)
				}
				vol := mounts[1]
				if vol.Type != mount.TypeVolume || vol.VolumeOptions == nil || !vol.VolumeOptions.NoCopy {
					t.Fatalf("volume mount = %+v", vol)
				}
				tmpfs := mounts[2]
				if tmpfs.Type != mount.TypeTmpfs || tmpfs.TmpfsOptions == nil ||
					tmpfs.TmpfsOptions.SizeBytes != 64<<20 ||
					tmpfs.TmpfsOptions.Mode == 0 {
					t.Fatalf("tmpfs mount = %+v", tmpfs)
				}
				if mounts[3].Type != mount.TypeNamedPipe {
					t.Fatalf("npipe mount = %+v", mounts[3])
				}
			},
		},
		{
			name: "extra hosts in hosts-file format",
			build: func(b *Builder) *Builder {
				return b.Hosts(map[string]string{"api.internal": "10.0.0.5", "db.internal": "10.0.0.6"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				hosts := s.TaskTemplate.ContainerSpec.Hosts
				if len(hosts) != 2 {
					t.Fatalf("Hosts = %v", hosts)
				}
				if hosts[0] != "10.0.0.5 api.internal" || hosts[1] != "10.0.0.6 db.internal" {
					t.Fatalf("Hosts = %v", hosts)
				}
			},
		},
		{
			name: "dns config",
			build: func(b *Builder) *Builder {
				return b.DNS([]string{"127.0.0.11"}, []string{"corp.local"}, []string{"ndots:2"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				dns := s.TaskTemplate.ContainerSpec.DNSConfig
				if dns == nil || len(dns.Nameservers) != 1 || dns.Nameservers[0].String() != "127.0.0.11" {
					t.Fatalf("DNSConfig = %+v", dns)
				}
				if len(dns.Search) != 1 || dns.Search[0] != "corp.local" || dns.Options[0] != "ndots:2" {
					t.Fatalf("DNSConfig = %+v", dns)
				}
			},
		},
		{
			name:  "capabilities",
			build: func(b *Builder) *Builder { return b.Capabilities([]string{"NET_ADMIN"}, []string{"CHOWN"}) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				cs := s.TaskTemplate.ContainerSpec
				if len(cs.CapabilityAdd) != 1 || cs.CapabilityAdd[0] != "NET_ADMIN" ||
					len(cs.CapabilityDrop) != 1 || cs.CapabilityDrop[0] != "CHOWN" {
					t.Fatalf("Capabilities add/drop = %v/%v", cs.CapabilityAdd, cs.CapabilityDrop)
				}
			},
		},
		{
			name: "ulimits",
			build: func(b *Builder) *Builder {
				return b.Ulimits(Ulimit{Name: "nofile", Soft: 1024, Hard: 4096})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				ulimits := s.TaskTemplate.ContainerSpec.Ulimits
				if len(ulimits) != 1 || ulimits[0].Name != "nofile" ||
					ulimits[0].Soft != 1024 || ulimits[0].Hard != 4096 {
					t.Fatalf("Ulimits = %+v", ulimits)
				}
			},
		},
		{
			name:  "init enabled",
			build: func(b *Builder) *Builder { return b.Init(true) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				init := s.TaskTemplate.ContainerSpec.Init
				if init == nil || !*init {
					t.Fatalf("Init = %+v", init)
				}
			},
		},
		{
			name:  "stop signal and grace period",
			build: func(b *Builder) *Builder { return b.StopSignal("SIGQUIT").StopGracePeriod(45 * time.Second) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				cs := s.TaskTemplate.ContainerSpec
				if cs.StopSignal != "SIGQUIT" || cs.StopGracePeriod == nil || *cs.StopGracePeriod != 45*time.Second {
					t.Fatalf("StopSignal/StopGracePeriod = %q/%+v", cs.StopSignal, cs.StopGracePeriod)
				}
			},
		},
		{
			name:  "sysctls",
			build: func(b *Builder) *Builder { return b.Sysctls(map[string]string{"net.ipv4.ip_forward": "1"}) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				if s.TaskTemplate.ContainerSpec.Sysctls["net.ipv4.ip_forward"] != "1" {
					t.Fatalf("Sysctls = %+v", s.TaskTemplate.ContainerSpec.Sysctls)
				}
			},
		},
		{
			name:  "groups tty stdin readonly",
			build: func(b *Builder) *Builder { return b.Groups([]string{"audio"}).TTY(true).OpenStdin(true).ReadOnly(true) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				cs := s.TaskTemplate.ContainerSpec
				if len(cs.Groups) != 1 || cs.Groups[0] != "audio" || !cs.TTY || !cs.OpenStdin || !cs.ReadOnly {
					t.Fatalf("Groups/TTY/OpenStdin/ReadOnly = %v/%v/%v/%v", cs.Groups, cs.TTY, cs.OpenStdin, cs.ReadOnly)
				}
			},
		},
		{
			name: "secret refs with defaults",
			build: func(b *Builder) *Builder {
				return b.Secrets(FileRef{ID: "sid-1", Name: "token"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				secrets := s.TaskTemplate.ContainerSpec.Secrets
				if len(secrets) != 1 {
					t.Fatalf("Secrets = %+v", secrets)
				}
				ref := secrets[0]
				if ref.SecretID != "sid-1" || ref.SecretName != "token" {
					t.Fatalf("secret ref = %+v", ref)
				}
				if ref.File == nil || ref.File.Name != "token" || ref.File.UID != "0" || ref.File.GID != "0" || ref.File.Mode != 0o444 {
					t.Fatalf("secret file target = %+v", ref.File)
				}
			},
		},
		{
			name: "secret refs custom target uid gid mode",
			build: func(b *Builder) *Builder {
				return b.Secrets(FileRef{ID: "sid", Name: "token", Target: "auth_token", UID: "33", GID: "33", Mode: 0o400})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				file := s.TaskTemplate.ContainerSpec.Secrets[0].File
				if file.Name != "auth_token" || file.UID != "33" || file.GID != "33" || file.Mode != 0o400 {
					t.Fatalf("file target = %+v", file)
				}
			},
		},
		{
			name: "config refs",
			build: func(b *Builder) *Builder {
				return b.Configs(FileRef{ID: "cid-1", Name: "nginx-conf", Target: "/etc/nginx/nginx.conf"})
			},
			check: func(t *testing.T, s swarm.ServiceSpec) {
				configs := s.TaskTemplate.ContainerSpec.Configs
				if len(configs) != 1 {
					t.Fatalf("Configs = %+v", configs)
				}
				ref := configs[0]
				if ref.ConfigID != "cid-1" || ref.ConfigName != "nginx-conf" {
					t.Fatalf("config ref = %+v", ref)
				}
				if ref.File == nil || ref.File.Name != "/etc/nginx/nginx.conf" || ref.File.Mode != 0o444 {
					t.Fatalf("config file target = %+v", ref.File)
				}
			},
		},
		{
			name:  "privileges no-new-privileges and selinux disable",
			build: func(b *Builder) *Builder { return b.Privileges(true, true) },
			check: func(t *testing.T, s swarm.ServiceSpec) {
				p := s.TaskTemplate.ContainerSpec.Privileges
				if p == nil || !p.NoNewPrivileges || p.SELinuxContext == nil || !p.SELinuxContext.Disable {
					t.Fatalf("Privileges = %+v", p)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := tt.build(NewService("svc")).Build()
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			tt.check(t, spec)
		})
	}
}

// TestBuilderErrorPropagation verifies setter errors surface from Build.
func TestBuilderErrorPropagation(t *testing.T) {
	t.Run("bad nameserver", func(t *testing.T) {
		_, err := NewService("svc").DNS([]string{"not-an-ip"}, nil, nil).Build()
		if err == nil || !errors.Is(err, errBadNameserverHint()) && err.Error() == "" {
			t.Fatalf("want error for bad nameserver, got %v", err)
		}
	})

	t.Run("resolver failure", func(t *testing.T) {
		resolverErr := errors.New("network not found")
		_, err := NewService("svc").
			NetworkResolver(func(string) (string, error) { return "", resolverErr }).
			Networks("missing").
			Build()
		if !errors.Is(err, resolverErr) {
			t.Fatalf("err = %v, want wrapped %v", err, resolverErr)
		}
	})

	t.Run("first error wins", func(t *testing.T) {
		_, err := NewService("svc").
			DNS([]string{"bogus"}, nil, nil).
			DNS([]string{"also-bad"}, nil, nil).
			Build()
		if err == nil || err.Error() != wantFirstDNSError() {
			t.Fatalf("err = %v, want first DNS error only", err)
		}
	})
}

func errBadNameserverHint() error { return errors.New("parse DNS nameserver") }

func wantFirstDNSError() string {
	b := NewService("svc")
	b.DNS([]string{"bogus"}, nil, nil)
	_, err := b.Build()
	return err.Error()
}
