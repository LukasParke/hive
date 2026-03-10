package store

import (
	"context"
	"time"
)

type DockerConfig struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	OrgID          string    `json:"org_id"`
	Name           string    `json:"name"`
	DockerConfigID string    `json:"docker_config_id"`
	Data           string    `json:"data"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AppConfig struct {
	ID         string `json:"id"`
	AppID      string `json:"app_id"`
	ConfigID   string `json:"config_id"`
	TargetPath string `json:"target_path"`
	UID        string `json:"uid"`
	GID        string `json:"gid"`
	Mode       int    `json:"mode"`
}

func (s *Store) CreateDockerConfig(ctx context.Context, projectID, orgID, name, data string) (*DockerConfig, error) {
	c := &DockerConfig{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO docker_config (project_id, org_id, name, data) VALUES ($1, $2, $3, $4)
		 RETURNING id, project_id, org_id, name, docker_config_id, data, created_at, updated_at`,
		projectID, orgID, name, data,
	).Scan(&c.ID, &c.ProjectID, &c.OrgID, &c.Name, &c.DockerConfigID, &c.Data, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (s *Store) ListDockerConfigs(ctx context.Context, projectID string) ([]DockerConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, org_id, name, docker_config_id, data, created_at, updated_at
		 FROM docker_config WHERE project_id = $1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []DockerConfig
	for rows.Next() {
		var c DockerConfig
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.OrgID, &c.Name, &c.DockerConfigID, &c.Data, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, nil
}

func (s *Store) DeleteDockerConfig(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM docker_config WHERE id = $1`, id)
	return err
}

func (s *Store) AttachConfig(ctx context.Context, appID, configID, targetPath, uid, gid string, mode int) (*AppConfig, error) {
	ac := &AppConfig{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO app_config (app_id, config_id, target_path, uid, gid, mode) VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (app_id, config_id) DO UPDATE SET target_path=$3, uid=$4, gid=$5, mode=$6
		 RETURNING id, app_id, config_id, target_path, uid, gid, mode`,
		appID, configID, targetPath, uid, gid, mode,
	).Scan(&ac.ID, &ac.AppID, &ac.ConfigID, &ac.TargetPath, &ac.UID, &ac.GID, &ac.Mode)
	return ac, err
}

func (s *Store) DetachConfig(ctx context.Context, appID, configID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_config WHERE app_id = $1 AND config_id = $2`, appID, configID)
	return err
}

func (s *Store) ListAppConfigs(ctx context.Context, appID string) ([]AppConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, config_id, target_path, uid, gid, mode FROM app_config WHERE app_id = $1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []AppConfig
	for rows.Next() {
		var c AppConfig
		if err := rows.Scan(&c.ID, &c.AppID, &c.ConfigID, &c.TargetPath, &c.UID, &c.GID, &c.Mode); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, nil
}
