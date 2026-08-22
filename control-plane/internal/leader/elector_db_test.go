package leader

import (
	"context"
	"testing"
	"time"

	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// TestCheckAliveRealOwnershipProbe runs the keepalive ownership query against
// a real Postgres advisory lock: held-by-this-backend → alive, released →
// gone. The lock is taken on the same dedicated connection whose backend PID
// the probe matches, exactly as the elector does while leading.
func TestCheckAliveRealOwnershipProbe(t *testing.T) {
	pool := testdb.Get(t)
	ctx := context.Background()

	e := newElector(nil, nil, nil)
	e.keepAliveInterval = time.Second

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", db.LockLeaderElection); err != nil {
		t.Fatalf("pg_advisory_lock: %v", err)
	}
	if !e.checkAlive(ctx, conn) {
		t.Fatal("ownership probe reported the lock lost while this session holds it")
	}

	if _, err := conn.Exec(ctx, "select pg_advisory_unlock($1)", db.LockLeaderElection); err != nil {
		t.Fatalf("pg_advisory_unlock: %v", err)
	}
	if e.checkAlive(ctx, conn) {
		t.Fatal("ownership probe reported the lock held after release")
	}
}

// TestRunAgainstRealPostgres exercises the full elector loop on a real
// database: try-lock succeeds, the ownership keepalive passes repeatedly, and
// cancelling the parent context exits cleanly without onLost.
func TestRunAgainstRealPostgres(t *testing.T) {
	pool := testdb.Get(t)

	cb := newCallbacks()
	e := New(pool, cb.onAcquire, cb.onLost)
	e.acquirePollInterval = 5 * time.Millisecond
	e.keepAliveInterval = 10 * time.Millisecond
	e.reacquireDelay = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	select {
	case <-cb.acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("elector never acquired the real advisory lock")
	}

	// Lead through several keepalive ticks before shutting down.
	time.Sleep(60 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after parent cancel")
	}
	if e.IsLeader() {
		t.Error("IsLeader should be false after shutdown")
	}
	select {
	case <-cb.lost:
		t.Error("onLost must not fire on clean shutdown")
	default:
	}

	// The lock was released with the session: another connection can take it.
	ok, err := db.TryAdvisoryLock(context.Background(), pool, db.LockLeaderElection)
	if err != nil || !ok {
		t.Fatalf("leader lock still held after shutdown: ok=%v err=%v", ok, err)
	}
	if err := db.ReleaseAdvisoryLock(context.Background(), pool, db.LockLeaderElection); err != nil {
		t.Fatal(err)
	}
}

// TestSessionLockMutualExclusionOnElectorLockID proves two sessions cannot
// hold the election lock at once: the second pg_try_advisory_lock fails until
// the first session releases.
func TestSessionLockMutualExclusionOnElectorLockID(t *testing.T) {
	pool := testdb.Get(t)
	ctx := context.Background()

	unlock, err := db.AcquireSessionLock(ctx, pool, db.LockLeaderElection)
	if err != nil {
		t.Fatalf("AcquireSessionLock: %v", err)
	}

	held, err := db.TryAdvisoryLock(ctx, pool, db.LockLeaderElection)
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("second session acquired an exclusively held lock")
	}

	unlock()

	held, err = db.TryAdvisoryLock(ctx, pool, db.LockLeaderElection)
	if err != nil {
		t.Fatal(err)
	}
	if !held {
		t.Fatal("lock not acquirable after unlock")
	}
	if err := db.ReleaseAdvisoryLock(ctx, pool, db.LockLeaderElection); err != nil {
		t.Fatal(err)
	}
}
