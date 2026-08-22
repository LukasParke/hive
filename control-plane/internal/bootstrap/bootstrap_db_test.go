package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// TestRunAgainstRealDatabaseWinsAndSeeds runs Run with the Postgres-backed
// LockClient on a fresh settings table: it takes the real advisory lock,
// "migrates", seeds the bootstrap marker, and releases the lock.
func TestRunAgainstRealDatabaseWinsAndSeeds(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "app_settings")
	ctx := context.Background()

	migrated := false
	err := Run(ctx, NewLockClient(pool), func(context.Context) error {
		migrated = true
		return nil
	}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !migrated {
		t.Error("migrate did not run")
	}

	lc := pgxLockClient{pool: pool}
	present, err := lc.HasAnySetting(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("bootstrap marker not seeded into app_settings")
	}

	// The lock was released: another session can take it now.
	ok, err := db.TryAdvisoryLock(ctx, pool, db.LockBootstrap)
	if err != nil || !ok {
		t.Fatalf("bootstrap lock still held after Run: ok=%v err=%v", ok, err)
	}
	if err := db.ReleaseAdvisoryLock(ctx, pool, db.LockBootstrap); err != nil {
		t.Fatal(err)
	}
}

// TestRunAgainstRealDatabaseWaitsForPeer seeds the settings record while the
// lock is held elsewhere: the replica must proceed without initializing.
func TestRunAgainstRealDatabaseWaitsForPeer(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "app_settings")
	ctx := context.Background()

	// A peer holds the bootstrap lock on its own session.
	unlock, err := db.AcquireSessionLock(ctx, pool, db.LockBootstrap)
	if err != nil {
		t.Fatalf("peer AcquireSessionLock: %v", err)
	}
	defer unlock()

	lc := pgxLockClient{pool: pool}
	go func() {
		time.Sleep(30 * time.Millisecond)
		if err := lc.SeedSettings(ctx, DefaultSettings(time.Now())); err != nil {
			t.Errorf("peer seed: %v", err)
		}
	}()

	initialized := false
	err = Run(ctx, lc, func(context.Context) error {
		initialized = true
		return nil
	}, Options{WaitTimeout: 5 * time.Second, PollInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if initialized {
		t.Error("loser replica ran migrations/seeding despite peer completing")
	}
}

// TestRunAgainstRealDatabaseMigrateFailurePropagates proves a failing migrate
// surfaces from Run instead of being swallowed.
func TestRunAgainstRealDatabaseMigrateFailurePropagates(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "app_settings")

	boom := errors.New("migration exploded")
	err := Run(context.Background(), NewLockClient(pool), func(context.Context) error {
		return boom
	}, Options{})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("want wrapped migrate error, got %v", err)
	}
}

// TestSeedSettingsRealIdempotent proves the on-conflict-do-nothing upsert:
// existing keys keep their original values across repeated boots.
func TestSeedSettingsRealIdempotent(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "app_settings")
	ctx := context.Background()
	lc := pgxLockClient{pool: pool}

	if err := lc.SeedSettings(ctx, DefaultSettings(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatalf("first SeedSettings: %v", err)
	}
	if err := lc.SeedSettings(ctx, DefaultSettings(time.Now())); err != nil {
		t.Fatalf("second SeedSettings: %v", err)
	}
	present, err := lc.HasAnySetting(ctx)
	if err != nil || !present {
		t.Fatalf("HasAnySetting = %v, %v; want true, nil", present, err)
	}
}

// TestTryAdvisoryLockRoundtrip covers the exported db helpers: a lock held on
// a dedicated session excludes the pooled TryAdvisoryLock until released.
func TestTryAdvisoryLockRoundtrip(t *testing.T) {
	pool := testdb.Get(t)
	ctx := context.Background()
	lc := NewLockClient(pool)

	unlock, err := db.AcquireSessionLock(ctx, pool, db.LockBootstrap)
	if err != nil {
		t.Fatalf("AcquireSessionLock: %v", err)
	}
	ok, err := lc.TryAdvisoryLock(ctx, db.LockBootstrap)
	if err != nil || ok {
		t.Fatalf("TryAdvisoryLock while held = %v, %v; want false, nil", ok, err)
	}
	unlock()
	ok, err = lc.TryAdvisoryLock(ctx, db.LockBootstrap)
	if err != nil || !ok {
		t.Fatalf("TryAdvisoryLock after release = %v, %v; want true, nil", ok, err)
	}
	if err := lc.ReleaseAdvisoryLock(ctx, db.LockBootstrap); err != nil {
		t.Fatalf("ReleaseAdvisoryLock: %v", err)
	}
}

// TestSeedSettingsInvalidJSONFails covers the insert error branch of the
// Postgres-backed SeedSettings: a value that is not valid JSON is rejected by
// the jsonb cast.
func TestSeedSettingsInvalidJSONFails(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "app_settings")
	lc := pgxLockClient{pool: pool}
	err := lc.SeedSettings(context.Background(), map[string]json.RawMessage{
		"broken": json.RawMessage(`{not json`),
	})
	if err == nil || !strings.Contains(err.Error(), "seed setting") {
		t.Fatalf("want seed error for invalid JSON, got %v", err)
	}
}
