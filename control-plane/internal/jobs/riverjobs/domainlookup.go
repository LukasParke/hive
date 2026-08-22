package riverjobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

// domainLookup returns a deploy.DomainLookup-compatible closure resolving
// the application's routed domains from the database. The result decides
// whether the deployed service attaches the shared hive_proxy overlay so
// Traefik can reach it.
func domainLookup(ctx context.Context, pool *pgxpool.Pool, appID string) func(ctx context.Context, appID string) ([]string, error) {
	return func(_ context.Context, lookupAppID string) ([]string, error) {
		id := lookupAppID
		if id == "" {
			id = appID
		}
		appUUID, err := uuid.Parse(id)
		if err != nil {
			return nil, nil // unparseable id: treat as no domains
		}
		q := dbgen.New(pool)
		domains, err := q.ListDomainsByApplication(ctx, pgtype.UUID{Bytes: appUUID, Valid: true})
		if err != nil {
			return nil, err
		}
		hosts := make([]string, 0, len(domains))
		for _, d := range domains {
			hosts = append(hosts, d.Hostname)
		}
		return hosts, nil
	}
}
