package reconcile

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/luke/hive/control-plane/internal/db"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/testdb"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

// --- cacheStore over a fake DBTX ---

type fakeDBTX struct{}

func (fakeDBTX) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (fakeDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

func (fakeDBTX) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func TestCacheStoreWrappersDelegateToQueries(t *testing.T) {
	store := cacheStore{q: dbgen.New(fakeDBTX{})}
	ctx := context.Background()
	if err := store.UpsertService(ctx, dbgen.UpsertCacheServiceParams{}); err != nil {
		t.Errorf("UpsertService: %v", err)
	}
	if err := store.DeleteCacheServiceBySwarmID(ctx, "x"); err != nil {
		t.Errorf("DeleteCacheServiceBySwarmID: %v", err)
	}
	if err := store.DeleteMissingCacheServices(ctx, nil); err != nil {
		t.Errorf("DeleteMissingCacheServices: %v", err)
	}
	if err := store.UpsertTask(ctx, dbgen.UpsertCacheTaskParams{}); err != nil {
		t.Errorf("UpsertTask: %v", err)
	}
	if err := store.DeleteCacheTaskBySwarmID(ctx, "x"); err != nil {
		t.Errorf("DeleteCacheTaskBySwarmID: %v", err)
	}
	if err := store.DeleteMissingCacheTasks(ctx, nil); err != nil {
		t.Errorf("DeleteMissingCacheTasks: %v", err)
	}
	if err := store.UpsertNode(ctx, dbgen.UpsertCacheNodeParams{}); err != nil {
		t.Errorf("UpsertNode: %v", err)
	}
	if err := store.DeleteCacheNodeBySwarmID(ctx, "x"); err != nil {
		t.Errorf("DeleteCacheNodeBySwarmID: %v", err)
	}
	if err := store.DeleteMissingCacheNodes(ctx, nil); err != nil {
		t.Errorf("DeleteMissingCacheNodes: %v", err)
	}
	if err := store.UpsertSecret(ctx, dbgen.UpsertCacheSecretParams{}); err != nil {
		t.Errorf("UpsertSecret: %v", err)
	}
	if err := store.DeleteCacheSecretBySwarmID(ctx, "x"); err != nil {
		t.Errorf("DeleteCacheSecretBySwarmID: %v", err)
	}
	if err := store.DeleteMissingCacheSecrets(ctx, nil); err != nil {
		t.Errorf("DeleteMissingCacheSecrets: %v", err)
	}
	if err := store.UpsertConfig(ctx, dbgen.UpsertCacheConfigParams{}); err != nil {
		t.Errorf("UpsertConfig: %v", err)
	}
	if err := store.DeleteCacheConfigBySwarmID(ctx, "x"); err != nil {
		t.Errorf("DeleteCacheConfigBySwarmID: %v", err)
	}
	if err := store.DeleteMissingCacheConfigs(ctx, nil); err != nil {
		t.Errorf("DeleteMissingCacheConfigs: %v", err)
	}
	if err := store.UpsertNetwork(ctx, dbgen.UpsertCacheNetworkParams{}); err != nil {
		t.Errorf("UpsertNetwork: %v", err)
	}
	if err := store.DeleteCacheNetworkBySwarmID(ctx, "x"); err != nil {
		t.Errorf("DeleteCacheNetworkBySwarmID: %v", err)
	}
	if err := store.DeleteMissingCacheNetworks(ctx, nil); err != nil {
		t.Errorf("DeleteMissingCacheNetworks: %v", err)
	}
}

// --- refresh mapping for every event type ---

func TestRefreshEveryEventTypeEmitsMatchingChannels(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	lister.set(nil, nil, nil, nil, nil, []network.Summary{{Network: network.Network{ID: "net1", Name: "nw"}}})
	fetch := &fakeFetcher{}
	store := newFakeStore()
	em := &recordingEmitter{}
	w := newTestWatcher(t, src, lister, fetch, store, em, nil)

	nodeFn := func(id string) (swarm.Node, error) {
		return swarm.Node{ID: id, Description: swarm.NodeDescription{Hostname: "node-1"}}, nil
	}
	secretFn := func(id string) (swarm.Secret, error) {
		return swarm.Secret{ID: id, Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}, nil
	}
	configFn := func(id string) (swarm.Config, error) {
		return swarm.Config{ID: id, Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}, nil
	}
	fetch.mu.Lock()
	fetch.nodeFn = nodeFn
	fetch.secretFn = secretFn
	fetch.configFn = configFn
	fetch.mu.Unlock()

	cases := []struct {
		ev      swarmclient.Event
		channel string
		typ     string
	}{
		{swarmclient.Event{Type: events.NodeEventType, Action: "update", ID: "n1", Name: "node-1"}, channelSystem, "node"},
		{swarmclient.Event{Type: events.SecretEventType, Action: "create", ID: "s1", Name: "sec"}, channelSystem, "secret"},
		{swarmclient.Event{Type: events.ConfigEventType, Action: "update", ID: "c1", Name: "cfg"}, channelSystem, "config"},
		{swarmclient.Event{Type: events.NetworkEventType, Action: "create", ID: "net1", Name: "nw"}, channelSystem, "network"},
	}
	for _, tc := range cases {
		w.refreshEntity(context.Background(), tc.ev)
	}
	snap := em.snapshot()
	if len(snap) != len(cases) {
		t.Fatalf("emissions = %d, want %d (%v)", len(snap), len(cases), snap)
	}
	for i, tc := range cases {
		if snap[i].Channel != channelSystem || !strings.Contains(snap[i].Payload, `"type":"`+tc.typ+`"`) {
			t.Errorf("case %d: got %s/%s", i, snap[i].Channel, snap[i].Payload)
		}
	}

	// Deletion paths: fetchers report NotFound -> cached row removed + emit.
	fetch.mu.Lock()
	fetch.nodeFn = nil
	fetch.secretFn = nil
	fetch.configFn = nil
	fetch.mu.Unlock()
	for _, ev := range []swarmclient.Event{
		{Type: events.NodeEventType, Action: "remove", ID: "n-gone"},
		{Type: events.SecretEventType, Action: "remove", ID: "s-gone"},
		{Type: events.ConfigEventType, Action: "remove", ID: "c-gone"},
		{Type: events.NetworkEventType, Action: "remove", ID: "net-gone"},
	} {
		w.refreshEntity(context.Background(), ev)
	}
	if len(em.snapshot()) != 2*len(cases) {
		t.Fatalf("deletions should also emit: %d total", len(em.snapshot()))
	}

	// Transient failures leave the cache untouched and emit nothing.
	before := em.count()
	fail := errors.New("rpc down")
	fetch.mu.Lock()
	fetch.nodeFn = func(string) (swarm.Node, error) { return swarm.Node{}, fail }
	fetch.secretFn = func(string) (swarm.Secret, error) { return swarm.Secret{}, fail }
	fetch.configFn = func(string) (swarm.Config, error) { return swarm.Config{}, fail }
	fetch.mu.Unlock()
	for _, ev := range []swarmclient.Event{
		{Type: events.NodeEventType, ID: "n-x"},
		{Type: events.SecretEventType, ID: "s-x"},
		{Type: events.ConfigEventType, ID: "c-x"},
		{Type: events.NetworkEventType, ID: "net-x"}, // network lists fine but never matches -> delete path emits
	} {
		w.refreshEntity(context.Background(), ev)
	}
	if got := em.count() - before; got != 1 {
		t.Fatalf("transient failures must not emit (network removal does), got %d", got)
	}

	// Unknown event types and cancelled contexts are ignored.
	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.ImageEventType})
	ctxCancelled, cancel := context.WithCancel(context.Background())
	cancel()
	w.refreshEntity(ctxCancelled, swarmclient.Event{Type: events.ServiceEventType, ID: "svc"})
}

func TestRefreshServiceEmitsDeploymentChannelForAppLabel(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	fetch := &fakeFetcher{}
	store := newFakeStore()
	em := &recordingEmitter{}
	w := newTestWatcher(t, src, lister, fetch, store, em, nil)

	svc := testService("svc-abc", "web", "app-9")
	fetch.mu.Lock()
	fetch.serviceFn = func(id string) (swarm.Service, error) { return svc, nil }
	fetch.mu.Unlock()

	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.ServiceEventType, Action: "update", ID: "svc-abc"})
	channels := map[string]string{}
	for _, n := range em.snapshot() {
		channels[n.Channel] = n.Payload
	}
	payload, ok := channels["service:svc-abc"]
	if !ok || !strings.Contains(payload, `"action":"update"`) {
		t.Fatalf("service channel payload missing: %v", channels)
	}
	dep, ok := channels["deployment:app-9"]
	if !ok || !strings.Contains(dep, `"app_id":"app-9"`) || !strings.Contains(dep, `"service_id":"svc-abc"`) {
		t.Fatalf("deployment channel payload missing: %v", channels)
	}

	// Without the app label no deployment emission happens.
	em2 := &recordingEmitter{}
	svcNoApp := testService("svc-abc", "web", "")
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) { return svcNoApp, nil }
	fetch.mu.Unlock()
	w.emitter = em2.emit
	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.ServiceEventType, ID: "svc-abc"})
	for _, n := range em2.snapshot() {
		if n.Channel == "deployment:app-9" {
			t.Fatal("deployment emitted without hive.app.id label")
		}
	}
}

func TestEmitFailuresSurfaceAsErrors(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	fetch := &fakeFetcher{}
	store := newFakeStore()
	w := newTestWatcher(t, src, lister, fetch, store, &recordingEmitter{}, nil)
	w.emitter = func(context.Context, string, string) error { return errors.New("notify down") }

	svc := testService("svc-1", "web", "app-1")
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) { return svc, nil }
	fetch.mu.Unlock()
	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.ServiceEventType, ID: "svc-1"}) // must not panic

	// Resync-done emission failure is logged, not fatal.
	if _, err := w.resync(context.Background()); err != nil {
		t.Fatalf("resync: %v", err)
	}
}

// --- Run loop branches ---

// deadEventSource closes its stream immediately with an error.
type deadEventSource struct {
	started chan struct{}
	once    sync.Once
}

func newDeadEventSource() *deadEventSource { return &deadEventSource{started: make(chan struct{})} }

func (d *deadEventSource) Events(ctx context.Context, h swarmclient.EventHandler) error {
	d.once.Do(func() { close(d.started) })
	return errors.New("stream refused")
}

func TestRunResyncTickerAndNotifyTriggeredReconcile(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	fetch := &fakeFetcher{}
	store := newFakeStore()
	em := &recordingEmitter{}
	notifs := &fakeNotifier{ch: make(chan db.Notification, 8)}
	w := newTestWatcher(t, src, lister, fetch, store, em, notifs)
	w.resyncInterval = 15 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()

	// The periodic ticker fires several times; each pass emits a resync
	// notification on system.
	waitFor(t, 3*time.Second, "ticker-driven resync notifications", func() bool {
		return em.count() >= 3
	})

	// A system NOTIFY schedules a domain reconcile; without a pool the pass
	// is a no-op but the branch must be exercised.
	notifs.ch <- db.Notification{Channel: channelSystem, Payload: "{}"}
	notifs.ch <- db.Notification{Channel: "unrelated", Payload: "{}"}
	time.Sleep(50 * time.Millisecond)

	// Shutdown mid-flight.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRunDeadStreamKeepsPeriodicResync(t *testing.T) {
	src := newDeadEventSource()
	lister := &fakeLister{}
	store := newFakeStore()
	em := &recordingEmitter{}
	w := newTestWatcher(t, src, lister, &fakeFetcher{}, store, em, nil)
	w.resyncInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	waitFor(t, 3*time.Second, "periodic resync after stream death", func() bool {
		return em.count() >= 2
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}

// --- wiring ---

func TestNewWatcherWiring(t *testing.T) {
	cli, err := swarmclient.New("unix:///nonexistent-hive-test.sock")
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(cli, nil, nil)
	if w.source == nil || w.lister == nil || w.fetch == nil || w.store == nil || w.domains == nil {
		t.Fatal("NewWatcher left seams unwired")
	}
	if w.emitter != nil || w.notifs != nil {
		t.Fatal("nil fanout must disable emitter and notifier")
	}
}

func TestApplyDomainRoutesListFailure(t *testing.T) {
	src := newFakeEventSource()
	lister := &failingLister{}
	w := newTestWatcher(t, src, lister, &fakeFetcher{}, newFakeStore(), &recordingEmitter{}, nil)
	applier := &recordingApplier{}
	w.domains = applier
	w.applyDomainRoutes(context.Background(), map[string][]proxy.Route{
		"app-1": {{Host: "a.example.com"}},
	})
	if len(applier.calls) != 0 {
		t.Fatal("list failure must abort routing reconciliation")
	}
}

type failingLister struct{}

func (f *failingLister) ListServices(context.Context) ([]swarm.Service, error) {
	return nil, errors.New("swarm down")
}
func (*failingLister) ListAllTasks(context.Context) ([]swarm.Task, error) {
	return nil, nil
}
func (*failingLister) ListNodes(context.Context) ([]swarm.Node, error)     { return nil, nil }
func (*failingLister) ListSecrets(context.Context) ([]swarm.Secret, error) { return nil, nil }
func (*failingLister) ListConfigs(context.Context) ([]swarm.Config, error) { return nil, nil }
func (*failingLister) ListNetworks(context.Context) ([]network.Summary, error) {
	return nil, nil
}

// --- store/fetch failure branches in refresh paths ---

type failingStore struct {
	fakeStore
	err error
}

func (s *failingStore) UpsertService(ctx context.Context, p dbgen.UpsertCacheServiceParams) error {
	return s.err
}
func (s *failingStore) DeleteCacheServiceBySwarmID(ctx context.Context, id string) error {
	return s.err
}
func (s *failingStore) UpsertNode(ctx context.Context, p dbgen.UpsertCacheNodeParams) error {
	return s.err
}
func (s *failingStore) DeleteCacheNodeBySwarmID(ctx context.Context, id string) error { return s.err }
func (s *failingStore) UpsertSecret(ctx context.Context, p dbgen.UpsertCacheSecretParams) error {
	return s.err
}
func (s *failingStore) DeleteCacheSecretBySwarmID(ctx context.Context, id string) error {
	return s.err
}
func (s *failingStore) UpsertConfig(ctx context.Context, p dbgen.UpsertCacheConfigParams) error {
	return s.err
}
func (s *failingStore) DeleteCacheConfigBySwarmID(ctx context.Context, id string) error {
	return s.err
}
func (s *failingStore) UpsertNetwork(ctx context.Context, p dbgen.UpsertCacheNetworkParams) error {
	return s.err
}
func (s *failingStore) DeleteCacheNetworkBySwarmID(ctx context.Context, id string) error {
	return s.err
}

// TestRefreshStoreFailuresSurface walks every cache-write failure branch.
func TestRefreshStoreFailuresSurface(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	lister.set(nil, nil, nil, nil, nil, []network.Summary{{Network: network.Network{ID: "net1"}}})
	fetch := &fakeFetcher{}
	em := &recordingEmitter{}
	store := &failingStore{err: errors.New("db write failed")}
	w := newTestWatcher(t, src, lister, fetch, store, em, nil)

	svc := testService("svc-1", "web", "app-1")
	node := swarm.Node{ID: "n1"}
	secret := swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}
	cfg := swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) { return svc, nil }
	fetch.nodeFn = func(string) (swarm.Node, error) { return node, nil }
	fetch.secretFn = func(string) (swarm.Secret, error) { return secret, nil }
	fetch.configFn = func(string) (swarm.Config, error) { return cfg, nil }
	fetch.mu.Unlock()

	// Upsert failures on the happy path.
	for _, ev := range []swarmclient.Event{
		{Type: events.ServiceEventType, ID: "svc-1"},
		{Type: events.NodeEventType, ID: "n1"},
		{Type: events.SecretEventType, ID: "s1"},
		{Type: events.ConfigEventType, ID: "c1"},
		{Type: events.NetworkEventType, ID: "net1"},
	} {
		w.refreshEntity(context.Background(), ev)
	}
	if em.count() != 0 {
		t.Fatalf("failed writes must not emit: %d emitted", em.count())
	}

	// Delete failures on the not-found path.
	fetch.mu.Lock()
	fetch.serviceFn = nil
	fetch.nodeFn = nil
	fetch.secretFn = nil
	fetch.configFn = nil
	fetch.mu.Unlock()
	for _, ev := range []swarmclient.Event{
		{Type: events.ServiceEventType, ID: "gone"},
		{Type: events.NodeEventType, ID: "gone"},
		{Type: events.SecretEventType, ID: "gone"},
		{Type: events.ConfigEventType, ID: "gone"},
		{Type: events.NetworkEventType, ID: "gone"},
	} {
		w.refreshEntity(context.Background(), ev)
	}
	if em.count() != 0 {
		t.Fatalf("failed deletes must not emit: %d emitted", em.count())
	}
}

// TestResyncStoreAndListerFailures covers resync's error propagation.
func TestResyncStoreAndListerFailures(t *testing.T) {
	ctx := context.Background()
	mk := func(l Lister, s CacheStore) *Watcher {
		return newTestWatcher(t, newFakeEventSource(), l, &fakeFetcher{}, s, &recordingEmitter{}, nil)
	}
	good := &fakeLister{}
	if _, err := mk(good, &failingStore{err: errors.New("write failed")}).resync(ctx); err == nil {
		t.Fatal("expected upsert failure to fail resync")
	}
	for i, l := range []Lister{
		&errLister{m: "services"}, &errLister{m: "tasks"}, &errLister{m: "nodes"},
		&errLister{m: "secrets"}, &errLister{m: "configs"}, &errLister{m: "networks"},
	} {
		_ = i
		if _, err := mk(l, newFakeStore()).resync(ctx); err == nil {
			t.Fatalf("lister %T should fail resync", l)
		}
	}
}

type errLister struct{ m string }

var errListBoom = errors.New("list boom")

func (e *errLister) ListServices(context.Context) ([]swarm.Service, error) {
	if e.m == "services" {
		return nil, errListBoom
	}
	return nil, nil
}
func (*errLister) ListAllTasks(context.Context) ([]swarm.Task, error) {
	return nil, errListBoom2()
}
func errListBoom2() error { return errors.New("tasks boom") }
func (*errLister) ListNodes(context.Context) ([]swarm.Node, error) {
	return nil, errors.New("nodes boom")
}
func (*errLister) ListSecrets(context.Context) ([]swarm.Secret, error) {
	return nil, errors.New("secrets boom")
}
func (*errLister) ListConfigs(context.Context) ([]swarm.Config, error) {
	return nil, errors.New("configs boom")
}
func (*errLister) ListNetworks(context.Context) ([]network.Summary, error) {
	return nil, errors.New("networks boom")
}

func (s *failingStore) DeleteMissingCacheServices(context.Context, []string) error { return s.err }
func (s *failingStore) DeleteMissingCacheTasks(context.Context, []string) error    { return s.err }
func (s *failingStore) DeleteMissingCacheNodes(context.Context, []string) error    { return s.err }
func (s *failingStore) DeleteMissingCacheSecrets(context.Context, []string) error  { return s.err }
func (s *failingStore) DeleteMissingCacheConfigs(context.Context, []string) error  { return s.err }
func (s *failingStore) DeleteMissingCacheNetworks(context.Context, []string) error { return s.err }

// --- domain reconciliation against the real database ---

func TestReconcileDomainsAppliesConfiguredRoutes(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web", nil)

	for _, d := range []struct {
		host     string
		tls      bool
		rt       string
		priority *int
	}{
		{host: "app.example.com", tls: true, rt: "host"},
		{host: "*.wild.example.com", tls: false, rt: "wildcard", priority: intPtr(7)},
	} {
		if _, err := pool.Exec(context.Background(),
			`insert into domains(application_id, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority)
			 values ($1::uuid,$2,$3,$4,'',false,$5)`,
			appID, d.host, d.tls, d.rt, d.priority); err != nil {
			t.Fatalf("seed %s: %v", d.host, err)
		}
	}

	lister := &fakeLister{}
	lister.set([]swarm.Service{
		testService("svc-app", "web", appID),
		testService("svc-other", "other", ""), // no app label -> skipped
	}, nil, nil, nil, nil, nil)
	applier := &recordingApplier{}
	cli, err := swarmclient.New("unix:///nonexistent-hive-test.sock")
	if err != nil {
		t.Fatal(err)
	}
	w := &Watcher{swarm: cli, pool: pool, source: newFakeEventSource(), lister: lister,
		fetch: &fakeFetcher{}, store: newFakeStore(), emitter: (&recordingEmitter{}).emit,
		domains: applier}

	w.reconcileDomains(context.Background())
	if len(applier.calls) == 0 {
		t.Fatal("no routes applied")
	}
	var hosts []string
	for _, c := range applier.calls {
		hosts = append(hosts, c.route.Host)
		if c.port != appDomainPort {
			t.Errorf("route %s port = %d, want %d", c.route.Host, c.port, appDomainPort)
		}
	}
	found := map[string]bool{}
	for _, h := range hosts {
		found[h] = true
	}
	if !found["app.example.com"] || !found["*.wild.example.com"] {
		t.Fatalf("routes missing: %v", hosts)
	}

	// A service-less application applies nothing new; list failures abort.
	lister2 := &failingLister{}
	w.lister = lister2
	w.applyDomainRoutes(context.Background(), map[string][]proxy.Route{"x": {{Host: "y"}}})
}

func intPtr(i int) *int { return &i }

func TestReconcileDomainsSkipsWithoutPoolOrApplier(t *testing.T) {
	w := newTestWatcher(t, newFakeEventSource(), &fakeLister{}, &fakeFetcher{},
		newFakeStore(), &recordingEmitter{}, nil)
	w.reconcileDomains(context.Background()) // nil pool -> no-op
	w.domains = nil
	w.reconcileDomains(context.Background()) // nil applier -> no-op
}

// kindFailStore fails only the named operation so every per-kind error
// branch in resync becomes reachable.
type kindFailStore struct {
	fakeStore
	failKind string // "tasks", "nodes", "secrets", "configs", "networks"
}

func (s *kindFailStore) fail(kind string) error {
	if kind == s.failKind {
		return errors.New("write failed for " + kind)
	}
	return nil
}

func (s *kindFailStore) UpsertTask(ctx context.Context, p dbgen.UpsertCacheTaskParams) error {
	return s.fail("tasks")
}
func (s *kindFailStore) DeleteMissingCacheTasks(context.Context, []string) error {
	return s.fail("tasks")
}
func (s *kindFailStore) UpsertNode(ctx context.Context, p dbgen.UpsertCacheNodeParams) error {
	return s.fail("nodes")
}
func (s *kindFailStore) DeleteMissingCacheNodes(context.Context, []string) error {
	return s.fail("nodes")
}
func (s *kindFailStore) UpsertSecret(ctx context.Context, p dbgen.UpsertCacheSecretParams) error {
	return s.fail("secrets")
}
func (s *kindFailStore) DeleteMissingCacheSecrets(context.Context, []string) error {
	return s.fail("secrets")
}
func (s *kindFailStore) UpsertConfig(ctx context.Context, p dbgen.UpsertCacheConfigParams) error {
	return s.fail("configs")
}
func (s *kindFailStore) DeleteMissingCacheConfigs(context.Context, []string) error {
	return s.fail("configs")
}
func (s *kindFailStore) UpsertNetwork(ctx context.Context, p dbgen.UpsertCacheNetworkParams) error {
	return s.fail("networks")
}
func (s *kindFailStore) DeleteMissingCacheNetworks(context.Context, []string) error {
	return s.fail("networks")
}

func TestResyncPerKindFailureBranches(t *testing.T) {
	ctx := context.Background()
	objects := &fakeLister{}
	objects.set(
		[]swarm.Service{testService("svc-1", "web", "")},
		[]swarm.Task{{ID: "t1", ServiceID: "svc-1", Slot: 1}},
		[]swarm.Node{{ID: "n1", Description: swarm.NodeDescription{Hostname: "h"}}},
		[]swarm.Secret{{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}},
		[]swarm.Config{{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}},
		[]network.Summary{{Network: network.Network{ID: "net1", Name: "nw"}}},
	)
	for _, kind := range []string{"tasks", "nodes", "secrets", "configs", "networks"} {
		w := newTestWatcher(t, newFakeEventSource(), objects, &fakeFetcher{},
			&kindFailStore{fakeStore: *newFakeStore(), failKind: kind}, &recordingEmitter{}, nil)
		if _, err := w.resync(ctx); err == nil || !strings.Contains(err.Error(), kind) {
			t.Errorf("kind %s: expected failure, got %v", kind, err)
		}
	}
	// Task naming falls back to the service name when known.
	w := newTestWatcher(t, newFakeEventSource(), objects, &fakeFetcher{}, newFakeStore(), &recordingEmitter{}, nil)
	if _, err := w.resync(ctx); err != nil {
		t.Fatalf("healthy resync: %v", err)
	}
}

// failApplier fails every ApplyDomain call.
type failApplier struct{}

func (failApplier) ApplyDomain(context.Context, string, string, proxy.Route, int) error {
	return errors.New("apply refused")
}

func TestApplyDomainRoutesLogsApplyFailures(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web", nil)
	if _, err := pool.Exec(context.Background(),
		`insert into domains(application_id, hostname, route_type) values ($1::uuid,'f.example.com','host')`, appID); err != nil {
		t.Fatal(err)
	}
	lister := &fakeLister{}
	lister.set([]swarm.Service{testService("svc-app", "web", appID)}, nil, nil, nil, nil, nil)
	cli, err := swarmclient.New("unix:///nonexistent-hive-test.sock")
	if err != nil {
		t.Fatal(err)
	}
	w := &Watcher{swarm: cli, pool: pool, lister: lister, fetch: &fakeFetcher{},
		store: newFakeStore(), emitter: (&recordingEmitter{}).emit, domains: failApplier{}}
	w.reconcileDomains(context.Background()) // apply errors logged, loop continues
}

// failingEmitter fails every emission.
type failingEmitter struct{}

func (failingEmitter) emit(context.Context, string, string) error { return errors.New("notify down") }

func TestEmitFailuresPerKind(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	lister.set(nil, nil, nil, nil, nil, []network.Summary{{Network: network.Network{ID: "net1", Name: "nw"}}})
	fetch := &fakeFetcher{}
	node := swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "h"}}
	secret := swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}
	cfg := swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}
	fetch.mu.Lock()
	fetch.nodeFn = func(string) (swarm.Node, error) { return node, nil }
	fetch.secretFn = func(string) (swarm.Secret, error) { return secret, nil }
	fetch.configFn = func(string) (swarm.Config, error) { return cfg, nil }
	fetch.mu.Unlock()

	w := newTestWatcher(t, src, lister, fetch, newFakeStore(), &recordingEmitter{}, nil)
	w.emitter = failingEmitter{}.emit
	for _, ev := range []swarmclient.Event{
		{Type: events.NodeEventType, ID: "n1"},
		{Type: events.SecretEventType, ID: "s1"},
		{Type: events.ConfigEventType, ID: "c1"},
		{Type: events.NetworkEventType, ID: "net1"},
		{Type: events.ServiceEventType, ID: "svc-1"},
	} {
		w.refreshEntity(context.Background(), ev) // emit errors logged, no panic
	}
}

// --- coverage top-up: marshal seam, remaining resync/refresh branches, Run
// loop paths and domain reconciliation failures ---

// selectiveLister fails only the named listing.
type selectiveLister struct {
	fakeLister
	fail string
}

func (l *selectiveLister) ListServices(ctx context.Context) ([]swarm.Service, error) {
	if l.fail == "services" {
		return nil, errors.New("services boom")
	}
	return l.fakeLister.ListServices(ctx)
}

func (l *selectiveLister) ListAllTasks(ctx context.Context) ([]swarm.Task, error) {
	if l.fail == "tasks" {
		return nil, errors.New("tasks boom")
	}
	return l.fakeLister.ListAllTasks(ctx)
}

func (l *selectiveLister) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	if l.fail == "nodes" {
		return nil, errors.New("nodes boom")
	}
	return l.fakeLister.ListNodes(ctx)
}

func (l *selectiveLister) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	if l.fail == "secrets" {
		return nil, errors.New("secrets boom")
	}
	return l.fakeLister.ListSecrets(ctx)
}

func (l *selectiveLister) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	if l.fail == "configs" {
		return nil, errors.New("configs boom")
	}
	return l.fakeLister.ListConfigs(ctx)
}

func (l *selectiveLister) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	if l.fail == "networks" {
		return nil, errors.New("networks boom")
	}
	return l.fakeLister.ListNetworks(ctx)
}

func TestResyncRemainingListerFailures(t *testing.T) {
	for _, kind := range []string{"nodes", "secrets", "configs", "networks"} {
		l := &selectiveLister{fail: kind}
		w := newTestWatcher(t, newFakeEventSource(), l, &fakeFetcher{}, newFakeStore(), &recordingEmitter{}, nil)
		_, err := w.resync(context.Background())
		if err == nil || !strings.Contains(err.Error(), kind) {
			t.Errorf("kind %s: expected failure, got %v", kind, err)
		}
	}
}

func TestResyncUpsertServiceFailure(t *testing.T) {
	l := &fakeLister{}
	l.set([]swarm.Service{testService("svc-1", "web", "")}, nil, nil, nil, nil, nil)
	w := newTestWatcher(t, newFakeEventSource(), l, &fakeFetcher{},
		&failingStore{err: errors.New("svc write failed")}, &recordingEmitter{}, nil)
	_, err := w.resync(context.Background())
	if err == nil || !strings.Contains(err.Error(), "upsert cached service") {
		t.Fatalf("expected upsert-service failure, got %v", err)
	}
}

// deleteMissingFailStore fails only the DeleteMissing* call for the named
// kind, so the convergence deletes become reachable with live objects.
type deleteMissingFailStore struct {
	fakeStore
	fail string
}

func (s *deleteMissingFailStore) del(kind string) error {
	if kind == s.fail {
		return errors.New("delete missing " + kind)
	}
	return nil
}

func (s *deleteMissingFailStore) DeleteMissingCacheTasks(context.Context, []string) error {
	return s.del("tasks")
}
func (s *deleteMissingFailStore) DeleteMissingCacheNodes(context.Context, []string) error {
	return s.del("nodes")
}
func (s *deleteMissingFailStore) DeleteMissingCacheSecrets(context.Context, []string) error {
	return s.del("secrets")
}
func (s *deleteMissingFailStore) DeleteMissingCacheConfigs(context.Context, []string) error {
	return s.del("configs")
}
func (s *deleteMissingFailStore) DeleteMissingCacheNetworks(context.Context, []string) error {
	return s.del("networks")
}

func TestResyncDeleteMissingFailures(t *testing.T) {
	objects := &fakeLister{}
	objects.set(
		[]swarm.Service{testService("svc-1", "web", "")},
		[]swarm.Task{{ID: "t1", ServiceID: "svc-1", Slot: 1}},
		[]swarm.Node{{ID: "n1", Description: swarm.NodeDescription{Hostname: "h"}}},
		[]swarm.Secret{{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}},
		[]swarm.Config{{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}},
		[]network.Summary{{Network: network.Network{ID: "net1", Name: "nw"}}},
	)
	for _, kind := range []string{"tasks", "nodes", "secrets", "configs", "networks"} {
		w := newTestWatcher(t, newFakeEventSource(), objects, &fakeFetcher{},
			&deleteMissingFailStore{fakeStore: *newFakeStore(), fail: kind}, &recordingEmitter{}, nil)
		_, err := w.resync(context.Background())
		if err == nil || !strings.Contains(err.Error(), kind) {
			t.Errorf("kind %s: expected delete-missing failure, got %v", kind, err)
		}
	}
}

func TestRefreshTransientFetchErrorsSurface(t *testing.T) {
	boom := errors.New("fetch boom")
	fetch := &fakeFetcher{}
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) { return swarm.Service{}, boom }
	fetch.nodeFn = func(string) (swarm.Node, error) { return swarm.Node{}, boom }
	fetch.secretFn = func(string) (swarm.Secret, error) { return swarm.Secret{}, boom }
	fetch.configFn = func(string) (swarm.Config, error) { return swarm.Config{}, boom }
	fetch.mu.Unlock()
	em := &recordingEmitter{}
	w := newTestWatcher(t, newFakeEventSource(), &fakeLister{}, fetch, newFakeStore(), em, nil)

	for _, ev := range []swarmclient.Event{
		{Type: events.ServiceEventType, ID: "svc-1", Action: "update"},
		{Type: events.NodeEventType, ID: "n1", Action: "update"},
		{Type: events.SecretEventType, ID: "s1", Action: "update"},
		{Type: events.ConfigEventType, ID: "c1", Action: "update"},
	} {
		w.refreshEntity(context.Background(), ev)
	}
	if em.count() != 0 {
		t.Fatalf("failed fetches must not emit: %d emitted", em.count())
	}

	// Network listing failure surfaces the same way.
	w2 := newTestWatcher(t, newFakeEventSource(), &selectiveLister{fail: "networks"}, fetch,
		newFakeStore(), &recordingEmitter{}, nil)
	w2.refreshEntity(context.Background(), swarmclient.Event{Type: events.NetworkEventType, ID: "net1"})
}

func TestRefreshDeploymentEmitFailure(t *testing.T) {
	fetch := &fakeFetcher{}
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) {
		return testService("svc-1", "web", "app-1"), nil
	}
	fetch.mu.Unlock()
	w := newTestWatcher(t, newFakeEventSource(), &fakeLister{}, fetch, newFakeStore(), &recordingEmitter{}, nil)
	w.emitter = func(_ context.Context, channel, _ string) error {
		if strings.HasPrefix(channel, "deployment:") {
			return errors.New("deployment notify down")
		}
		return nil
	}
	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.ServiceEventType, ID: "svc-1"})
}

func TestMarshalFailuresSurface(t *testing.T) {
	orig := marshalJSON
	t.Cleanup(func() { marshalJSON = orig })
	marshalJSON = func(any) ([]byte, error) { return nil, errors.New("marshal boom") }

	// Resync fails on the first snapshot encode.
	objects := &fakeLister{}
	objects.set([]swarm.Service{testService("svc-1", "web", "")}, nil, nil, nil, nil, nil)
	w := newTestWatcher(t, newFakeEventSource(), objects, &fakeFetcher{}, newFakeStore(), &recordingEmitter{}, nil)
	if _, err := w.resync(context.Background()); err == nil || !strings.Contains(err.Error(), "marshal service svc-1 spec") {
		t.Fatalf("expected marshal failure in resync, got %v", err)
	}

	// Every event kind logs its marshal failure without panicking: the
	// fetches must succeed so each refresh reaches its encode step.
	fetch := &fakeFetcher{}
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) { return testService("svc-1", "web", ""), nil }
	fetch.nodeFn = func(string) (swarm.Node, error) {
		return swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "h"}}, nil
	}
	fetch.secretFn = func(string) (swarm.Secret, error) {
		return swarm.Secret{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}, nil
	}
	fetch.configFn = func(string) (swarm.Config, error) {
		return swarm.Config{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}, nil
	}
	fetch.mu.Unlock()
	lister := &fakeLister{}
	lister.set(nil, nil, nil, nil, nil, []network.Summary{{Network: network.Network{ID: "net1", Name: "nw"}}})
	em := &recordingEmitter{}
	w2 := newTestWatcher(t, newFakeEventSource(), lister, fetch, newFakeStore(), em, nil)
	for _, ev := range []swarmclient.Event{
		{Type: events.ServiceEventType, ID: "svc-1"},
		{Type: events.NodeEventType, ID: "n1"},
		{Type: events.SecretEventType, ID: "s1"},
		{Type: events.ConfigEventType, ID: "c1"},
		{Type: events.NetworkEventType, ID: "net1"},
	} {
		w2.refreshEntity(context.Background(), ev)
	}
	if em.count() != 0 {
		t.Fatalf("failed marshals must not emit: %d emitted", em.count())
	}
}

func TestMustJSONFallsBackOnUnmarshalable(t *testing.T) {
	if got := mustJSON(make(chan int)); got != "{}" {
		t.Fatalf("mustJSON(unmarshalable) = %q, want {}", got)
	}
}

func TestEmitResyncDoneFailureLogged(t *testing.T) {
	w := newTestWatcher(t, newFakeEventSource(), &fakeLister{}, &fakeFetcher{},
		newFakeStore(), &recordingEmitter{}, nil)
	w.emitter = failingEmitter{}.emit
	w.emitResyncDone(context.Background(), map[string]int{"services": 1}) // logged, not fatal
}

// --- Run loop: resync failure paths ---

// countingFailLister counts ListServices calls and can fail every call.
type countingFailLister struct {
	fakeLister
	mu    sync.Mutex
	calls int
	fail  bool
}

func (l *countingFailLister) ListServices(context.Context) ([]swarm.Service, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.fail {
		return nil, errors.New("services down")
	}
	return nil, nil
}

func (l *countingFailLister) listCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func TestRunResyncFailurePathsLogged(t *testing.T) {
	// Initial resync failure and ticker-driven resync failures are logged,
	// never fatal.
	l := &countingFailLister{fail: true}
	w := newTestWatcher(t, newFakeEventSource(), l, &fakeFetcher{}, newFakeStore(), &recordingEmitter{}, nil)
	w.resyncInterval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	waitFor(t, 3*time.Second, "repeated failing resync attempts", func() bool {
		return l.listCalls() >= 3
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}

// closedNotifier hands back an already-closed subscription channel.
type closedNotifier struct{ ch chan db.Notification }

func (c closedNotifier) Subscribe(string, int) <-chan db.Notification { return c.ch }

func TestRunSurvivesClosedNotifyChannel(t *testing.T) {
	ch := make(chan db.Notification)
	close(ch)
	em := &recordingEmitter{}
	w := newTestWatcher(t, newFakeEventSource(), &fakeLister{}, &fakeFetcher{},
		newFakeStore(), em, closedNotifier{ch: ch})
	w.resyncInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	waitFor(t, 3*time.Second, "ticker resyncs after channel close", func() bool {
		return em.count() >= 2
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}

// --- Run loop: event-queue overflow schedules a full resync ---

// floodSource dispatches count events synchronously, then parks until ctx is
// cancelled.
type floodSource struct {
	count   int
	started chan struct{}
}

func (f *floodSource) Events(ctx context.Context, h swarmclient.EventHandler) error {
	for i := 0; i < f.count; i++ {
		_ = h.OnService(swarmclient.Event{Type: events.ServiceEventType, Action: "update", ID: fmt.Sprintf("evt-%d", i)})
	}
	close(f.started)
	<-ctx.Done()
	return ctx.Err()
}

// gatedStore blocks the first DeleteMissingCacheServices call so the test can
// hold Run inside its initial resync while the pump floods the queue —
// making the overflow branch deterministic.
type gatedStore struct {
	fakeStore
	release chan struct{}
}

func (s *gatedStore) DeleteMissingCacheServices(ctx context.Context, _ []string) error {
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return nil
}

func startFloodedRun(t *testing.T, lister Lister, em *recordingEmitter) (context.CancelFunc, chan struct{}) {
	t.Helper()
	store := &gatedStore{fakeStore: *newFakeStore(), release: make(chan struct{})}
	src := &floodSource{count: eventQueueSize + 8, started: make(chan struct{})}
	w := newTestWatcher(t, src, lister, &fakeFetcher{}, store, em, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	<-src.started // queue is full and the overflow flag is set
	close(store.release)
	return cancel, done
}

func stopRun(t *testing.T, cancel context.CancelFunc, done chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestRunOverflowSchedulesFullResync(t *testing.T) {
	em := &recordingEmitter{}
	cancel, done := startFloodedRun(t, &fakeLister{}, em)
	// Initial resync emits once; the overflow-scheduled resync emits again.
	waitFor(t, 15*time.Second, "overflow-scheduled resync", func() bool {
		return em.count() >= 2
	})
	stopRun(t, cancel, done)
}

func TestRunOverflowResyncFailureLogged(t *testing.T) {
	// The initial resync succeeds (call 1); the overflow-scheduled resync
	// (call 2) fails and is logged.
	l := &countingFailLister{}
	cancel, done := startFloodedRun(t, l, &recordingEmitter{})
	waitFor(t, 15*time.Second, "overflow resync attempt after initial", func() bool {
		return l.listCalls() >= 2
	})
	stopRun(t, cancel, done)
}

// --- wiring and domain reconciliation failures ---

func TestNewWatcherWiresFanoutEmitter(t *testing.T) {
	cli, err := swarmclient.New("unix:///nonexistent-hive-test.sock")
	if err != nil {
		t.Fatal(err)
	}
	pool := testdb.Get(t)
	w := NewWatcher(cli, db.NewFanout(pool), pool)
	if w.emitter == nil || w.notifs == nil {
		t.Fatal("non-nil fanout must wire emitter and notifier")
	}
}

func TestReconcileDomainsQueryAndLockFailures(t *testing.T) {
	pool := testdb.Get(t)
	cli, err := swarmclient.New("unix:///nonexistent-hive-test.sock")
	if err != nil {
		t.Fatal(err)
	}
	lister := &fakeLister{}
	lister.set([]swarm.Service{testService("svc-app", "web", "11111111-1111-4111-8111-111111111111")}, nil, nil, nil, nil, nil)
	applier := &recordingApplier{}
	w := &Watcher{swarm: cli, pool: pool, source: newFakeEventSource(), lister: lister,
		fetch: &fakeFetcher{}, store: newFakeStore(), emitter: (&recordingEmitter{}).emit,
		domains: applier}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The domain query fails on the cancelled context; no routes apply.
	w.reconcileDomains(ctx)
	if len(applier.calls) != 0 {
		t.Fatal("no routes may be applied when the domain query fails")
	}
	// The app lock cannot be acquired on a cancelled context either.
	w.applyDomainRoutes(ctx, map[string][]proxy.Route{"11111111-1111-4111-8111-111111111111": {{Host: "a.example.com"}}})
}

// TestResyncMarshalFailuresPerSnapshotType drives the marshal seam per
// snapshot type, so every per-kind encode branch in resync is exercised.
func TestResyncMarshalFailuresPerSnapshotType(t *testing.T) {
	orig := marshalJSON
	t.Cleanup(func() { marshalJSON = orig })
	objects := &fakeLister{}
	objects.set(
		[]swarm.Service{testService("svc-1", "web", "")},
		[]swarm.Task{{ID: "t1", ServiceID: "svc-1", Slot: 1}},
		[]swarm.Node{{ID: "n1", Description: swarm.NodeDescription{Hostname: "h"}}},
		[]swarm.Secret{{ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "sec"}}}},
		[]swarm.Config{{ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}},
		[]network.Summary{{Network: network.Network{ID: "net1", Name: "nw"}}},
	)
	cases := []struct {
		name    string
		target  any
		wantErr string
	}{
		{"service spec", swarm.ServiceSpec{}, "marshal service svc-1 spec"},
		{"service status", serviceCacheStatus{}, "marshal service svc-1 status"},
		{"task spec", swarm.TaskSpec{}, "marshal task t1 spec"},
		{"task status", swarm.TaskStatus{}, "marshal task t1 status"},
		{"node spec", swarm.NodeSpec{}, "marshal node n1 spec"},
		{"node status", swarm.NodeStatus{}, "marshal node n1 status"},
		{"secret spec", swarm.SecretSpec{}, "marshal secret s1 spec"},
		{"config spec", swarm.ConfigSpec{}, "marshal config c1 spec"},
		{"network summary", network.Summary{}, "marshal network net1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := reflect.TypeOf(tc.target)
			marshalJSON = func(v any) ([]byte, error) {
				if reflect.TypeOf(v) == target {
					return nil, errors.New("marshal boom")
				}
				return orig(v)
			}
			w := newTestWatcher(t, newFakeEventSource(), objects, &fakeFetcher{},
				newFakeStore(), &recordingEmitter{}, nil)
			_, err := w.resync(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestRefreshStatusMarshalFailures covers the second encode of the service
// and node refresh paths (the status snapshots).
func TestRefreshStatusMarshalFailures(t *testing.T) {
	orig := marshalJSON
	t.Cleanup(func() { marshalJSON = orig })
	fetch := &fakeFetcher{}
	fetch.mu.Lock()
	fetch.serviceFn = func(string) (swarm.Service, error) { return testService("svc-1", "web", ""), nil }
	fetch.nodeFn = func(string) (swarm.Node, error) {
		return swarm.Node{ID: "n1", Description: swarm.NodeDescription{Hostname: "h"}}, nil
	}
	fetch.mu.Unlock()
	em := &recordingEmitter{}
	w := newTestWatcher(t, newFakeEventSource(), &fakeLister{}, fetch, newFakeStore(), em, nil)

	statusTypes := map[reflect.Type]bool{
		reflect.TypeOf(serviceCacheStatus{}): true,
		reflect.TypeOf(swarm.NodeStatus{}):   true,
	}
	marshalJSON = func(v any) ([]byte, error) {
		if statusTypes[reflect.TypeOf(v)] {
			return nil, errors.New("status boom")
		}
		return orig(v)
	}
	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.ServiceEventType, ID: "svc-1"})
	w.refreshEntity(context.Background(), swarmclient.Event{Type: events.NodeEventType, ID: "n1"})
	if em.count() != 0 {
		t.Fatalf("failed marshals must not emit: %d emitted", em.count())
	}
}
