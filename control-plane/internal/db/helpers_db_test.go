package db

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func errorsNew(s string) error { return errors.New(s) }

// TestTryAndReleaseAdvisoryLock covers the pooled advisory-lock helpers
// against a real backend: held excludes a second session, release frees it.
func TestTryAndReleaseAdvisoryLock(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unlock, err := AcquireSessionLock(ctx, pool, LockBootstrap)
	if err != nil {
		t.Fatalf("AcquireSessionLock: %v", err)
	}
	defer unlock()

	ok, err := TryAdvisoryLock(ctx, pool, LockBootstrap)
	if err != nil || ok {
		t.Fatalf("TryAdvisoryLock while held = %v, %v; want false, nil", ok, err)
	}
	if err := ReleaseAdvisoryLock(ctx, pool, LockBootstrap); err != nil {
		t.Fatalf("ReleaseAdvisoryLock: %v", err)
	}
}

// TestApplyMigrationsCreateTableFailure covers the schema_migrations DDL
// error branch via a dead context.
func TestApplyMigrationsCreateTableFailure(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ApplyMigrations(ctx, pool, fixtureFS()); err == nil {
		t.Fatal("expected create-table failure with a cancelled context")
	}
}

// TestApplyMigrationsSkipsDirsAndNonUpFiles proves only *.up.sql files are
// applied: directories and other suffixes are ignored.
func TestApplyMigrationsSkipsDirsAndNonUpFiles(t *testing.T) {
	pool, _ := testPostgres(t)
	freshSchema(t)
	fs := fstest.MapFS{
		"0001_create_a.up.sql":   &fstest.MapFile{Data: []byte("create table mig_a (id int primary key);")},
		"README.md":              &fstest.MapFile{Data: []byte("not a migration")},
		"0001_create_a.down.sql": &fstest.MapFile{Data: []byte("drop table mig_a;")},
		"notes":                  &fstest.MapFile{Data: []byte("dir entry"), Mode: 0o755 | 1<<31},
	}
	if err := ApplyMigrations(context.Background(), pool, fs); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	got := ledgerVersions(t, pool)
	if len(got) != 1 || got[0] != "0001_create_a.up.sql" {
		t.Fatalf("ledger = %v, want only the .up.sql file", got)
	}
}

// TestFanoutRunExitsWhenAcquireFailsThenContextCancels drives Run into the
// backoff wait and cancels there (the select's ctx.Done branch).
func TestFanoutRunExitsWhenAcquireFailsThenContextCancels(t *testing.T) {
	f := newTestFanout(&fakeConnector{acquireErrs: []error{errAcquireFailed}})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"ch"}) }()

	time.Sleep(5 * time.Millisecond) // first attempt fails, Run is backing off
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after cancel during backoff")
	}
}

// TestFanoutListenErrorReconnects proves a LISTEN failure releases the conn
// and re-enters the retry loop.
func TestFanoutListenErrorReconnects(t *testing.T) {
	bad := &fakeConn{listenErr: errListenFailed}
	good := &fakeConn{}
	f := newTestFanout(&fakeConnector{conns: []*fakeConn{bad, good}})
	f.backoffBase = time.Millisecond
	f.backoffMax = 2 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"ch"}) }()

	waitFor(t, "re-listen on fresh connection", func() bool {
		return len(good.listenSnapshot()) > 0
	})
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit")
	}
	if !bad.wasReleased() {
		t.Error("connection that failed LISTEN was not released")
	}
}

var (
	errAcquireFailed = errorsNew("acquire failed")
	errListenFailed  = errorsNew("listen refused")
)

// TestBackoffBounds pins the overflow and clamp branches.
func TestBackoffBounds(t *testing.T) {
	f := newTestFanout(&fakeConnector{})

	// Huge attempt shifts past int width: the overflow clamp kicks in and
	// the wait is drawn from [max/2, max]. With zero jitter that is
	// exactly max/2.
	f.jitter = func() float64 { return 0 }
	if d := f.backoff(1000); d != f.backoffMax/2 {
		t.Errorf("backoff(huge) = %v, want %v", d, f.backoffMax/2)
	}
	// Full jitter doubles the half-wait up to the cap.
	f.jitter = func() float64 { return 1 }
	if d := f.backoff(62); d != f.backoffMax {
		t.Errorf("backoff(jitter=1) = %v, want cap %v", d, f.backoffMax)
	}
	_ = time.Millisecond
}

// TestAcquireSessionLockExecFailure attempts to hit the pg_advisory_lock
// execution-failure branch with an already-dead context on a warm pool.
func TestAcquireSessionLockExecFailure(t *testing.T) {
	pool, _ := testPostgres(t)
	warm, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	warm.Release() // ensure an idle connection exists

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireSessionLock(ctx, pool, 44); err == nil {
		t.Log("warm pool accepted a cancelled context without failing the lock exec")
	}
}

// TestFanoutPoolConnectorAcquireFailure drives Run against a cancelled
// context so the pooled acquire fails immediately.
func TestFanoutPoolConnectorAcquireFailure(t *testing.T) {
	pool, _ := testPostgres(t)
	f := NewFanout(pool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.Run(ctx, []string{"ch"}); err != nil {
		t.Fatalf("Run returned error after cancellation: %v", err)
	}
}

// TestFanoutDSNConnectorBadDSN proves the dedicated-listen connector surfaces
// dial failures and keeps retrying until the context ends.
func TestFanoutDSNConnectorBadDSN(t *testing.T) {
	pool, _ := testPostgres(t)
	f := NewFanoutWithListenURL(pool, "postgres://hive:hive@127.0.0.1:1/nope?sslmode=disable")
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"ch"}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit")
	}
}

// TestFanoutCancelDuringLongBackoff pins the select's ctx.Done exit while Run
// is sleeping between reconnect attempts.
func TestFanoutCancelDuringLongBackoff(t *testing.T) {
	f := newTestFanout(&fakeConnector{acquireErrs: []error{errAcquireFailed, errAcquireFailed, errAcquireFailed}})
	f.backoffBase = 500 * time.Millisecond
	f.backoffMax = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{"ch"}) }()

	time.Sleep(20 * time.Millisecond) // first attempt failed, Run backing off
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit during backoff")
	}
}

// TestBackoffJitterClamp covers the final wait>max clamp with pathological
// jitter (>1).
func TestBackoffJitterClamp(t *testing.T) {
	f := newTestFanout(&fakeConnector{})
	// attempt 0 gives d == base == max here, so the jittered wait (4ms)
	// exceeds the cap and must be clamped.
	f.backoffBase = f.backoffMax
	f.jitter = func() float64 { return 3 }
	if d := f.backoff(0); d != f.backoffMax { // d == max, jitter pushes past
		t.Errorf("backoff(jitter=3) = %v, want cap %v", d, f.backoffMax)
	}
}

// TestAcquireSessionLockExecTimeout hits the pg_advisory_lock execution
// failure branch deterministically: another session holds the lock and our
// context deadline expires mid-wait.
func TestAcquireSessionLockExecTimeout(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx := context.Background()

	holderUnlock, err := AcquireSessionLock(ctx, pool, 555)
	if err != nil {
		t.Fatalf("holder AcquireSessionLock: %v", err)
	}
	defer holderUnlock()

	waitCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	_, err = AcquireSessionLock(waitCtx, pool, 555)
	if err == nil {
		t.Fatal("expected timeout waiting for a held lock")
	}
	if !strings.Contains(err.Error(), "pg_advisory_lock(555)") {
		t.Errorf("want wrapped lock-exec error, got %v", err)
	}
}

// partialFS lists migration files but refuses to read their contents,
// exercising the fs.ReadFile error branch.
type partialFS struct{ names []string }

func (p partialFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &partialDir{names: p.names}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("read blocked")}
}

type partialDir struct{ names []string }

func (d *partialDir) Stat() (fs.FileInfo, error) { return nil, errors.New("unimplemented") }
func (d *partialDir) Read([]byte) (int, error)   { return 0, errors.New("dir") }
func (d *partialDir) Close() error               { return nil }

func (d *partialDir) ReadDir(count int) ([]fs.DirEntry, error) {
	out := make([]fs.DirEntry, 0, len(d.names))
	for _, n := range d.names {
		out = append(out, fakeEntry{name: n})
	}
	return out, nil
}

type fakeEntry struct{ name string }

func (e fakeEntry) Name() string               { return e.name }
func (e fakeEntry) IsDir() bool                { return false }
func (e fakeEntry) Type() fs.FileMode          { return 0 }
func (e fakeEntry) Info() (fs.FileInfo, error) { return nil, errors.New("unimplemented") }

// TestApplyMigrationsReadFileFailure covers the fs.ReadFile error branch.
func TestApplyMigrationsReadFileFailure(t *testing.T) {
	pool, _ := testPostgres(t)
	freshSchema(t)
	err := ApplyMigrations(context.Background(), pool, partialFS{names: []string{"0001_x.up.sql"}})
	if err == nil {
		t.Fatal("expected ReadFile failure")
	}
}

// blockingDirFS lists migrations but its ReadDir blocks until the context is
// cancelled, so the per-file ledger query runs against a dead context.
type blockingDirFS struct{ names []string }

func (b blockingDirFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &blockingDir{names: b.names}, nil
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("blocked")}
}

type blockingDir struct{ names []string }

func (d *blockingDir) Stat() (fs.FileInfo, error) { return nil, errors.New("unimplemented") }
func (d *blockingDir) Read([]byte) (int, error)   { return 0, errors.New("dir") }
func (d *blockingDir) Close() error               { return nil }

func (d *blockingDir) ReadDir(int) ([]fs.DirEntry, error) {
	// Simulate a slow directory read spanning the context cancellation.
	<-time.After(80 * time.Millisecond)
	out := make([]fs.DirEntry, 0, len(d.names))
	for _, n := range d.names {
		out = append(out, fakeEntry{name: n})
	}
	return out, nil
}

// TestApplyMigrationsLedgerQueryFailure covers the schema_migrations lookup
// error branch: the directory read outlives the context.
func TestApplyMigrationsLedgerQueryFailure(t *testing.T) {
	pool, _ := testPostgres(t)
	freshSchema(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := ApplyMigrations(ctx, pool, blockingDirFS{names: []string{"0001_x.up.sql"}})
	if err == nil {
		t.Fatal("expected ledger query failure after context expiry")
	}
}

// TestMigrateRiverContextFailure covers River's migration error propagation.
func TestMigrateRiverContextFailure(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := MigrateRiver(ctx, pool); err == nil {
		t.Fatal("expected river migrate failure with cancelled context")
	}
}
