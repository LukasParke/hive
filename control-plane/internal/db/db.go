package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Advisory lock IDs used by control-plane subsystems.
const (
	LockLeaderElection int64 = 1
	LockBootstrap      int64 = 2
	LockCertRenewal    int64 = 3
)

// NewPool opens a pgx pool with the control-plane connection defaults.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 5
	cfg.HealthCheckPeriod = 30 * time.Second
	return pgxpool.NewWithConfig(ctx, cfg)
}

// TryAdvisoryLock reports whether the given advisory lock was acquired.
func TryAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockID int64) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, "select pg_try_advisory_lock($1)", lockID).Scan(&ok)
	return ok, err
}

// ReleaseAdvisoryLock releases the given advisory lock.
func ReleaseAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockID int64) error {
	_, err := pool.Exec(ctx, "select pg_advisory_unlock($1)", lockID)
	return err
}

// WaitForPing blocks until the pool answers a ping or maxWait elapses.
func WaitForPing(ctx context.Context, pool *pgxpool.Pool, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		if err := pool.Ping(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("postgres did not become ready before timeout")
		}
		time.Sleep(1 * time.Second)
	}
}

// AcquireSessionLock takes a session-level advisory lock (pg_advisory_lock)
// on a dedicated pooled connection held until the returned unlock func is
// called. The lock is released and the connection returned to the pool only
// via the unlock func; use defer immediately after acquisition.
func AcquireSessionLock(ctx context.Context, pool *pgxpool.Pool, id int64) (func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire conn for session lock %d: %w", id, err)
	}
	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", id); err != nil {
		conn.Release()
		return nil, fmt.Errorf("pg_advisory_lock(%d): %w", id, err)
	}
	unlock := func() {
		// Detached context: unlocking must succeed even if the caller's
		// request context was cancelled while holding the lock.
		_, _ = conn.Exec(context.WithoutCancel(ctx), "select pg_advisory_unlock($1)", id)
		conn.Release()
	}
	return unlock, nil
}
