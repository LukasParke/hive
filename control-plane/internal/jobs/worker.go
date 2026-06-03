package jobs

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	pool     *pgxpool.Pool
	handlers map[string]func(context.Context, string) error
}

func NewWorker(pool *pgxpool.Pool) *Worker {
	return &Worker{
		pool:     pool,
		handlers: map[string]func(context.Context, string) error{},
	}
}

func (w *Worker) Register(kind string, h func(context.Context, string) error) {
	w.handlers[kind] = h
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.processOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (w *Worker) processOnce(ctx context.Context) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var id string
	var trigger string
	err = tx.QueryRow(ctx, `
		select id::text, trigger
		from build_jobs
		where status = 'queued'
		order by created_at asc
		for update skip locked
		limit 1
	`).Scan(&id, &trigger)
	if err != nil {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, `update build_jobs set status='building', started_at=now() where id=$1::uuid`, id); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	handler := w.handlers[trigger]
	if handler == nil {
		_, err = w.pool.Exec(ctx, `update build_jobs set status='failed', error_message='no handler', completed_at=now() where id=$1::uuid`, id)
		return err
	}
	if err := handler(ctx, id); err != nil {
		_, _ = w.pool.Exec(ctx, `
			update build_jobs
			set status=case when retries < 3 then 'queued' else 'failed' end,
			    retries = retries + 1,
			    error_message=$2,
			    completed_at=case when retries >= 3 then now() else completed_at end
			where id=$1::uuid
		`, id, err.Error())
		return nil
	}
	_, err = w.pool.Exec(ctx, `update build_jobs set status='complete', completed_at=now() where id=$1::uuid`, id)
	return err
}
