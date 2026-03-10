package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type ScheduledJob struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	OrgID     string          `json:"org_id"`
	Name      string          `json:"name"`
	Image     string          `json:"image"`
	Command   string          `json:"command"`
	Schedule  string          `json:"schedule"`
	Timezone  string          `json:"timezone"`
	Env       json.RawMessage `json:"env"`
	LastRunAt *time.Time      `json:"last_run_at"`
	NextRunAt *time.Time      `json:"next_run_at"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"created_at"`
}

type JobRun struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	ExitCode    *int       `json:"exit_code"`
	Logs        string     `json:"logs"`
	ContainerID string     `json:"container_id"`
}

func (s *Store) CreateScheduledJob(ctx context.Context, projectID, orgID, name, image, command, schedule, timezone string, env json.RawMessage) (*ScheduledJob, error) {
	j := &ScheduledJob{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO scheduled_job (project_id, org_id, name, image, command, schedule, timezone, env)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, project_id, org_id, name, image, command, schedule, timezone, env, last_run_at, next_run_at, enabled, created_at`,
		projectID, orgID, name, image, command, schedule, timezone, env,
	).Scan(&j.ID, &j.ProjectID, &j.OrgID, &j.Name, &j.Image, &j.Command, &j.Schedule, &j.Timezone, &j.Env, &j.LastRunAt, &j.NextRunAt, &j.Enabled, &j.CreatedAt)
	return j, err
}

func (s *Store) ListScheduledJobs(ctx context.Context, projectID string) ([]ScheduledJob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, org_id, name, image, command, schedule, timezone, env, last_run_at, next_run_at, enabled, created_at
		 FROM scheduled_job WHERE project_id = $1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []ScheduledJob
	for rows.Next() {
		var j ScheduledJob
		if err := rows.Scan(&j.ID, &j.ProjectID, &j.OrgID, &j.Name, &j.Image, &j.Command, &j.Schedule, &j.Timezone, &j.Env, &j.LastRunAt, &j.NextRunAt, &j.Enabled, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Store) GetScheduledJob(ctx context.Context, id string) (*ScheduledJob, error) {
	j := &ScheduledJob{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, org_id, name, image, command, schedule, timezone, env, last_run_at, next_run_at, enabled, created_at
		 FROM scheduled_job WHERE id = $1`, id,
	).Scan(&j.ID, &j.ProjectID, &j.OrgID, &j.Name, &j.Image, &j.Command, &j.Schedule, &j.Timezone, &j.Env, &j.LastRunAt, &j.NextRunAt, &j.Enabled, &j.CreatedAt)
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Store) UpdateScheduledJob(ctx context.Context, id, name, image, command, schedule, timezone string, env json.RawMessage, enabled bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE scheduled_job SET name=$2, image=$3, command=$4, schedule=$5, timezone=$6, env=$7, enabled=$8 WHERE id=$1`,
		id, name, image, command, schedule, timezone, env, enabled)
	return err
}

func (s *Store) DeleteScheduledJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_job WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateJobLastRun(ctx context.Context, id string, nextRun *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scheduled_job SET last_run_at=NOW(), next_run_at=$2 WHERE id=$1`, id, nextRun)
	return err
}

func (s *Store) ListAllEnabledJobs(ctx context.Context) ([]ScheduledJob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, org_id, name, image, command, schedule, timezone, env, last_run_at, next_run_at, enabled, created_at
		 FROM scheduled_job WHERE enabled = true ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []ScheduledJob
	for rows.Next() {
		var j ScheduledJob
		if err := rows.Scan(&j.ID, &j.ProjectID, &j.OrgID, &j.Name, &j.Image, &j.Command, &j.Schedule, &j.Timezone, &j.Env, &j.LastRunAt, &j.NextRunAt, &j.Enabled, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (s *Store) CreateJobRun(ctx context.Context, jobID, status, containerID string) (*JobRun, error) {
	r := &JobRun{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO job_run (job_id, status, container_id) VALUES ($1, $2, $3)
		 RETURNING id, job_id, status, started_at, finished_at, exit_code, logs, container_id`,
		jobID, status, containerID,
	).Scan(&r.ID, &r.JobID, &r.Status, &r.StartedAt, &r.FinishedAt, &r.ExitCode, &r.Logs, &r.ContainerID)
	return r, err
}

func (s *Store) UpdateJobRun(ctx context.Context, id, status string, exitCode *int, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE job_run SET status=$2, exit_code=$3, logs=$4, finished_at=NOW() WHERE id=$1`,
		id, status, exitCode, logs)
	return err
}

func (s *Store) ListJobRuns(ctx context.Context, jobID string) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, status, started_at, finished_at, exit_code, logs, container_id
		 FROM job_run WHERE job_id = $1 ORDER BY started_at DESC LIMIT 50`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []JobRun
	for rows.Next() {
		var r JobRun
		var exitCode sql.NullInt32
		if err := rows.Scan(&r.ID, &r.JobID, &r.Status, &r.StartedAt, &r.FinishedAt, &exitCode, &r.Logs, &r.ContainerID); err != nil {
			return nil, err
		}
		if exitCode.Valid {
			v := int(exitCode.Int32)
			r.ExitCode = &v
		}
		runs = append(runs, r)
	}
	return runs, nil
}

func (s *Store) GetJobRun(ctx context.Context, id string) (*JobRun, error) {
	r := &JobRun{}
	var exitCode sql.NullInt32
	err := s.db.QueryRowContext(ctx,
		`SELECT id, job_id, status, started_at, finished_at, exit_code, logs, container_id FROM job_run WHERE id = $1`, id,
	).Scan(&r.ID, &r.JobID, &r.Status, &r.StartedAt, &r.FinishedAt, &exitCode, &r.Logs, &r.ContainerID)
	if exitCode.Valid {
		v := int(exitCode.Int32)
		r.ExitCode = &v
	}
	return r, err
}
