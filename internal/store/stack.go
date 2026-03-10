package store

import (
	"context"
)

func (s *Store) CreateStack(ctx context.Context, st *Stack) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO stack (project_id, name, domain, compose_content, status) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`,
		st.ProjectID, st.Name, st.Domain, st.ComposeContent, st.Status,
	).Scan(&st.ID, &st.CreatedAt, &st.UpdatedAt)
}

func (s *Store) GetStack(ctx context.Context, id string) (*Stack, error) {
	st := &Stack{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, domain, compose_content, status, created_at, updated_at FROM stack WHERE id = $1`, id,
	).Scan(&st.ID, &st.ProjectID, &st.Name, &st.Domain, &st.ComposeContent, &st.Status, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) ListStacks(ctx context.Context, projectID string) ([]Stack, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, domain, compose_content, status, created_at, updated_at FROM stack WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var stacks []Stack
	for rows.Next() {
		var st Stack
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.Domain, &st.ComposeContent, &st.Status, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return nil, err
		}
		stacks = append(stacks, st)
	}
	return stacks, nil
}

func (s *Store) ListAllStacks(ctx context.Context) ([]Stack, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, domain, compose_content, status, created_at, updated_at
		 FROM stack ORDER BY status = 'failed' DESC, updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var stacks []Stack
	for rows.Next() {
		var st Stack
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.Domain, &st.ComposeContent, &st.Status, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return nil, err
		}
		stacks = append(stacks, st)
	}
	return stacks, nil
}

func (s *Store) ListAllStacksByOrg(ctx context.Context, orgID string) ([]Stack, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT st.id, st.project_id, st.name, st.domain, st.compose_content, st.status, st.created_at, st.updated_at
		 FROM stack st
		 JOIN project p ON p.id = st.project_id
		 WHERE p.org_id = $1
		 ORDER BY st.status = 'failed' DESC, st.updated_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var stacks []Stack
	for rows.Next() {
		var st Stack
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.Domain, &st.ComposeContent, &st.Status, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return nil, err
		}
		stacks = append(stacks, st)
	}
	return stacks, nil
}

func (s *Store) UpdateStackStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stack SET status = $1, updated_at = NOW() WHERE id = $2`, status, id)
	return err
}

func (s *Store) UpdateStack(ctx context.Context, st *Stack) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE stack SET name=$1, domain=$2, compose_content=$3, status=$4, updated_at=NOW() WHERE id=$5`,
		st.Name, st.Domain, st.ComposeContent, st.Status, st.ID,
	)
	return err
}

func (s *Store) DeleteStack(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM stack WHERE id = $1`, id)
	return err
}
