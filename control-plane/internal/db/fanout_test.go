package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeConn records LISTEN calls and scripts WaitForNotification results.
type fakeConn struct {
	mu        sync.Mutex
	listens   []string
	listenErr error                  // returned by Listen when set
	waitErrs  []error                // consumed one per WaitForNotification call
	notifs    []*pgconn.Notification // delivered after waitErrs are exhausted
	released  bool
}

func (c *fakeConn) Listen(_ context.Context, channel string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listenErr != nil {
		return c.listenErr
	}
	c.listens = append(c.listens, channel)
	return nil
}

func (c *fakeConn) WaitForNotification(ctx context.Context) (*pgconn.Notification, error) {
	c.mu.Lock()
	if len(c.waitErrs) > 0 {
		err := c.waitErrs[0]
		c.waitErrs = c.waitErrs[1:]
		c.mu.Unlock()
		return nil, err
	}
	if len(c.notifs) > 0 {
		n := c.notifs[0]
		c.notifs = c.notifs[1:]
		c.mu.Unlock()
		return n, nil
	}
	// Scripted content exhausted: block until the context is cancelled,
	// like a healthy idle connection would. The lock must NOT be held
	// while waiting, or Release/listenSnapshot would deadlock.
	c.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *fakeConn) Release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.released = true
}

func (c *fakeConn) listenSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.listens))
	copy(out, c.listens)
	return out
}

func (c *fakeConn) wasReleased() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.released
}

// fakeConnector hands out scripted connection failures followed by the
// queued fake connections in order.
type fakeConnector struct {
	mu          sync.Mutex
	acquireErrs []error
	conns       []*fakeConn
	acquires    int
}

func (f *fakeConnector) acquire(context.Context) (fanoutConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acquires++
	if len(f.acquireErrs) > 0 {
		err := f.acquireErrs[0]
		f.acquireErrs = f.acquireErrs[1:]
		return nil, err
	}
	if len(f.conns) == 0 {
		return nil, errors.New("fakeConnector: no connections queued")
	}
	conn := f.conns[0]
	f.conns = f.conns[1:]
	return conn, nil
}

func newTestFanout(c fanoutConnector) *Fanout {
	f := newFanout(nil, c)
	f.backoffBase = 2 * time.Millisecond
	f.backoffMax = 20 * time.Millisecond
	f.jitter = func() float64 { return 0.5 }
	return f
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestRunReconnectsReissuesListenAndKeepsSubscribers proves that after an
// acquire failure and a mid-stream connection drop, Run re-acquires, re-issues
// LISTEN for every channel, releases the dead connection, and keeps delivering
// notifications to subscriber channels registered before the drop.
func TestRunReconnectsReissuesListenAndKeepsSubscribers(t *testing.T) {
	dropped := &fakeConn{waitErrs: []error{errors.New("connection reset by peer")}}
	healthy := &fakeConn{notifs: []*pgconn.Notification{
		{Channel: "system", Payload: `{"before":"drop"}`},
		{Channel: "system", Payload: `{"after":"reconnect"}`},
	}}
	// First acquire fails entirely, second hands out a conn that drops,
	// third hands out the healthy conn.
	connector := &fakeConnector{
		acquireErrs: []error{errors.New("pool exhausted")},
		conns:       []*fakeConn{dropped, healthy},
	}
	f := newTestFanout(connector)

	ch := f.Subscribe("system", 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"system", "deployment:app1"}) }()

	for _, want := range []string{`{"before":"drop"}`, `{"after":"reconnect"}`} {
		select {
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for notification %q", want)
		case got := <-ch:
			if got.Payload != want {
				t.Fatalf("payload = %q, want %q", got.Payload, want)
			}
		}
	}

	wantChannels := []string{"system", "deployment:app1"}
	for name, conn := range map[string]*fakeConn{"dropped": dropped, "healthy": healthy} {
		got := conn.listenSnapshot()
		if len(got) != len(wantChannels) {
			t.Fatalf("%s conn listens = %v, want %v", name, got, wantChannels)
		}
		for i, chName := range wantChannels {
			if got[i] != chName {
				t.Fatalf("%s conn listens[%d] = %q, want %q", name, i, got[i], chName)
			}
		}
	}
	if !dropped.wasReleased() {
		t.Error("dropped connection was not released")
	}
	if healthy.wasReleased() {
		t.Error("healthy connection released while still listening")
	}

	cancel()
	select {
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on graceful close: %v", err)
		}
	}
}

// TestRunRetriesUntilConnectorSucceeds proves Run keeps retrying while the
// connector keeps failing, without returning early.
func TestRunRetriesUntilConnectorSucceeds(t *testing.T) {
	conn := &fakeConn{}
	connector := &fakeConnector{
		acquireErrs: []error{
			errors.New("fail 1"),
			errors.New("fail 2"),
			errors.New("fail 3"),
		},
		conns: []*fakeConn{conn},
	}
	f := newTestFanout(connector)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"system"}) }()

	waitFor(t, "successful acquire after retries", func() bool {
		connector.mu.Lock()
		defer connector.mu.Unlock()
		return len(connector.acquireErrs) == 0 && connector.acquires == 4
	})

	cancel()
	select {
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	}
}

// TestRunReturnsNilWhenCancelledWhileWaiting covers graceful shutdown when
// the context dies during WaitForNotification rather than between retries.
func TestRunReturnsNilWhenCancelledWhileWaiting(t *testing.T) {
	conn := &fakeConn{} // blocks in WaitForNotification until ctx done
	f := newTestFanout(&fakeConnector{conns: []*fakeConn{conn}})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"system"}) }()

	waitFor(t, "LISTEN issued", func() bool { return len(conn.listenSnapshot()) == 1 })

	cancel()
	select {
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error on cancellation: %v", err)
		}
	}
	if !conn.wasReleased() {
		t.Error("connection was not released on graceful close")
	}
}

// TestBackoffGrowsAndCaps checks exponential growth from the base, the 30s
// cap, and jitter staying inside [d/2, d].
func TestBackoffGrowsAndCaps(t *testing.T) {
	f := newFanout(&pgxpool.Pool{}, &fakeConnector{}) // defaults: 1s base, 30s cap
	f.jitter = func() float64 { return 0.5 }

	prev := time.Duration(0)
	for attempt := range 6 {
		got := f.backoff(attempt)
		wantMid := f.backoffBase << min(attempt, 62)
		if wantMid > f.backoffMax || wantMid <= 0 {
			wantMid = f.backoffMax
		}
		want := wantMid/2 + time.Duration(0.5*float64(wantMid/2))
		if got != want {
			t.Fatalf("backoff(%d) = %v, want %v", attempt, got, want)
		}
		if got <= prev {
			t.Fatalf("backoff(%d) = %v did not grow past previous %v", attempt, got, prev)
		}
		prev = got
	}

	// Far past the cap it must never exceed backoffMax even with max jitter.
	f.jitter = func() float64 { return 1.0 }
	if got := f.backoff(100); got > f.backoffMax {
		t.Fatalf("backoff(100) = %v exceeds cap %v", got, f.backoffMax)
	}
}

// Regression: the connector refactor left Fanout.pool nil, so Emit panicked
// on first pg_notify (caught by the dind smoke run as a watcher crash).
func TestFanoutConstructorsSetPool(t *testing.T) {
	pool := &pgxpool.Pool{}
	if f := NewFanout(pool); f.pool != pool {
		t.Fatal("NewFanout must store the pool for Emit")
	}
	if f := NewFanoutWithListenURL(pool, "postgres://listen-only"); f.pool != pool {
		t.Fatal("NewFanoutWithListenURL must store the pool for Emit")
	}
	if f := NewFanoutWithListenURL(pool, ""); f.pool != pool {
		t.Fatal("empty listen URL must fall back to pool connector AND keep pool")
	}
}
