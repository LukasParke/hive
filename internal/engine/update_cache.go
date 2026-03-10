package engine

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
)

type nodeUpdateEntry struct {
	NodeID          string          `json:"node_id"`
	Hostname        string          `json:"hostname"`
	OS              string          `json:"os"`
	KernelVersion   string          `json:"kernel_version"`
	PackageManager  string          `json:"package_manager"`
	PendingCount    int             `json:"pending_count"`
	SecurityCount   int             `json:"security_count"`
	RebootRequired  bool            `json:"reboot_required"`
	PendingPackages json.RawMessage `json:"pending_packages"`
	LastChecked     time.Time       `json:"last_checked"`
}

type updateProgressEntry struct {
	NodeID    string `json:"node_id"`
	Action    string `json:"action"`
	Status    string `json:"status"`
	Output    string `json:"output"`
	Progress  int    `json:"progress"`
	Timestamp int64  `json:"timestamp"`
}

type UpdateCache struct {
	mu          sync.RWMutex
	nodes       map[string]*nodeUpdateEntry
	nc          *nats.Conn
	db          *store.Store
	log         *zap.SugaredLogger
	statusSub   *nats.Subscription
	progressSub *nats.Subscription
}

func NewUpdateCache(nc *nats.Conn, db *store.Store, log *zap.SugaredLogger) *UpdateCache {
	return &UpdateCache{
		nodes: make(map[string]*nodeUpdateEntry),
		nc:    nc,
		db:    db,
		log:   log,
	}
}

func (uc *UpdateCache) Start(ctx context.Context) {
	var err error

	uc.statusSub, err = uc.nc.Subscribe("hive.node.updates.status.>", func(msg *nats.Msg) {
		var entry nodeUpdateEntry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			uc.log.Warnf("invalid node update status: %v", err)
			return
		}
		uc.mu.Lock()
		uc.nodes[entry.NodeID] = &entry
		uc.mu.Unlock()

		if uc.db != nil {
			pkgData := entry.PendingPackages
			if pkgData == nil {
				pkgData = json.RawMessage("[]")
			}
			n := &store.NodeUpdateStatus{
				NodeID:          entry.NodeID,
				Hostname:        entry.Hostname,
				OSInfo:          entry.OS,
				KernelVersion:   entry.KernelVersion,
				PackageManager:  entry.PackageManager,
				PendingCount:    entry.PendingCount,
				SecurityCount:   entry.SecurityCount,
				RebootRequired:  entry.RebootRequired,
				PendingPackages: pkgData,
			}
			if err := uc.db.UpsertNodeUpdateStatus(ctx, n); err != nil {
				uc.log.Warnf("persist node update status: %v", err)
			}
		}

		data, _ := json.Marshal(map[string]any{
			"type":    "node_update_status",
			"payload": entry,
			"ts":      time.Now().Unix(),
		})
		getUpdatesHub().broadcast(data)
	})
	if err != nil {
		uc.log.Errorf("subscribe hive.node.updates.status.>: %v", err)
	}

	uc.progressSub, err = uc.nc.Subscribe("hive.node.updates.progress.>", func(msg *nats.Msg) {
		var progress updateProgressEntry
		if err := json.Unmarshal(msg.Data, &progress); err != nil {
			return
		}
		data, _ := json.Marshal(map[string]any{
			"type":    "update_progress",
			"payload": progress,
			"ts":      time.Now().Unix(),
		})
		getUpdatesHub().broadcast(data)
	})
	if err != nil {
		uc.log.Errorf("subscribe hive.node.updates.progress.>: %v", err)
	}

	if uc.db != nil {
		statuses, err := uc.db.ListNodeUpdateStatuses(ctx)
		if err == nil {
			uc.mu.Lock()
			for _, s := range statuses {
				uc.nodes[s.NodeID] = &nodeUpdateEntry{
					NodeID:          s.NodeID,
					Hostname:        s.Hostname,
					OS:              s.OSInfo,
					KernelVersion:   s.KernelVersion,
					PackageManager:  s.PackageManager,
					PendingCount:    s.PendingCount,
					SecurityCount:   s.SecurityCount,
					RebootRequired:  s.RebootRequired,
					PendingPackages: s.PendingPackages,
					LastChecked:     s.LastCheckedAt,
				}
			}
			uc.mu.Unlock()
		}
	}

	go func() {
		<-ctx.Done()
		if uc.statusSub != nil {
			_ = uc.statusSub.Unsubscribe()
		}
		if uc.progressSub != nil {
			_ = uc.progressSub.Unsubscribe()
		}
	}()
}

func (uc *UpdateCache) GetAll() map[string]*nodeUpdateEntry {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	result := make(map[string]*nodeUpdateEntry, len(uc.nodes))
	for k, v := range uc.nodes {
		result[k] = v
	}
	return result
}

func (uc *UpdateCache) Get(nodeID string) *nodeUpdateEntry {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return uc.nodes[nodeID]
}
