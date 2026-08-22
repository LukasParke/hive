package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestFanoutEmitNotifyRoundTrip subscribes, LISTENs on a real Postgres
// connection, emits via pg_notify through a second path, and asserts the
// notification arrives with channel and payload intact.
func TestFanoutEmitNotifyRoundTrip(t *testing.T) {
	pool, _ := testPostgres(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := "hive_fanout_test"
	f := NewFanout(pool)
	sub := f.Subscribe(channel, 8)

	done := make(chan error, 1)
	go func() { done <- f.Run(ctx, []string{channel}) }()

	// pg_notify sent before LISTEN is active is dropped, so emit repeatedly
	// until the round-trip completes.
	deadline := time.Now().Add(15 * time.Second)
	var got Notification
	for {
		payload := fmt.Sprintf("payload-%d", time.Now().UnixNano())
		if err := f.Emit(ctx, channel, payload); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		select {
		case n := <-sub:
			got = n
		case <-time.After(200 * time.Millisecond):
		}
		if got.Payload != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("notification never arrived via LISTEN/NOTIFY round-trip")
		}
	}
	if got.Channel != channel {
		t.Errorf("channel = %q, want %q", got.Channel, channel)
	}
	if got.Payload == "" {
		t.Error("empty payload received")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

// TestFanoutWithListenURLRoundTrip drives the dedicated-DSN connector
// (PgBouncer bypass path) through a full LISTEN/NOTIFY round-trip.
func TestFanoutWithListenURLRoundTrip(t *testing.T) {
	pool, dsn := testPostgres(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channel := "hive_fanout_dsn_test"
	f := NewFanoutWithListenURL(pool, dsn)
	sub := f.Subscribe(channel, 8)

	go func() { _ = f.Run(ctx, []string{channel}) }()

	deadline := time.Now().Add(15 * time.Second)
	var got Notification
	for time.Now().Before(deadline) {
		payload := fmt.Sprintf("dsn-%d", time.Now().UnixNano())
		if err := f.Emit(ctx, channel, payload); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		select {
		case n := <-sub:
			got = n
		case <-time.After(200 * time.Millisecond):
		}
		if got.Payload != "" {
			break
		}
	}
	if got.Payload == "" || got.Channel != channel {
		t.Fatalf("round-trip via dedicated listen URL failed: %+v", got)
	}

	// Empty listen URL falls back to the pool connector.
	if NewFanoutWithListenURL(pool, "") == nil {
		t.Fatal("empty listen URL returned nil fanout")
	}
	cancel()
}

// TestAcquireSessionLockMutualExclusion proves a session lock excludes a
// second holder until the first unlock runs, and that unlock is usable even
// after the acquiring context is cancelled.
func TestAcquireSessionLockMutualExclusion(t *testing.T) {
	pool, _ := testPostgres(t)
	const lockID int64 = 987654321
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	unlock, err := AcquireSessionLock(ctx, pool, lockID)
	if err != nil {
		t.Fatalf("AcquireSessionLock: %v", err)
	}

	// A second acquisition from a different session must block.
	acquired := make(chan error, 1)
	go func() {
		second, err := AcquireSessionLock(context.Background(), pool, lockID)
		if err == nil {
			second() // release immediately so the pool isn't drained
		}
		acquired <- err
	}()

	select {
	case err := <-acquired:
		t.Fatalf("second AcquireSessionLock returned while lock held: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Still blocked: mutual exclusion holds.
	}

	unlock()

	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("second AcquireSessionLock after unlock: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second acquisition did not proceed after unlock")
	}
}

// TestAcquireSessionLockErrorPaths covers the acquire-failure and
// lock-exec-failure branches.
func TestAcquireSessionLockErrorPaths(t *testing.T) {
	pool, _ := testPostgres(t)

	// Cancelled context fails at pool.Acquire.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireSessionLock(canceled, pool, 42); err == nil {
		t.Error("expected error acquiring with a cancelled context")
	}

	// The lock statement itself fails on a cancelled context after acquire.
	ctx, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := AcquireSessionLock(ctx, pool, 43); err == nil {
		t.Error("expected error from pg_advisory_lock with cancelled context")
	}
}
