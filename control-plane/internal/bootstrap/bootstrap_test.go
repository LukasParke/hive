package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luke/hive/control-plane/internal/db"
)

// fakeLockClient is a scriptable LockClient.
type fakeLockClient struct {
	mu sync.Mutex

	// lockTaken tracks the advisory lock: only one holder at a time, like
	// Postgres session locks.
	lockTaken atomic.Bool
	refuse    bool // TryAdvisoryLock returns false while set

	settings     map[string]json.RawMessage
	probeErr     error
	tryLockErr   error
	seedErr      error
	releaseCalls int
	seedCalls    atomic.Int32
}

func newFakeLockClient() *fakeLockClient {
	return &fakeLockClient{settings: map[string]json.RawMessage{}}
}

func (f *fakeLockClient) TryAdvisoryLock(ctx context.Context, id int64) (bool, error) {
	if f.tryLockErr != nil {
		return false, f.tryLockErr
	}
	if id != db.LockBootstrap {
		return false, errors.New("unexpected lock id")
	}
	if f.refuse {
		return false, nil
	}
	if f.lockTaken.CompareAndSwap(false, true) {
		return true, nil
	}
	return false, nil
}

func (f *fakeLockClient) ReleaseAdvisoryLock(ctx context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	f.lockTaken.Store(false)
	return nil
}

func (f *fakeLockClient) HasAnySetting(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.probeErr != nil {
		return false, f.probeErr
	}
	return len(f.settings) > 0, nil
}

func (f *fakeLockClient) SeedSettings(ctx context.Context, defaults map[string]json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seedErr != nil {
		return f.seedErr
	}
	f.seedCalls.Add(1)
	for k, v := range defaults {
		if _, ok := f.settings[k]; !ok {
			f.settings[k] = v
		}
	}
	return nil
}

func (f *fakeLockClient) settingCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.settings)
}

// counters tracks migrate invocations across "processes".
type counters struct {
	migrate atomic.Int32
}

// TestBootstrapRaceOnlyOneProcessInitializes simulates two replicas racing
// to boot: replica A takes the bootstrap lock and initializes; replica B
// starts while the lock is held, waits for A's settings record, and proceeds
// without running migrations or seeding.
func TestBootstrapRaceOnlyOneProcessInitializes(t *testing.T) {
	lc := newFakeLockClient()
	cnt := &counters{}

	// Replica A's migrate blocks until replica B has attempted the lock, so
	// B is guaranteed to hit TryAdvisoryLock while A still holds it — a true
	// race loser — before A seeds the settings record that releases B.
	aMigrating := make(chan struct{})
	releaseA := make(chan struct{})
	migrateA := func(context.Context) error {
		close(aMigrating)
		<-releaseA
		cnt.migrate.Add(1)
		return nil
	}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = Run(context.Background(), lc, migrateA, Options{
			WaitTimeout:  5 * time.Second,
			PollInterval: time.Millisecond,
		})
	}()
	<-aMigrating // A holds the bootstrap lock and is initializing
	go func() {
		defer wg.Done()
		errs[1] = Run(context.Background(), lc, migrateFunc(cnt), Options{
			WaitTimeout:  5 * time.Second,
			PollInterval: time.Millisecond,
		})
	}()
	// Give B a moment to attempt (and lose) the lock, then let A finish.
	time.Sleep(5 * time.Millisecond)
	close(releaseA)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d: Run: %v", i, err)
		}
	}
	if n := cnt.migrate.Load(); n != 1 {
		t.Errorf("migrations ran %d times, want 1", n)
	}
	if n := lc.seedCalls.Load(); n != 1 {
		t.Errorf("seeding ran %d times, want 1", n)
	}
	if lc.settingCount() == 0 {
		t.Error("expected seeded settings record")
	}
	if lc.releaseCalls != 1 {
		t.Errorf("bootstrap lock released %d times, want 1", lc.releaseCalls)
	}
}

func migrateFunc(c *counters) func(context.Context) error {
	return func(context.Context) error {
		c.migrate.Add(1)
		return nil
	}
}

// TestRunSeedsAndMigratesWhenLockAcquired proves the winning path: take the
// lock, run migrations, seed defaults, release the lock.
func TestRunSeedsAndMigratesWhenLockAcquired(t *testing.T) {
	lc := newFakeLockClient()
	cnt := &counters{}

	if err := Run(context.Background(), lc, migrateFunc(cnt), Options{PollInterval: time.Millisecond}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cnt.migrate.Load() != 1 || lc.seedCalls.Load() != 1 {
		t.Errorf("migrate=%d seed=%d, want 1/1", cnt.migrate.Load(), lc.seedCalls.Load())
	}
	if lc.settingCount() == 0 {
		t.Error("expected settings seeded")
	}
	raw, ok := lc.settings["bootstrap"]
	if !ok {
		t.Fatal("expected bootstrap marker setting")
	}
	var marker struct {
		InitializedAt string `json:"initialized_at"`
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		t.Fatalf("marker not valid JSON: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, marker.InitializedAt); err != nil {
		t.Errorf("initialized_at not RFC3339: %v", err)
	}
	if lc.releaseCalls != 1 {
		t.Errorf("lock released %d times, want 1", lc.releaseCalls)
	}
}

// TestRunWaitsForPeerSettingsRecord proves the losing replica does not run
// migrations or seeding when the peer's settings record appears in time.
func TestRunWaitsForPeerSettingsRecord(t *testing.T) {
	lc := newFakeLockClient()
	lc.refuse = true // lock always held by the peer
	cnt := &counters{}

	// Peer finishes bootstrapping shortly after Run starts.
	go func() {
		time.Sleep(10 * time.Millisecond)
		lc.mu.Lock()
		lc.settings["bootstrap"] = json.RawMessage(`{"initialized_at":"2026-01-01T00:00:00Z"}`)
		lc.mu.Unlock()
	}()

	if err := Run(context.Background(), lc, migrateFunc(cnt), Options{
		WaitTimeout:  5 * time.Second,
		PollInterval: time.Millisecond,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cnt.migrate.Load() != 0 || lc.seedCalls.Load() != 0 {
		t.Errorf("loser initialized: migrate=%d seed=%d, want 0/0", cnt.migrate.Load(), lc.seedCalls.Load())
	}
}

// TestRunTimesOutAndInitializesLocally proves the timeout fallback: if the
// peer dies mid-bootstrap (settings never appear), this replica initializes
// itself instead of failing or hanging forever.
func TestRunTimesOutAndInitializesLocally(t *testing.T) {
	lc := newFakeLockClient()
	lc.refuse = true
	cnt := &counters{}

	if err := Run(context.Background(), lc, migrateFunc(cnt), Options{
		WaitTimeout:  20 * time.Millisecond,
		PollInterval: time.Millisecond,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cnt.migrate.Load() != 1 || lc.seedCalls.Load() != 1 {
		t.Errorf("fallback init: migrate=%d seed=%d, want 1/1", cnt.migrate.Load(), lc.seedCalls.Load())
	}
}

// TestRunCancelledWhileWaiting proves a cancelled context surfaces from the
// peer wait instead of triggering the timeout fallback.
func TestRunCancelledWhileWaiting(t *testing.T) {
	lc := newFakeLockClient()
	lc.refuse = true
	cnt := &counters{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, lc, migrateFunc(cnt), Options{
			WaitTimeout:  30 * time.Second,
			PollInterval: time.Millisecond,
		})
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if cnt.migrate.Load() != 0 || lc.seedCalls.Load() != 0 {
		t.Errorf("cancelled wait initialized: migrate=%d seed=%d, want 0/0", cnt.migrate.Load(), lc.seedCalls.Load())
	}
}

// TestSeedSettingsIdempotent proves seeding never overwrites existing keys.
func TestSeedSettingsIdempotent(t *testing.T) {
	lc := newFakeLockClient()
	lc.mu.Lock()
	lc.settings["bootstrap"] = json.RawMessage(`{"initialized_at":"2000-01-01T00:00:00Z"}`)
	lc.mu.Unlock()

	defaults := DefaultSettings(time.Now())
	if err := lc.SeedSettings(context.Background(), defaults); err != nil {
		t.Fatalf("SeedSettings: %v", err)
	}
	lc.mu.Lock()
	got := string(lc.settings["bootstrap"])
	lc.mu.Unlock()
	if got != `{"initialized_at":"2000-01-01T00:00:00Z"}` {
		t.Errorf("existing setting overwritten: %s", got)
	}
}

// TestOptionsDefaults covers the zero-value fallbacks of Options.
func TestOptionsDefaults(t *testing.T) {
	opts := Options{}
	if got := opts.waitTimeout(); got != DefaultWaitTimeout {
		t.Errorf("waitTimeout() = %v, want %v", got, DefaultWaitTimeout)
	}
	if got := opts.pollInterval(); got != DefaultPollInterval {
		t.Errorf("pollInterval() = %v, want %v", got, DefaultPollInterval)
	}
	custom := Options{WaitTimeout: time.Second, PollInterval: time.Millisecond}
	if custom.waitTimeout() != time.Second || custom.pollInterval() != time.Millisecond {
		t.Error("custom options not honored")
	}
}

// TestRunTryLockErrorPropagates proves a failing advisory-lock probe surfaces
// from Run instead of being treated as "lock busy".
func TestRunTryLockErrorPropagates(t *testing.T) {
	lc := newFakeLockClient()
	lc.tryLockErr = errors.New("postgres down")
	err := Run(context.Background(), lc, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "try bootstrap lock") {
		t.Fatalf("want try-lock error, got %v", err)
	}
}

// TestRunSeedErrorPropagates proves a seeding failure aborts boot with the
// wrapped error.
func TestRunSeedErrorPropagates(t *testing.T) {
	lc := newFakeLockClient()
	lc.seedErr = errors.New("disk full")
	err := Run(context.Background(), lc, nil, Options{})
	if err == nil || !strings.Contains(err.Error(), "seed settings") {
		t.Fatalf("want seed error, got %v", err)
	}
}

// TestRunProbeErrorsKeepWaitingUntilTimeout proves transient settings-probe
// failures do not crash the wait loop; the replica falls back to local
// initialization at the deadline.
func TestRunProbeErrorsKeepWaitingUntilTimeout(t *testing.T) {
	lc := newFakeLockClient()
	lc.refuse = true
	lc.probeErr = errors.New("connection reset")
	cnt := &counters{}

	if err := Run(context.Background(), lc, migrateFunc(cnt), Options{
		WaitTimeout:  15 * time.Millisecond,
		PollInterval: time.Millisecond,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cnt.migrate.Load() != 1 || lc.seedCalls.Load() != 1 {
		t.Errorf("fallback after probe errors: migrate=%d seed=%d, want 1/1", cnt.migrate.Load(), lc.seedCalls.Load())
	}
}

// TestRunMigrateErrorPropagates proves migration failures abort boot.
func TestRunMigrateErrorPropagates(t *testing.T) {
	lc := newFakeLockClient()
	err := Run(context.Background(), lc, func(context.Context) error {
		return errors.New("boom")
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("want migrate error, got %v", err)
	}
	if lc.seedCalls.Load() != 0 {
		t.Error("seeding ran despite migration failure")
	}
}
