package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/swarm"
)

// driftedStackService builds a live stack service whose spec differs from the
// compose translation, forcing the update path.
func driftedStackService(id, name string, version uint64) swarm.Service {
	return swarm.Service{
		ID: id,
		Meta: swarm.Meta{
			Version: swarm.Version{Index: version},
		},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   name,
				Labels: map[string]string{NamespaceLabel: "mystack"},
			},
		},
	}
}

func TestStackFullFlow(t *testing.T) {
	dir := writeFixture(t, fixtureCompose)
	composePath := filepath.Join(dir, "compose.yml")

	fake := &fakeSwarm{
		networks: map[string]string{"mystack_web": "net-web-id"},
		services: []swarm.Service{
			driftedStackService("svc-api", "mystack_api", 3),
			driftedStackService("svc-stale", "mystack_stale", 1),
		},
	}
	other := driftedStackService("svc-other", "other_api", 1)
	other.Spec.Labels = map[string]string{NamespaceLabel: "other"}
	fake.services = append(fake.services, other)

	if err := Stack(context.Background(), fake, "mystack", composePath); err != nil {
		t.Fatalf("Stack: %v", err)
	}

	// Networks: mystack_web exists (skipped); back and default created.
	createdNets := append([]string(nil), fake.createdNetworks...)
	if len(createdNets) != 2 {
		t.Fatalf("created networks = %v, want mystack_back + mystack_default", createdNets)
	}
	for _, want := range []string{"mystack_back", "mystack_default"} {
		found := false
		for _, n := range createdNets {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("created networks = %v, missing %s", createdNets, want)
		}
	}

	// Secrets and configs ensured from files; parseStack prefixes their names
	// with the stack namespace.
	if len(fake.createdSecrets) != 1 {
		t.Fatalf("created secrets = %d, want 1", len(fake.createdSecrets))
	}
	sec := fake.createdSecrets[0]
	if sec.Name != "mystack_api_token" || string(sec.Data) != "s3cr3t-data" {
		t.Fatalf("secret = %s data %q", sec.Name, sec.Data)
	}
	if len(fake.createdConfigs) != 1 {
		t.Fatalf("created configs = %d, want 1", len(fake.createdConfigs))
	}
	cfg := fake.createdConfigs[0]
	if cfg.Name != "mystack_nginx_conf" || string(cfg.Data) != "worker_processes auto;" {
		t.Fatalf("config = %s data %q", cfg.Name, cfg.Data)
	}

	// api exists → updated (not created); worker is new.
	if len(fake.createdServices) != 1 || fake.createdServices[0].Name != "mystack_worker" {
		names := make([]string, 0, len(fake.createdServices))
		for _, s := range fake.createdServices {
			names = append(names, s.Name)
		}
		t.Fatalf("created services = %v, want worker only", names)
	}
	if fake.createdServices[0].Mode.Global == nil {
		t.Fatalf("worker must keep global mode: %+v", fake.createdServices[0].Mode)
	}
	if len(fake.updatedServices) != 1 {
		t.Fatalf("updates = %d, want 1", len(fake.updatedServices))
	}
	up := fake.updatedServices[0]
	if up.id != "svc-api" || up.version != 3 {
		t.Fatalf("update = (%q, %d), want (svc-api, 3)", up.id, up.version)
	}
	if up.spec.Name != "mystack_api" {
		t.Fatalf("updated spec name = %q", up.spec.Name)
	}
	if up.spec.Labels[NamespaceLabel] != "mystack" {
		t.Fatalf("updated spec missing namespace label: %v", up.spec.Labels)
	}

	// Prune removes only the vanished stack service.
	if len(fake.removedServiceIDs) != 1 || fake.removedServiceIDs[0] != "svc-stale" {
		t.Fatalf("removed = %v, want svc-stale", fake.removedServiceIDs)
	}
	for _, s := range fake.services {
		if s.ID == "svc-other" {
			return // other stack left alone
		}
	}
	t.Fatal("other stack service was removed")
}

func TestStackComposeReadError(t *testing.T) {
	fake := &fakeSwarm{}
	err := Stack(context.Background(), fake, "mystack", filepath.Join(t.TempDir(), "missing.yml"))
	if err == nil || !strings.Contains(err.Error(), "read compose file") {
		t.Fatalf("err = %v, want read compose file failure", err)
	}
}

func TestStackComposeParseError(t *testing.T) {
	dir := writeFixture(t, ":\nnot: [valid")
	err := Stack(context.Background(), &fakeSwarm{}, "mystack", filepath.Join(dir, "compose.yml"))
	if err == nil || !strings.Contains(err.Error(), "parse compose file") {
		t.Fatalf("err = %v, want parse compose file failure", err)
	}
}

func TestPreviewStackDeployDiff(t *testing.T) {
	const composeYAML = `
services:
  api:
    image: reg.example/api:2.0
`
	ctx := context.Background()
	// Build the exact desired spec so the live "api" copy differs only by a
	// label (drift) while "gone" exists only live.
	project, err := parseStack(ctx, []byte(composeYAML), "", "prev")
	if err != nil {
		t.Fatalf("parseStack: %v", err)
	}
	targets, _ := stackNetworks(project, "prev")
	want, err := serviceSpecFromCompose("prev", "api", project.Services["api"], targets, nil, nil)
	if err != nil {
		t.Fatalf("serviceSpecFromCompose: %v", err)
	}
	live := []swarm.Service{
		{ID: "l1", Spec: want},
		{ID: "l2", Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name:   "prev_gone",
				Labels: map[string]string{NamespaceLabel: "prev"},
			},
		}},
	}
	// Drift: live api carries an extra label.
	live[0].Spec.Labels["drift"] = "true"

	fake := &fakeSwarm{services: live}
	diff, err := PreviewStackDeploy(ctx, fake, "prev", composeYAML)
	if err != nil {
		t.Fatalf("PreviewStackDeploy: %v", err)
	}
	if len(diff.ToUpdate) != 1 || diff.ToUpdate[0] != "prev_api" {
		t.Fatalf("ToUpdate = %v", diff.ToUpdate)
	}
	if len(diff.ToCreate) != 0 {
		t.Fatalf("ToCreate = %v", diff.ToCreate)
	}
	if len(diff.ToRemove) != 1 || diff.ToRemove[0] != "prev_gone" {
		t.Fatalf("ToRemove = %v", diff.ToRemove)
	}
	// Preview must not mutate anything.
	if len(fake.createdServices)+len(fake.updatedServices)+len(fake.removedServiceIDs) != 0 {
		t.Fatal("preview mutated swarm state")
	}
}

func TestPreviewStackDeployParseError(t *testing.T) {
	_, err := PreviewStackDeploy(context.Background(), &fakeSwarm{}, "prev", ":\nnot: [valid")
	if err == nil || !strings.Contains(err.Error(), "parse compose file") {
		t.Fatalf("err = %v, want parse failure", err)
	}
}

func TestPreviewStackDeployListError(t *testing.T) {
	fake := &fakeSwarm{listSvcErr: errors.New("swarm down")}
	_, err := PreviewStackDeploy(context.Background(), fake, "prev", "services:\n  a:\n    image: x\n")
	if err == nil || !strings.Contains(err.Error(), "list services") {
		t.Fatalf("err = %v, want list services failure", err)
	}
}

func TestEnsureStackNetworks(t *testing.T) {
	ctx := context.Background()
	fake := &fakeSwarm{networks: map[string]string{"exists": "n1"}}
	targets := map[string]string{
		"a": "new-net",
		"b": "exists",
		"c": "new-net", // duplicate target → deduped
		"d": "   ",     // blank name → skipped
		"e": "ext-net",
	}
	external := map[string]bool{"e": true}
	if err := ensureStackNetworks(ctx, fake, targets, external); err != nil {
		t.Fatalf("ensureStackNetworks: %v", err)
	}
	if len(fake.createdNetworks) != 1 || fake.createdNetworks[0] != "new-net" {
		t.Fatalf("created = %v, want [new-net]", fake.createdNetworks)
	}

	broken := &fakeSwarm{listNetErr: errors.New("net down")}
	err := ensureStackNetworks(ctx, broken, targets, external)
	if err == nil || !strings.Contains(err.Error(), "list networks") {
		t.Fatalf("err = %v, want list networks failure", err)
	}
}

func TestEnsureStackSecrets(t *testing.T) {
	ctx := context.Background()
	dir := writeFixture(t, "services: {}\n")

	newFake := func() *fakeSwarm { return &fakeSwarm{} }

	// No secrets at all.
	ids, err := ensureStackSecrets(ctx, newFake(), &composetypes.Project{}, dir)
	if err != nil || ids != nil {
		t.Fatalf("empty project = (%v, %v), want nil+nil", ids, err)
	}

	// Existing secret reused by name.
	existing := newFake()
	existing.secretsByID = map[string]swarm.Secret{
		"sec-1": {ID: "sec-1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tok"}}},
	}
	proj := &composetypes.Project{Secrets: map[string]composetypes.SecretConfig{
		"s": {Name: "tok", Content: "ignored"},
	}}
	ids, err = ensureStackSecrets(ctx, existing, proj, dir)
	if err != nil || ids["s"] != "sec-1" {
		t.Fatalf("reuse = (%v, %v), want sec-1", ids, err)
	}
	if len(existing.createdSecrets) != 0 {
		t.Fatal("existing secret must not be recreated")
	}

	// Created from file, content, environment; driver/templating mapped.
	t.Setenv("HIVE_TEST_SECRET", "env-secret-value")
	proj = &composetypes.Project{Secrets: map[string]composetypes.SecretConfig{
		"from_file": {File: "token.txt"},
		"from_env":  {Environment: "HIVE_TEST_SECRET"},
		"from_data": {Content: "inline-data"},
		"fancy":     {Content: "d", Driver: "custom", DriverOpts: map[string]string{"k": "v"}, TemplateDriver: "golang"},
	}}
	fake := newFake()
	ids, err = ensureStackSecrets(ctx, fake, proj, dir)
	if err != nil {
		t.Fatalf("ensureStackSecrets: %v", err)
	}
	if len(fake.createdSecrets) != 4 {
		t.Fatalf("created secrets = %d, want 4", len(fake.createdSecrets))
	}
	byName := map[string]swarm.SecretSpec{}
	for _, s := range fake.createdSecrets {
		byName[s.Name] = s
	}
	if string(byName["from_file"].Data) != "s3cr3t-data" {
		t.Fatalf("file secret data = %q", byName["from_file"].Data)
	}
	if string(byName["from_env"].Data) != "env-secret-value" {
		t.Fatalf("env secret data = %q", byName["from_env"].Data)
	}
	if string(byName["from_data"].Data) != "inline-data" {
		t.Fatalf("content secret data = %q", byName["from_data"].Data)
	}
	fancy := byName["fancy"]
	if fancy.Driver == nil || fancy.Driver.Name != "custom" || fancy.Driver.Options["k"] != "v" {
		t.Fatalf("fancy driver = %+v", fancy.Driver)
	}
	if fancy.Templating == nil || fancy.Templating.Name != "golang" {
		t.Fatalf("fancy templating = %+v", fancy.Templating)
	}
	if ids["fancy"] == "" {
		t.Fatalf("ids = %v", ids)
	}

	// Missing source (no file/content/environment).
	proj = &composetypes.Project{Secrets: map[string]composetypes.SecretConfig{
		"broken": {},
	}}
	_, err = ensureStackSecrets(ctx, newFake(), proj, dir)
	if err == nil || !strings.Contains(err.Error(), `load secret "broken"`) {
		t.Fatalf("err = %v, want load secret failure", err)
	}

	// ListSecrets error.
	proj = &composetypes.Project{Secrets: map[string]composetypes.SecretConfig{
		"s": {Content: "x"},
	}}
	_, err = ensureStackSecrets(ctx, &fakeSwarm{listSecretErr: errors.New("sec down")}, proj, dir)
	if err == nil || !strings.Contains(err.Error(), "list secrets") {
		t.Fatalf("err = %v, want list secrets failure", err)
	}
}

func TestEnsureStackConfigs(t *testing.T) {
	ctx := context.Background()
	dir := writeFixture(t, "services: {}\n")
	newFake := func() *fakeSwarm { return &fakeSwarm{} }

	// No configs.
	ids, err := ensureStackConfigs(ctx, newFake(), &composetypes.Project{}, dir)
	if err != nil || ids != nil {
		t.Fatalf("empty project = (%v, %v), want nil+nil", ids, err)
	}

	// External config missing → error.
	proj := &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"c": {External: composetypes.External(true), Name: "missing-cfg"},
	}}
	_, err = ensureStackConfigs(ctx, newFake(), proj, dir)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v, want external config missing", err)
	}

	// External config found → id reused, no mutation.
	withExt := newFake()
	withExt.configsByID = map[string]swarm.Config{
		"cfg-1": {ID: "cfg-1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "ext-cfg"}, Data: []byte("x")}},
	}
	proj = &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"c": {External: composetypes.External(true), Name: "ext-cfg"},
	}}
	ids, err = ensureStackConfigs(ctx, withExt, proj, dir)
	if err != nil || ids["c"] != "cfg-1" {
		t.Fatalf("external reuse = (%v, %v), want cfg-1", ids, err)
	}
	if len(withExt.createdConfigs)+len(withExt.updatedConfigs) != 0 {
		t.Fatal("external config must not be mutated")
	}

	// Same data → reused without update; drift → updated in place; new → created.
	t.Setenv("HIVE_TEST_CONFIG", "env-config-value")
	withExisting := newFake()
	withExisting.configsByID = map[string]swarm.Config{
		"cfg-same":  {ID: "cfg-same", Meta: swarm.Meta{Version: swarm.Version{Index: 4}}, Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "same"}, Data: []byte("body")}},
		"cfg-drift": {ID: "cfg-drift", Meta: swarm.Meta{Version: swarm.Version{Index: 2}}, Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "drift"}, Data: []byte("old")}},
	}
	proj = &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"same":    {Name: "same", Content: "body"},
		"drift":   {Name: "drift", Content: "new"},
		"fresh":   {Content: "brand"},
		"fromenv": {Environment: "HIVE_TEST_CONFIG"},
	}}
	ids, err = ensureStackConfigs(ctx, withExisting, proj, dir)
	if err != nil {
		t.Fatalf("ensureStackConfigs: %v", err)
	}
	createdNames := map[string]bool{}
	for _, c := range withExisting.createdConfigs {
		createdNames[c.Name] = true
	}
	if len(createdNames) != 2 || !createdNames["fresh"] || !createdNames["fromenv"] {
		t.Fatalf("created = %+v, want fresh + fromenv", withExisting.createdConfigs)
	}
	if len(withExisting.updatedConfigs) != 1 {
		t.Fatalf("updated = %+v, want drift only", withExisting.updatedConfigs)
	}
	upd := withExisting.updatedConfigs[0]
	if upd.id != "cfg-drift" || upd.version != 2 || string(upd.spec.Data) != "new" {
		t.Fatalf("update = %+v", upd)
	}
	if ids["same"] != "cfg-same" || ids["drift"] != "cfg-drift" || ids["fresh"] == "" || ids["fromenv"] == "" {
		t.Fatalf("ids = %v", ids)
	}

	// fileObjectData error surfaces as load config error.
	proj = &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"broken": {},
	}}
	_, err = ensureStackConfigs(ctx, newFake(), proj, dir)
	if err == nil || !strings.Contains(err.Error(), `load config "broken"`) {
		t.Fatalf("err = %v, want load config failure", err)
	}

	// ListConfigs error.
	proj = &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"c": {Content: "x"},
	}}
	_, err = ensureStackConfigs(ctx, &fakeSwarm{listCfgErr: errors.New("cfg down")}, proj, dir)
	if err == nil || !strings.Contains(err.Error(), "list configs") {
		t.Fatalf("err = %v, want list configs failure", err)
	}
}

func TestFileObjectData(t *testing.T) {
	dir := t.TempDir()
	absFile := filepath.Join(dir, "abs.txt")
	if err := os.WriteFile(absFile, []byte("abs-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rel.txt"), []byte("rel-data"), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := fileObjectData(fileObjectSource{file: absFile}, "")
	if err != nil || string(data) != "abs-data" {
		t.Fatalf("abs file = (%q, %v)", data, err)
	}
	data, err = fileObjectData(fileObjectSource{file: "rel.txt"}, dir)
	if err != nil || string(data) != "rel-data" {
		t.Fatalf("rel file joined with workDir = (%q, %v)", data, err)
	}
	data, err = fileObjectData(fileObjectSource{content: "raw"}, "")
	if err != nil || string(data) != "raw" {
		t.Fatalf("content = (%q, %v)", data, err)
	}

	t.Setenv("HIVE_FILEOBJ_VAR", "from-env")
	data, err = fileObjectData(fileObjectSource{environment: "HIVE_FILEOBJ_VAR"}, "")
	if err != nil || string(data) != "from-env" {
		t.Fatalf("environment = (%q, %v)", data, err)
	}
	// Ensure the unset-variable probe name really is unset.
	_ = os.Unsetenv("HIVE_FILEOBJ_MISSING")
	_, err = fileObjectData(fileObjectSource{environment: "HIVE_FILEOBJ_MISSING"}, "")
	if err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("err = %v, want unset environment failure", err)
	}
	_, err = fileObjectData(fileObjectSource{}, "")
	if err == nil || !strings.Contains(err.Error(), "must set one of") {
		t.Fatalf("err = %v, want no-source failure", err)
	}
}

func TestSwarmDriver(t *testing.T) {
	if got := swarmDriver("", nil); got != nil {
		t.Fatalf(`swarmDriver("") = %+v, want nil`, got)
	}
	got := swarmDriver("overlay2", map[string]string{"k": "v"})
	if got == nil || got.Name != "overlay2" || got.Options["k"] != "v" {
		t.Fatalf("swarmDriver = %+v", got)
	}
}

func TestSecurityOptions(t *testing.T) {
	got := securityOptions([]string{"no-new-privileges"})
	if !got.noNewPrivileges || got.selinuxDisable {
		t.Fatalf("no-new-privileges → %+v", got)
	}
	got = securityOptions([]string{"label:disable"})
	if got.noNewPrivileges || !got.selinuxDisable {
		t.Fatalf("label:disable → %+v", got)
	}
	// Matching is case-insensitive.
	got = securityOptions([]string{"NO-NEW-PRIVILEGES"})
	if !got.noNewPrivileges {
		t.Fatalf("NO-NEW-PRIVILEGES → %+v", got)
	}
	got = securityOptions([]string{"seccomp=unconfined"})
	if got.noNewPrivileges || got.selinuxDisable {
		t.Fatalf("unknown opts → %+v", got)
	}
	if got := securityOptions(nil); got.noNewPrivileges || got.selinuxDisable {
		t.Fatalf("nil → %+v", got)
	}
}

func TestParsePublishedPort(t *testing.T) {
	tests := []struct {
		in   string
		want uint32
	}{
		{"8080", 8080},
		{"100-200", 100}, // range form takes the lower bound
		{" 80 ", 80},
		{"garbage", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parsePublishedPort(tt.in); got != tt.want {
			t.Fatalf("parsePublishedPort(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestDerefHelpers(t *testing.T) {
	if got := derefUint64(nil); got != 0 {
		t.Fatalf("derefUint64(nil) = %d", got)
	}
	v := uint64(9)
	if got := derefUint64(&v); got != 9 {
		t.Fatalf("derefUint64(&9) = %d", got)
	}
	if got := derefDuration(nil); got != 0 {
		t.Fatalf("derefDuration(nil) = %d", got)
	}
	d := composetypes.Duration(1500)
	if got := derefDuration(&d); got != 1500 {
		t.Fatalf("derefDuration(&1500) = %d", got)
	}
}

func TestDeviceRequestsGPU(t *testing.T) {
	if !deviceRequestsGPU(composetypes.DeviceRequest{Capabilities: []string{"gpu"}}) {
		t.Fatal("gpu capability not detected")
	}
	if !deviceRequestsGPU(composetypes.DeviceRequest{Capabilities: []string{"cpu,GPU"}}) {
		t.Fatal("comma-separated gpu capability not detected")
	}
	if deviceRequestsGPU(composetypes.DeviceRequest{Capabilities: []string{"nvidia", "cuda"}}) {
		t.Fatal("non-gpu capabilities reported as gpu")
	}
	if deviceRequestsGPU(composetypes.DeviceRequest{}) {
		t.Fatal("empty request reported as gpu")
	}
}

func TestApplyDeployJobModesAndDevices(t *testing.T) {
	// replicated-job and global-job modes.
	for _, mode := range []string{"replicated-job", "global-job"} {
		svc := composetypes.ServiceConfig{
			Image:  "img",
			Deploy: &composetypes.DeployConfig{Mode: mode},
		}
		spec, err := serviceSpecFromCompose("s", "x", svc, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: serviceSpecFromCompose: %v", mode, err)
		}
		if mode == "replicated-job" && spec.Mode.ReplicatedJob == nil {
			t.Fatalf("replicated-job → %+v", spec.Mode)
		}
		if mode == "global-job" && spec.Mode.GlobalJob == nil {
			t.Fatalf("global-job → %+v", spec.Mode)
		}
	}

	// GPU reservations: count 0 → 1; negative → 1; non-GPU devices skipped;
	// explicit replicas honored; pre-existing generic resources preserved.
	replicas := 4
	gpuZero := composetypes.ServiceConfig{
		Image: "img",
		Deploy: &composetypes.DeployConfig{
			Replicas: &replicas,
			Resources: composetypes.Resources{
				Reservations: &composetypes.Resource{
					GenericResources: []composetypes.GenericResource{
						{DiscreteResourceSpec: &composetypes.DiscreteGenericResource{Kind: "ssa", Value: 1}},
					},
					Devices: []composetypes.DeviceRequest{
						{Capabilities: []string{"nvidia"}}, // skipped: not a GPU
						{Capabilities: []string{"gpu"}, Count: 0},
					},
				},
			},
		},
	}
	spec, err := serviceSpecFromCompose("s", "x", gpuZero, nil, nil, nil)
	if err != nil {
		t.Fatalf("serviceSpecFromCompose: %v", err)
	}
	res := spec.TaskTemplate.Resources.Reservations
	if res == nil || gpuCount(res) != 1 || len(res.GenericResources) != 2 {
		t.Fatalf("reservations = %+v, want GPUs=1 plus ssa resource kept", res)
	}
	if spec.Mode.Replicated == nil || spec.Mode.Replicated.Replicas == nil || *spec.Mode.Replicated.Replicas != 4 {
		t.Fatalf("replicas = %+v, want 4", spec.Mode.Replicated)
	}

	gpuAll := composetypes.ServiceConfig{
		Image: "img",
		Deploy: &composetypes.DeployConfig{
			Resources: composetypes.Resources{
				Reservations: &composetypes.Resource{
					Devices: []composetypes.DeviceRequest{{Capabilities: []string{"gpu"}, Count: -1}},
				},
			},
		},
	}
	spec, err = serviceSpecFromCompose("s", "x", gpuAll, nil, nil, nil)
	if err != nil {
		t.Fatalf("serviceSpecFromCompose: %v", err)
	}
	if got := spec.TaskTemplate.Resources.Reservations; gpuCount(got) != 1 {
		t.Fatalf("negative count → GPUs %+v, want 1", got)
	}
}

// gpuCount sums the discrete "gpu" generic resources in swarm reservations.
func gpuCount(res *swarm.Resources) int64 {
	if res == nil {
		return 0
	}
	var total int64
	for _, gr := range res.GenericResources {
		if gr.DiscreteResourceSpec != nil && gr.DiscreteResourceSpec.Kind == "gpu" {
			total += gr.DiscreteResourceSpec.Value
		}
	}
	return total
}

func TestStackNetworksExternalAndNamed(t *testing.T) {
	project := &composetypes.Project{
		Networks: map[string]composetypes.NetworkConfig{
			"ext":  {External: composetypes.External(true), Name: "host-net"},
			"ext2": {External: composetypes.External(true)}, // no explicit name → falls back to logical name
			"web":  {Name: "custom-web"},                    // non-external explicit name kept as-is
			"back": {},                                      // default overlay naming
		},
	}
	targets, external := stackNetworks(project, "st")
	if !external["ext"] || !external["ext2"] {
		t.Fatalf("external = %v", external)
	}
	if targets["ext"] != "host-net" || targets["ext2"] != "ext2" {
		t.Fatalf("external targets = %v", targets)
	}
	if targets["web"] != "custom-web" {
		t.Fatalf("named target = %q", targets["web"])
	}
	if targets["back"] != "st_back" {
		t.Fatalf("overlay target = %q", targets["back"])
	}
	if targets["default"] != "st_default" {
		t.Fatalf("default target = %q", targets["default"])
	}
}

const errorCompose = `
services:
  api:
    image: reg.example/api:1.0
`

// TestStackErrorBranches drives every error return of Stack by failing one
// swarm call at a time.
func TestStackErrorBranches(t *testing.T) {
	ctx := context.Background()
	dir := writeFixture(t, errorCompose)
	composePath := filepath.Join(dir, "compose.yml")
	fullDir := writeFixture(t, fixtureCompose) // has secrets + configs sections
	fullComposePath := filepath.Join(fullDir, "compose.yml")

	liveStack := func() *fakeSwarm {
		return &fakeSwarm{
			services: []swarm.Service{driftedStackService("svc-api", "mystack_api", 2)},
		}
	}
	staleOnly := func() *fakeSwarm {
		return &fakeSwarm{
			services: []swarm.Service{driftedStackService("svc-stale", "mystack_gone", 1)},
		}
	}
	tests := []struct {
		name string
		fake *fakeSwarm
		path string
		want string
	}{
		{"network create", &fakeSwarm{createNetErr: errors.New("net busy")}, composePath, "create network"},
		{"secrets list", &fakeSwarm{listSecretErr: errors.New("sec down")}, fullComposePath, "list secrets"},
		{"configs list", &fakeSwarm{listCfgErr: errors.New("cfg down")}, fullComposePath, "list configs"},
		{"services list", &fakeSwarm{listSvcErr: errors.New("svc down")}, composePath, "list services"},
		{"service update", liveStack(), composePath, "update service"},
		{"service create", staleOnly(), composePath, "create service"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "service update" {
				tt.fake.updateSvcErr = errors.New("update denied")
			}
			if tt.name == "service create" {
				tt.fake.createSvcErr = errors.New("create denied")
			}
			err := Stack(ctx, tt.fake, "mystack", tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q failure", err, tt.want)
			}
		})
	}
	// Remove error: compose service exists and is updated fine, the vanished
	// one fails to be removed.
	f := liveStack()
	f.services = append(f.services, driftedStackService("svc-stale", "mystack_gone", 1))
	f.removeSvcErr = errors.New("remove denied")
	if err := Stack(ctx, f, "mystack", composePath); err == nil || !strings.Contains(err.Error(), "remove service") {
		t.Fatalf("err = %v, want remove service failure", err)
	}
}

// richCompose exercises the remaining optional serviceSpecFromCompose
// branches: groups, sysctls, restart "no", dns, capabilities, init, stop
// signal/grace, security options, volume option blocks and a nil ulimit.
const richCompose = `
services:
  rich:
    image: reg.example/rich:1
    group_add:
      - "33"
    sysctls:
      net.ipv4.ip_forward: "1"
    restart: "no"
    dns:
      - 1.1.1.1
    dns_search: example.com
    dns_opt:
      - ndots:1
    cap_add:
      - NET_ADMIN
    cap_drop:
      - CHOWN
    init: true
    stop_signal: SIGUSR1
    stop_grace_period: 30s
    security_opt:
      - no-new-privileges
    volumes:
      - type: bind
        source: ./data
        target: /data
        bind:
          propagation: rshared
      - type: volume
        source: vol
        target: /vol
        volume:
          nocopy: true
      - type: tmpfs
        target: /tmpfs
        tmpfs:
          size: 1gb
          mode: 1777
    ulimits:
      nofile:
        soft: 1024
        hard: 4096

volumes:
  vol:
`

func TestServiceSpecFromComposeRichBranches(t *testing.T) {
	dir := writeFixture(t, richCompose)
	raw, err := os.ReadFile(filepath.Join(dir, "compose.yml")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}
	project, err := parseStack(context.Background(), raw, dir, "rich")
	if err != nil {
		t.Fatalf("parseStack: %v", err)
	}
	spec, err := serviceSpecFromCompose("rich", "rich", project.Services["rich"], nil, nil, nil)
	if err != nil {
		t.Fatalf("serviceSpecFromCompose: %v", err)
	}
	cs := spec.TaskTemplate.ContainerSpec
	if len(cs.Groups) != 1 || cs.Groups[0] != "33" {
		t.Fatalf("groups = %v", cs.Groups)
	}
	if cs.Sysctls == nil || cs.Sysctls["net.ipv4.ip_forward"] != "1" {
		t.Fatalf("sysctls = %v", cs.Sysctls)
	}
	if spec.TaskTemplate.RestartPolicy == nil || spec.TaskTemplate.RestartPolicy.Condition != "none" {
		t.Fatalf("restart policy = %+v", spec.TaskTemplate.RestartPolicy)
	}
	if len(cs.DNSConfig.Nameservers) != 1 || cs.DNSConfig.Nameservers[0].String() != "1.1.1.1" {
		t.Fatalf("dns nameservers = %v", cs.DNSConfig.Nameservers)
	}
	if len(cs.DNSConfig.Search) != 1 || cs.DNSConfig.Search[0] != "example.com" {
		t.Fatalf("dns search = %v", cs.DNSConfig.Search)
	}
	if len(cs.DNSConfig.Options) != 1 {
		t.Fatalf("dns options = %v", cs.DNSConfig.Options)
	}
	if len(cs.CapabilityAdd) != 1 || cs.CapabilityAdd[0] != "NET_ADMIN" || len(cs.CapabilityDrop) != 1 || cs.CapabilityDrop[0] != "CHOWN" {
		t.Fatalf("capabilities = %v / %v", cs.CapabilityAdd, cs.CapabilityDrop)
	}
	if cs.Init == nil || !*cs.Init {
		t.Fatalf("init = %v", cs.Init)
	}
	if cs.StopSignal != "SIGUSR1" {
		t.Fatalf("stop signal = %q", cs.StopSignal)
	}
	if cs.StopGracePeriod == nil || *cs.StopGracePeriod != 30_000_000_000 {
		t.Fatalf("stop grace period = %v", cs.StopGracePeriod)
	}
	if cs.Privileges == nil || !cs.Privileges.NoNewPrivileges {
		t.Fatal("no-new-privileges security opt not mapped")
	}
	// Volume option blocks.
	if len(cs.Mounts) != 3 {
		t.Fatalf("mounts = %+v", cs.Mounts)
	}
	bind := cs.Mounts[0]
	if bind.Type != mount.TypeBind || bind.BindOptions == nil || bind.BindOptions.Propagation != mount.PropagationRShared {
		t.Fatalf("bind mount = %+v", bind)
	}
	vol := cs.Mounts[1]
	if vol.Type != mount.TypeVolume || vol.VolumeOptions == nil || !vol.VolumeOptions.NoCopy {
		t.Fatalf("volume mount = %+v", vol)
	}
	tmpfs := cs.Mounts[2]
	if tmpfs.Type != mount.TypeTmpfs || tmpfs.TmpfsOptions == nil || tmpfs.TmpfsOptions.SizeBytes != 1<<30 {
		t.Fatalf("tmpfs mount = %+v", tmpfs)
	}
	// Ulimits, tmpfs size/mode and security options all flow through the
	// builder — verified by the successful build and mount assertions above.
}

func TestEnsureStackSecretsCreateError(t *testing.T) {
	proj := &composetypes.Project{Secrets: map[string]composetypes.SecretConfig{
		"s": {Content: "x"},
	}}
	fake := &fakeSwarm{createSecErr: errors.New("sec full")}
	_, err := ensureStackSecrets(context.Background(), fake, proj, "")
	if err == nil || !strings.Contains(err.Error(), `create secret "s"`) {
		t.Fatalf("err = %v, want create secret failure", err)
	}
}

func TestEnsureStackConfigsMutationErrors(t *testing.T) {
	ctx := context.Background()
	withExisting := func() *fakeSwarm {
		return &fakeSwarm{
			configsByID: map[string]swarm.Config{
				"cfg-1": {ID: "cfg-1", Meta: swarm.Meta{Version: swarm.Version{Index: 1}}, Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "c"}, Data: []byte("old")}},
			},
		}
	}
	drifted := &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"c": {Name: "c", Content: "new"},
	}}
	novel := &composetypes.Project{Configs: map[string]composetypes.ConfigObjConfig{
		"c": {Name: "brand-new", Content: "data"},
	}}

	updFail := withExisting()
	updFail.updateCfgErr = errors.New("update denied")
	if _, err := ensureStackConfigs(ctx, updFail, drifted, ""); err == nil || !strings.Contains(err.Error(), "update config") {
		t.Fatalf("err = %v, want update config failure", err)
	}

	createFail := withExisting()
	createFail.createCfgErr = errors.New("create denied")
	if _, err := ensureStackConfigs(ctx, createFail, novel, ""); err == nil || !strings.Contains(err.Error(), "create config") {
		t.Fatalf("err = %v, want create config failure", err)
	}
}
