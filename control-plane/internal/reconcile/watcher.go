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
	type domainRoute struct {
		Host       string
		TLSEnabled bool
	}
	domainsByApp := map[string][]domainRoute{}
	rows, err := w.pool.Query(ctx, `select application_id::text, hostname, tls_enabled from domains`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var appID, host string
			var tlsEnabled bool
			if err := rows.Scan(&appID, &host, &tlsEnabled); err == nil {
				domainsByApp[appID] = append(domainsByApp[appID], domainRoute{Host: host, TLSEnabled: tlsEnabled})
			}
		}
	}
	manager := proxy.NewDomainManager(w.swarm)
	for _, svc := range services {
		appID := svc.Spec.Labels["hive.app.id"]
		if appID == "" {
			continue
		}
		port := 3000
		for _, route := range domainsByApp[appID] {
			_ = manager.ApplyDomain(ctx, svc.ID, proxy.RouterNameFromHost(route.Host), route.Host, port, route.TLSEnabled)
		}
	}
	payload, _ := json.Marshal(map[string]any{"services": len(services)})
	_ = w.fanout.Emit(ctx, "system", string(payload))
}
