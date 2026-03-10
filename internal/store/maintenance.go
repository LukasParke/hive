package store

import (
	"context"
)

func (s *Store) CreateMaintenanceTask(ctx context.Context, mt *MaintenanceTask) error {
	if mt.Config == nil {
		mt.Config = []byte("{}")
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO maintenance_task (org_id, type, schedule, enabled, last_status, config) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		mt.OrgID, mt.Type, mt.Schedule, mt.Enabled, mt.LastStatus, mt.Config,
	).Scan(&mt.ID, &mt.CreatedAt)
}

func (s *Store) ListMaintenanceTasks(ctx context.Context, orgID string) ([]MaintenanceTask, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, type, schedule, enabled, last_run_at, last_status, config, created_at FROM maintenance_task WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tasks []MaintenanceTask
	for rows.Next() {
		var mt MaintenanceTask
		if err := rows.Scan(&mt.ID, &mt.OrgID, &mt.Type, &mt.Schedule, &mt.Enabled, &mt.LastRunAt, &mt.LastStatus, &mt.Config, &mt.CreatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, mt)
	}
	return tasks, nil
}

func (s *Store) GetMaintenanceTask(ctx context.Context, id string) (*MaintenanceTask, error) {
	mt := &MaintenanceTask{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, type, schedule, enabled, last_run_at, last_status, config, created_at FROM maintenance_task WHERE id = $1`, id,
	).Scan(&mt.ID, &mt.OrgID, &mt.Type, &mt.Schedule, &mt.Enabled, &mt.LastRunAt, &mt.LastStatus, &mt.Config, &mt.CreatedAt)
	if err != nil {
		return nil, err
	}
	return mt, nil
}

func (s *Store) UpdateMaintenanceTask(ctx context.Context, mt *MaintenanceTask) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE maintenance_task SET type = $1, schedule = $2, enabled = $3, last_status = $4, config = $5 WHERE id = $6`,
		mt.Type, mt.Schedule, mt.Enabled, mt.LastStatus, mt.Config, mt.ID,
	)
	return err
}

func (s *Store) DeleteMaintenanceTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM maintenance_task WHERE id = $1`, id)
	return err
}

func (s *Store) CreateMaintenanceRun(ctx context.Context, mr *MaintenanceRun) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO maintenance_run (task_id, status, details) VALUES ($1, $2, $3) RETURNING id, started_at`,
		mr.TaskID, mr.Status, mr.Details,
	).Scan(&mr.ID, &mr.StartedAt)
}

func (s *Store) UpdateMaintenanceRun(ctx context.Context, id, status, details string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE maintenance_run SET status = $1, details = $2, finished_at = NOW() WHERE id = $3`,
		status, details, id,
	)
	return err
}

func (s *Store) ListMaintenanceRuns(ctx context.Context, taskID string) ([]MaintenanceRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, status, details, started_at, finished_at FROM maintenance_run WHERE task_id = $1 ORDER BY started_at DESC LIMIT 50`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var runs []MaintenanceRun
	for rows.Next() {
		var mr MaintenanceRun
		if err := rows.Scan(&mr.ID, &mr.TaskID, &mr.Status, &mr.Details, &mr.StartedAt, &mr.FinishedAt); err != nil {
			return nil, err
		}
		runs = append(runs, mr)
	}
	return runs, nil
}

func (s *Store) UpdateMaintenanceTaskLastRun(ctx context.Context, taskID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE maintenance_task SET last_run_at = NOW(), last_status = $1 WHERE id = $2`,
		status, taskID,
	)
	return err
}
