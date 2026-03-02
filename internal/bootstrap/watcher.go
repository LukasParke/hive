package bootstrap

import (
	"context"

	"github.com/docker/docker/api/types/events"

	"github.com/lholliger/hive/internal/notify"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

type NodeWatcher struct {
	swarm *swarm.Client
	cfg   *config.Config
	log   *zap.SugaredLogger
	db    *store.Store
}

func NewNodeWatcher(sc *swarm.Client, cfg *config.Config, db *store.Store, log *zap.SugaredLogger) *NodeWatcher {
	return &NodeWatcher{swarm: sc, cfg: cfg, db: db, log: log}
}

func (w *NodeWatcher) Start(ctx context.Context) {
	w.log.Info("starting node event watcher")
	w.swarm.WatchEvents(ctx, func(event events.Message) {
		if event.Type == events.NodeEventType {
			w.handleNodeEvent(ctx, event)
		}
	})
}

func (w *NodeWatcher) handleNodeEvent(ctx context.Context, event events.Message) {
	w.log.Infof("node event: action=%s node=%s", event.Action, event.Actor.ID)

	if w.db != nil {
		d := notify.NewDispatcher(w.db, w.log)
		switch event.Action {
		case "create":
			d.Send(ctx, notify.Event{
				Type:    "node.joined",
				Title:   "Node Joined Swarm",
				Message: "A new node has joined the swarm cluster.",
			})
		case "remove":
			d.Send(ctx, notify.Event{
				Type:    "node.left",
				Title:   "Node Left Swarm",
				Message: "A node has left the swarm cluster.",
			})
		}
	}

	multiNode, err := w.swarm.IsMultiNode(ctx)
	if err != nil {
		w.log.Warnf("check multi-node: %v", err)
		return
	}

	if multiNode && !w.cfg.MultiNode {
		w.log.Info("transitioning to multi-node mode")
		w.cfg.MultiNode = true

		b := &Bootstrapper{cfg: w.cfg, log: w.log, swarm: w.swarm}
		if err := b.ensureRegistry(ctx); err != nil {
			w.log.Errorf("auto-deploy registry: %v", err)
		}
	}
}
