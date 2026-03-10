package store

import (
	"context"
	"time"
)

type ResourceQuota struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	CPULimit     float64   `json:"cpu_limit"`
	MemoryLimit  int64     `json:"memory_limit"`
	StorageLimit int64     `json:"storage_limit"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Store) GetResourceQuota(ctx context.Context, projectID string) (*ResourceQuota, error) {
	q := &ResourceQuota{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, cpu_limit, memory_limit, storage_limit, created_at, updated_at
		 FROM resource_quota WHERE project_id = $1`, projectID,
	).Scan(&q.ID, &q.ProjectID, &q.CPULimit, &q.MemoryLimit, &q.StorageLimit, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return q, nil
}

func (s *Store) UpsertResourceQuota(ctx context.Context, projectID string, cpu float64, mem, storage int64) (*ResourceQuota, error) {
	q := &ResourceQuota{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO resource_quota (project_id, cpu_limit, memory_limit, storage_limit)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id) DO UPDATE SET cpu_limit=$2, memory_limit=$3, storage_limit=$4, updated_at=NOW()
		 RETURNING id, project_id, cpu_limit, memory_limit, storage_limit, created_at, updated_at`,
		projectID, cpu, mem, storage,
	).Scan(&q.ID, &q.ProjectID, &q.CPULimit, &q.MemoryLimit, &q.StorageLimit, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return q, nil
}
