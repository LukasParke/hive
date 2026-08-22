package reconcile

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/luke/hive/control-plane/internal/db"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

// --- fakes ---

type fakeEventSource struct {
	mu      sync.Mutex
	handler swarmclient.EventHandler
	started chan struct{}
	start   sync.Once
}

func newFakeEventSource() *fakeEventSource {
	return &fakeEventSource{started: make(chan struct{})}
}

func (f *fakeEventSource) Events(ctx context.Context, h swarmclient.EventHandler) error {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
	f.start.Do(func() { close(f.started) })
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeEventSource) dispatch(ev swarmclient.Event) {
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	var fn func(swarmclient.Event) error
	switch ev.Type {
	case events.ServiceEventType:
		fn = h.OnService
	case events.NodeEventType:
		fn = h.OnNode
	case events.SecretEventType:
		fn = h.OnSecret
	case events.ConfigEventType:
		fn = h.OnConfig
	case events.NetworkEventType:
		fn = h.OnNetwork
	}
	if fn != nil {
		_ = fn(ev)
	}
}

type fakeLister struct {
	mu       sync.Mutex
	services []swarm.Service
	tasks    []swarm.Task
	nodes    []swarm.Node
	secrets  []swarm.Secret
	configs  []swarm.Config
	networks []network.Summary
}

func (f *fakeLister) set(services []swarm.Service, tasks []swarm.Task, nodes []swarm.Node, secrets []swarm.Secret, configs []swarm.Config, networks []network.Summary) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services, f.tasks, f.nodes, f.secrets, f.configs, f.networks = services, tasks, nodes, secrets, configs, networks
}

func (f *fakeLister) ListServices(ctx context.Context) ([]swarm.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.services, nil
}

func (f *fakeLister) ListAllTasks(ctx context.Context) ([]swarm.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks, nil
}

func (f *fakeLister) ListNodes(ctx context.Context) ([]swarm.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nodes, nil
}

func (f *fakeLister) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.secrets, nil
}

func (f *fakeLister) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configs, nil
}

func (f *fakeLister) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.networks, nil
}

type fakeFetcher struct {
	mu        sync.Mutex
	serviceFn func(id string) (swarm.Service, error)
	nodeFn    func(id string) (swarm.Node, error)
	secretFn  func(id string) (swarm.Secret, error)
	configFn  func(id string) (swarm.Config, error)
}

func (f *fakeFetcher) GetService(ctx context.Context, id string) (swarm.Service, error) {
	f.mu.Lock()
	fn := f.serviceFn
	f.mu.Unlock()
	if fn == nil {
		return swarm.Service{}, fmt.Errorf("service %s: %w", id, errdefs.ErrNotFound)
	}
	return fn(id)
}

func (f *fakeFetcher) GetNode(ctx context.Context, id string) (swarm.Node, error) {
	f.mu.Lock()
	fn := f.nodeFn
	f.mu.Unlock()
	if fn == nil {
		return swarm.Node{}, fmt.Errorf("node %s: %w", id, errdefs.ErrNotFound)
	}
	return fn(id)
}

func (f *fakeFetcher) GetSecret(ctx context.Context, id string) (swarm.Secret, error) {
	f.mu.Lock()
	fn := f.secretFn
	f.mu.Unlock()
	if fn == nil {
		return swarm.Secret{}, fmt.Errorf("secret %s: %w", id, errdefs.ErrNotFound)
	}
	return fn(id)
}

func (f *fakeFetcher) GetConfig(ctx context.Context, id string) (swarm.Config, error) {
	f.mu.Lock()
	fn := f.configFn
	f.mu.Unlock()
	if fn == nil {
		return swarm.Config{}, fmt.Errorf("config %s: %w", id, errdefs.ErrNotFound)
	}
	return fn(id)
}

type fakeStore struct {
	mu sync.Mutex

	services map[string]dbgen.UpsertCacheServiceParams
	tasks    map[string]dbgen.UpsertCacheTaskParams
	nodes    map[string]dbgen.UpsertCacheNodeParams
	secrets  map[string]dbgen.UpsertCacheSecretParams
	configs  map[string]dbgen.UpsertCacheConfigParams
	networks map[string]dbgen.UpsertCacheNetworkParams

	upsertServiceCalls int
	upsertTaskCalls    int
	upsertNodeCalls    int
	upsertSecretCalls  int
	upsertConfigCalls  int
	upsertNetworkCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		services: map[string]dbgen.UpsertCacheServiceParams{},
		tasks:    map[string]dbgen.UpsertCacheTaskParams{},
		nodes:    map[string]dbgen.UpsertCacheNodeParams{},
		secrets:  map[string]dbgen.UpsertCacheSecretParams{},
		configs:  map[string]dbgen.UpsertCacheConfigParams{},
		networks: map[string]dbgen.UpsertCacheNetworkParams{},
	}
}

func (s *fakeStore) UpsertService(ctx context.Context, arg dbgen.UpsertCacheServiceParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.services[arg.SwarmID] = arg
	s.upsertServiceCalls++
	return nil
}

func (s *fakeStore) DeleteCacheServiceBySwarmID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.services, id)
	return nil
}

func (s *fakeStore) DeleteMissingCacheServices(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for id := range s.services {
		if !keep[id] {
			delete(s.services, id)
		}
	}
	return nil
}

func (s *fakeStore) UpsertTask(ctx context.Context, arg dbgen.UpsertCacheTaskParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[arg.SwarmID] = arg
	s.upsertTaskCalls++
	return nil
}

func (s *fakeStore) DeleteCacheTaskBySwarmID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

func (s *fakeStore) DeleteMissingCacheTasks(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for id := range s.tasks {
		if !keep[id] {
			delete(s.tasks, id)
		}
	}
	return nil
}

func (s *fakeStore) UpsertNode(ctx context.Context, arg dbgen.UpsertCacheNodeParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[arg.SwarmID] = arg
	s.upsertNodeCalls++
	return nil
}

func (s *fakeStore) DeleteCacheNodeBySwarmID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.nodes, id)
	return nil
}

func (s *fakeStore) DeleteMissingCacheNodes(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for id := range s.nodes {
		if !keep[id] {
			delete(s.nodes, id)
		}
	}
	return nil
}

func (s *fakeStore) UpsertSecret(ctx context.Context, arg dbgen.UpsertCacheSecretParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secrets[arg.SwarmID] = arg
	s.upsertSecretCalls++
	return nil
}

func (s *fakeStore) DeleteCacheSecretBySwarmID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.secrets, id)
	return nil
}

func (s *fakeStore) DeleteMissingCacheSecrets(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for id := range s.secrets {
		if !keep[id] {
			delete(s.secrets, id)
		}
	}
	return nil
}

func (s *fakeStore) UpsertConfig(ctx context.Context, arg dbgen.UpsertCacheConfigParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[arg.SwarmID] = arg
	s.upsertConfigCalls++
	return nil
}

func (s *fakeStore) DeleteCacheConfigBySwarmID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.configs, id)
	return nil
}

func (s *fakeStore) DeleteMissingCacheConfigs(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for id := range s.configs {
		if !keep[id] {
			delete(s.configs, id)
		}
	}
	return nil
}

func (s *fakeStore) UpsertNetwork(ctx context.Context, arg dbgen.UpsertCacheNetworkParams) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networks[arg.SwarmID] = arg
	s.upsertNetworkCalls++
	return nil
}

func (s *fakeStore) DeleteCacheNetworkBySwarmID(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.networks, id)
	return nil
}

func (s *fakeStore) DeleteMissingCacheNetworks(ctx context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		keep[id] = true
	}
	for id := range s.networks {
		if !keep[id] {
			delete(s.networks, id)
		}
	}
	return nil
}

type recordingEmitter struct {
	mu    sync.Mutex
	calls []db.Notification
}

func (e *recordingEmitter) emit(ctx context.Context, channel, payload string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, db.Notification{Channel: channel, Payload: payload})
	return nil
}

func (e *recordingEmitter) snapshot() []db.Notification {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]db.Notification, len(e.calls))
	copy(out, e.calls)
	return out
}

func (e *recordingEmitter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

type fakeNotifier struct {
	ch chan db.Notification
}

func (f *fakeNotifier) Subscribe(channel string, size int) <-chan db.Notification {
	return f.ch
}

// --- helpers ---

func newTestWatcher(t *testing.T, src EventSource, lister Lister, fetch Fetcher, store CacheStore, em *recordingEmitter, notifs notifier) *Watcher {
	t.Helper()
	cli, err := swarmclient.New("unix:///nonexistent-hive-test.sock")
	if err != nil {
		t.Fatalf("construct swarm client: %v", err)
	}
	return &Watcher{
		swarm:          cli,
		pool:           nil, // domain pass is skipped without a pool
		source:         src,
		lister:         lister,
		fetch:          fetch,
		store:          store,
		emitter:        em.emit,
		notifs:         notifs,
		domains:        nil,
		resyncInterval: time.Hour,
		debounceWindow: 25 * time.Millisecond,
	}
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testService(id, name, appID string) swarm.Service {
	labels := map[string]string{}
	if appID != "" {
		labels[appIDLabel] = appID
	}
	return swarm.Service{
		ID:   id,
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: name, Labels: labels}},
	}
}

// --- tests ---

func TestServiceEventsMirrorCacheAndNotify(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	fetch := &fakeFetcher{}
	store := newFakeStore()
	em := &recordingEmitter{}

	const (
		svcID = "svc-abc"
		appID = "app-123"
	)
	svc := testService(svcID, "web", appID)
	fetch.mu.Lock()
	fetch.serviceFn = func(id string) (swarm.Service, error) {
		if id != svcID {
			return swarm.Service{}, fmt.Errorf("no such service: %w", errdefs.ErrNotFound)
		}
		return svc, nil
	}
	fetch.mu.Unlock()

	w := newTestWatcher(t, src, lister, fetch, store, em, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	select {
	case <-src.started:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher never subscribed to events")
	}

	// create
	src.dispatch(swarmclient.Event{Type: events.ServiceEventType, Action: events.ActionCreate, ID: svcID, Name: "web"})
	waitFor(t, 2*time.Second, "service row after create", func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.services[svcID].Name == "web"
	})

	// update
	src.dispatch(swarmclient.Event{Type: events.ServiceEventType, Action: events.ActionUpdate, ID: svcID, Name: "web"})
	waitFor(t, 2*time.Second, "second upsert after update", func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.upsertServiceCalls == 2
	})

	// remove
	fetch.mu.Lock()
	fetch.serviceFn = func(id string) (swarm.Service, error) {
		return swarm.Service{}, fmt.Errorf("removed: %w", errdefs.ErrNotFound)
	}
	fetch.mu.Unlock()
	src.dispatch(swarmclient.Event{Type: events.ServiceEventType, Action: events.ActionRemove, ID: svcID})
	waitFor(t, 2*time.Second, "row deleted after remove", func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.services) == 0
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}

	calls := em.snapshot()
	var sawService, sawServiceWithApp, sawDeployment bool
	for _, c := range calls {
		switch c.Channel {
		case "service:" + svcID:
			sawService = true
			if !strings.Contains(c.Payload, `"id":"`+svcID+`"`) {
				t.Errorf("service payload %s missing id", c.Payload)
			}
			// create/update payloads carry the app label; the remove
			// payload legitimately does not (the object is gone).
			if strings.Contains(c.Payload, `"action":"remove"`) {
				continue
			}
			if strings.Contains(c.Payload, `"app_id":"`+appID+`"`) {
				sawServiceWithApp = true
			}
		case "deployment:" + appID:
			sawDeployment = true
			if !strings.Contains(c.Payload, `"service_id":"`+svcID+`"`) {
				t.Errorf("deployment payload %s missing service_id", c.Payload)
			}
		}
	}
	if !sawServiceWithApp {
		t.Error("create/update service payload missing app_id")
	}
	if !sawService {
		t.Error("no notification emitted on service channel")
	}
	if !sawDeployment {
		t.Error("no notification emitted on deployment channel despite hive.app.id label")
	}
}

func TestResyncConvergesCacheAndEmitsSystem(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	store := newFakeStore()
	em := &recordingEmitter{}

	// Stale cache state from before the cluster changed.
	staleSvc := dbgen.UpsertCacheServiceParams{SwarmID: "svc-stale", Name: "old"}
	staleNode := dbgen.UpsertCacheNodeParams{SwarmID: "node-stale", Name: "old-node"}
	staleNet := dbgen.UpsertCacheNetworkParams{SwarmID: "net-stale", Name: "old-net"}
	if err := store.UpsertService(context.Background(), staleSvc); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNode(context.Background(), staleNode); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertNetwork(context.Background(), staleNet); err != nil {
		t.Fatal(err)
	}

	lister.set(
		[]swarm.Service{testService("svc-live", "web", "")},
		[]swarm.Task{{ID: "task-live", ServiceID: "svc-live", Slot: 1}},
		[]swarm.Node{{ID: "node-live", Description: swarm.NodeDescription{Hostname: "worker-1"}}},
		[]swarm.Secret{{ID: "secret-live", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "tok"}}}},
		[]swarm.Config{{ID: "config-live", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "cfg"}}}},
		[]network.Summary{{Network: network.Network{Name: "hive_internal", ID: "net-live"}}},
	)

	w := newTestWatcher(t, src, lister, &fakeFetcher{}, store, em, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	waitFor(t, 2*time.Second, "cache convergence", func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.services) == 1 && store.services["svc-live"].SwarmID == "svc-live" &&
			len(store.tasks) == 1 && store.tasks["task-live"].Name == "web.1" &&
			len(store.nodes) == 1 && store.nodes["node-live"].Name == "worker-1" &&
			len(store.secrets) == 1 && len(store.configs) == 1 &&
			len(store.networks) == 1 && store.networks["net-live"].Name == "hive_internal"
	})

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}

	found := false
	for _, c := range em.snapshot() {
		if c.Channel == channelSystem && strings.Contains(c.Payload, `"type":"resync"`) &&
			strings.Contains(c.Payload, `"services":1`) && strings.Contains(c.Payload, `"nodes":1`) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected resync completion NOTIFY on system channel, got %+v", em.snapshot())
	}
}

func TestDebounceCoalescesBursts(t *testing.T) {
	src := newFakeEventSource()
	lister := &fakeLister{}
	fetch := &fakeFetcher{}
	store := newFakeStore()
	em := &recordingEmitter{}

	const svcID = "svc-burst"
	svc := testService(svcID, "bursty", "")
	fetch.mu.Lock()
	fetch.serviceFn = func(id string) (swarm.Service, error) { return svc, nil }
	fetch.mu.Unlock()

	w := newTestWatcher(t, src, lister, fetch, store, em, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	<-src.started
	for range 5 {
		src.dispatch(swarmclient.Event{Type: events.ServiceEventType, Action: events.ActionUpdate, ID: svcID, Name: "bursty"})
	}

	waitFor(t, 2*time.Second, "coalesced refresh", func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.upsertServiceCalls == 1
	})
	// Well past the debounce window: no further refreshes may appear.
	time.Sleep(10 * w.debounceWindow)
	store.mu.Lock()
	calls := store.upsertServiceCalls
	store.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected exactly 1 debounced refresh, got %d", calls)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestReconcilePassEmitsNoNotifications(t *testing.T) {
	w := &Watcher{pool: nil, domains: nil, emitter: (&recordingEmitter{}).emit}

	// Direct call: the domain reconcile pass must never emit.
	w.reconcileDomains(context.Background())

	// A system notification triggers a domain pass inside Run; it must not
	// produce any emissions either (loop guard).
	em := &recordingEmitter{}
	src := newFakeEventSource()
	lister := &fakeLister{}
	store := newFakeStore()
	notifyCh := make(chan db.Notification, 8)
	w2 := newTestWatcher(t, src, lister, &fakeFetcher{}, store, em, &fakeNotifier{ch: notifyCh})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w2.Run(ctx) }()
	<-src.started

	// The initial resync emits its completion NOTIFY; wait for it so the
	// baseline reflects a settled watcher.
	waitFor(t, 2*time.Second, "initial resync notification", func() bool { return em.count() > 0 })
	baseline := em.count()
	notifyCh <- db.Notification{Channel: channelSystem, Payload: `{"type":"domains_changed"}`}
	time.Sleep(5 * w2.debounceWindow)
	if got := em.count(); got != baseline {
		t.Errorf("reconcile pass emitted %d notifications; must emit none", got-baseline)
	}

	// Node events notify on system exactly once each — no cascade.
	node := swarm.Node{ID: "node-1", Description: swarm.NodeDescription{Hostname: "n1"}}
	fetch := &fakeFetcher{}
	fetch.mu.Lock()
	fetch.nodeFn = func(id string) (swarm.Node, error) { return node, nil }
	fetch.mu.Unlock()
	w2.fetch = fetch
	src.dispatch(swarmclient.Event{Type: events.NodeEventType, Action: events.ActionUpdate, ID: "node-1"})
	waitFor(t, 2*time.Second, "node cached", func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.nodes) == 1
	})
	time.Sleep(5 * w2.debounceWindow)
	calls := em.snapshot()
	if len(calls)-baseline != 1 {
		t.Fatalf("expected exactly 1 system notification from node event, got %d (%+v)", len(calls)-baseline, calls)
	}
	if calls[len(calls)-1].Channel != channelSystem || !strings.Contains(calls[len(calls)-1].Payload, `"type":"node"`) {
		t.Errorf("unexpected notification %+v", calls[len(calls)-1])
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestWatcherStopsCleanlyOnContextCancel(t *testing.T) {
	src := newFakeEventSource()
	w := newTestWatcher(t, src, &fakeLister{}, &fakeFetcher{}, newFakeStore(), &recordingEmitter{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	<-src.started
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after leader context cancellation")
	}
}

// recordingApplier captures ApplyDomain calls made by the domain reconcile
// pass so tests can assert which routes reach the proxy layer.
type recordingApplier struct {
	mu    sync.Mutex
	calls []appliedRoute
}

type appliedRoute struct {
	serviceID  string
	routerName string
	route      proxy.Route
	port       int
}

func (r *recordingApplier) ApplyDomain(ctx context.Context, serviceID, routerName string, route proxy.Route, port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, appliedRoute{serviceID, routerName, route, port})
	return nil
}

// TestApplyDomainRoutesPassesRoutes verifies the watcher forwards the full
// routing configuration (route type, prefix, strip flag, priority) per
// application service, deriving router names from the host.
func TestApplyDomainRoutesPassesRoutes(t *testing.T) {
	lister := &fakeLister{}
	// An unparseable app id keeps WithAppLock on its unlocked fast path so
	// the test needs no database.
	lister.set([]swarm.Service{testService("svc-1", "app", "not-a-uuid")}, nil, nil, nil, nil, nil)
	applier := &recordingApplier{}
	w := &Watcher{lister: lister, domains: applier, pool: nil}

	domainsByApp := map[string][]proxy.Route{
		"not-a-uuid": {
			{Host: "app.example.com", RouteType: proxy.RouteTypeHost, TLSEnabled: true},
			{Host: "*.example.com", RouteType: proxy.RouteTypeWildcard},
			{Host: "api.example.com", RouteType: proxy.RouteTypePath, PathPrefix: "/api", StripPrefix: true, Priority: 20},
		},
	}
	w.applyDomainRoutes(context.Background(), domainsByApp)

	applier.mu.Lock()
	defer applier.mu.Unlock()
	if len(applier.calls) != 3 {
		t.Fatalf("got %d ApplyDomain calls, want 3: %+v", len(applier.calls), applier.calls)
	}
	wantRouters := []string{"app-app-example-com", "app-example-com", "app-api-example-com"}
	for i, call := range applier.calls {
		if call.serviceID != "svc-1" || call.port != appDomainPort {
			t.Fatalf("call %d = %+v, want service svc-1 port %d", i, call, appDomainPort)
		}
		if call.routerName != wantRouters[i] {
			t.Fatalf("call %d router=%q want %q", i, call.routerName, wantRouters[i])
		}
	}
	if got := applier.calls[2].route; got.PathPrefix != "/api" || !got.StripPrefix || got.Priority != 20 || got.RouteType != proxy.RouteTypePath {
		t.Fatalf("path route not carried through: %+v", got)
	}
	if got := applier.calls[0].route; got.TLSEnabled != true {
		t.Fatalf("tls flag not carried through: %+v", got)
	}
}

// TestApplyDomainRoutesSkipsServicesWithoutDomains ensures apps without any
// configured domain never get routing labels re-applied.
func TestApplyDomainRoutesSkipsServicesWithoutDomains(t *testing.T) {
	lister := &fakeLister{}
	lister.set([]swarm.Service{
		testService("svc-1", "app", "not-a-uuid"),
	}, nil, nil, nil, nil, nil)
	applier := &recordingApplier{}
	w := &Watcher{lister: lister, domains: applier, pool: nil}

	w.applyDomainRoutes(context.Background(), map[string][]proxy.Route{})

	applier.mu.Lock()
	defer applier.mu.Unlock()
	if len(applier.calls) != 0 {
		t.Fatalf("expected no ApplyDomain calls, got %+v", applier.calls)
	}
}
