package tunnels

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/cloudflare"
)

// fakeRepo is an in-memory Repository recording mutations.
type fakeRepo struct {
	rows        map[string]*Row
	byName      map[string]string
	nextID      int
	dnsUpdates  []map[string]string
	statusCalls [][3]string
	forgetNames [][]string
	createErr   error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[string]*Row{}, byName: map[string]string{}}
}

func (f *fakeRepo) Create(_ context.Context, row *Row) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.nextID++
	row.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", f.nextID)
	cp := *row
	f.rows[row.ID] = &cp
	f.byName[row.Name] = row.ID
	return nil
}

func (f *fakeRepo) Get(_ context.Context, id string) (*Row, error) {
	row, ok := f.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *row
	return &cp, nil
}

func (f *fakeRepo) GetByName(_ context.Context, name string) (*Row, error) {
	id, ok := f.byName[name]
	if !ok {
		return nil, ErrNotFound
	}
	return f.Get(context.Background(), id)
}

func (f *fakeRepo) List(_ context.Context) ([]*Row, error) {
	out := []*Row{}
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRepo) UpdateIngress(_ context.Context, id string, rules []IngressRule) error {
	row, ok := f.rows[id]
	if !ok {
		return ErrNotFound
	}
	row.Ingress = rules
	return nil
}

func (f *fakeRepo) UpdateDNSRecords(_ context.Context, id string, records map[string]string) error {
	row, ok := f.rows[id]
	if !ok {
		return ErrNotFound
	}
	f.dnsUpdates = append(f.dnsUpdates, records)
	row.DNSRecords = records
	return nil
}

func (f *fakeRepo) SetStatus(_ context.Context, id, status, errorMessage string) error {
	row, ok := f.rows[id]
	if !ok {
		return ErrNotFound
	}
	f.statusCalls = append(f.statusCalls, [3]string{id, status, errorMessage})
	row.Status = status
	row.ErrorMessage = errorMessage
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, id string) error {
	row, ok := f.rows[id]
	if !ok {
		return ErrNotFound
	}
	delete(f.rows, id)
	delete(f.byName, row.Name)
	return nil
}

func (f *fakeRepo) ForgetSecrets(_ context.Context, names []string) error {
	f.forgetNames = append(f.forgetNames, names)
	return nil
}

// fakeStore is an in-memory CredentialStore recording calls.
type fakeStore struct {
	puts    map[string][]byte
	putKeys []string
	gets    []string
	getErr  error
}

func newFakeStore() *fakeStore { return &fakeStore{puts: map[string][]byte{}} }

func (f *fakeStore) Put(_ context.Context, name, _ string, plain []byte) error {
	if f.puts == nil {
		f.puts = map[string][]byte{}
	}
	f.putKeys = append(f.putKeys, name)
	f.puts[name] = plain
	return nil
}

func (f *fakeStore) Get(_ context.Context, name, _ string) ([]byte, error) {
	f.gets = append(f.gets, name)
	if f.getErr != nil {
		return nil, f.getErr
	}
	raw, ok := f.puts[name]
	if !ok {
		return nil, errors.New("not found")
	}
	return raw, nil
}

// fakeCF records Cloudflare calls.
type fakeCF struct {
	tunnelID     string
	tunnels      map[string]bool
	createdNames []string
	dnsRecords   map[string]string // hostname -> recordID
	nextRecord   int
	deleteErr    error
	getStatus    string
}

func newFakeCF() *fakeCF {
	return &fakeCF{
		tunnels:    map[string]bool{},
		dnsRecords: map[string]string{},
	}
}

func (f *fakeCF) CreateTunnel(_ context.Context, _, name string) (cloudflare.TunnelRef, error) {
	f.createdNames = append(f.createdNames, name)
	id := fmt.Sprintf("cf-%d", len(f.createdNames))
	f.tunnels[id] = true
	return cloudflare.TunnelRef{
		ID:              id,
		Token:           "conn-token",
		CredentialsJSON: []byte(fmt.Sprintf(`{"a":"acc","t":%q,"s":"secret"}`, id)),
	}, nil
}

func (f *fakeCF) DeleteTunnel(_ context.Context, _, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.tunnels, id)
	return nil
}

func (f *fakeCF) GetTunnel(_ context.Context, _, _ string) (string, error) {
	return f.getStatus, nil
}

func (f *fakeCF) CreateDNSRoute(_ context.Context, _, hostname, tunnelID string) (string, error) {
	f.nextRecord++
	recordID := fmt.Sprintf("%s-rec%d", tunnelID, f.nextRecord)
	f.dnsRecords[hostname] = recordID
	return recordID, nil
}

func (f *fakeCF) DeleteDNSRecord(_ context.Context, _, recordID string) error {
	for host, rid := range f.dnsRecords {
		if rid == recordID {
			delete(f.dnsRecords, host)
		}
	}
	return nil
}

// fakeSwarm is an in-memory SwarmAPI double.
type fakeSwarm struct {
	services         map[string]swarm.Service
	secrets          map[string]swarm.Secret
	tasks            []swarm.Task
	nextID           int
	createdSpecs     []swarm.ServiceSpec
	updatedServices  [][3]any // id, version, spec name+labels
	removedServices  []string
	removedSecretIDs []string
}

func newFakeSwarm() *fakeSwarm {
	return &fakeSwarm{services: map[string]swarm.Service{}, secrets: map[string]swarm.Secret{}}
}

func (f *fakeSwarm) next(name string) string {
	f.nextID++
	return fmt.Sprintf("%s-id%d", name, f.nextID)
}

func (f *fakeSwarm) CreateService(_ context.Context, spec swarm.ServiceSpec) (string, error) {
	id := f.next(spec.Name) //nolint:staticcheck // test fixture
	// The spec keeps its requested name; only the service ID is minted.
	svc := swarm.Service{ID: id, Spec: spec}
	f.services[id] = svc
	f.createdSpecs = append(f.createdSpecs, spec)
	return id, nil
}

func (f *fakeSwarm) GetService(_ context.Context, idOrName string) (swarm.Service, error) {
	for _, svc := range f.services {
		if svc.ID == idOrName || svc.Spec.Name == idOrName { //nolint:staticcheck // test fixture
			return svc, nil
		}
	}
	return swarm.Service{}, cerrdefs.ErrNotFound
}

func (f *fakeSwarm) UpdateService(_ context.Context, id string, version uint64, spec swarm.ServiceSpec) error {
	svc, ok := f.services[id]
	if !ok {
		return cerrdefs.ErrNotFound
	}
	svc.Version.Index = version + 1
	svc.Spec = spec
	f.services[id] = svc
	f.updatedServices = append(f.updatedServices, [3]any{id, version, spec})
	return nil
}

func (f *fakeSwarm) RemoveService(_ context.Context, idOrName string) error {
	for id, svc := range f.services {
		if id == idOrName || svc.Spec.Name == idOrName {
			f.removedServices = append(f.removedServices, id)
			delete(f.services, id)
			return nil
		}
	}
	f.removedServices = append(f.removedServices, idOrName)
	return nil
}

func (f *fakeSwarm) ListTasks(_ context.Context, serviceID string) ([]swarm.Task, error) {
	var out []swarm.Task
	for _, t := range f.tasks {
		if t.ServiceID == serviceID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeSwarm) CreateSecret(_ context.Context, spec swarm.SecretSpec) (string, error) {
	id := f.next(spec.Name)
	f.secrets[id] = swarm.Secret{ID: id, Spec: spec}
	return id, nil
}

func (f *fakeSwarm) RemoveSecret(_ context.Context, idOrName string) error {
	for id, sec := range f.secrets {
		if id == idOrName || sec.Spec.Name == idOrName {
			f.removedSecretIDs = append(f.removedSecretIDs, id)
			delete(f.secrets, id)
			return nil
		}
	}
	return nil
}

func (f *fakeSwarm) ListSecrets(_ context.Context) ([]swarm.Secret, error) {
	out := []swarm.Secret{}
	for _, s := range f.secrets {
		out = append(out, s)
	}
	return out, nil
}

// newTestManager wires a Manager over the doubles.
func newTestManager() (*Manager, *fakeRepo, *fakeSwarm, *fakeStore, *fakeCF) {
	repo := newFakeRepo()
	swarmer := newFakeSwarm()
	store := newFakeStore()
	cf := newFakeCF()
	cf.getStatus = "healthy"
	mgr := &Manager{
		Repo:      repo,
		Swarm:     swarmer,
		Store:     store,
		NewClient: func(string) cloudflare.API { return cf },
	}
	return mgr, repo, swarmer, store, cf
}

var sampleRules = []IngressRule{
	{Hostname: "app.example.com", Service: "http://traefik:80"},
	{Hostname: "*.example.com", Path: "/api", Service: "https://backend.internal:8443"},
}

func validParams() CreateParams {
	return CreateParams{
		Name:      "prod-edge",
		AccountID: "acc-1",
		ZoneID:    "zone-1",
		APIToken:  "tok",
		Ingress: []IngressRule{
			{Hostname: "app.example.com", Service: "http://traefik:80"},
			{Hostname: "*.example.com", Path: "/api", Service: "https://backend.internal:8443"},
		},
	}
}

func TestManagerCreateHappyPath(t *testing.T) {
	mgr, _, swarmer, store, cf := newTestManager()

	view, err := mgr.Create(context.Background(), validParams())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Cloudflare tunnel created under the request name.
	if len(cf.createdNames) != 1 || cf.createdNames[0] != "prod-edge" {
		t.Errorf("created tunnels = %v, want [prod-edge]", cf.createdNames)
	}

	// Credentials JSON and API token stored encrypted under stable keys.
	cfID := view.Row.CfTunnelID
	if got := store.puts["tunnel:"+cfID]; !strings.Contains(string(got), `"s":"secret"`) {
		t.Errorf("stored credentials = %q", got)
	}
	if got := store.puts["tunnel-api-token:"+cfID]; string(got) != "tok" {
		t.Errorf("stored api token = %q", got)
	}
	if len(store.putKeys) != 2 {
		t.Errorf("put calls = %v", store.putKeys)
	}

	// Row persisted deployed.
	if view.Row.Status != StatusDeployed {
		t.Errorf("status = %q, want deployed", view.Row.Status)
	}

	// Connector service exists with correct shape.
	svcName := "hive_tunnel_prod-edge"
	svc, err := swarmer.GetService(context.Background(), svcName)
	if err != nil {
		t.Fatalf("connector service missing: %v", err)
	}
	labels := svc.Spec.Labels
	if labels[LabelConfigRevision] != "1" {
		t.Errorf("revision label = %v", labels)
	}
	args := svc.Spec.TaskTemplate.ContainerSpec.Args
	if len(args) < 2 || args[0] != "tunnel" || args[1] != "--config" {
		t.Errorf("container args = %v", args)
	}
	if svc.Spec.Mode.Replicated == nil || svc.Spec.Mode.Replicated.Replicas == nil || *svc.Spec.Mode.Replicated.Replicas != 1 {
		t.Errorf("replicas = %+v", svc.Spec.Mode.Replicated)
	}
	nets := svc.Spec.TaskTemplate.Networks
	if len(nets) != 1 || nets[0].Target != "hive_internal" {
		t.Errorf("networks = %+v", nets)
	}
}

func TestManagerRenderedConfigOrderAndCatchAll(t *testing.T) {
	mgr, _, swarmer, _, _ := newTestManager()
	if _, err := mgr.Create(context.Background(), validParams()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc, err := swarmer.GetService(context.Background(), "hive_tunnel_prod-edge")
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	var configData string
	for _, ref := range svc.Spec.TaskTemplate.ContainerSpec.Secrets {
		sec, ok := swarmer.secrets[ref.SecretID]
		if ok && strings.Contains(sec.Spec.Name, "-config-r") {
			configData = string(sec.Spec.Data)
		}
	}
	wantOrder := []string{
		`tunnel: `,
		`credentials-file: /run/secrets/hive-tunnel-prod-edge-cred`,
		`[[ingress]]`,
		`hostname = "app.example.com"`,
		`service = "http://traefik:80"`,
		`[[ingress]]`,
		`hostname = "*.example.com"`,
		`path = "/api"`,
		`service = "https://backend.internal:8443"`,
		`[[ingress]]`,
		`service = "http_status:404"`,
	}
	offset := 0
	for _, want := range wantOrder {
		idx := strings.Index(configData[offset:], want)
		if idx < 0 {
			t.Fatalf("config missing or out of order %q:\n%s", want, configData)
		}
		offset += idx + len(want)
	}
}

func TestManagerPublishesDNSAndTracksRecords(t *testing.T) {
	mgr, repo, _, _, cf := newTestManager()
	view, err := mgr.Create(context.Background(), validParams())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(cf.dnsRecords) != 2 {
		t.Fatalf("dns records = %v", cf.dnsRecords)
	}
	if cf.dnsRecords["app.example.com"] == "" || cf.dnsRecords["*.example.com"] == "" {
		t.Errorf("wildcard/exact routes not both published: %v", cf.dnsRecords)
	}
	stored, err := repo.Get(context.Background(), view.Row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.DNSRecords["app.example.com"] != cf.dnsRecords["app.example.com"] {
		t.Errorf("tracked records = %v, cloudflare = %v", stored.DNSRecords, cf.dnsRecords)
	}
}

func TestManagerCreateValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateParams)
	}{
		{"bad name", func(p *CreateParams) { p.Name = "Prod_Edge" }},
		{"empty ingress", func(p *CreateParams) { p.Ingress = nil }},
		{"bad hostname", func(p *CreateParams) { p.Ingress[0].Hostname = "app..example.com" }},
		{"bad service", func(p *CreateParams) { p.Ingress[0].Service = "ftp://x" }},
		{"missing account", func(p *CreateParams) { p.AccountID = "" }},
		{"missing token", func(p *CreateParams) { p.APIToken = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr, repo, _, _, cf := newTestManager()
			params := validParams()
			tc.mutate(&params)
			if _, err := mgr.Create(context.Background(), params); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if len(repo.rows) != 0 || len(cf.createdNames) != 0 {
				t.Errorf("validation failure had side effects: rows=%d cf=%d", len(repo.rows), len(cf.createdNames))
			}
		})
	}
}

func TestManagerCreateConflict(t *testing.T) {
	mgr, _, _, _, _ := newTestManager()
	if _, err := mgr.Create(context.Background(), validParams()); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := mgr.Create(context.Background(), validParams()); !errors.Is(err, ErrConflict) {
		t.Fatalf("second create err = %v, want ErrConflict", err)
	}
}

func TestManagerDeployFailureMarksError(t *testing.T) {
	mgr, repo, _, _, _ := newTestManager()
	mgr.Swarm = &failingCreateSwarm{newFakeSwarm()}
	_, err := mgr.Create(context.Background(), validParams())
	if err == nil {
		t.Fatal("expected deploy failure")
	}
	if len(repo.statusCalls) == 0 {
		t.Fatal("no status transitions recorded")
	}
	last := repo.statusCalls[len(repo.statusCalls)-1]
	if last[1] != StatusError || last[2] == "" {
		t.Errorf("status call = %v, want error status with message", last)
	}
}

type failingCreateSwarm struct{ *fakeSwarm }

func (failingCreateSwarm) CreateService(context.Context, swarm.ServiceSpec) (string, error) {
	return "", errors.New("swarm unavailable")
}

func TestManagerDeleteTearDown(t *testing.T) {
	mgr, repo, swarmer, store, cf := newTestManager()
	view, err := mgr.Create(context.Background(), validParams())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	cfID := view.Row.CfTunnelID

	if err := mgr.Delete(context.Background(), view.Row.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if cf.tunnels[cfID] {
		t.Error("cloudflare tunnel still present")
	}
	if len(cf.dnsRecords) != 0 {
		t.Errorf("dns records left behind: %v", cf.dnsRecords)
	}
	if len(swarmer.services) != 0 {
		t.Errorf("services left behind: %v", swarmer.services)
	}
	if len(swarmer.secrets) != 0 {
		t.Errorf("swarm secrets left behind: %v", swarmer.secrets)
	}
	if _, ok := store.puts["tunnel:"+cfID]; !ok {
		// puts map keeps history; the purge is asserted via ForgetSecrets.
		_ = ok
	}
	var forgotten []string
	for _, names := range repo.forgetNames {
		forgotten = append(forgotten, names...)
	}
	sort.Strings(forgotten)
	want := []string{"tunnel:" + cfID, "tunnel-api-token:" + cfID}
	sort.Strings(want)
	if strings.Join(forgotten, ",") != strings.Join(want, ",") {
		t.Errorf("forgotten secrets = %v, want %v", forgotten, want)
	}
	if _, err := repo.Get(context.Background(), view.Row.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("row still present after delete: %v", err)
	}
}

func TestManagerUpdateIngressRotatesRevisionAndDiffDNS(t *testing.T) {
	mgr, _, swarmer, _, cf := newTestManager()
	view, err := mgr.Create(context.Background(), validParams())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newRules := []IngressRule{
		{Hostname: "*.example.com", Service: "http://traefik:80"},
		{Hostname: "admin.example.org", Service: "http://admin:3000"},
	}
	updated, err := mgr.UpdateIngress(context.Background(), view.Row.ID, newRules)
	if err != nil {
		t.Fatalf("UpdateIngress: %v", err)
	}

	// Config revision rotated to 2 and old revision secret pruned.
	var rev int
	for _, s := range swarmer.secrets {
		if strings.Contains(s.Spec.Name, "-config-r") {
			rev++
		}
	}
	if rev != 1 {
		t.Errorf("config revisions remaining = %d, want exactly one", rev)
	}
	svc, err := swarmer.GetService(context.Background(), "hive_tunnel_prod-edge")
	if err != nil {
		t.Fatal(err)
	}
	labels := svc.Spec.Labels
	if labels[LabelConfigRevision] != "2" {
		t.Errorf("revision label = %q, want 2", labels[LabelConfigRevision])
	}
	if len(mgr.Swarm.(*fakeSwarm).updatedServices) != 1 {
		t.Errorf("expected one service update, got %d", len(mgr.Swarm.(*fakeSwarm).updatedServices))
	}

	// DNS diff: app.example.com dropped, admin.example.org added.
	if _, still := cf.dnsRecords["app.example.com"]; still {
		t.Errorf("dropped route still published: %v", cf.dnsRecords)
	}
	if cf.dnsRecords["admin.example.org"] == "" || cf.dnsRecords["*.example.com"] == "" {
		t.Errorf("dns records after update = %v", cf.dnsRecords)
	}
	if updated.Row.Ingress[0].Hostname != "*.example.com" {
		t.Errorf("persisted ingress = %+v", updated.Row.Ingress)
	}
	rendered := RenderConfig(cf.tunnelID, "", updated.Row.Ingress)
	if !strings.Contains(rendered, `hostname = "admin.example.org"`) {
		t.Errorf("rendered config stale:\n%s", rendered)
	}
}

func TestManagerConnectorStatus(t *testing.T) {
	mgr, _, swarmer, _, cf := newTestManager()
	view, err := mgr.Create(context.Background(), validParams())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	svc, err := swarmer.GetService(context.Background(), "hive_tunnel_prod-edge")
	if err != nil {
		t.Fatal(err)
	}
	swarmer.tasks = []swarm.Task{
		{ServiceID: svc.ID, Status: swarm.TaskStatus{State: swarm.TaskStateRunning}},
		{ServiceID: svc.ID, Status: swarm.TaskStatus{State: swarm.TaskStateShutdown}},
	}
	cf.getStatus = "down"

	status, err := mgr.Status(context.Background(), view.Row.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.DesiredReplicas != 1 || status.RunningReplicas != 1 {
		t.Errorf("replica counts = %+v", status)
	}
	if status.CloudflareStatus != "down" {
		t.Errorf("cloudflareStatus = %q, want down", status.CloudflareStatus)
	}
}

func TestManagerListIncludesViews(t *testing.T) {
	mgr, _, _, _, _ := newTestManager()
	if _, err := mgr.Create(context.Background(), validParams()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	views, err := mgr.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 || views[0].Row.Name != "prod-edge" || views[0].Row.Status != StatusDeployed {
		t.Fatalf("views = %+v", views)
	}
}
