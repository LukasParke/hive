package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	LockLeaderElection int64 = 1
	LockBootstrap      int64 = 2
	LockCertRenewal    int64 = 3
)

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

func TryAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockID int64) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, "select pg_try_advisory_lock($1)", lockID).Scan(&ok)
	return ok, err
}

func ReleaseAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockID int64) error {
	_, err := pool.Exec(ctx, "select pg_advisory_unlock($1)", lockID)
	return err
}

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
