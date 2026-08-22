package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestAppLockIDDeterministic(t *testing.T) {
	a1, err := appLockID(testAppID)
	if err != nil {
		t.Fatalf("appLockID: %v", err)
	}
	a2, err := appLockID(testAppID)
	if err != nil {
		t.Fatalf("appLockID: %v", err)
	}
	if a1 != a2 {
		t.Fatalf("same input produced different keys: %d vs %d", a1, a2)
	}

	b, err := appLockID("abcdef01-1234-1234-1234-123456789abc")
	if err != nil {
		t.Fatalf("appLockID: %v", err)
	}
	if b == a1 {
		t.Fatal("different applications must map to different lock keys")
	}
	// Namespace offset keeps app locks away from the global lock ids (1,2,3).
	if a1 < appLockNamespace {
		t.Fatalf("key %d does not carry the namespace offset", a1)
	}

	if _, err := appLockID("not-a-uuid"); err == nil {
		t.Fatal("expected error for unparseable id")
	}
}

func TestWithAppLockRunsFnUnderAdvisoryLock(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	key, err := appLockID(testAppID)
	if err != nil {
		t.Fatalf("appLockID: %v", err)
	}

	var tryLocked bool
	err = WithAppLock(context.Background(), pool, testAppID, func(ctx context.Context) error {
		// A second session must NOT be able to take the same advisory lock
		// while fn runs — this proves the session lock is genuinely held.
		conn, acqErr := pool.Acquire(ctx)
		if acqErr != nil {
			return acqErr
		}
		defer conn.Release()
		return conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", key).Scan(&tryLocked)
	})
	if err != nil {
		t.Fatalf("WithAppLock: %v", err)
	}
	if tryLocked {
		t.Fatal("second session acquired the advisory lock while fn was running")
	}
}

func TestWithAppLockPropagatesFnError(t *testing.T) {
	pool := testdb.Get(t)
	sentinel := errors.New("fn failed")
	err := WithAppLock(context.Background(), pool, testAppID, func(context.Context) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel propagated", err)
	}
}

func TestWithAppLockInvalidIDRunsUnlocked(t *testing.T) {
	pool := testdb.Get(t)
	ran := false
	err := WithAppLock(context.Background(), pool, "not-a-uuid", func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("err = %v, ran = %v; unparseable id must run fn unlocked", err, ran)
	}
}

func TestWithAppLockAcquireError(t *testing.T) {
	// A pool pointed at a dead port makes AcquireSessionLock fail without
	// touching the shared test pool.
	badPool, err := pgxpool.New(context.Background(), "postgres://hive:hive@127.0.0.1:1/hive")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer badPool.Close()

	err = WithAppLock(context.Background(), badPool, testAppID, func(context.Context) error {
		t.Fatal("fn must not run when the lock cannot be acquired")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "acquire app lock") {
		t.Fatalf("err = %v, want acquire app lock failure", err)
	}
}
