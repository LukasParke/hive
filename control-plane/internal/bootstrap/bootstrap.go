package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/db"
)

// Timing per the HA plan (P7.3): a replica that loses the bootstrap race
// waits up to 60s for the winner's settings record to appear, polling every
// 500ms.
const (
	DefaultWaitTimeout  = 60 * time.Second
	DefaultPollInterval = 500 * time.Millisecond
)

// LockClient abstracts the advisory-lock and settings probes so first-boot
// initialization can be unit-tested without a database. NewLockClient
// provides the Postgres-backed implementation.
type LockClient interface {
	TryAdvisoryLock(ctx context.Context, id int64) (bool, error)
	ReleaseAdvisoryLock(ctx context.Context, id int64) error
	HasAnySetting(ctx context.Context) (bool, error)
	SeedSettings(ctx context.Context, defaults map[string]json.RawMessage) error
}

// Options tunes the peer-wait behavior.
type Options struct {
	// WaitTimeout bounds how long a replica waits for the bootstrapping
	// peer's settings record before proceeding on its own.
	WaitTimeout time.Duration
	// PollInterval is the settings-record poll period while waiting.
	PollInterval time.Duration
}

func (o Options) waitTimeout() time.Duration {
	if o.WaitTimeout > 0 {
		return o.WaitTimeout
	}
	return DefaultWaitTimeout
}

func (o Options) pollInterval() time.Duration {
	if o.PollInterval > 0 {
		return o.PollInterval
	}
	return DefaultPollInterval
}

// DefaultSettings returns the settings rows seeded on first boot. The
// bootstrap marker doubles as the "settings record" whose presence tells a
// waiting replica that first-boot initialization has finished.
func DefaultSettings(now time.Time) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"bootstrap": json.RawMessage(fmt.Sprintf(`{"initialized_at":%q}`, now.UTC().Format(time.RFC3339))),
	}
}

// Run performs first-boot initialization protected against replica races.
// Exactly one process takes the LockBootstrap advisory lock and runs migrate
// plus settings seeding; peers wait for the resulting settings record (up to
// Options.WaitTimeout) and then proceed. If the wait times out — e.g. the
// winning replica died mid-bootstrap — this replica runs the idempotent
// migration and seeding itself rather than failing boot.
func Run(ctx context.Context, lc LockClient, migrate func(context.Context) error, opts Options) error {
	ok, err := lc.TryAdvisoryLock(ctx, db.LockBootstrap)
	if err != nil {
		return fmt.Errorf("try bootstrap lock %d: %w", db.LockBootstrap, err)
	}
	if ok {
		defer func() { _ = lc.ReleaseAdvisoryLock(context.WithoutCancel(ctx), db.LockBootstrap) }()
		return initialize(ctx, lc, migrate)
	}

	log.Printf("bootstrap lock held by another replica; waiting up to %s for its settings record", opts.waitTimeout())
	if err := waitForSettings(ctx, lc, opts); err != nil {
		var timeoutErr errWaitTimeout
		if !errors.As(err, &timeoutErr) {
			return err
		}
		log.Printf("bootstrap: %v; initializing locally", err)
		return initialize(ctx, lc, migrate)
	}
	return nil
}

type errWaitTimeout struct{ d time.Duration }

func (e errWaitTimeout) Error() string {
	return fmt.Sprintf("timed out waiting for bootstrap settings record after %s", e.d)
}

func initialize(ctx context.Context, lc LockClient, migrate func(context.Context) error) error {
	if migrate != nil {
		if err := migrate(ctx); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := lc.SeedSettings(ctx, DefaultSettings(time.Now())); err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}
	return nil
}

func waitForSettings(ctx context.Context, lc LockClient, opts Options) error {
	timeout := opts.waitTimeout()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(opts.pollInterval())
	defer tick.Stop()
	for {
		present, err := lc.HasAnySetting(ctx)
		if err != nil {
			log.Printf("bootstrap: settings probe failed: %v", err)
		} else if present {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for bootstrap settings cancelled: %w", ctx.Err())
		case <-deadline.C:
			return errWaitTimeout{d: timeout}
		case <-tick.C:
		}
	}
}

// pgxLockClient is the Postgres-backed LockClient.
type pgxLockClient struct{ pool *pgxpool.Pool }

// NewLockClient returns a LockClient backed by the given pool.
func NewLockClient(pool *pgxpool.Pool) LockClient { return pgxLockClient{pool: pool} }

// TryAdvisoryLock attempts to take the session-scoped advisory lock with the
// given ID, reporting whether it was acquired.
func (c pgxLockClient) TryAdvisoryLock(ctx context.Context, id int64) (bool, error) {
	return db.TryAdvisoryLock(ctx, c.pool, id)
}

// ReleaseAdvisoryLock releases the advisory lock with the given ID.
func (c pgxLockClient) ReleaseAdvisoryLock(ctx context.Context, id int64) error {
	return db.ReleaseAdvisoryLock(ctx, c.pool, id)
}

// HasAnySetting reports whether any app_settings row exists.
func (c pgxLockClient) HasAnySetting(ctx context.Context) (bool, error) {
	var exists bool
	err := c.pool.QueryRow(ctx, `select exists(select 1 from app_settings)`).Scan(&exists)
	return exists, err
}

// SeedSettings inserts default settings rows that are absent; existing keys
// are never overwritten, making repeated boots idempotent.
func (c pgxLockClient) SeedSettings(ctx context.Context, defaults map[string]json.RawMessage) error {
	for key, raw := range defaults {
		if _, err := c.pool.Exec(ctx, `
			insert into app_settings(key, value, updated_at)
			values ($1, $2::jsonb, now())
			on conflict (key) do nothing
		`, key, string(raw)); err != nil {
			return fmt.Errorf("seed setting %q: %w", key, err)
		}
	}
	return nil
}
