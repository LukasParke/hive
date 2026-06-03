package leader

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/db"
)

type Elector struct {
	pool      *pgxpool.Pool
	onAcquire func(context.Context)
	onLost    func()
}

func New(pool *pgxpool.Pool, onAcquire func(context.Context), onLost func()) *Elector {
	return &Elector{pool: pool, onAcquire: onAcquire, onLost: onLost}
}

func (e *Elector) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		acquired, err := db.TryAdvisoryLock(ctx, e.pool, db.LockLeaderElection)
		if err != nil {
			log.Printf("leader lock failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if !acquired {
			time.Sleep(5 * time.Second)
			continue
		}

		leaderCtx, cancel := context.WithCancel(ctx)
		e.onAcquire(leaderCtx)
		<-ctx.Done()
		cancel()
		_ = db.ReleaseAdvisoryLock(context.Background(), e.pool, db.LockLeaderElection)
		e.onLost()
	}
}
