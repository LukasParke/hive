// Package reconcile keeps the control plane's view of the Swarm cluster
// warm: a long-running watcher mirrors every service, task, node, secret,
// config and network into the swarm_cache_* tables (event-driven with
// periodic full resyncs) and re-applies domain routing for applications.
package reconcile

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/db"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/swarm"
	"log/slog"
	"sync/atomic"
	"time"
)

const (
	// resyncInterval is how often the full cache resync runs. Events keep
	// the cache fresh in between; nothing polls faster than this.
	resyncInterval = 5 * time.Minute

	// debounceWindow coalesces bursts of events for the same entity into a
	// single refresh.
	debounceWindow = 500 * time.Millisecond

	// eventQueueSize bounds the in-memory event queue; when it overflows
	// the watcher falls back to a full resync instead of dropping changes.
	eventQueueSize = 1024

	// Special debounce keys that are not "<type>:<id>" event keys.
	keyResync  = "resync"
	keyDomains = "domains"

	// appDomainPort is the container port domain routers forward to.
	appDomainPort = 3000
)

// Watcher reconciles swarm state and emits change notifications.
type Watcher struct {
	// swarm is the concrete client used for single-object fetches and the
	// proxy.DomainManager wiring.
	swarm *swarmclient.Client
	pool  *pgxpool.Pool

	source  EventSource
	lister  Lister
	fetch   Fetcher
	store   CacheStore
	emitter EmitFunc
	notifs  notifier // nil disables "system" NOTIFY triggered domain reconciles

	domains domainApplier

	resyncInterval time.Duration
	debounceWindow time.Duration
}

// notifier is the slice of db.Fanout the watcher needs to observe "system"
// notifications emitted by other parts of the control plane (domain table
// changes among them).
type notifier interface {
	Subscribe(channel string, size int) <-chan db.Notification
}

// NewWatcher builds the leader-side cluster watcher. Run blocks until the
// elector's leader context is cancelled.
func NewWatcher(s *swarmclient.Client, fanout *db.Fanout, pool *pgxpool.Pool) *Watcher {
	w := &Watcher{
		swarm:          s,
		pool:           pool,
		source:         s,
		lister:         s,
		fetch:          s,
		store:          newCacheStore(pool),
		domains:        proxy.NewDomainManager(s),
		resyncInterval: resyncInterval,
		debounceWindow: debounceWindow,
	}
	if fanout != nil {
		w.emitter = fanout.Emit
		w.notifs = fanout
	}
	return w
}

// Run blocks until ctx is cancelled (the elector's leader context). It
// subscribes to swarm events, resyncs the cache on start and every
// resyncInterval, and re-applies domain routing on start, on service events,
// and whenever a system notification arrives. The domain reconcile pass
// itself never emits notifications, so notification-triggered passes cannot
// loop.
func (w *Watcher) Run(ctx context.Context) error {
	slog.InfoContext(ctx, "reconcile watcher started")

	eventCh := make(chan swarmclient.Event, eventQueueSize)
	var overflow atomic.Bool
	push := func(ev swarmclient.Event) error {
		select {
		case eventCh <- ev:
		default:
			overflow.Store(true)
		}
		return nil // never kill the subscription from our side
	}
	pumpDone := make(chan error, 1)
	go func() {
		pumpDone <- w.source.Events(ctx, swarmclient.EventHandler{
			OnService: push,
			OnNode:    push,
			OnSecret:  push,
			OnConfig:  push,
			OnNetwork: push,
		})
	}()

	var notifyCh <-chan db.Notification
	if w.notifs != nil {
		notifyCh = w.notifs.Subscribe(channelSystem, 32)
	}

	due := make(chan string, 64)
	pending := make(map[string]*time.Timer)
	lastEvent := make(map[string]swarmclient.Event)
	defer func() {
		for _, t := range pending {
			t.Stop()
		}
	}()
	schedule := func(key string) {
		if _, ok := pending[key]; ok {
			return // already debouncing this key
		}
		pending[key] = time.AfterFunc(w.debounceWindow, func() {
			select {
			case due <- key:
			default:
			}
		})
	}
	scheduleEvent := func(ev swarmclient.Event) {
		key := fmt.Sprintf("%s:%s", ev.Type, ev.ID)
		lastEvent[key] = ev // latest wins while debounced
		schedule(key)
	}

	ticker := time.NewTicker(w.resyncInterval)
	defer ticker.Stop()

	if counts, err := w.resync(ctx); err != nil {
		slog.ErrorContext(ctx, "initial swarm cache resync failed", "error", err)
	} else {
		w.emitResyncDone(ctx, counts)
		w.reconcileDomains(ctx)
	}

	for {
		if overflow.CompareAndSwap(true, false) {
			schedule(keyResync)
		}
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "reconcile watcher stopped")
			return nil

		case err := <-pumpDone:
			if ctx.Err() == nil && err != nil {
				slog.ErrorContext(ctx, "swarm event stream ended; relying on periodic resync", "error", err)
			}
			pumpDone = nil // stream is dead; periodic resync still converges

		case <-ticker.C:
			counts, err := w.resync(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "swarm cache resync failed", "error", err)
				continue
			}
			w.emitResyncDone(ctx, counts)
			w.reconcileDomains(ctx)

		case n, ok := <-notifyCh:
			if !ok {
				notifyCh = nil
				continue
			}
			if n.Channel == channelSystem {
				// Domain rows changed elsewhere in the control plane;
				// re-apply routing. The pass itself emits nothing.
				schedule(keyDomains)
			}

		case ev := <-eventCh:
			scheduleEvent(ev)

		case key := <-due:
			delete(pending, key)
			switch key {
			case keyResync:
				counts, err := w.resync(ctx)
				if err != nil {
					slog.ErrorContext(ctx, "swarm cache resync failed", "error", err)
					continue
				}
				w.emitResyncDone(ctx, counts)
			case keyDomains:
				w.reconcileDomains(ctx)
			default:
				ev := lastEvent[key]
				delete(lastEvent, key)
				w.refreshEntity(ctx, ev)
				if ev.Type == events.ServiceEventType {
					// Routing targets may have appeared or disappeared.
					schedule(keyDomains)
				}
			}
		}
	}
}

func (w *Watcher) emitResyncDone(ctx context.Context, counts map[string]int) {
	payload := mustJSON(resyncEvent{Type: "resync", Counts: counts})
	if err := w.emitter(ctx, channelSystem, payload); err != nil {
		slog.ErrorContext(ctx, "notify resync completion", "error", err)
	}
}

// serviceCacheStatus captures the runtime status worth caching for a service.
type serviceCacheStatus struct {
	Update *swarm.UpdateStatus  `json:"update,omitempty"`
	Tasks  *swarm.ServiceStatus `json:"tasks,omitempty"`
}

// resync upserts every live object into the cache and removes cached rows
// whose Swarm object no longer exists, so the cache converges even after
// missed events. It returns per-kind counts of upserted objects.
func (w *Watcher) resync(ctx context.Context) (map[string]int, error) {
	svcs, err := w.lister.ListServices(ctx)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	tasks, err := w.lister.ListAllTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	nodes, err := w.lister.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	secrets, err := w.lister.ListSecrets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	configs, err := w.lister.ListConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list configs: %w", err)
	}
	nets, err := w.lister.ListNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}

	serviceNames := make(map[string]string, len(svcs))
	serviceIDs := make([]string, 0, len(svcs))
	for i := range svcs {
		spec, mErr := marshalJSON(svcs[i].Spec)
		if mErr != nil {
			return nil, fmt.Errorf("marshal service %s spec: %w", svcs[i].ID, mErr)
		}
		status, mErr := marshalJSON(serviceCacheStatus{Update: svcs[i].UpdateStatus, Tasks: svcs[i].ServiceStatus})
		if mErr != nil {
			return nil, fmt.Errorf("marshal service %s status: %w", svcs[i].ID, mErr)
		}
		if err := w.store.UpsertService(ctx, dbgen.UpsertCacheServiceParams{
			SwarmID: svcs[i].ID,
			Name:    svcs[i].Spec.Name,
			Spec:    spec,
			Status:  status,
		}); err != nil {
			return nil, fmt.Errorf("upsert cached service %s: %w", svcs[i].ID, err)
		}
		serviceNames[svcs[i].ID] = svcs[i].Spec.Name
		serviceIDs = append(serviceIDs, svcs[i].ID)
	}
	if err := w.store.DeleteMissingCacheServices(ctx, serviceIDs); err != nil {
		return nil, fmt.Errorf("delete missing cached services: %w", err)
	}

	taskIDs := make([]string, 0, len(tasks))
	for i := range tasks {
		spec, mErr := marshalJSON(tasks[i].Spec)
		if mErr != nil {
			return nil, fmt.Errorf("marshal task %s spec: %w", tasks[i].ID, mErr)
		}
		status, mErr := marshalJSON(tasks[i].Status)
		if mErr != nil {
			return nil, fmt.Errorf("marshal task %s status: %w", tasks[i].ID, mErr)
		}
		name := fmt.Sprintf("%s.%d", tasks[i].ServiceID, tasks[i].Slot)
		if svcName, ok := serviceNames[tasks[i].ServiceID]; ok && svcName != "" {
			name = fmt.Sprintf("%s.%d", svcName, tasks[i].Slot)
		}
		if err := w.store.UpsertTask(ctx, dbgen.UpsertCacheTaskParams{
			SwarmID: tasks[i].ID,
			Name:    name,
			Spec:    spec,
			Status:  status,
		}); err != nil {
			return nil, fmt.Errorf("upsert cached task %s: %w", tasks[i].ID, err)
		}
		taskIDs = append(taskIDs, tasks[i].ID)
	}
	if err := w.store.DeleteMissingCacheTasks(ctx, taskIDs); err != nil {
		return nil, fmt.Errorf("delete missing cached tasks: %w", err)
	}

	nodeIDs := make([]string, 0, len(nodes))
	for i := range nodes {
		spec, mErr := marshalJSON(nodes[i].Spec)
		if mErr != nil {
			return nil, fmt.Errorf("marshal node %s spec: %w", nodes[i].ID, mErr)
		}
		status, mErr := marshalJSON(nodes[i].Status)
		if mErr != nil {
			return nil, fmt.Errorf("marshal node %s status: %w", nodes[i].ID, mErr)
		}
		if err := w.store.UpsertNode(ctx, dbgen.UpsertCacheNodeParams{
			SwarmID: nodes[i].ID,
			Name:    nodes[i].Description.Hostname,
			Spec:    spec,
			Status:  status,
		}); err != nil {
			return nil, fmt.Errorf("upsert cached node %s: %w", nodes[i].ID, err)
		}
		nodeIDs = append(nodeIDs, nodes[i].ID)
	}
	if err := w.store.DeleteMissingCacheNodes(ctx, nodeIDs); err != nil {
		return nil, fmt.Errorf("delete missing cached nodes: %w", err)
	}

	secretIDs := make([]string, 0, len(secrets))
	for i := range secrets {
		spec, mErr := marshalJSON(secrets[i].Spec)
		if mErr != nil {
			return nil, fmt.Errorf("marshal secret %s spec: %w", secrets[i].ID, mErr)
		}
		if err := w.store.UpsertSecret(ctx, dbgen.UpsertCacheSecretParams{
			SwarmID: secrets[i].ID,
			Name:    secrets[i].Spec.Name,
			Spec:    spec,
		}); err != nil {
			return nil, fmt.Errorf("upsert cached secret %s: %w", secrets[i].ID, err)
		}
		secretIDs = append(secretIDs, secrets[i].ID)
	}
	if err := w.store.DeleteMissingCacheSecrets(ctx, secretIDs); err != nil {
		return nil, fmt.Errorf("delete missing cached secrets: %w", err)
	}

	configIDs := make([]string, 0, len(configs))
	for i := range configs {
		spec, mErr := marshalJSON(configs[i].Spec)
		if mErr != nil {
			return nil, fmt.Errorf("marshal config %s spec: %w", configs[i].ID, mErr)
		}
		if err := w.store.UpsertConfig(ctx, dbgen.UpsertCacheConfigParams{
			SwarmID: configs[i].ID,
			Name:    configs[i].Spec.Name,
			Spec:    spec,
		}); err != nil {
			return nil, fmt.Errorf("upsert cached config %s: %w", configs[i].ID, err)
		}
		configIDs = append(configIDs, configs[i].ID)
	}
	if err := w.store.DeleteMissingCacheConfigs(ctx, configIDs); err != nil {
		return nil, fmt.Errorf("delete missing cached configs: %w", err)
	}

	netIDs := make([]string, 0, len(nets))
	for i := range nets {
		spec, mErr := marshalJSON(nets[i])
		if mErr != nil {
			return nil, fmt.Errorf("marshal network %s: %w", nets[i].ID, mErr)
		}
		if err := w.store.UpsertNetwork(ctx, dbgen.UpsertCacheNetworkParams{
			SwarmID: nets[i].ID,
			Name:    nets[i].Name,
			Spec:    spec,
		}); err != nil {
			return nil, fmt.Errorf("upsert cached network %s: %w", nets[i].ID, err)
		}
		netIDs = append(netIDs, nets[i].ID)
	}
	if err := w.store.DeleteMissingCacheNetworks(ctx, netIDs); err != nil {
		return nil, fmt.Errorf("delete missing cached networks: %w", err)
	}

	return map[string]int{
		"services": len(svcs),
		"tasks":    len(tasks),
		"nodes":    len(nodes),
		"secrets":  len(secrets),
		"configs":  len(configs),
		"networks": len(nets),
	}, nil
}

// domainApplier is the slice of proxy.DomainManager the watcher needs.
type domainApplier interface {
	ApplyDomain(ctx context.Context, serviceID, routerName string, r proxy.Route, port int) error
}

// reconcileDomains re-applies domain routing for every application that has
// domains configured. It performs NO fanout emissions by design: it runs on
// start, after resyncs, on service events, and on every "system"
// notification — emitting from here would create a NOTIFY feedback loop.

func (w *Watcher) reconcileDomains(ctx context.Context) {
	if w.pool == nil || w.domains == nil {
		return
	}
	domainsByApp := map[string][]proxy.Route{}
	rows, err := w.pool.Query(ctx, `
		select application_id::text, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority
		from domains`)
	if err != nil {
		slog.ErrorContext(ctx, "query domains for reconciliation", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var appID string
		var route proxy.Route
		var priority *int
		if err := rows.Scan(&appID, &route.Host, &route.TLSEnabled, &route.RouteType,
			&route.PathPrefix, &route.StripPrefix, &priority); err != nil {
			slog.ErrorContext(ctx, "scan domain row", "error", err)
			continue
		}
		if priority != nil {
			route.Priority = *priority
		}
		domainsByApp[appID] = append(domainsByApp[appID], route)
	}
	w.applyDomainRoutes(ctx, domainsByApp)
}

// applyDomainRoutes re-applies routing labels for every application in the
// given domain map that currently has a running service.
func (w *Watcher) applyDomainRoutes(ctx context.Context, domainsByApp map[string][]proxy.Route) {
	services, err := w.lister.ListServices(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "list services for domain reconciliation", "error", err)
		return
	}
	for _, svc := range services {
		appID := svc.Spec.Labels[appIDLabel]
		if appID == "" {
			continue
		}
		routes := domainsByApp[appID]
		if len(routes) == 0 {
			continue
		}
		// Same lock deploys take: ApplyDomain is a full-spec
		// read-modify-write and must not interleave with them.
		lockErr := deploy.WithAppLock(ctx, w.pool, appID, func(ctx context.Context) error {
			for _, route := range routes {
				routerName := proxy.RouterNameFromHost(route.Host)
				if err := w.domains.ApplyDomain(ctx, svc.ID, routerName, route, appDomainPort); err != nil {
					slog.ErrorContext(ctx, "apply domain routing", "service", svc.ID, "host", route.Host, "error", err)
				}
			}
			return nil
		})
		if lockErr != nil {
			slog.ErrorContext(ctx, "app lock for domain reconciliation", "app", appID, "error", lockErr)
		}
	}
}
