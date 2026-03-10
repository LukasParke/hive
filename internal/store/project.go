package store

import (
	"context"
)

func (s *Store) CreateProject(ctx context.Context, p *Project) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO project (name, org_id, description) VALUES ($1, $2, $3) RETURNING id, created_at, updated_at`,
		p.Name, p.OrgID, p.Description,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	p := &Project{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, org_id, description, created_at, updated_at FROM project WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.OrgID, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) ListProjects(ctx context.Context, orgID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, org_id, description, created_at, updated_at FROM project WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.OrgID, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (s *Store) UpdateProject(ctx context.Context, p *Project) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE project SET name = $1, description = $2, updated_at = NOW() WHERE id = $3`,
		p.Name, p.Description, p.ID,
	)
	return err
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project WHERE id = $1`, id)
	return err
}

func (s *Store) GetProjectByResourceID(ctx context.Context, resourceID string) (*Project, error) {
	p := &Project{}
	err := s.db.QueryRowContext(ctx,
		`SELECT p.id, p.name, p.org_id, p.description, p.created_at, p.updated_at
		 FROM project p WHERE p.id IN (
			SELECT project_id FROM managed_database WHERE id = $1
			UNION
			SELECT project_id FROM volume WHERE id = $1
			UNION
			SELECT bc2.resource_id FROM backup_config bc2
			  JOIN managed_database md ON md.id = bc2.resource_id
			  JOIN project p2 ON p2.id = md.project_id
			  WHERE bc2.id = $1
		 ) LIMIT 1`, resourceID,
	).Scan(&p.ID, &p.Name, &p.OrgID, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}
