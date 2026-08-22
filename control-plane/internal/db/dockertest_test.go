package db

// The internal/testdb harness cannot be imported here (it imports this
// package), so these tests start their own throwaway Postgres container,
// once per test binary, and skip cleanly when Docker is unreachable — same
// contract as testdb.Get. Run the suite as a user that can access the
// docker socket (see Makefile / CI), otherwise every test here skips.

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	dbOnce sync.Once
	dbPool *pgxpool.Pool
	dbDSN  string
	dbErr  error
)

const (
	testUser     = "hive"
	testPassword = "hive"
	testDBName   = "hive"
)

func testPostgres(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dbOnce.Do(func() {
		if !dockerReachable() {
			dbErr = fmt.Errorf("docker daemon not reachable")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase(testDBName),
			tcpostgres.WithUsername(testUser),
			tcpostgres.WithPassword(testPassword),
		)
		if err != nil {
			dbErr = fmt.Errorf("start postgres container: %w", err)
			return
		}
		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			dbErr = fmt.Errorf("connection string: %w", err)
			return
		}
		p, err := NewPool(ctx, dsn)
		if err != nil {
			dbErr = fmt.Errorf("connect pool: %w", err)
			return
		}
		// The container may still be finishing its init sequence; poll
		// until Postgres actually answers, like testdb does.
		if err := waitForDB(ctx, p); err != nil {
			dbErr = fmt.Errorf("ping pool: %w", err)
			return
		}
		dbPool, dbDSN = p, dsn
	})
	if dbErr != nil {
		t.Skipf("postgres unavailable: %v", dbErr)
	}
	return dbPool, dbDSN
}

// waitForDB polls the pool until Postgres answers or the context expires.
func waitForDB(ctx context.Context, p *pgxpool.Pool) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := p.Ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return lastErr
}

func dockerReachable() bool {
	host := os.Getenv("DOCKER_HOST")
	addr := "/var/run/docker.sock"
	network := "unix"
	if h := host; h != "" && !strings.HasPrefix(h, "unix://") {
		network = "tcp"
		addr = strings.TrimPrefix(h, "tcp://")
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, "2375")
		}
	}
	conn, err := net.DialTimeout(network, addr, time.Second) //nolint:gosec // test fixture
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// freshSchema drops objects created by the migration fixtures so each
// migration test starts from an empty ledger.
func freshSchema(t *testing.T) {
	t.Helper()
	pool, _ := testPostgres(t)
	_, err := pool.Exec(context.Background(), `
		drop table if exists schema_migrations, mig_a, mig_b, mig_tmp, mig_never cascade
	`)
	if err != nil {
		t.Fatalf("freshSchema: %v", err)
	}
}
