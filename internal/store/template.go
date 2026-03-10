package store

import (
	"context"
	"database/sql"
)

func (s *Store) CreateTemplateSource(ctx context.Context, ts *TemplateSource) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO template_source (org_id, name, url, type) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		ts.OrgID, ts.Name, ts.URL, ts.Type,
	).Scan(&ts.ID, &ts.CreatedAt)
}

func (s *Store) ListTemplateSources(ctx context.Context, orgID string) ([]TemplateSource, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, url, type, last_synced_at, created_at FROM template_source WHERE org_id = $1 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []TemplateSource
	for rows.Next() {
		var ts TemplateSource
		if err := rows.Scan(&ts.ID, &ts.OrgID, &ts.Name, &ts.URL, &ts.Type, &ts.LastSyncedAt, &ts.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, ts)
	}
	return list, nil
}

func (s *Store) DeleteTemplateSource(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM template_source WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateTemplateSyncTime(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE template_source SET last_synced_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) CreateCustomTemplate(ctx context.Context, ct *CustomTemplate) error {
	sourceID := sql.NullString{String: ct.SourceID, Valid: ct.SourceID != ""}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO custom_template (org_id, source_id, name, description, category, icon, image, version, ports, env, volumes, domain, replicas, is_stack, compose_content)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING id, created_at, updated_at`,
		ct.OrgID, sourceID, ct.Name, ct.Description, ct.Category, ct.Icon, ct.Image, ct.Version,
		ct.Ports, ct.Env, ct.Volumes, ct.Domain, ct.Replicas, ct.IsStack, ct.ComposeContent,
	).Scan(&ct.ID, &ct.CreatedAt, &ct.UpdatedAt)
}

func (s *Store) ListCustomTemplates(ctx context.Context, orgID string) ([]CustomTemplate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, source_id, name, description, category, icon, image, version, ports, env, volumes, domain, replicas, is_stack, compose_content, created_at, updated_at
		 FROM custom_template WHERE org_id = $1 ORDER BY created_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var list []CustomTemplate
	for rows.Next() {
		var ct CustomTemplate
		var sourceID sql.NullString
		if err := rows.Scan(&ct.ID, &ct.OrgID, &sourceID, &ct.Name, &ct.Description, &ct.Category, &ct.Icon, &ct.Image, &ct.Version,
			&ct.Ports, &ct.Env, &ct.Volumes, &ct.Domain, &ct.Replicas, &ct.IsStack, &ct.ComposeContent, &ct.CreatedAt, &ct.UpdatedAt); err != nil {
			return nil, err
		}
		if sourceID.Valid {
			ct.SourceID = sourceID.String
		}
		list = append(list, ct)
	}
	return list, nil
}

func (s *Store) GetCustomTemplate(ctx context.Context, id string) (*CustomTemplate, error) {
	ct := &CustomTemplate{}
	var sourceID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, source_id, name, description, category, icon, image, version, ports, env, volumes, domain, replicas, is_stack, compose_content, created_at, updated_at
		 FROM custom_template WHERE id = $1`, id,
	).Scan(&ct.ID, &ct.OrgID, &sourceID, &ct.Name, &ct.Description, &ct.Category, &ct.Icon, &ct.Image, &ct.Version,
		&ct.Ports, &ct.Env, &ct.Volumes, &ct.Domain, &ct.Replicas, &ct.IsStack, &ct.ComposeContent, &ct.CreatedAt, &ct.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if sourceID.Valid {
		ct.SourceID = sourceID.String
	}
	return ct, nil
}

func (s *Store) GetCustomTemplateByName(ctx context.Context, orgID, name string) (*CustomTemplate, error) {
	ct := &CustomTemplate{}
	var sourceID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, source_id, name, description, category, icon, image, version, ports, env, volumes, domain, replicas, is_stack, compose_content, created_at, updated_at
		 FROM custom_template WHERE org_id = $1 AND name = $2`, orgID, name,
	).Scan(&ct.ID, &ct.OrgID, &sourceID, &ct.Name, &ct.Description, &ct.Category, &ct.Icon, &ct.Image, &ct.Version,
		&ct.Ports, &ct.Env, &ct.Volumes, &ct.Domain, &ct.Replicas, &ct.IsStack, &ct.ComposeContent, &ct.CreatedAt, &ct.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if sourceID.Valid {
		ct.SourceID = sourceID.String
	}
	return ct, nil
}

func (s *Store) UpdateCustomTemplate(ctx context.Context, ct *CustomTemplate) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE custom_template SET name = $1, description = $2, category = $3, icon = $4, image = $5, version = $6, ports = $7, env = $8, volumes = $9, domain = $10, replicas = $11, is_stack = $12, compose_content = $13, updated_at = NOW() WHERE id = $14`,
		ct.Name, ct.Description, ct.Category, ct.Icon, ct.Image, ct.Version, ct.Ports, ct.Env, ct.Volumes, ct.Domain, ct.Replicas, ct.IsStack, ct.ComposeContent, ct.ID,
	)
	return err
}

func (s *Store) DeleteCustomTemplate(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM custom_template WHERE id = $1`, id)
	return err
}
