package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/containerd/errdefs"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
)

const (
	// channelSystem carries control-plane-wide notifications (node, secret,
	// config, network changes and resync completions).
	channelSystem = "system"
	// appIDLabel marks a Swarm service as belonging to a hive application.
	appIDLabel = "hive.app.id"
)

// EventSource subscribes to swarm-scoped cluster events. *swarmclient.Client
// implements it; tests inject a fake stream.
type EventSource interface {
	Events(ctx context.Context, handler swarmclient.EventHandler) error
}

// Fetcher reads single objects for event-driven cache refreshes.
type Fetcher interface {
	GetService(ctx context.Context, id string) (swarm.Service, error)
	GetNode(ctx context.Context, nodeID string) (swarm.Node, error)
	GetSecret(ctx context.Context, id string) (swarm.Secret, error)
	GetConfig(ctx context.Context, id string) (swarm.Config, error)
}

// Lister reads the full cluster state for cache resyncs.
type Lister interface {
	ListServices(ctx context.Context) ([]swarm.Service, error)
	ListAllTasks(ctx context.Context) ([]swarm.Task, error)
	ListNodes(ctx context.Context) ([]swarm.Node, error)
	ListSecrets(ctx context.Context) ([]swarm.Secret, error)
	ListConfigs(ctx context.Context) ([]swarm.Config, error)
	ListNetworks(ctx context.Context) ([]network.Summary, error)
}

// CacheStore mirrors Swarm objects into the swarm_cache_* tables.
type CacheStore interface {
	UpsertService(ctx context.Context, arg dbgen.UpsertCacheServiceParams) error
	DeleteCacheServiceBySwarmID(ctx context.Context, swarmID string) error
	DeleteMissingCacheServices(ctx context.Context, ids []string) error

	UpsertTask(ctx context.Context, arg dbgen.UpsertCacheTaskParams) error
	DeleteCacheTaskBySwarmID(ctx context.Context, swarmID string) error
	DeleteMissingCacheTasks(ctx context.Context, ids []string) error

	UpsertNode(ctx context.Context, arg dbgen.UpsertCacheNodeParams) error
	DeleteCacheNodeBySwarmID(ctx context.Context, swarmID string) error
	DeleteMissingCacheNodes(ctx context.Context, ids []string) error

	UpsertSecret(ctx context.Context, arg dbgen.UpsertCacheSecretParams) error
	DeleteCacheSecretBySwarmID(ctx context.Context, swarmID string) error
	DeleteMissingCacheSecrets(ctx context.Context, ids []string) error

	UpsertConfig(ctx context.Context, arg dbgen.UpsertCacheConfigParams) error
	DeleteCacheConfigBySwarmID(ctx context.Context, swarmID string) error
	DeleteMissingCacheConfigs(ctx context.Context, ids []string) error

	UpsertNetwork(ctx context.Context, arg dbgen.UpsertCacheNetworkParams) error
	DeleteCacheNetworkBySwarmID(ctx context.Context, swarmID string) error
	DeleteMissingCacheNetworks(ctx context.Context, ids []string) error
}

// EmitFunc publishes a NOTIFY payload on a fanout channel. *db.Fanout.Emit
// matches this signature.
type EmitFunc func(ctx context.Context, channel, payload string) error

// cacheStore adapts the sqlc-generated queries to CacheStore.
type cacheStore struct {
	q *dbgen.Queries
}

func newCacheStore(pool *pgxpool.Pool) CacheStore {
	return cacheStore{q: dbgen.New(pool)}
}

// UpsertService caches a service snapshot.
func (s cacheStore) UpsertService(ctx context.Context, arg dbgen.UpsertCacheServiceParams) error {
	return s.q.UpsertCacheService(ctx, arg)
}

// DeleteCacheServiceBySwarmID removes a cached service by swarm ID.
func (s cacheStore) DeleteCacheServiceBySwarmID(ctx context.Context, swarmID string) error {
	return s.q.DeleteCacheServiceBySwarmID(ctx, swarmID)
}

// DeleteMissingCacheServices drops cached services absent from keep.
func (s cacheStore) DeleteMissingCacheServices(ctx context.Context, ids []string) error {
	return s.q.DeleteMissingCacheServices(ctx, ids)
}

// UpsertTask caches a task snapshot.
func (s cacheStore) UpsertTask(ctx context.Context, arg dbgen.UpsertCacheTaskParams) error {
	return s.q.UpsertCacheTask(ctx, arg)
}

// DeleteCacheTaskBySwarmID removes a cached task by swarm ID.
func (s cacheStore) DeleteCacheTaskBySwarmID(ctx context.Context, swarmID string) error {
	return s.q.DeleteCacheTaskBySwarmID(ctx, swarmID)
}

// DeleteMissingCacheTasks drops cached tasks absent from keep.
func (s cacheStore) DeleteMissingCacheTasks(ctx context.Context, ids []string) error {
	return s.q.DeleteMissingCacheTasks(ctx, ids)
}

// UpsertNode caches a node snapshot.
func (s cacheStore) UpsertNode(ctx context.Context, arg dbgen.UpsertCacheNodeParams) error {
	return s.q.UpsertCacheNode(ctx, arg)
}

// DeleteCacheNodeBySwarmID removes a cached node by swarm ID.
func (s cacheStore) DeleteCacheNodeBySwarmID(ctx context.Context, swarmID string) error {
	return s.q.DeleteCacheNodeBySwarmID(ctx, swarmID)
}

// DeleteMissingCacheNodes drops cached nodes absent from keep.
func (s cacheStore) DeleteMissingCacheNodes(ctx context.Context, ids []string) error {
	return s.q.DeleteMissingCacheNodes(ctx, ids)
}

// UpsertSecret caches a secret snapshot.
func (s cacheStore) UpsertSecret(ctx context.Context, arg dbgen.UpsertCacheSecretParams) error {
	return s.q.UpsertCacheSecret(ctx, arg)
}

// DeleteCacheSecretBySwarmID removes a cached secret by swarm ID.
func (s cacheStore) DeleteCacheSecretBySwarmID(ctx context.Context, swarmID string) error {
	return s.q.DeleteCacheSecretBySwarmID(ctx, swarmID)
}

// DeleteMissingCacheSecrets drops cached secrets absent from keep.
func (s cacheStore) DeleteMissingCacheSecrets(ctx context.Context, ids []string) error {
	return s.q.DeleteMissingCacheSecrets(ctx, ids)
}

// UpsertConfig caches a config snapshot.
func (s cacheStore) UpsertConfig(ctx context.Context, arg dbgen.UpsertCacheConfigParams) error {
	return s.q.UpsertCacheConfig(ctx, arg)
}

// DeleteCacheConfigBySwarmID removes a cached config by swarm ID.
func (s cacheStore) DeleteCacheConfigBySwarmID(ctx context.Context, swarmID string) error {
	return s.q.DeleteCacheConfigBySwarmID(ctx, swarmID)
}

// DeleteMissingCacheConfigs drops cached configs absent from keep.
func (s cacheStore) DeleteMissingCacheConfigs(ctx context.Context, ids []string) error {
	return s.q.DeleteMissingCacheConfigs(ctx, ids)
}

// UpsertNetwork caches a network snapshot.
func (s cacheStore) UpsertNetwork(ctx context.Context, arg dbgen.UpsertCacheNetworkParams) error {
	return s.q.UpsertCacheNetwork(ctx, arg)
}

// DeleteCacheNetworkBySwarmID removes a cached network by swarm ID.
func (s cacheStore) DeleteCacheNetworkBySwarmID(ctx context.Context, swarmID string) error {
	return s.q.DeleteCacheNetworkBySwarmID(ctx, swarmID)
}

// DeleteMissingCacheNetworks drops cached networks absent from keep.
func (s cacheStore) DeleteMissingCacheNetworks(ctx context.Context, ids []string) error {
	return s.q.DeleteMissingCacheNetworks(ctx, ids)
}

// changeEvent is the JSON NOTIFY payload shape emitted on fanout channels.
type changeEvent struct {
	Type      string `json:"type"`
	Action    string `json:"action"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	AppID     string `json:"app_id,omitempty"`
	ServiceID string `json:"service_id,omitempty"`
}

// resyncEvent is the JSON NOTIFY payload emitted on "system" after a full
// cache resync converges.
type resyncEvent struct {
	Type   string         `json:"type"`
	Counts map[string]int `json:"counts"`
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// marshalJSON encodes cache snapshots; a package variable so tests can
// inject encode failures for the otherwise unreachable error branches.
var marshalJSON = json.Marshal

// refreshEntity re-fetches one object from the cluster after a debounced
// event burst and mirrors it into the cache. Objects that no longer exist
// are removed from the cache. Every successful refresh emits the matching
// fanout notification(s): service changes go to service:{id} (plus
// deployment:{appID} when the service carries the hive.app.id label), all
// other object kinds go to system.
func (w *Watcher) refreshEntity(ctx context.Context, ev swarmclient.Event) {
	if ctx.Err() != nil {
		return
	}
	var err error
	switch ev.Type {
	case events.ServiceEventType:
		err = w.refreshService(ctx, ev)
	case events.NodeEventType:
		err = w.refreshNode(ctx, ev)
	case events.SecretEventType:
		err = w.refreshSecret(ctx, ev)
	case events.ConfigEventType:
		err = w.refreshConfig(ctx, ev)
	case events.NetworkEventType:
		err = w.refreshNetwork(ctx, ev)
	default:
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "reconcile event refresh failed", "type", ev.Type, "action", string(ev.Action), "id", ev.ID, "error", err)
	}
}

func (w *Watcher) refreshService(ctx context.Context, ev swarmclient.Event) error {
	svc, err := w.fetch.GetService(ctx, ev.ID)
	if errdefs.IsNotFound(err) {
		if err := w.store.DeleteCacheServiceBySwarmID(ctx, ev.ID); err != nil {
			return fmt.Errorf("delete cached service %s: %w", ev.ID, err)
		}
		return w.emitServiceChange(ctx, string(ev.Action), ev.ID, ev.Name, "")
	} else if err != nil {
		// Transient failure: leave the cached row untouched, the periodic
		// resync converges it.
		return fmt.Errorf("get service %s: %w", ev.ID, err)
	}
	spec, err := marshalJSON(svc.Spec)
	if err != nil {
		return fmt.Errorf("marshal service %s spec: %w", svc.ID, err)
	}
	status, err := marshalJSON(serviceCacheStatus{Update: svc.UpdateStatus, Tasks: svc.ServiceStatus})
	if err != nil {
		return fmt.Errorf("marshal service %s status: %w", svc.ID, err)
	}
	if err := w.store.UpsertService(ctx, dbgen.UpsertCacheServiceParams{
		SwarmID: svc.ID,
		Name:    svc.Spec.Name,
		Spec:    spec,
		Status:  status,
	}); err != nil {
		return fmt.Errorf("upsert cached service %s: %w", svc.ID, err)
	}
	return w.emitServiceChange(ctx, string(ev.Action), svc.ID, svc.Spec.Name, svc.Spec.Labels[appIDLabel])
}

// emitServiceChange notifies the service channel and, when the service is
// bound to an application, the deployment channel too.
func (w *Watcher) emitServiceChange(ctx context.Context, action, serviceID, name, appID string) error {
	payload := mustJSON(changeEvent{Type: "service", Action: action, ID: serviceID, Name: name, AppID: appID})
	if err := w.emitter(ctx, "service:"+serviceID, payload); err != nil {
		return fmt.Errorf("notify service %s change: %w", serviceID, err)
	}
	if appID != "" {
		payload := mustJSON(changeEvent{Type: "deployment", Action: action, ServiceID: serviceID, AppID: appID})
		if err := w.emitter(ctx, "deployment:"+appID, payload); err != nil {
			return fmt.Errorf("notify deployment %s change: %w", appID, err)
		}
	}
	return nil
}

func (w *Watcher) refreshNode(ctx context.Context, ev swarmclient.Event) error {
	node, err := w.fetch.GetNode(ctx, ev.ID)
	if errdefs.IsNotFound(err) {
		if err := w.store.DeleteCacheNodeBySwarmID(ctx, ev.ID); err != nil {
			return fmt.Errorf("delete cached node %s: %w", ev.ID, err)
		}
		return w.emitObjectChange(ctx, "node", string(ev.Action), ev.ID, ev.Name)
	} else if err != nil {
		return fmt.Errorf("get node %s: %w", ev.ID, err)
	}
	spec, err := marshalJSON(node.Spec)
	if err != nil {
		return fmt.Errorf("marshal node %s spec: %w", node.ID, err)
	}
	status, err := marshalJSON(node.Status)
	if err != nil {
		return fmt.Errorf("marshal node %s status: %w", node.ID, err)
	}
	if err := w.store.UpsertNode(ctx, dbgen.UpsertCacheNodeParams{
		SwarmID: node.ID,
		Name:    node.Description.Hostname,
		Spec:    spec,
		Status:  status,
	}); err != nil {
		return fmt.Errorf("upsert cached node %s: %w", node.ID, err)
	}
	return w.emitObjectChange(ctx, "node", string(ev.Action), node.ID, node.Description.Hostname)
}

// emitObjectChange notifies the system channel for non-service objects.
func (w *Watcher) emitObjectChange(ctx context.Context, kind, action, id, name string) error {
	payload := mustJSON(changeEvent{Type: kind, Action: action, ID: id, Name: name})
	if err := w.emitter(ctx, channelSystem, payload); err != nil {
		return fmt.Errorf("notify %s change: %w", kind, err)
	}
	return nil
}

func (w *Watcher) refreshSecret(ctx context.Context, ev swarmclient.Event) error {
	secret, err := w.fetch.GetSecret(ctx, ev.ID)
	if errdefs.IsNotFound(err) {
		if err := w.store.DeleteCacheSecretBySwarmID(ctx, ev.ID); err != nil {
			return fmt.Errorf("delete cached secret %s: %w", ev.ID, err)
		}
		return w.emitObjectChange(ctx, "secret", string(ev.Action), ev.ID, ev.Name)
	} else if err != nil {
		return fmt.Errorf("get secret %s: %w", ev.ID, err)
	}
	spec, err := marshalJSON(secret.Spec)
	if err != nil {
		return fmt.Errorf("marshal secret %s spec: %w", secret.ID, err)
	}
	if err := w.store.UpsertSecret(ctx, dbgen.UpsertCacheSecretParams{
		SwarmID: secret.ID,
		Name:    secret.Spec.Name,
		Spec:    spec,
	}); err != nil {
		return fmt.Errorf("upsert cached secret %s: %w", secret.ID, err)
	}
	return w.emitObjectChange(ctx, "secret", string(ev.Action), secret.ID, secret.Spec.Name)
}

func (w *Watcher) refreshConfig(ctx context.Context, ev swarmclient.Event) error {
	cfg, err := w.fetch.GetConfig(ctx, ev.ID)
	if errdefs.IsNotFound(err) {
		if err := w.store.DeleteCacheConfigBySwarmID(ctx, ev.ID); err != nil {
			return fmt.Errorf("delete cached config %s: %w", ev.ID, err)
		}
		return w.emitObjectChange(ctx, "config", string(ev.Action), ev.ID, ev.Name)
	} else if err != nil {
		return fmt.Errorf("get config %s: %w", ev.ID, err)
	}
	spec, err := marshalJSON(cfg.Spec)
	if err != nil {
		return fmt.Errorf("marshal config %s spec: %w", cfg.ID, err)
	}
	if err := w.store.UpsertConfig(ctx, dbgen.UpsertCacheConfigParams{
		SwarmID: cfg.ID,
		Name:    cfg.Spec.Name,
		Spec:    spec,
	}); err != nil {
		return fmt.Errorf("upsert cached config %s: %w", cfg.ID, err)
	}
	return w.emitObjectChange(ctx, "config", string(ev.Action), cfg.ID, cfg.Spec.Name)
}

// refreshNetwork has no single-object getter in swarm.Client, so it lists
// networks and matches by ID; a missing match means the network is gone.
func (w *Watcher) refreshNetwork(ctx context.Context, ev swarmclient.Event) error {
	nets, err := w.lister.ListNetworks(ctx)
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	for _, n := range nets {
		if n.ID != ev.ID {
			continue
		}
		spec, err := marshalJSON(n)
		if err != nil {
			return fmt.Errorf("marshal network %s: %w", n.ID, err)
		}
		if err := w.store.UpsertNetwork(ctx, dbgen.UpsertCacheNetworkParams{
			SwarmID: n.ID,
			Name:    n.Name,
			Spec:    spec,
		}); err != nil {
			return fmt.Errorf("upsert cached network %s: %w", n.ID, err)
		}
		return w.emitObjectChange(ctx, "network", string(ev.Action), n.ID, n.Name)
	}
	if err := w.store.DeleteCacheNetworkBySwarmID(ctx, ev.ID); err != nil {
		return fmt.Errorf("delete cached network %s: %w", ev.ID, err)
	}
	return w.emitObjectChange(ctx, "network", string(ev.Action), ev.ID, ev.Name)
}
