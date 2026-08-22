package db

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fixtureFS returns two valid migration files.
func fixtureFS() fstest.MapFS {
	return fstest.MapFS{
		"0001_create_a.up.sql": &fstest.MapFile{Data: []byte("create table mig_a (id int primary key);")},
		"0002_create_b.up.sql": &fstest.MapFile{Data: []byte("create table mig_b (id int primary key);")},
	}
}

func ledgerVersions(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "select version from schema_migrations order by version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		"select exists(select 1 from information_schema.tables where table_name=$1)", name).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

// TestApplyMigrationsHappyPathAndIdempotent applies a fresh fixture set,
// verifies tables and the ledger, then re-runs to prove idempotency.
func TestApplyMigrationsHappyPathAndIdempotent(t *testing.T) {
	pool, _ := testPostgres(t)
	freshSchema(t)
	ctx := context.Background()

	fs := fixtureFS()
	if err := ApplyMigrations(ctx, pool, fs); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	if !tableExists(t, pool, "mig_a") || !tableExists(t, pool, "mig_b") {
		t.Fatal("fixture tables missing after migration")
	}
	got := ledgerVersions(t, pool)
	if len(got) != 2 || got[0] != "0001_create_a.up.sql" || got[1] != "0002_create_b.up.sql" {
		t.Fatalf("ledger = %v, want both versions in order", got)
	}

	// Re-run: everything already applied must be skipped without error.
	if err := ApplyMigrations(ctx, pool, fs); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
	if n := len(ledgerVersions(t, pool)); n != 2 {
		t.Errorf("ledger grew on re-run: %d entries", n)
	}
}

// TestApplyMigrationsSkipsPartiallyAppliedLedger seeds the ledger entry for
// 0002 without its table (a crashed deploy that committed the ledger but the
// table was dropped): the runner must skip 0002 and still apply nothing new.
func TestApplyMigrationsSkipsPartiallyAppliedLedger(t *testing.T) {
	pool, _ := testPostgres(t)
	freshSchema(t)
	ctx := context.Background()

	if err := ApplyMigrations(ctx, pool, fixtureFS()); err != nil {
		t.Fatal(err)
	}
	// Simulate out-of-order/partial state: 0002 recorded, table removed.
	if _, err := pool.Exec(ctx, "drop table mig_b"); err != nil {
		t.Fatal(err)
	}

	if err := ApplyMigrations(ctx, pool, fixtureFS()); err != nil {
		t.Fatalf("re-apply with partial ledger: %v", err)
	}
	if tableExists(t, pool, "mig_b") {
		t.Error("recorded migration was re-applied; runner must trust the ledger")
	}
	if got := ledgerVersions(t, pool); len(got) != 2 {
		t.Errorf("ledger = %v, want unchanged", got)
	}
}

// TestApplyMigrationsFailureRollsBack proves a broken migration leaves no
// ledger entry and no partially-created objects behind.
func TestApplyMigrationsFailureRollsBack(t *testing.T) {
	pool, _ := testPostgres(t)
	freshSchema(t)
	ctx := context.Background()

	broken := fstest.MapFS{
		"0001_ok.up.sql":    &fstest.MapFile{Data: []byte("create table mig_a (id int primary key);")},
		"0002_boom.up.sql":  &fstest.MapFile{Data: []byte("create table mig_tmp (id int); this is not valid sql")},
		"0003_never.up.sql": &fstest.MapFile{Data: []byte("create table mig_never (id int);")},
	}
	err := ApplyMigrations(ctx, pool, broken)
	if err == nil || !strings.Contains(err.Error(), "migration 0002_boom.up.sql failed") {
		t.Fatalf("want wrapped migration failure, got %v", err)
	}
	// The transaction rolled back: neither the temp table nor later
	// migrations exist, and only 0001 is in the ledger.
	if tableExists(t, pool, "mig_tmp") || tableExists(t, pool, "mig_never") {
		t.Error("objects from failed/aborted migrations survived")
	}
	if got := ledgerVersions(t, pool); len(got) != 1 || got[0] != "0001_ok.up.sql" {
		t.Errorf("ledger = %v, want only 0001", got)
	}
}

// TestApplyMigrationsBadRoot covers the fs.ReadDir error branch.
func TestApplyMigrationsBadRoot(t *testing.T) {
	pool, _ := testPostgres(t)
	err := ApplyMigrations(context.Background(), pool, missingFS{})
	if err == nil {
		t.Fatal("expected error for unreadable migration root")
	}
}

type missingFS struct{}

func (missingFS) Open(string) (fs.File, error) { return nil, errors.New("no such directory") }

// TestNewPoolRejectsGarbageDSN covers config-parse failure.
func TestNewPoolRejectsGarbageDSN(t *testing.T) {
	if _, err := NewPool(context.Background(), "not a dsn://\x00bad"); err == nil {
		t.Error("NewPool accepted an invalid DSN")
	}
}

// TestWaitForPingSuccessAndTimeout exercises both exits of the readiness
// wait loop against a real database and a dead endpoint.
func TestWaitForPingSuccessAndTimeout(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx := context.Background()
	if err := WaitForPing(ctx, pool, 5*time.Second); err != nil {
		t.Fatalf("WaitForPing on healthy pool: %v", err)
	}

	dead, err := pgxpool.New(ctx, "postgres://hive:hive@127.0.0.1:1/nope")
	if err != nil {
		t.Skipf("could not build dead pool: %v", err)
	}
	defer dead.Close()
	start := time.Now()
	if err := WaitForPing(ctx, dead, 1200*time.Millisecond); err == nil {
		t.Error("WaitForPing succeeded against a dead endpoint")
	} else if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("WaitForPing gave up too early: %v", elapsed)
	}
}

// TestMigrateRiverAppliesUpMigrations runs River's embedded migrations on a
// fresh schema and proves the re-run is a no-op.
func TestMigrateRiverAppliesUpMigrations(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx := context.Background()

	if err := MigrateRiver(ctx, pool); err != nil {
		t.Fatalf("MigrateRiver: %v", err)
	}
	if !tableExists(t, pool, "river_job") {
		t.Fatal("river_job table missing after river migrations")
	}
	if err := MigrateRiver(ctx, pool); err != nil {
		t.Fatalf("idempotent MigrateRiver: %v", err)
	}
}
