package leader

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/db"
)

// Timing defaults from the HA plan (P7.1): poll for the lock every 5s while
// not leading, keep the lock session alive with a check every 5s, and wait
// 15s after losing leadership before retrying acquisition.
const (
	DefaultAcquirePollInterval = 5 * time.Second
	DefaultKeepAliveInterval   = 5 * time.Second
	DefaultReacquireDelay      = 15 * time.Second
)

// PooledConn is the subset of a pooled pgx connection the elector needs.
// *pgxpool.Conn satisfies it; tests supply fakes.
type PooledConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row
	Ping(ctx context.Context) error
	Release()
}

// Pool acquires dedicated connections. *pgxpool.Pool is adapted via New.
type Pool interface {
	Acquire(ctx context.Context) (PooledConn, error)
}

type pgxPool struct{ pool *pgxpool.Pool }

// Acquire implements Pool by acquiring a connection from the pgx pool.
func (p pgxPool) Acquire(ctx context.Context) (PooledConn, error) {
	return p.pool.Acquire(ctx)
}

// Elector provides single-leader semantics via a session-level Postgres
// advisory lock held on a dedicated pooled connection. While leading, the
// connection is kept alive with periodic ownership checks; if the connection
// dies or the lock is found lost, the leader context is cancelled (stopping
// singleton tasks), onLost fires, and acquisition retries after a delay.
type Elector struct {
	pool      Pool
	onAcquire func(context.Context)
	onLost    func()

	acquirePollInterval time.Duration
	keepAliveInterval   time.Duration
	reacquireDelay      time.Duration

	leader atomic.Bool
}

// New builds an elector backed by a pgx connection pool with plan-default
// timings.
func New(pool *pgxpool.Pool, onAcquire func(context.Context), onLost func()) *Elector {
	return newElector(pgxPool{pool: pool}, onAcquire, onLost)
}

func newElector(pool Pool, onAcquire func(context.Context), onLost func()) *Elector {
	return &Elector{
		pool:                pool,
		onAcquire:           onAcquire,
		onLost:              onLost,
		acquirePollInterval: DefaultAcquirePollInterval,
		keepAliveInterval:   DefaultKeepAliveInterval,
		reacquireDelay:      DefaultReacquireDelay,
	}
}

// IsLeader reports whether this process currently holds the leader lock.
func (e *Elector) IsLeader() bool { return e.leader.Load() }

// Run blocks until ctx is cancelled, leading whenever the advisory lock is
// held. After losing leadership it waits reacquireDelay and tries again.
func (e *Elector) Run(ctx context.Context) {
	for ctx.Err() == nil {
		e.leadOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(e.reacquireDelay):
		}
	}
}

// leadOnce acquires the lock on a dedicated connection and leads until the
// keepalive detects loss or the parent context is cancelled.
func (e *Elector) leadOnce(ctx context.Context) {
	conn := e.acquireConn(ctx)
	if conn == nil {
		return
	}
	defer conn.Release()

	if !e.tryLockLoop(ctx, conn) {
		return
	}

	e.leader.Store(true)
	leaderCtx, cancel := context.WithCancel(ctx)
	e.onAcquire(leaderCtx)

	lost := e.keepAliveLoop(ctx, conn)

	cancel()
	e.leader.Store(false)
	e.unlock(conn)

	if lost {
		log.Printf("leader: lost advisory lock %d; will retry in %s", db.LockLeaderElection, e.reacquireDelay)
		e.onLost()
	}
}

// acquireConn fetches a dedicated connection, retrying until ctx is done.
func (e *Elector) acquireConn(ctx context.Context) PooledConn {
	for {
		conn, err := e.pool.Acquire(ctx)
		if err == nil {
			return conn
		}
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("leader: acquire conn failed: %v", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(e.acquirePollInterval):
		}
	}
}

// tryLockLoop polls pg_try_advisory_lock on the dedicated connection until
// acquired (true) or ctx is done (false).
func (e *Elector) tryLockLoop(ctx context.Context, conn PooledConn) bool {
	for {
		var ok bool
		err := conn.QueryRow(ctx, "select pg_try_advisory_lock($1)", db.LockLeaderElection).Scan(&ok)
		if err == nil && ok {
			return true
		}
		if err != nil {
			log.Printf("leader: try advisory lock failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(e.acquirePollInterval):
		}
	}
}

// keepAliveLoop verifies every keepAliveInterval that this session still
// holds the leader lock. It returns true when leadership was lost (connection
// failure or ownership gone) and false when the parent context was cancelled.
func (e *Elector) keepAliveLoop(ctx context.Context, conn PooledConn) bool {
	ticker := time.NewTicker(e.keepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if !e.checkAlive(ctx, conn) {
				// A probe that failed only because the parent context was
				// cancelled mid-flight is a clean shutdown, not lock loss.
				if ctx.Err() != nil {
					return false
				}
				return true
			}
		}
	}
}

// checkAlive runs one ownership probe: the advisory lock must still be held
// by this backend's pid on a live connection.
func (e *Elector) checkAlive(ctx context.Context, conn PooledConn) bool {
	kctx, cancel := context.WithTimeout(ctx, e.keepAliveInterval)
	defer cancel()
	var held int
	err := conn.QueryRow(kctx, `select count(*) from pg_locks where locktype = 'advisory' and objid = $1 and pid = pg_backend_pid()`, db.LockLeaderElection).Scan(&held)
	if err == nil {
		return held == 1
	}
	// The ownership query failed — distinguish a broken connection (lock is
	// gone server-side once the session dies) from a transient error.
	if pingErr := conn.Ping(kctx); pingErr != nil {
		log.Printf("leader: keepalive connection lost: %v", pingErr)
		return false
	}
	log.Printf("leader: keepalive ownership check failed transiently: %v", err)
	return true
}

// unlock releases the advisory lock best-effort. A false result or error
// means the lock was already gone (e.g. the session died); nothing to do.
func (e *Elector) unlock(conn PooledConn) {
	uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	if err := conn.QueryRow(uctx, "select pg_advisory_unlock($1)", db.LockLeaderElection).Scan(&unlocked); err != nil {
		log.Printf("leader: release advisory lock failed: %v", err)
	} else if !unlocked {
		log.Printf("leader: advisory lock %d was not held at release", db.LockLeaderElection)
	}
}
