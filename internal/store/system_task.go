package store

import (
	"context"
	"database/sql"
	"time"
)

type SystemTask struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Category        string     `json:"category"`
	IntervalSeconds int        `json:"interval_seconds"`
	Enabled         bool       `json:"enabled"`
	LastRunAt       *time.Time `json:"last_run_at"`
	LastDurationMs  int        `json:"last_duration_ms"`
	LastStatus      string     `json:"last_status"`
	LastError       string     `json:"last_error"`
	RunCount        int64      `json:"run_count"`
	ErrorCount      int64      `json:"error_count"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (s *Store) UpsertSystemTask(ctx context.Context, t *SystemTask) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_task (id, name, description, category, interval_seconds, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
		   name = EXCLUDED.name,
		   description = EXCLUDED.description,
		   category = EXCLUDED.category,
		   interval_seconds = EXCLUDED.interval_seconds,
		   updated_at = NOW()`,
		t.ID, t.Name, t.Description, t.Category, t.IntervalSeconds, t.Enabled,
	)
	return err
}

func (s *Store) ListSystemTasks(ctx context.Context) ([]SystemTask, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, category, interval_seconds, enabled,
		        last_run_at, last_duration_ms, last_status, last_error,
		        run_count, error_count, created_at, updated_at
		 FROM system_task ORDER BY category, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []SystemTask
	for rows.Next() {
		var t SystemTask
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &t.Category, &t.IntervalSeconds, &t.Enabled,
			&t.LastRunAt, &t.LastDurationMs, &t.LastStatus, &t.LastError,
			&t.RunCount, &t.ErrorCount, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (s *Store) GetSystemTask(ctx context.Context, id string) (*SystemTask, error) {
	t := &SystemTask{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, category, interval_seconds, enabled,
		        last_run_at, last_duration_ms, last_status, last_error,
		        run_count, error_count, created_at, updated_at
		 FROM system_task WHERE id = $1`, id,
	).Scan(
		&t.ID, &t.Name, &t.Description, &t.Category, &t.IntervalSeconds, &t.Enabled,
		&t.LastRunAt, &t.LastDurationMs, &t.LastStatus, &t.LastError,
		&t.RunCount, &t.ErrorCount, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *Store) UpdateSystemTaskEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE system_task SET enabled = $2, updated_at = NOW() WHERE id = $1`,
		id, enabled)
	return err
}

func (s *Store) RecordSystemTaskRun(ctx context.Context, id string, durationMs int, status, errMsg string) error {
	incrErr := "error_count"
	if status != "error" {
		incrErr = "error_count" // no-op below
	}
	_ = incrErr

	if status == "error" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE system_task SET
			   last_run_at = NOW(), last_duration_ms = $2, last_status = $3, last_error = $4,
			   run_count = run_count + 1, error_count = error_count + 1, updated_at = NOW()
			 WHERE id = $1`,
			id, durationMs, status, errMsg)
		return err
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE system_task SET
		   last_run_at = NOW(), last_duration_ms = $2, last_status = $3, last_error = '',
		   run_count = run_count + 1, updated_at = NOW()
		 WHERE id = $1`,
		id, durationMs, status)
	return err
}
