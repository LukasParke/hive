package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

const fixtureCompose = `
services:
  api:
    image: registry.example/api:1.4
    environment:
      LOG_LEVEL: debug
      EMPTY_VAR: ""
    entrypoint: ["/docker-entrypoint.sh"]
    command: ["serve", "--port", "8080"]
    labels:
      com.example.tier: "frontend"
    ports:
      - target: 8080
        published: "8080"
        protocol: tcp
        mode: ingress
      - target: 53
        published: "1053"
        protocol: udp
        mode: host
    networks:
      - web
      - back
    logging:
      driver: json-file
      options:
        max-size: "10m"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
    volumes:
      - type: bind
        source: ./data
        target: /data
        read_only: true
      - type: volume
        source: api-cache
        target: /cache
    extra_hosts:
      - "db.internal:10.0.0.6"
    ulimits:
      nofile:
        soft: 1024
        hard: 4096
    secrets:
      - source: api_token
        target: auth_token
        uid: "33"
        gid: "33"
        mode: 0400
    configs:
      - source: nginx_conf
        target: /etc/nginx/nginx.conf
    deploy:
      replicas: 3
      labels:
        hive.stack: "prod"
      update_config:
        parallelism: 2
        delay: 10s
        failure_action: rollback
        monitor: 30s
        max_failure_ratio: 0.25
        order: start-first
      rollback_config:
        parallelism: 1
        order: stop-first
      resources:
        limits:
          cpus: "1.50"
          memory: 256M
          pids: 200
        reservations:
          cpus: "0.50"
          memory: 128M
          generic_resources:
            - discrete_resource_spec:
                kind: ssa
                value: 1
          devices:
            - capabilities: ["gpu"]
              count: 2
      restart_policy:
        condition: on-failure
        delay: 5s
        max_attempts: 3
        window: 90s
      placement:
        constraints:
          - node.role==worker
        preferences:
          - spread: node.labels.zone
        max_replicas_per_node: 2
      endpoint_mode: dnsrr

  worker:
    image: registry.example/worker:2.0
    environment:
      - QUEUE=default
      - FLAG
    deploy:
      mode: global

networks:
  web:
    driver: overlay
  back:

secrets:
  api_token:
    file: token.txt

configs:
  nginx_conf:
    file: nginx.conf

volumes:
  api-cache:
`

func writeFixture(t *testing.T, compose string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token.txt"), []byte("s3cr3t-data"), 0o600); err != nil {
		t.Fatalf("write secret fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte("worker_processes auto;"), 0o600); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatalf("write compose fixture: %v", err)
	}
	return dir
}

// TestParseStackFixture validates the compose-go loader path end to end,
// replacing the removed hand-rolled yaml structs.
func TestParseStackFixture(t *testing.T) {
	dir := writeFixture(t, fixtureCompose)
	raw, err := os.ReadFile(filepath.Join(dir, "compose.yml")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	project, err := parseStack(context.Background(), raw, dir, "mystack")
	if err != nil {
		t.Fatalf("parseStack: %v", err)
	}
	if len(project.Services) != 2 {
		t.Fatalf("services = %d, want 2", len(project.Services))
	}
	if _, ok := project.Secrets["api_token"]; !ok {
		t.Fatalf("missing secret api_token in %+v", project.Secrets)
	}
	cfg, hasCfg := project.Configs["nginx_conf"]
	if !hasCfg || cfg.File == "" {
		t.Fatalf("config nginx_conf = %+v (present=%v)", cfg, hasCfg)
	}
}

func TestServiceSpecFromComposeFullMapping(t *testing.T) {
	dir := writeFixture(t, fixtureCompose)
	raw, err := os.ReadFile(filepath.Join(dir, "compose.yml")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project, err := parseStack(ctx, raw, dir, "mystack")
	if err != nil {
		t.Fatalf("parseStack: %v", err)
	}
	networkTargets, external := stackNetworks(project, "mystack")
	if external["web"] || external["back"] {
		t.Fatalf("fixture networks must not be external: %v", external)
	}
	wantTargets := map[string]string{"web": "mystack_web", "back": "mystack_back", "default": "mystack_default"}
	for name, want := range wantTargets {
		if networkTargets[name] != want {
			t.Fatalf("network %q target = %q, want %q", name, networkTargets[name], want)
		}
	}

	spec, err := serviceSpecFromCompose("mystack", "api", project.Services["api"], networkTargets, map[string]string{"api_token": "secret-id-1"}, map[string]string{"nginx_conf": "config-id-1"})
	if err != nil {
		t.Fatalf("serviceSpecFromCompose: %v", err)
	}

	if spec.Name != "mystack_api" {
		t.Fatalf("Name = %q", spec.Name)
	}
	cs := spec.TaskTemplate.ContainerSpec

	if cs.Image != "registry.example/api:1.4" {
		t.Fatalf("Image = %q", cs.Image)
	}
	envSet := map[string]bool{}
	for _, e := range cs.Env {
		envSet[e] = true
	}
	if !envSet["LOG_LEVEL=debug"] || !envSet["EMPTY_VAR="] {
		t.Fatalf("Env = %v", cs.Env)
	}
	if len(cs.Command) != 1 || cs.Command[0] != "/docker-entrypoint.sh" {
		t.Fatalf("Command = %v", cs.Command)
	}
	if len(cs.Args) != 3 || cs.Args[0] != "serve" {
		t.Fatalf("Args = %v", cs.Args)
	}

	// Labels: compose service labels + namespace on containers; deploy.labels
	// + namespace on the service object.
	if cs.Labels["com.example.tier"] != "frontend" || cs.Labels[NamespaceLabel] != "mystack" {
		t.Fatalf("container labels = %v", cs.Labels)
	}
	if spec.Labels["hive.stack"] != "prod" || spec.Labels[NamespaceLabel] != "mystack" {
		t.Fatalf("service labels = %v", spec.Labels)
	}
	if _, leaked := spec.Labels["com.example.tier"]; leaked {
		t.Fatalf("compose label leaked into service labels: %v", spec.Labels)
	}

	ports := spec.EndpointSpec.Ports
	if len(ports) != 2 {
		t.Fatalf("ports = %+v", ports)
	}
	if ports[0].TargetPort != 8080 || ports[0].PublishedPort != 8080 ||
		ports[0].PublishMode != swarm.PortConfigPublishModeIngress {
		t.Fatalf("tcp port = %+v", ports[0])
	}
	if ports[1].TargetPort != 53 || ports[1].Protocol != network.UDP ||
		ports[1].PublishMode != swarm.PortConfigPublishModeHost {
		t.Fatalf("udp port = %+v", ports[1])
	}
	if spec.EndpointSpec.Mode != swarm.ResolutionModeDNSRR {
		t.Fatalf("endpoint mode = %q", spec.EndpointSpec.Mode)
	}

	nets := csNetworkTargets(spec)
	netSet := map[string]bool{}
	for _, n := range nets {
		netSet[n] = true
	}
	if len(nets) != 2 || !netSet["mystack_web"] || !netSet["mystack_back"] {
		t.Fatalf("networks = %v", nets)
	}

	// Mode and resources.
	if spec.Mode.Replicated == nil || spec.Mode.Replicated.Replicas == nil || *spec.Mode.Replicated.Replicas != 3 {
		t.Fatalf("Mode = %+v", spec.Mode)
	}
	limits := spec.TaskTemplate.Resources.Limits
	if limits == nil || limits.NanoCPUs != 1_500_000_000 || limits.MemoryBytes != 256<<20 || limits.Pids != 200 {
		t.Fatalf("Limits = %+v", limits)
	}
	res := spec.TaskTemplate.Resources.Reservations
	if res == nil || res.NanoCPUs != 500_000_000 || res.MemoryBytes != 128<<20 {
		t.Fatalf("Reservations = %+v", res)
	}
	// generic_resources pass through plus one GPU device request → 2 discrete kinds.
	kinds := map[string]int64{}
	for _, gr := range res.GenericResources {
		if gr.DiscreteResourceSpec != nil {
			kinds[gr.DiscreteResourceSpec.Kind] = gr.DiscreteResourceSpec.Value
		}
	}
	if kinds["ssa"] != 1 || kinds["gpu"] != 2 {
		t.Fatalf("generic resources kinds = %v", kinds)
	}

	// Placement.
	pl := spec.TaskTemplate.Placement
	if pl == nil || len(pl.Constraints) != 1 || pl.Constraints[0] != "node.role==worker" {
		t.Fatalf("Placement = %+v", pl)
	}
	if len(pl.Preferences) != 1 || pl.Preferences[0].Spread == nil ||
		pl.Preferences[0].Spread.SpreadDescriptor != "node.labels.zone" {
		t.Fatalf("Preferences = %+v", pl.Preferences)
	}
	if pl.MaxReplicas != 2 {
		t.Fatalf("MaxReplicas = %d", pl.MaxReplicas)
	}

	// Update / rollback config.
	uc := spec.UpdateConfig
	if uc == nil || uc.Parallelism != 2 || uc.Delay.String() != "10s" ||
		uc.FailureAction != "rollback" || uc.Monitor.String() != "30s" ||
		uc.MaxFailureRatio != 0.25 || uc.Order != "start-first" {
		t.Fatalf("UpdateConfig = %+v", uc)
	}
	rc := spec.RollbackConfig
	if rc == nil || rc.Parallelism != 1 || rc.Order != "stop-first" {
		t.Fatalf("RollbackConfig = %+v", rc)
	}

	// Restart policy.
	rp := spec.TaskTemplate.RestartPolicy
	if rp == nil || rp.Condition != swarm.RestartPolicyConditionOnFailure ||
		rp.Delay == nil || *rp.Delay != 5*time.Second ||
		rp.MaxAttempts == nil || *rp.MaxAttempts != 3 ||
		rp.Window == nil || *rp.Window != 90*time.Second {
		t.Fatalf("RestartPolicy = %+v", rp)
	}

	// Log driver and healthcheck.
	ld := spec.TaskTemplate.LogDriver
	if ld == nil || ld.Name != "json-file" || ld.Options["max-size"] != "10m" {
		t.Fatalf("LogDriver = %+v", ld)
	}
	hc := cs.Healthcheck
	if hc == nil || len(hc.Test) != 4 || hc.Interval != 30*time.Second ||
		hc.Timeout != 5*time.Second || hc.Retries != 3 {
		t.Fatalf("Healthcheck = %+v", hc)
	}

	// Mounts.
	if len(cs.Mounts) != 2 {
		t.Fatalf("Mounts = %+v", cs.Mounts)
	}
	bind := cs.Mounts[0]
	if bind.Type != mount.TypeBind || !bind.ReadOnly || bind.Target != "/data" {
		t.Fatalf("bind mount = %+v", bind)
	}
	vol := cs.Mounts[1]
	if vol.Type != mount.TypeVolume || vol.Source != "api-cache" {
		t.Fatalf("volume mount = %+v", vol)
	}

	// Extra hosts and ulimits.
	hostJoined := false
	for _, h := range cs.Hosts {
		if h == "10.0.0.6 db.internal" {
			hostJoined = true
		}
	}
	if !hostJoined {
		t.Fatalf("Hosts = %v", cs.Hosts)
	}
	if len(cs.Ulimits) != 1 || cs.Ulimits[0].Name != "nofile" || cs.Ulimits[0].Soft != 1024 || cs.Ulimits[0].Hard != 4096 {
		t.Fatalf("Ulimits = %+v", cs.Ulimits)
	}

	// Secret and config references.
	if len(cs.Secrets) != 1 {
		t.Fatalf("Secrets = %+v", cs.Secrets)
	}
	secRef := cs.Secrets[0]
	if secRef.SecretID != "secret-id-1" || secRef.SecretName != "api_token" ||
		secRef.File == nil || secRef.File.Name != "auth_token" ||
		secRef.File.UID != "33" || secRef.File.Mode != 0o400 {
		t.Fatalf("secret ref = %+v", secRef)
	}
	if len(cs.Configs) != 1 {
		t.Fatalf("Configs = %+v", cs.Configs)
	}
	cfgRef := cs.Configs[0]
	if cfgRef.ConfigID != "config-id-1" || cfgRef.File == nil || cfgRef.File.Name != "/etc/nginx/nginx.conf" {
		t.Fatalf("config ref = %+v", cfgRef)
	}
}

func csNetworkTargets(spec swarm.ServiceSpec) []string {
	out := make([]string, 0, len(spec.TaskTemplate.Networks))
	for _, n := range spec.TaskTemplate.Networks {
		out = append(out, n.Target)
	}
	return out
}

// TestWorkerGlobalModeAndShortSyntax covers global deploy mode and list-style
// environment parsing through the loader.
func TestWorkerGlobalModeAndShortSyntax(t *testing.T) {
	t.Setenv("FLAG", "1") // bare `- FLAG` entries resolve from the controller environment
	dir := writeFixture(t, fixtureCompose)
	raw, err := os.ReadFile(filepath.Join(dir, "compose.yml")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	project, err := parseStack(context.Background(), raw, dir, "mystack")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := serviceSpecFromCompose("mystack", "worker", project.Services["worker"], nil, nil, nil)
	if err != nil {
		t.Fatalf("serviceSpecFromCompose: %v", err)
	}
	if spec.Mode.Global == nil {
		t.Fatalf("expected global mode, got %+v", spec.Mode)
	}
	found := map[string]bool{}
	for _, e := range spec.TaskTemplate.ContainerSpec.Env {
		found[e] = true
	}
	if !found["QUEUE=default"] || !found["FLAG=1"] {
		t.Fatalf("Env = %v", spec.TaskTemplate.ContainerSpec.Env)
	}
	// No explicit networks → default overlay.
	targets := csNetworkTargets(spec)
	if len(targets) != 1 || targets[0] != "mystack_default" {
		t.Fatalf("networks = %v", targets)
	}
}

// TestDiffStackFixtures exercises the pure diff logic used by
// PreviewStackDeploy.
func TestDiffStackFixtures(t *testing.T) {
	desiredA := func(name string) swarm.ServiceSpec {
		return swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   name,
				Labels: map[string]string{NamespaceLabel: "mystack"},
			},
		}
	}
	live := []swarm.Service{
		{ID: "svc-kept", Spec: desiredA("mystack_kept")},
		{ID: "svc-drifted", Spec: desiredA("mystack_drifted")},
		{ID: "svc-gone-name", Spec: desiredA("mystack_removed")},
		{ID: "svc-other-stack", Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "other_api", Labels: map[string]string{NamespaceLabel: "other"}},
		}},
	}
	// Mark drifted as different from desired.
	drifted := live[1]
	drifted.Spec.Labels["drift"] = "true"
	live[1] = drifted

	diff := diffStack("mystack", []swarm.ServiceSpec{
		desiredA("mystack_kept"),
		desiredA("mystack_drifted"), // clean copy; live copy carries an extra label below
		desiredA("mystack_new"),
	}, live)
	if got := diff.ToCreate; len(got) != 1 || got[0] != "mystack_new" {
		t.Fatalf("ToCreate = %v", diff.ToCreate)
	}
	if got := diff.ToUpdate; len(got) != 1 || got[0] != "mystack_drifted" {
		t.Fatalf("ToUpdate = %v", diff.ToUpdate)
	}
	if got := diff.ToRemove; len(got) != 1 || got[0] != "mystack_removed" {
		t.Fatalf("ToRemove = %v", diff.ToRemove)
	}
}

// TestDiffStackIdenticalReportsNoChanges guards against false positives when
// live specs match the desired translation exactly.
func TestDiffStackIdenticalReportsNoChanges(t *testing.T) {
	dir := writeFixture(t, `
services:
  api:
    image: nginx:1.27
    ports:
      - target: 80
        published: "8080"
`)
	raw, err := os.ReadFile(filepath.Join(dir, "compose.yml")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project, err := parseStack(ctx, raw, "", "mystack")
	if err != nil {
		t.Fatal(err)
	}
	targets, _ := stackNetworks(project, "mystack")
	spec, err := serviceSpecFromCompose("mystack", "api", project.Services["api"], targets, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	live := []swarm.Service{{ID: "id-1", Spec: spec}}
	diff := diffStack("mystack", []swarm.ServiceSpec{spec}, live)
	if len(diff.ToCreate)+len(diff.ToUpdate)+len(diff.ToRemove) != 0 {
		t.Fatalf("identical specs produced diff: %+v", diff)
	}
}
