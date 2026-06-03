package reconcile

import (
	"context"
	"encoding/json"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/proxy"
	"github.com/luke/hive/control-plane/internal/swarm"
)

type Watcher struct {
	swarm  *swarm.Client
	fanout *db.Fanout
	pool   *pgxpool.Pool
}

func NewWatcher(s *swarm.Client, fanout *db.Fanout, pool *pgxpool.Pool) *Watcher {
	return &Watcher{swarm: s, fanout: fanout, pool: pool}
}

func (w *Watcher) Run(ctx context.Context) {
	services, err := w.swarm.ListServices(ctx)
	if err != nil {
		log.Printf("reconcile failed: %v", err)
		return
	}
	domainsByApp := map[string][]string{}
	rows, err := w.pool.Query(ctx, `select application_id::text, hostname from domains`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var appID, host string
			if err := rows.Scan(&appID, &host); err == nil {
				domainsByApp[appID] = append(domainsByApp[appID], host)
			}
		}
	}
	manager := proxy.NewDomainManager(w.swarm)
	for _, svc := range services {
		appID := svc.Spec.Labels["dokploy.app.id"]
		if appID == "" {
			continue
		}
		port := 3000
		for _, host := range domainsByApp[appID] {
			_ = manager.ApplyDomain(ctx, svc.ID, proxy.RouterNameFromHost(host), host, port)
		}
	}
	payload, _ := json.Marshal(map[string]any{"services": len(services)})
	_ = w.fanout.Emit(ctx, "system", string(payload))
}
