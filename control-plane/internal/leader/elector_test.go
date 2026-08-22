package leader

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeConn is a scriptable PooledConn. Behavior is keyed off the SQL text:
// try-lock, ownership (keepalive), unlock, and ping probes.
type fakeConn struct {
	mu sync.Mutex

	tryLockOK  bool
	tryLockErr error
	owned      int   // value scanned from the ownership query
	ownedErr   error // error returned by the ownership query
	pingErr    error // error returned by Ping (connection death)
	unlockOK   bool  // value scanned from pg_advisory_unlock
	unlockErr  error // error returned by pg_advisory_unlock

	released        bool
	ownershipChecks int
	unlockCalls     int

	// onOwnership, if set, runs after each ownership check.
	onOwnership func()
}

func (c *fakeConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func (c *fakeConn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case strings.HasPrefix(sql, "select pg_try_advisory_lock"):
		return staticRow{val: c.tryLockOK, err: c.tryLockErr}
	case strings.HasPrefix(sql, "select count(*) from pg_locks"):
		c.ownershipChecks++
		hook := c.onOwnership
		if hook != nil {
			hook()
		}
		return staticRow{val: c.owned, err: c.ownedErr}
	case strings.HasPrefix(sql, "select pg_advisory_unlock"):
		c.unlockCalls++
		return staticRow{val: c.unlockOK, err: c.unlockErr}
	}
	return staticRow{err: errors.New("unexpected query: " + sql)}
}

func (c *fakeConn) Ping(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pingErr
}

func (c *fakeConn) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.released = true
}

func (c *fakeConn) snapshot() (released bool, unlockCalls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.released, c.unlockCalls
}

// staticRow is a one-value pgx.Row.
type staticRow struct {
	val any
	err error
}

func (r staticRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("staticRow expects one destination")
	}
	switch d := dest[0].(type) {
	case *bool:
		v, ok := r.val.(bool)
		if !ok {
			return errors.New("staticRow: not a bool")
		}
		*d = v
	case *int:
		v, ok := r.val.(int)
		if !ok {
			return errors.New("staticRow: not an int")
		}
		*d = v
	default:
		return errors.New("staticRow: unsupported destination")
	}
	return nil
}

// fakePool hands out scripted connections in order.
type fakePool struct {
	mu    sync.Mutex
	conns []*fakeConn
	i     int
}

func (p *fakePool) Acquire(ctx context.Context) (PooledConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.i >= len(p.conns) {
		return nil, errors.New("fakePool: no more connections")
	}
	c := p.conns[p.i]
	p.i++
	return c, nil
}

// callbacks records onAcquire/onLost invocations.
type callbacks struct {
	mu       sync.Mutex
	acquires int
	losses   int
	acquired chan context.Context // receives each leaderCtx
	lost     chan struct{}
}

func newCallbacks() *callbacks {
	return &callbacks{acquired: make(chan context.Context, 16), lost: make(chan struct{}, 16)}
}

func (c *callbacks) onAcquire(ctx context.Context) {
	c.mu.Lock()
	c.acquires++
	c.mu.Unlock()
	c.acquired <- ctx
}

func (c *callbacks) onLost() {
	c.mu.Lock()
	c.losses++
	c.mu.Unlock()
	c.lost <- struct{}{}
}

func fastElector(pool Pool, cb *callbacks) *Elector {
	e := newElector(pool, cb.onAcquire, cb.onLost)
	e.acquirePollInterval = time.Millisecond
	e.keepAliveInterval = 2 * time.Millisecond
	e.reacquireDelay = 5 * time.Millisecond
	return e
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// waitForCond polls cond until true, for state settled asynchronously by the
// elector goroutine (e.g. deferred connection release).
func waitForCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestKeepaliveLossTriggersOnLostAndReacquire proves the full cycle: leading,
// keepalive connection death cancels the leader context and fires onLost,
// then after the reacquire delay the elector re-acquires on a fresh
// connection.
func TestKeepaliveLossTriggersOnLostAndReacquire(t *testing.T) {
	losing := &fakeConn{tryLockOK: true, owned: 1, pingErr: errors.New("connection refused")}
	losing.onOwnership = func() {
		// Runs while fakeConn.QueryRow already holds losing.mu; no locking.
		if losing.ownershipChecks > 1 {
			losing.ownedErr = errors.New("conn closed")
		}
	}
	healthy := &fakeConn{tryLockOK: true, owned: 1, unlockOK: true}

	pool := &fakePool{conns: []*fakeConn{losing, healthy}}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	// First acquisition.
	firstCtx := <-cb.acquired
	// Leadership is lost when the keepalive connection dies.
	<-cb.lost
	select {
	case <-firstCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("leader context was not cancelled after keepalive loss")
	}

	// Re-acquisition happens after the reacquire delay on the second conn.
	secondCtx := <-cb.acquired
	if secondCtx == firstCtx {
		t.Fatal("expected a fresh leader context after re-acquisition")
	}
	if !e.IsLeader() {
		t.Fatal("expected IsLeader after re-acquisition")
	}

	cancel()
	waitFor(t, done, "Run exit after parent cancel")

	if released, unlockCalls := losing.snapshot(); !released {
		t.Error("losing connection was not released")
	} else if unlockCalls == 0 {
		t.Error("expected an unlock attempt on the losing connection (pg_advisory_unlock returning false is expected)")
	}
	if released, _ := healthy.snapshot(); !released {
		t.Error("healthy connection was not released after shutdown")
	}

	cb.mu.Lock()
	losses := cb.losses
	cb.mu.Unlock()
	if losses != 1 {
		t.Errorf("onLost called %d times, want 1", losses)
	}
}

// TestParentCancelExitsLoopWithoutOnLost proves shutdown: cancelling the
// parent context stops the elector while leading without firing onLost and
// without deadlocking.
func TestParentCancelExitsLoopWithoutOnLost(t *testing.T) {
	conn := &fakeConn{tryLockOK: true, owned: 1, unlockOK: true}
	pool := &fakePool{conns: []*fakeConn{conn}}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	<-cb.acquired
	cancel()
	waitFor(t, done, "Run exit on parent cancel")

	if e.IsLeader() {
		t.Error("IsLeader should be false after shutdown")
	}
	cb.mu.Lock()
	losses := cb.losses
	cb.mu.Unlock()
	if losses != 0 {
		t.Errorf("onLost called %d times on clean shutdown, want 0", losses)
	}
	if released, _ := conn.snapshot(); !released {
		t.Error("connection was not released on shutdown")
	}
}

// TestOwnershipGoneDetectsLockLoss proves leadership is dropped when the
// ownership query reports the advisory lock is no longer held by this
// backend, even though the connection is still alive.
func TestOwnershipGoneDetectsLockLoss(t *testing.T) {
	conn := &fakeConn{tryLockOK: true, owned: 1, unlockOK: true}
	pool := &fakePool{conns: []*fakeConn{conn}}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	<-cb.acquired
	conn.mu.Lock()
	conn.owned = 0 // lock no longer held by this backend
	conn.mu.Unlock()

	waitFor(t, cb.lost, "onLost after ownership gone")

	// Release happens in leadOnce's defer, just after onLost fires; poll
	// instead of asserting immediately to avoid racing it.
	waitForCond(t, "release after losing the lock", func() bool {
		released, _ := conn.snapshot()
		return released
	})

	cancel()
	waitFor(t, done, "Run exit")
}

// TestNeverAcquiresExitsOnCancel proves the acquire poll loop is cancellable
// when the lock is held by another process forever.
func TestNeverAcquiresExitsOnCancel(t *testing.T) {
	conn := &fakeConn{tryLockOK: false}
	pool := &fakePool{conns: []*fakeConn{conn}}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	waitFor(t, done, "Run exit while lock held elsewhere")

	cb.mu.Lock()
	acquires := cb.acquires
	cb.mu.Unlock()
	if acquires != 0 {
		t.Errorf("onAcquire called %d times, want 0", acquires)
	}
	if released, _ := conn.snapshot(); !released {
		t.Error("connection was not released when lock never acquired")
	}
}

// TestUnlockLogsWhenLockAlreadyGone covers both best-effort release branches:
// pg_advisory_unlock returning false (lock already gone) and erroring.
func TestUnlockLogsWhenLockAlreadyGone(t *testing.T) {
	notHeld := &fakeConn{tryLockOK: true, owned: 1, unlockOK: false}
	errConn := &fakeConn{tryLockOK: true, owned: 1, unlockErr: errors.New("session closed")}

	for name, tc := range map[string]*fakeConn{"not-held": notHeld, "error": errConn} {
		t.Run(name, func(t *testing.T) {
			pool := &fakePool{conns: []*fakeConn{tc}}
			cb := newCallbacks()
			e := fastElector(pool, cb)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				e.Run(ctx)
				close(done)
			}()

			<-cb.acquired
			cancel()
			waitFor(t, done, "Run exit")

			if released, _ := tc.snapshot(); !released {
				t.Error("connection was not released")
			}
			tc.mu.Lock()
			calls := tc.unlockCalls
			tc.mu.Unlock()
			if calls == 0 {
				t.Error("unlock was never attempted")
			}
		})
	}
}

// TestAcquireConnRetriesThenCancels covers the acquire-retry loop: pool
// failures are logged and retried until the parent context is cancelled.
func TestAcquireConnRetriesThenCancels(t *testing.T) {
	// leadOnce with a cancelled context exits before touching the pool.
	e := fastElector(&fakePool{}, newCallbacks())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if conn := e.acquireConn(ctx); conn != nil {
		t.Fatal("acquireConn returned a connection for a cancelled context")
	}

	// A failing pool retries until ctx is done, then gives up.
	pool := &fakePool{} // always errors: no connections queued
	cb := newCallbacks()
	e = fastElector(pool, cb)
	e.acquirePollInterval = time.Millisecond

	runCtx, runCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(runCtx)
		close(done)
	}()
	time.Sleep(15 * time.Millisecond)
	runCancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit while the pool kept failing")
	}
	cb.mu.Lock()
	acquires := cb.acquires
	cb.mu.Unlock()
	if acquires != 0 {
		t.Errorf("onAcquire fired %d times despite pool failures", acquires)
	}
}

// TestTryLockErrorKeepsPolling proves a failing pg_try_advisory_lock query is
// logged and retried rather than treated as leadership.
func TestTryLockErrorKeepsPolling(t *testing.T) {
	conn := &fakeConn{tryLockErr: errors.New("backend restarted"), owned: 0}
	pool := &fakePool{conns: []*fakeConn{conn}}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	waitFor(t, done, "Run exit")

	conn.mu.Lock()
	checks := conn.ownershipChecks // unused guard against optimization
	_ = checks
	tryLocked := conn.tryLockErr != nil
	conn.mu.Unlock()
	if !tryLocked {
		t.Error("expected scripted try-lock error")
	}
	cb.mu.Lock()
	acquires := cb.acquires
	cb.mu.Unlock()
	if acquires != 0 {
		t.Errorf("onAcquire fired %d times despite try-lock errors", acquires)
	}
}

// TestCancelDuringFailedProbeIsCleanShutdown pins the race where the parent
// context is cancelled while an ownership probe is failing: the elector must
// treat it as a clean shutdown (no onLost), not leadership loss.
func TestCancelDuringFailedProbeIsCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &fakeConn{tryLockOK: true, owned: 1, unlockOK: true}
	conn.onOwnership = func() {
		// Runs while fakeConn.QueryRow holds conn.mu: fail the probe and
		// cancel the parent at the same moment.
		conn.mu.Unlock()
		conn.ownedErr = errors.New("probe aborted")
		cancel()
		conn.mu.Lock()
	}
	pool := &fakePool{conns: []*fakeConn{conn}}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()

	<-cb.acquired
	waitFor(t, done, "Run exit")

	cb.mu.Lock()
	losses := cb.losses
	cb.mu.Unlock()
	if losses != 0 {
		t.Errorf("onLost fired %d times for a cancellation raced probe", losses)
	}
}

// TestTransientOwnershipFailureKeepsLeadership covers the branch where the
// ownership query fails but the connection still pings: leadership continues.
func TestTransientOwnershipFailureKeepsLeadership(t *testing.T) {
	e := newElector(&fakePool{}, nil, nil)
	failing := &fakeConn{ownedErr: errors.New("statement timeout")} // Ping succeeds
	if !e.checkAlive(context.Background(), failing) {
		t.Fatal("transient ownership failure was treated as lock loss")
	}
}

// TestRunWithCancelledContextSkipsLead covers leadOnce's bail-out when the
// context is already dead on entry.
func TestRunWithCancelledContextSkipsLead(t *testing.T) {
	pool := &fakePool{}
	cb := newCallbacks()
	e := fastElector(pool, cb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		e.Run(ctx)
		close(done)
	}()
	waitFor(t, done, "Run exit on pre-cancelled context")

	cb.mu.Lock()
	acquires := cb.acquires
	cb.mu.Unlock()
	if acquires != 0 {
		t.Error("onAcquire fired for a cancelled context")
	}
}
