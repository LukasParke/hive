package riverjobs

import (
	"context"
	"testing"

	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestDomainLookup(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "routedapp", "", nil)
	for _, host := range []string{"a.example.test", "b.example.test"} {
		if _, err := pool.Exec(context.Background(),
			`insert into domains(application_id, hostname) values ($1::uuid, $2)`, appID, host); err != nil {
			t.Fatalf("seed domain %s: %v", host, err)
		}
	}
	otherApp := testdb.SeedApplication(t, fixture.ProjectID, "otherapp", "", nil)

	lookup := domainLookup(context.Background(), pool, appID)

	t.Run("explicit app id", func(t *testing.T) {
		hosts, err := lookup(context.Background(), appID)
		if err != nil {
			t.Fatalf("lookup: %v", err)
		}
		if len(hosts) != 2 || hosts[0] != "a.example.test" || hosts[1] != "b.example.test" {
			t.Fatalf("hosts = %v, want a/b.example.test", hosts)
		}
	})

	t.Run("empty id falls back to captured app", func(t *testing.T) {
		hosts, err := lookup(context.Background(), "")
		if err != nil {
			t.Fatalf("lookup: %v", err)
		}
		if len(hosts) != 2 {
			t.Fatalf("hosts = %v, want the routed app's domains", hosts)
		}
	})

	t.Run("unknown valid uuid returns empty", func(t *testing.T) {
		hosts, err := lookup(context.Background(), otherApp)
		if err != nil {
			t.Fatalf("lookup: %v", err)
		}
		if len(hosts) != 0 {
			t.Fatalf("hosts = %v, want none for an unrouted app", hosts)
		}
	})
}
