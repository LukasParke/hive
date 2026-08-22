package deploy

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/db"
)

// appLockNamespace offsets per-application lock keys far away from the
// global lock IDs (db.LockLeaderElection=1, LockBootstrap=2,
// LockCertRenewal=3) so they can never collide.
const appLockNamespace = int64(1) << 62

// appLockID derives a stable advisory-lock key from an application UUID.
func appLockID(appID string) (int64, error) {
	id, err := uuid.Parse(appID)
	if err != nil {
		return 0, fmt.Errorf("parse app id for lock: %w", err)
	}
	return appLockNamespace | int64(binary.BigEndian.Uint64(id[:8])&0x7FFF_FFFF_FFFF_FFFF), nil //nolint:gosec // G115: top bit masked, value fits int64
}

// WithAppLock serializes spec mutations (service create/update, domain
// label apply/remove) for one application across ALL control-plane
// replicas. Every writer of an application's swarm spec does a
// read-modify-write of the full ServiceSpec; without this lock two
// concurrent writers (e.g. two deploys, or a deploy racing a domain
// change) can write back stale specs and resurrect dropped networks or
// labels. fn runs while the session lock is held.
func WithAppLock(ctx context.Context, pool *pgxpool.Pool, appID string, fn func(ctx context.Context) error) error {
	key, err := appLockID(appID)
	if err != nil {
		// Unparseable id: run unlocked; callers already validated ids and
		// this keeps preview/edge flows alive rather than wedged.
		return fn(ctx)
	}
	unlock, err := db.AcquireSessionLock(ctx, pool, key)
	if err != nil {
		return fmt.Errorf("acquire app lock: %w", err)
	}
	defer unlock()
	return fn(ctx)
}
