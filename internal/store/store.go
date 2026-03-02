package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func New(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &Store{db: db}, nil
}

func NewFromDB(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- Projects ---

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
	defer rows.Close()

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

// --- Apps ---

func (s *Store) CreateApp(ctx context.Context, a *App) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO app (project_id, name, deploy_type, image, git_repo, git_branch, dockerfile_path, domain, port, replicas, env_encrypted, template_name, template_version)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id, created_at, updated_at`,
		a.ProjectID, a.Name, a.DeployType, a.Image, a.GitRepo, a.GitBranch, a.DockerfilePath, a.Domain, a.Port, a.Replicas, a.EnvEncrypted, a.TemplateName, a.TemplateVersion,
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

func (s *Store) GetApp(ctx context.Context, id string) (*App, error) {
	a := &App{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, deploy_type, image, git_repo, git_branch, dockerfile_path, domain, port, replicas, env_encrypted, status,
		 cpu_limit, memory_limit, health_check_path, health_check_interval,
		 homepage_labels, extra_labels, placement_constraints, placement_preferences,
		 update_strategy, update_parallelism, update_delay, update_failure_action, update_order,
		 template_name, template_version, created_at, updated_at
		 FROM app WHERE id = $1`, id,
	).Scan(&a.ID, &a.ProjectID, &a.Name, &a.DeployType, &a.Image, &a.GitRepo, &a.GitBranch, &a.DockerfilePath, &a.Domain, &a.Port, &a.Replicas, &a.EnvEncrypted, &a.Status,
		&a.CPULimit, &a.MemoryLimit, &a.HealthCheckPath, &a.HealthCheckInterval,
		&a.HomepageLabels, &a.ExtraLabels, &a.PlacementConstraints, &a.PlacementPreferences,
		&a.UpdateStrategy, &a.UpdateParallelism, &a.UpdateDelay, &a.UpdateFailureAction, &a.UpdateOrder,
		&a.TemplateName, &a.TemplateVersion, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) ListAppsByGitRepo(ctx context.Context, cloneURL string) ([]App, error) {
	if cloneURL == "" {
		return nil, nil
	}
	norm := cloneURL
	if len(norm) > 4 && norm[len(norm)-4:] == ".git" {
		norm = norm[:len(norm)-4]
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, deploy_type, image, git_repo, git_branch, dockerfile_path, domain, port, replicas, env_encrypted, status,
		 cpu_limit, memory_limit, health_check_path, health_check_interval,
		 homepage_labels, extra_labels, placement_constraints, placement_preferences,
		 update_strategy, update_parallelism, update_delay, update_failure_action, update_order,
		 build_cache_enabled, auto_deploy_branch, preview_environments,
		 created_at, updated_at
		 FROM app WHERE deploy_type = 'git' AND (git_repo = $1 OR git_repo = $2 OR
			TRIM(TRAILING '/' FROM regexp_replace(git_repo, '\.git$', '')) = TRIM(TRAILING '/' FROM $2))`,
		cloneURL, norm,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Name, &a.DeployType, &a.Image, &a.GitRepo, &a.GitBranch, &a.DockerfilePath, &a.Domain, &a.Port, &a.Replicas, &a.EnvEncrypted, &a.Status,
			&a.CPULimit, &a.MemoryLimit, &a.HealthCheckPath, &a.HealthCheckInterval,
			&a.HomepageLabels, &a.ExtraLabels, &a.PlacementConstraints, &a.PlacementPreferences,
			&a.UpdateStrategy, &a.UpdateParallelism, &a.UpdateDelay, &a.UpdateFailureAction, &a.UpdateOrder,
			&a.BuildCacheEnabled, &a.AutoDeployBranch, &a.PreviewEnvironments,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}

func (s *Store) ListApps(ctx context.Context, projectID string) ([]App, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, deploy_type, image, git_repo, git_branch, dockerfile_path, domain, port, replicas, env_encrypted, status,
		 cpu_limit, memory_limit, health_check_path, health_check_interval,
		 homepage_labels, extra_labels, placement_constraints, placement_preferences,
		 update_strategy, update_parallelism, update_delay, update_failure_action, update_order,
		 build_cache_enabled, auto_deploy_branch, preview_environments,
		 template_name, template_version, created_at, updated_at
		 FROM app WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Name, &a.DeployType, &a.Image, &a.GitRepo, &a.GitBranch, &a.DockerfilePath, &a.Domain, &a.Port, &a.Replicas, &a.EnvEncrypted, &a.Status,
			&a.CPULimit, &a.MemoryLimit, &a.HealthCheckPath, &a.HealthCheckInterval,
			&a.HomepageLabels, &a.ExtraLabels, &a.PlacementConstraints, &a.PlacementPreferences,
			&a.UpdateStrategy, &a.UpdateParallelism, &a.UpdateDelay, &a.UpdateFailureAction, &a.UpdateOrder,
			&a.BuildCacheEnabled, &a.AutoDeployBranch, &a.PreviewEnvironments,
			&a.TemplateName, &a.TemplateVersion, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}

func (s *Store) UpdateAppStatus(ctx context.Context, id string, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE app SET status = $1, updated_at = NOW() WHERE id = $2`, status, id)
	return err
}

func (s *Store) UpdateAppEnv(ctx context.Context, id string, envEncrypted []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE app SET env_encrypted = $1, updated_at = NOW() WHERE id = $2`, envEncrypted, id)
	return err
}

func (s *Store) UpdateAppDomain(ctx context.Context, id string, domain string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE app SET domain = $1, updated_at = NOW() WHERE id = $2`, domain, id)
	return err
}

func (s *Store) UpdateApp(ctx context.Context, app *App) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET name=$1, image=$2, git_repo=$3, git_branch=$4, dockerfile_path=$5, domain=$6, port=$7, replicas=$8, updated_at=NOW() WHERE id=$9`,
		app.Name, app.Image, app.GitRepo, app.GitBranch, app.DockerfilePath, app.Domain, app.Port, app.Replicas, app.ID,
	)
	return err
}

func (s *Store) DeleteApp(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app WHERE id = $1`, id)
	return err
}

// --- Deployments ---

func (s *Store) CreateDeployment(ctx context.Context, d *Deployment) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO deployment (app_id, status, commit_sha, image_digest, logs) VALUES ($1, $2, $3, $4, $5) RETURNING id, started_at`,
		d.AppID, d.Status, d.CommitSHA, d.ImageDigest, d.Logs,
	).Scan(&d.ID, &d.StartedAt)
}

func (s *Store) UpdateDeployment(ctx context.Context, id string, status string, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployment SET status = $1, logs = $2, finished_at = NOW() WHERE id = $3`,
		status, logs, id,
	)
	return err
}

func (s *Store) ListDeployments(ctx context.Context, appID string) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, status, commit_sha, image_digest, logs, started_at, finished_at
		 FROM deployment WHERE app_id = $1 ORDER BY started_at DESC LIMIT 50`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.AppID, &d.Status, &d.CommitSHA, &d.ImageDigest, &d.Logs, &d.StartedAt, &d.FinishedAt); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, nil
}

func (s *Store) GetDeployment(ctx context.Context, id string) (*Deployment, error) {
	d := &Deployment{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, status, commit_sha, image_digest, logs, started_at, finished_at FROM deployment WHERE id = $1`, id,
	).Scan(&d.ID, &d.AppID, &d.Status, &d.CommitSHA, &d.ImageDigest, &d.Logs, &d.StartedAt, &d.FinishedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) DeleteDeployment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deployment WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateDeploymentStatus(ctx context.Context, id, status, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployment SET status = $1, logs = CASE WHEN $2 = '' THEN logs ELSE $2 END, finished_at = NOW() WHERE id = $3`,
		status, logs, id,
	)
	return err
}

func (s *Store) AppendDeploymentLogs(ctx context.Context, id, logs string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE deployment SET logs = logs || $1 WHERE id = $2`,
		logs, id,
	)
	return err
}

// --- Git Sources ---

func (s *Store) CreateGitSource(ctx context.Context, gs *GitSource) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO git_source (org_id, provider, token_encrypted) VALUES ($1, $2, $3) RETURNING id, created_at`,
		gs.OrgID, gs.Provider, gs.TokenEncrypted,
	).Scan(&gs.ID, &gs.CreatedAt)
}

func (s *Store) ListGitSources(ctx context.Context, orgID string) ([]GitSource, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, provider, token_encrypted, created_at FROM git_source WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []GitSource
	for rows.Next() {
		var gs GitSource
		if err := rows.Scan(&gs.ID, &gs.OrgID, &gs.Provider, &gs.TokenEncrypted, &gs.CreatedAt); err != nil {
			return nil, err
		}
		sources = append(sources, gs)
	}
	return sources, nil
}

func (s *Store) UpdateGitSource(ctx context.Context, gs *GitSource) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE git_source SET provider = $1, provider_name = $2, token_encrypted = $3 WHERE id = $4`,
		gs.Provider, gs.ProviderName, gs.TokenEncrypted, gs.ID,
	)
	return err
}

func (s *Store) DeleteGitSource(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM git_source WHERE id = $1`, id)
	return err
}

func (s *Store) GetGitSource(ctx context.Context, id string) (*GitSource, error) {
	gs := &GitSource{}
	var webhookSecret []byte
	var webhookIDs []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, provider, COALESCE(provider_name,''), token_encrypted, webhook_secret_encrypted, COALESCE(webhook_ids,'{}'::jsonb), created_at FROM git_source WHERE id = $1`, id,
	).Scan(&gs.ID, &gs.OrgID, &gs.Provider, &gs.ProviderName, &gs.TokenEncrypted, &webhookSecret, &webhookIDs, &gs.CreatedAt)
	if err != nil {
		return nil, err
	}
	gs.WebhookSecretEncrypted = webhookSecret
	if len(webhookIDs) > 0 {
		_ = json.Unmarshal(webhookIDs, &gs.WebhookIDs)
	}
	if gs.WebhookIDs == nil {
		gs.WebhookIDs = make(map[string]string)
	}
	return gs, nil
}

func (s *Store) AddRepoWebhookID(ctx context.Context, sourceID, repo, webhookID string) error {
	payload, err := json.Marshal(map[string]string{repo: webhookID})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE git_source SET webhook_ids = COALESCE(webhook_ids,'{}'::jsonb) || $2::jsonb WHERE id = $1`,
		sourceID, payload,
	)
	return err
}

func (s *Store) UpdateGitSourceWebhookSecret(ctx context.Context, id string, secretEncrypted []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE git_source SET webhook_secret_encrypted = $1 WHERE id = $2`,
		secretEncrypted, id,
	)
	return err
}

// --- Managed Databases ---

func (s *Store) CreateManagedDatabase(ctx context.Context, d *ManagedDatabase) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO managed_database (project_id, name, db_type, version, connection_encrypted)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		d.ProjectID, d.Name, d.DBType, d.Version, d.ConnectionEncrypted,
	).Scan(&d.ID, &d.CreatedAt)
}

func (s *Store) GetManagedDatabase(ctx context.Context, id string) (*ManagedDatabase, error) {
	d := &ManagedDatabase{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, db_type, version, status, connection_encrypted, created_at FROM managed_database WHERE id = $1`, id,
	).Scan(&d.ID, &d.ProjectID, &d.Name, &d.DBType, &d.Version, &d.Status, &d.ConnectionEncrypted, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) UpdateManagedDatabaseStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE managed_database SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (s *Store) UpdateManagedDatabaseConnection(ctx context.Context, id string, connEncrypted []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE managed_database SET connection_encrypted = $1, status = 'running' WHERE id = $2`, connEncrypted, id)
	return err
}

func (s *Store) DeleteManagedDatabase(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM managed_database WHERE id = $1`, id)
	return err
}

func (s *Store) ListManagedDatabases(ctx context.Context, projectID string) ([]ManagedDatabase, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, db_type, version, status, connection_encrypted, created_at
		 FROM managed_database WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbs []ManagedDatabase
	for rows.Next() {
		var d ManagedDatabase
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.DBType, &d.Version, &d.Status, &d.ConnectionEncrypted, &d.CreatedAt); err != nil {
			return nil, err
		}
		dbs = append(dbs, d)
	}
	return dbs, nil
}

// --- Backup Configs ---

func (s *Store) CreateBackupConfig(ctx context.Context, bc *BackupConfig) error {
	if bc.BackupType == "" {
		bc.BackupType = "database"
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO backup_config (resource_id, schedule, s3_bucket, s3_prefix, backup_type, volume_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		bc.ResourceID, bc.Schedule, bc.S3Bucket, bc.S3Prefix, bc.BackupType, bc.VolumeID,
	).Scan(&bc.ID, &bc.CreatedAt)
}

func (s *Store) GetBackupConfig(ctx context.Context, id string) (*BackupConfig, error) {
	bc := &BackupConfig{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, resource_id, schedule, s3_bucket, s3_prefix, backup_type, volume_id, created_at FROM backup_config WHERE id = $1`, id,
	).Scan(&bc.ID, &bc.ResourceID, &bc.Schedule, &bc.S3Bucket, &bc.S3Prefix, &bc.BackupType, &bc.VolumeID, &bc.CreatedAt)
	if err != nil {
		return nil, err
	}
	return bc, nil
}

func (s *Store) ListBackupConfigs(ctx context.Context) ([]BackupConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, resource_id, schedule, s3_bucket, s3_prefix, backup_type, volume_id, created_at FROM backup_config ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []BackupConfig
	for rows.Next() {
		var bc BackupConfig
		if err := rows.Scan(&bc.ID, &bc.ResourceID, &bc.Schedule, &bc.S3Bucket, &bc.S3Prefix, &bc.BackupType, &bc.VolumeID, &bc.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, bc)
	}
	return configs, nil
}

func (s *Store) UpdateBackupConfig(ctx context.Context, bc *BackupConfig) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_config SET schedule = $1, s3_bucket = $2, s3_prefix = $3 WHERE id = $4`,
		bc.Schedule, bc.S3Bucket, bc.S3Prefix, bc.ID,
	)
	return err
}

func (s *Store) DeleteBackupConfig(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_config WHERE id = $1`, id)
	return err
}

func (s *Store) CreateBackupRun(ctx context.Context, br *BackupRun) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO backup_run (config_id, status) VALUES ($1, $2) RETURNING id, started_at`,
		br.ConfigID, br.Status,
	).Scan(&br.ID, &br.StartedAt)
}

func (s *Store) UpdateBackupRun(ctx context.Context, id, status string, size int64, targetPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE backup_run SET status = $1, size = $2, target_path = $3, finished_at = NOW() WHERE id = $4`,
		status, size, targetPath, id,
	)
	return err
}

// --- Audit Log ---

func (s *Store) CreateAuditLog(ctx context.Context, al *AuditLog) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO audit_log (user_id, org_id, action, resource, resource_id, details) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		al.UserID, al.OrgID, al.Action, al.Resource, al.ResourceID, al.Details,
	).Scan(&al.ID, &al.CreatedAt)
}

func (s *Store) ListAuditLogs(ctx context.Context, orgID string, limit int) ([]AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, org_id, action, resource, resource_id, details, created_at FROM audit_log WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2`,
		orgID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var al AuditLog
		if err := rows.Scan(&al.ID, &al.UserID, &al.OrgID, &al.Action, &al.Resource, &al.ResourceID, &al.Details, &al.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, al)
	}
	return logs, nil
}

// --- Secrets ---

func (s *Store) CreateSecret(ctx context.Context, sec *Secret) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO secret (project_id, name, docker_secret_id, description) VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`,
		sec.ProjectID, sec.Name, sec.DockerSecretID, sec.Description,
	).Scan(&sec.ID, &sec.CreatedAt, &sec.UpdatedAt)
}

func (s *Store) ListSecrets(ctx context.Context, projectID string) ([]Secret, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, docker_secret_id, description, created_at, updated_at FROM secret WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var secrets []Secret
	for rows.Next() {
		var sec Secret
		if err := rows.Scan(&sec.ID, &sec.ProjectID, &sec.Name, &sec.DockerSecretID, &sec.Description, &sec.CreatedAt, &sec.UpdatedAt); err != nil {
			return nil, err
		}
		secrets = append(secrets, sec)
	}
	return secrets, nil
}

func (s *Store) GetSecret(ctx context.Context, id string) (*Secret, error) {
	sec := &Secret{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, docker_secret_id, description, created_at, updated_at FROM secret WHERE id = $1`, id,
	).Scan(&sec.ID, &sec.ProjectID, &sec.Name, &sec.DockerSecretID, &sec.Description, &sec.CreatedAt, &sec.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return sec, nil
}

func (s *Store) DeleteSecret(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM secret WHERE id = $1`, id)
	return err
}

func (s *Store) AttachSecret(ctx context.Context, as *AppSecret) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_secret (app_id, secret_id, target, uid, gid, mode) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (app_id, secret_id) DO UPDATE SET target=$3, uid=$4, gid=$5, mode=$6`,
		as.AppID, as.SecretID, as.Target, as.UID, as.GID, as.Mode,
	)
	return err
}

func (s *Store) DetachSecret(ctx context.Context, appID, secretID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_secret WHERE app_id = $1 AND secret_id = $2`, appID, secretID)
	return err
}

func (s *Store) ListAppSecrets(ctx context.Context, appID string) ([]AppSecret, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT app_id, secret_id, target, uid, gid, mode FROM app_secret WHERE app_id = $1`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AppSecret
	for rows.Next() {
		var as AppSecret
		if err := rows.Scan(&as.AppID, &as.SecretID, &as.Target, &as.UID, &as.GID, &as.Mode); err != nil {
			return nil, err
		}
		result = append(result, as)
	}
	return result, nil
}

// --- Volumes ---

func (s *Store) CreateVolume(ctx context.Context, vol *Volume) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO volume (project_id, name, driver, driver_opts, labels, mount_type, remote_host, remote_path, mount_options, scope, status, storage_host_id, local_path, ceph_pool, ceph_image, ceph_fs_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NULLIF($12,''), $13, $14, $15, $16) RETURNING id, created_at`,
		vol.ProjectID, vol.Name, vol.Driver, vol.DriverOpts, vol.Labels, vol.MountType, vol.RemoteHost, vol.RemotePath, vol.MountOptions, vol.Scope, vol.Status,
		vol.StorageHostID, vol.LocalPath, vol.CephPool, vol.CephImage, vol.CephFSName,
	).Scan(&vol.ID, &vol.CreatedAt)
}

func (s *Store) ListVolumes(ctx context.Context, projectID string) ([]Volume, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, driver, driver_opts, labels, mount_type, remote_host, remote_path, mount_options, scope, status,
		 COALESCE(storage_host_id,''), local_path, ceph_pool, ceph_image, ceph_fs_name, created_at
		 FROM volume WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vols []Volume
	for rows.Next() {
		var v Volume
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.DriverOpts, &v.Labels, &v.MountType, &v.RemoteHost, &v.RemotePath, &v.MountOptions, &v.Scope, &v.Status,
			&v.StorageHostID, &v.LocalPath, &v.CephPool, &v.CephImage, &v.CephFSName, &v.CreatedAt); err != nil {
			return nil, err
		}
		vols = append(vols, v)
	}
	return vols, nil
}

func (s *Store) GetVolume(ctx context.Context, id string) (*Volume, error) {
	v := &Volume{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, driver, driver_opts, labels, mount_type, remote_host, remote_path, mount_options, scope, status,
		 COALESCE(storage_host_id,''), local_path, ceph_pool, ceph_image, ceph_fs_name, created_at
		 FROM volume WHERE id = $1`, id,
	).Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.DriverOpts, &v.Labels, &v.MountType, &v.RemoteHost, &v.RemotePath, &v.MountOptions, &v.Scope, &v.Status,
		&v.StorageHostID, &v.LocalPath, &v.CephPool, &v.CephImage, &v.CephFSName, &v.CreatedAt)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) UpdateVolumeStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE volume SET status = $1 WHERE id = $2`, status, id)
	return err
}

func (s *Store) DeleteVolume(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM volume WHERE id = $1`, id)
	return err
}

func (s *Store) AttachVolume(ctx context.Context, av *AppVolume) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_volume (app_id, volume_id, container_path, read_only) VALUES ($1, $2, $3, $4) ON CONFLICT (app_id, volume_id) DO UPDATE SET container_path=$3, read_only=$4`,
		av.AppID, av.VolumeID, av.ContainerPath, av.ReadOnly,
	)
	return err
}

func (s *Store) DetachVolume(ctx context.Context, appID, volumeID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_volume WHERE app_id = $1 AND volume_id = $2`, appID, volumeID)
	return err
}

func (s *Store) ListAppVolumes(ctx context.Context, appID string) ([]AppVolume, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT app_id, volume_id, container_path, read_only FROM app_volume WHERE app_id = $1`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AppVolume
	for rows.Next() {
		var av AppVolume
		if err := rows.Scan(&av.AppID, &av.VolumeID, &av.ContainerPath, &av.ReadOnly); err != nil {
			return nil, err
		}
		result = append(result, av)
	}
	return result, nil
}

// --- Notification Channels ---

func (s *Store) CreateNotificationChannel(ctx context.Context, nc *NotificationChannel) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO notification_channel (org_id, name, type, config) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		nc.OrgID, nc.Name, nc.Type, nc.Config,
	).Scan(&nc.ID, &nc.CreatedAt)
}

func (s *Store) ListNotificationChannels(ctx context.Context, orgID string) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, type, config, created_at FROM notification_channel WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []NotificationChannel
	for rows.Next() {
		var ch NotificationChannel
		if err := rows.Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Config, &ch.CreatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, nil
}

func (s *Store) GetNotificationChannel(ctx context.Context, id string) (*NotificationChannel, error) {
	ch := &NotificationChannel{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, type, config, created_at FROM notification_channel WHERE id = $1`, id,
	).Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Config, &ch.CreatedAt)
	if err != nil {
		return nil, err
	}
	return ch, nil
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notification_channel WHERE id = $1`, id)
	return err
}

func (s *Store) ListAllNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, type, config, created_at FROM notification_channel ORDER BY created_at DESC LIMIT 100`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []NotificationChannel
	for rows.Next() {
		var ch NotificationChannel
		if err := rows.Scan(&ch.ID, &ch.OrgID, &ch.Name, &ch.Type, &ch.Config, &ch.CreatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, nil
}

func (s *Store) CreateNotificationEvent(ctx context.Context, ne *NotificationEvent) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO notification_event (channel_id, event_type, title, message, status) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		ne.ChannelID, ne.EventType, ne.Title, ne.Message, ne.Status,
	).Scan(&ne.ID, &ne.CreatedAt)
}

func (s *Store) ListNotificationEvents(ctx context.Context, orgID string, limit int) ([]NotificationEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ne.id, ne.channel_id, ne.event_type, ne.title, ne.message, ne.status, ne.created_at
		 FROM notification_event ne JOIN notification_channel nc ON ne.channel_id = nc.id
		 WHERE nc.org_id = $1 ORDER BY ne.created_at DESC LIMIT $2`,
		orgID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []NotificationEvent
	for rows.Next() {
		var ne NotificationEvent
		if err := rows.Scan(&ne.ID, &ne.ChannelID, &ne.EventType, &ne.Title, &ne.Message, &ne.Status, &ne.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ne)
	}
	return events, nil
}

// --- Backup Runs ---

func (s *Store) GetBackupRun(ctx context.Context, id string) (*BackupRun, error) {
	br := &BackupRun{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, config_id, status, size, target_path, started_at, finished_at FROM backup_run WHERE id = $1`, id,
	).Scan(&br.ID, &br.ConfigID, &br.Status, &br.Size, &br.TargetPath, &br.StartedAt, &br.FinishedAt)
	if err != nil {
		return nil, err
	}
	return br, nil
}

func (s *Store) ListBackupRuns(ctx context.Context, configID string) ([]BackupRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, config_id, status, size, target_path, started_at, finished_at FROM backup_run WHERE config_id = $1 ORDER BY started_at DESC LIMIT 50`,
		configID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []BackupRun
	for rows.Next() {
		var br BackupRun
		if err := rows.Scan(&br.ID, &br.ConfigID, &br.Status, &br.Size, &br.TargetPath, &br.StartedAt, &br.FinishedAt); err != nil {
			return nil, err
		}
		runs = append(runs, br)
	}
	return runs, nil
}

// --- App Resource Limits ---

func (s *Store) UpdateAppResources(ctx context.Context, id string, cpuLimit float64, memoryLimit int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET cpu_limit = $1, memory_limit = $2, updated_at = NOW() WHERE id = $3`,
		cpuLimit, memoryLimit, id,
	)
	return err
}

func (s *Store) UpdateAppHealthCheck(ctx context.Context, id string, path string, interval int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET health_check_path = $1, health_check_interval = $2, updated_at = NOW() WHERE id = $3`,
		path, interval, id,
	)
	return err
}

// --- Proxy Routes ---

func (s *Store) CreateProxyRoute(ctx context.Context, r *ProxyRoute) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO proxy_route (project_id, name, domain, target_service, target_port, protocol, upstream_port, ssl_mode, custom_cert_id, middleware_config, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id, created_at`,
		r.ProjectID, r.Name, r.Domain, r.TargetService, r.TargetPort, r.Protocol, r.UpstreamPort, r.SSLMode, r.CustomCertID, r.MiddlewareConfig, r.Enabled,
	).Scan(&r.ID, &r.CreatedAt)
}

func (s *Store) ListProxyRoutes(ctx context.Context, projectID string) ([]ProxyRoute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, domain, target_service, target_port, protocol, upstream_port, ssl_mode, custom_cert_id, middleware_config, enabled, created_at
		 FROM proxy_route WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []ProxyRoute
	for rows.Next() {
		var r ProxyRoute
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Domain, &r.TargetService, &r.TargetPort, &r.Protocol, &r.UpstreamPort, &r.SSLMode, &r.CustomCertID, &r.MiddlewareConfig, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	return routes, nil
}

func (s *Store) GetProxyRoute(ctx context.Context, id string) (*ProxyRoute, error) {
	r := &ProxyRoute{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, domain, target_service, target_port, protocol, upstream_port, ssl_mode, custom_cert_id, middleware_config, enabled, created_at
		 FROM proxy_route WHERE id = $1`, id,
	).Scan(&r.ID, &r.ProjectID, &r.Name, &r.Domain, &r.TargetService, &r.TargetPort, &r.Protocol, &r.UpstreamPort, &r.SSLMode, &r.CustomCertID, &r.MiddlewareConfig, &r.Enabled, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) UpdateProxyRoute(ctx context.Context, r *ProxyRoute) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE proxy_route SET name=$1, domain=$2, target_service=$3, target_port=$4, protocol=$5, upstream_port=$6, ssl_mode=$7, custom_cert_id=$8, middleware_config=$9, enabled=$10 WHERE id=$11`,
		r.Name, r.Domain, r.TargetService, r.TargetPort, r.Protocol, r.UpstreamPort, r.SSLMode, r.CustomCertID, r.MiddlewareConfig, r.Enabled, r.ID,
	)
	return err
}

func (s *Store) DeleteProxyRoute(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxy_route WHERE id = $1`, id)
	return err
}

func (s *Store) ListAllProxyRoutes(ctx context.Context) ([]ProxyRoute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, domain, target_service, target_port, protocol, upstream_port, ssl_mode, custom_cert_id, middleware_config, enabled, created_at
		 FROM proxy_route WHERE enabled = true ORDER BY domain`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var routes []ProxyRoute
	for rows.Next() {
		var r ProxyRoute
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Name, &r.Domain, &r.TargetService, &r.TargetPort, &r.Protocol, &r.UpstreamPort, &r.SSLMode, &r.CustomCertID, &r.MiddlewareConfig, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	return routes, nil
}

// --- Custom Certificates ---

func (s *Store) CreateCustomCertificate(ctx context.Context, c *CustomCertificate) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO custom_certificate (project_id, domain, cert_pem, key_pem_encrypted, is_wildcard, provider, expires_at, auto_renew, dns_provider_id, renewal_error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id, created_at`,
		c.ProjectID, c.Domain, c.CertPEM, c.KeyPEMEncrypted, c.IsWildcard, c.Provider, c.ExpiresAt, c.AutoRenew, c.DNSProviderID, c.RenewalError,
	).Scan(&c.ID, &c.CreatedAt)
}

func (s *Store) ListCustomCertificates(ctx context.Context, projectID string) ([]CustomCertificate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, domain, cert_pem, is_wildcard, provider, expires_at, auto_renew, dns_provider_id, last_renewed_at, renewal_error, created_at
		 FROM custom_certificate WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var certs []CustomCertificate
	for rows.Next() {
		var c CustomCertificate
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Domain, &c.CertPEM, &c.IsWildcard, &c.Provider, &c.ExpiresAt, &c.AutoRenew, &c.DNSProviderID, &c.LastRenewedAt, &c.RenewalError, &c.CreatedAt); err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, nil
}

func (s *Store) GetCustomCertificate(ctx context.Context, id string) (*CustomCertificate, error) {
	c := &CustomCertificate{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, domain, cert_pem, key_pem_encrypted, is_wildcard, provider, expires_at, auto_renew, dns_provider_id, last_renewed_at, renewal_error, created_at
		 FROM custom_certificate WHERE id = $1`, id,
	).Scan(&c.ID, &c.ProjectID, &c.Domain, &c.CertPEM, &c.KeyPEMEncrypted, &c.IsWildcard, &c.Provider, &c.ExpiresAt, &c.AutoRenew, &c.DNSProviderID, &c.LastRenewedAt, &c.RenewalError, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) UpdateCustomCertificate(ctx context.Context, c *CustomCertificate) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE custom_certificate SET domain = $1, cert_pem = $2, key_pem_encrypted = $3, is_wildcard = $4, provider = $5, expires_at = $6, auto_renew = $7, dns_provider_id = $8, last_renewed_at = $9, renewal_error = $10 WHERE id = $11`,
		c.Domain, c.CertPEM, c.KeyPEMEncrypted, c.IsWildcard, c.Provider, c.ExpiresAt, c.AutoRenew, c.DNSProviderID, c.LastRenewedAt, c.RenewalError, c.ID,
	)
	return err
}

func (s *Store) DeleteCustomCertificate(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM custom_certificate WHERE id = $1`, id)
	return err
}

// --- Stacks ---

func (s *Store) CreateStack(ctx context.Context, st *Stack) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO stack (project_id, name, compose_content, status) VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`,
		st.ProjectID, st.Name, st.ComposeContent, st.Status,
	).Scan(&st.ID, &st.CreatedAt, &st.UpdatedAt)
}

func (s *Store) GetStack(ctx context.Context, id string) (*Stack, error) {
	st := &Stack{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, project_id, name, compose_content, status, created_at, updated_at FROM stack WHERE id = $1`, id,
	).Scan(&st.ID, &st.ProjectID, &st.Name, &st.ComposeContent, &st.Status, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) ListStacks(ctx context.Context, projectID string) ([]Stack, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, compose_content, status, created_at, updated_at FROM stack WHERE project_id = $1 ORDER BY created_at DESC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stacks []Stack
	for rows.Next() {
		var st Stack
		if err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.ComposeContent, &st.Status, &st.CreatedAt, &st.UpdatedAt); err != nil {
			return nil, err
		}
		stacks = append(stacks, st)
	}
	return stacks, nil
}

func (s *Store) UpdateStack(ctx context.Context, st *Stack) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE stack SET name=$1, compose_content=$2, status=$3, updated_at=NOW() WHERE id=$4`,
		st.Name, st.ComposeContent, st.Status, st.ID,
	)
	return err
}

func (s *Store) DeleteStack(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM stack WHERE id = $1`, id)
	return err
}

// --- Alert Thresholds ---

func (s *Store) CreateAlertThreshold(ctx context.Context, at *AlertThreshold) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO alert_threshold (org_id, metric, operator, value, cooldown_minutes, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		at.OrgID, at.Metric, at.Operator, at.Value, at.CooldownMinutes, at.Enabled,
	).Scan(&at.ID, &at.CreatedAt)
}

func (s *Store) ListAlertThresholds(ctx context.Context, orgID string) ([]AlertThreshold, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, metric, operator, value, cooldown_minutes, enabled, last_fired_at, created_at
		 FROM alert_threshold WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var thresholds []AlertThreshold
	for rows.Next() {
		var at AlertThreshold
		if err := rows.Scan(&at.ID, &at.OrgID, &at.Metric, &at.Operator, &at.Value, &at.CooldownMinutes, &at.Enabled, &at.LastFiredAt, &at.CreatedAt); err != nil {
			return nil, err
		}
		thresholds = append(thresholds, at)
	}
	return thresholds, nil
}

func (s *Store) GetAlertThreshold(ctx context.Context, id string) (*AlertThreshold, error) {
	at := &AlertThreshold{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, metric, operator, value, cooldown_minutes, enabled, last_fired_at, created_at
		 FROM alert_threshold WHERE id = $1`, id,
	).Scan(&at.ID, &at.OrgID, &at.Metric, &at.Operator, &at.Value, &at.CooldownMinutes, &at.Enabled, &at.LastFiredAt, &at.CreatedAt)
	if err != nil {
		return nil, err
	}
	return at, nil
}

func (s *Store) UpdateAlertThreshold(ctx context.Context, at *AlertThreshold) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE alert_threshold SET metric = $1, operator = $2, value = $3, cooldown_minutes = $4, enabled = $5 WHERE id = $6`,
		at.Metric, at.Operator, at.Value, at.CooldownMinutes, at.Enabled, at.ID,
	)
	return err
}

func (s *Store) DeleteAlertThreshold(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM alert_threshold WHERE id = $1`, id)
	return err
}

func (s *Store) ListAllAlertThresholds(ctx context.Context) ([]AlertThreshold, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, metric, operator, value, cooldown_minutes, enabled, last_fired_at, created_at
		 FROM alert_threshold WHERE enabled = true ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var thresholds []AlertThreshold
	for rows.Next() {
		var at AlertThreshold
		if err := rows.Scan(&at.ID, &at.OrgID, &at.Metric, &at.Operator, &at.Value, &at.CooldownMinutes, &at.Enabled, &at.LastFiredAt, &at.CreatedAt); err != nil {
			return nil, err
		}
		thresholds = append(thresholds, at)
	}
	return thresholds, nil
}

func (s *Store) UpdateAlertThresholdFired(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alert_threshold SET last_fired_at = NOW() WHERE id = $1`, id)
	return err
}

// --- App Placement ---

func (s *Store) UpdateAppPlacement(ctx context.Context, id string, constraints, preferences []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET placement_constraints = $1, placement_preferences = $2, updated_at = NOW() WHERE id = $3`,
		constraints, preferences, id,
	)
	return err
}

// --- App Update Strategy ---

func (s *Store) UpdateAppUpdateStrategy(ctx context.Context, id string, strategy string, parallelism int, delay, failureAction, order string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET update_strategy=$1, update_parallelism=$2, update_delay=$3, update_failure_action=$4, update_order=$5, updated_at=NOW() WHERE id=$6`,
		strategy, parallelism, delay, failureAction, order, id,
	)
	return err
}

// --- App Labels ---

func (s *Store) UpdateAppLabels(ctx context.Context, id string, homepageLabels, extraLabels []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET homepage_labels = $1, extra_labels = $2, updated_at = NOW() WHERE id = $3`,
		homepageLabels, extraLabels, id,
	)
	return err
}

// --- Storage Hosts ---

func (s *Store) CreateStorageHost(ctx context.Context, sh *StorageHost) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO storage_host (name, node_id, address, type, default_export_path, default_mount_type, mount_options_default, credentials_encrypted, capabilities, node_label, status)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id, created_at, updated_at`,
		sh.Name, sh.NodeID, sh.Address, sh.Type, sh.DefaultExportPath, sh.DefaultMountType, sh.MountOptionsDefault,
		sh.CredentialsEncrypted, sh.Capabilities, sh.NodeLabel, sh.Status,
	).Scan(&sh.ID, &sh.CreatedAt, &sh.UpdatedAt)
}

func (s *Store) ListStorageHosts(ctx context.Context) ([]StorageHost, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(node_id,''), address, type, default_export_path, default_mount_type, mount_options_default,
		 credentials_encrypted, capabilities, node_label, status, created_at, updated_at
		 FROM storage_host ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hosts []StorageHost
	for rows.Next() {
		var h StorageHost
		if err := rows.Scan(&h.ID, &h.Name, &h.NodeID, &h.Address, &h.Type, &h.DefaultExportPath, &h.DefaultMountType, &h.MountOptionsDefault,
			&h.CredentialsEncrypted, &h.Capabilities, &h.NodeLabel, &h.Status, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	return hosts, nil
}

func (s *Store) GetStorageHost(ctx context.Context, id string) (*StorageHost, error) {
	h := &StorageHost{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(node_id,''), address, type, default_export_path, default_mount_type, mount_options_default,
		 credentials_encrypted, capabilities, node_label, status, created_at, updated_at
		 FROM storage_host WHERE id = $1`, id,
	).Scan(&h.ID, &h.Name, &h.NodeID, &h.Address, &h.Type, &h.DefaultExportPath, &h.DefaultMountType, &h.MountOptionsDefault,
		&h.CredentialsEncrypted, &h.Capabilities, &h.NodeLabel, &h.Status, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (s *Store) UpdateStorageHost(ctx context.Context, sh *StorageHost) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE storage_host SET name=$1, node_id=NULLIF($2,''), address=$3, type=$4, default_export_path=$5, default_mount_type=$6,
		 mount_options_default=$7, credentials_encrypted=$8, capabilities=$9, node_label=$10, status=$11, updated_at=now()
		 WHERE id=$12`,
		sh.Name, sh.NodeID, sh.Address, sh.Type, sh.DefaultExportPath, sh.DefaultMountType, sh.MountOptionsDefault,
		sh.CredentialsEncrypted, sh.Capabilities, sh.NodeLabel, sh.Status, sh.ID,
	)
	return err
}

func (s *Store) DeleteStorageHost(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_host WHERE id = $1`, id)
	return err
}

// --- Node Metrics Snapshots ---

func (s *Store) InsertMetricsSnapshot(ctx context.Context, nodeID string, metrics []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO node_metrics_snapshot (node_id, metrics, collected_at) VALUES ($1, $2, now())`,
		nodeID, metrics,
	)
	return err
}

func (s *Store) GetLatestMetricsSnapshots(ctx context.Context) ([]NodeMetricsSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT ON (node_id) id, node_id, metrics, collected_at
		 FROM node_metrics_snapshot ORDER BY node_id, collected_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snaps []NodeMetricsSnapshot
	for rows.Next() {
		var snap NodeMetricsSnapshot
		if err := rows.Scan(&snap.ID, &snap.NodeID, &snap.Metrics, &snap.CollectedAt); err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

func (s *Store) GetNodeMetricsHistory(ctx context.Context, nodeID string, since time.Time) ([]NodeMetricsSnapshot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, node_id, metrics, collected_at FROM node_metrics_snapshot
		 WHERE node_id = $1 AND collected_at >= $2 ORDER BY collected_at ASC`, nodeID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snaps []NodeMetricsSnapshot
	for rows.Next() {
		var snap NodeMetricsSnapshot
		if err := rows.Scan(&snap.ID, &snap.NodeID, &snap.Metrics, &snap.CollectedAt); err != nil {
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	return snaps, nil
}

func (s *Store) PurgeOldMetricsSnapshots(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	res, err := s.db.ExecContext(ctx, `DELETE FROM node_metrics_snapshot WHERE collected_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- DNS Providers ---

func (s *Store) CreateDNSProvider(ctx context.Context, p *DNSProvider) error {
	if p.IsDefault {
		_, _ = s.db.ExecContext(ctx, `UPDATE dns_provider SET is_default = false WHERE org_id = $1`, p.OrgID)
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO dns_provider (org_id, name, type, config_encrypted, is_default)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		p.OrgID, p.Name, p.Type, p.ConfigEncrypted, p.IsDefault,
	).Scan(&p.ID, &p.CreatedAt)
}

func (s *Store) ListDNSProviders(ctx context.Context, orgID string) ([]DNSProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, type, config_encrypted, is_default, created_at
		 FROM dns_provider WHERE org_id = $1 ORDER BY is_default DESC, created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var providers []DNSProvider
	for rows.Next() {
		var p DNSProvider
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.Type, &p.ConfigEncrypted, &p.IsDefault, &p.CreatedAt); err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return providers, nil
}

func (s *Store) GetDNSProvider(ctx context.Context, id string) (*DNSProvider, error) {
	p := &DNSProvider{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, type, config_encrypted, is_default, created_at
		 FROM dns_provider WHERE id = $1`, id,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.Type, &p.ConfigEncrypted, &p.IsDefault, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) DeleteDNSProvider(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dns_provider WHERE id = $1`, id)
	return err
}

func (s *Store) GetDefaultDNSProvider(ctx context.Context, orgID string) (*DNSProvider, error) {
	p := &DNSProvider{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, type, config_encrypted, is_default, created_at
		 FROM dns_provider WHERE org_id = $1 AND is_default = true LIMIT 1`, orgID,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.Type, &p.ConfigEncrypted, &p.IsDefault, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// --- DNS Records ---

func (s *Store) CreateDNSRecord(ctx context.Context, r *DNSRecord) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO dns_record (provider_id, app_id, domain, record_type, value, proxied, managed, external_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		r.ProviderID, r.AppID, r.Domain, r.RecordType, r.Value, r.Proxied, r.Managed, r.ExternalID,
	).Scan(&r.ID, &r.CreatedAt)
}

func (s *Store) ListDNSRecords(ctx context.Context, providerID string) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider_id, app_id, domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE provider_id = $1 ORDER BY created_at DESC`, providerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []DNSRecord
	for rows.Next() {
		var rec DNSRecord
		if err := rows.Scan(&rec.ID, &rec.ProviderID, &rec.AppID, &rec.Domain, &rec.RecordType, &rec.Value, &rec.Proxied, &rec.Managed, &rec.ExternalID, &rec.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

func (s *Store) DeleteDNSRecord(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dns_record WHERE id = $1`, id)
	return err
}

func (s *Store) GetDNSRecordByAppDomain(ctx context.Context, appID, domain string) (*DNSRecord, error) {
	r := &DNSRecord{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, provider_id, app_id, domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE app_id = $1 AND domain = $2 LIMIT 1`, appID, domain,
	).Scan(&r.ID, &r.ProviderID, &r.AppID, &r.Domain, &r.RecordType, &r.Value, &r.Proxied, &r.Managed, &r.ExternalID, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) UpsertDNSRecord(ctx context.Context, r *DNSRecord) error {
	existing, err := s.db.QueryContext(ctx,
		`SELECT id, created_at FROM dns_record WHERE provider_id = $1 AND COALESCE(app_id,'') = COALESCE($2,'') AND domain = $3 LIMIT 1`,
		r.ProviderID, r.AppID, r.Domain,
	)
	if err != nil {
		return err
	}
	defer existing.Close()

	if existing.Next() {
		var id string
		var createdAt time.Time
		if err := existing.Scan(&id, &createdAt); err != nil {
			return err
		}
		_, err := s.db.ExecContext(ctx,
			`UPDATE dns_record SET record_type = $1, value = $2, proxied = $3, managed = $4, external_id = $5 WHERE id = $6`,
			r.RecordType, r.Value, r.Proxied, r.Managed, r.ExternalID, id,
		)
		if err != nil {
			return err
		}
		r.ID = id
		r.CreatedAt = createdAt
		return nil
	}
	return s.CreateDNSRecord(ctx, r)
}

// --- OrgRole CRUD ---

func (s *Store) CreateOrgRole(ctx context.Context, or *OrgRole) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO org_role (org_id, user_id, role) VALUES ($1, $2, $3) RETURNING id, created_at`,
		or.OrgID, or.UserID, or.Role,
	).Scan(&or.ID, &or.CreatedAt)
}

func (s *Store) GetOrgRole(ctx context.Context, orgID, userID string) (*OrgRole, error) {
	or := &OrgRole{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, user_id, role, created_at FROM org_role WHERE org_id = $1 AND user_id = $2`,
		orgID, userID,
	).Scan(&or.ID, &or.OrgID, &or.UserID, &or.Role, &or.CreatedAt)
	if err != nil {
		return nil, err
	}
	return or, nil
}

func (s *Store) ListOrgRoles(ctx context.Context, orgID string) ([]OrgRole, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, user_id, role, created_at FROM org_role WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []OrgRole
	for rows.Next() {
		var or OrgRole
		if err := rows.Scan(&or.ID, &or.OrgID, &or.UserID, &or.Role, &or.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, or)
	}
	return roles, nil
}

func (s *Store) UpdateOrgRole(ctx context.Context, orgID, userID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE org_role SET role = $1 WHERE org_id = $2 AND user_id = $3`,
		role, orgID, userID,
	)
	return err
}

func (s *Store) DeleteOrgRole(ctx context.Context, orgID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM org_role WHERE org_id = $1 AND user_id = $2`, orgID, userID)
	return err
}

// --- Maintenance CRUD ---

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
	defer rows.Close()
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
	defer rows.Close()
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

// --- Enhanced audit ---

func (s *Store) ListAuditLogsFiltered(ctx context.Context, orgID, userID, action, resource string, limit, offset int) ([]AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query := `SELECT id, user_id, org_id, action, resource, resource_id, details, created_at FROM audit_log WHERE org_id = $1`
	args := []interface{}{orgID}
	argNum := 2
	if userID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argNum)
		args = append(args, userID)
		argNum++
	}
	if action != "" {
		query += fmt.Sprintf(" AND action = $%d", argNum)
		args = append(args, action)
		argNum++
	}
	if resource != "" {
		query += fmt.Sprintf(" AND resource = $%d", argNum)
		args = append(args, resource)
		argNum++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argNum, argNum+1)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []AuditLog
	for rows.Next() {
		var al AuditLog
		if err := rows.Scan(&al.ID, &al.UserID, &al.OrgID, &al.Action, &al.Resource, &al.ResourceID, &al.Details, &al.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, al)
	}
	return logs, nil
}

func (s *Store) GetAuditLogStats(ctx context.Context, orgID string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT action, COUNT(*) FROM audit_log WHERE org_id = $1 GROUP BY action`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := make(map[string]int)
	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, err
		}
		stats[action] = count
	}
	return stats, nil
}

// --- Preview Deployments ---

func (s *Store) CreatePreviewDeployment(ctx context.Context, pd *PreviewDeployment) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO preview_deployment (app_id, branch, pr_number, domain, status, service_name) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at, updated_at`,
		pd.AppID, pd.Branch, pd.PRNumber, pd.Domain, pd.Status, pd.ServiceName,
	).Scan(&pd.ID, &pd.CreatedAt, &pd.UpdatedAt)
}

func (s *Store) ListPreviewDeployments(ctx context.Context, appID string) ([]PreviewDeployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, branch, pr_number, domain, status, service_name, created_at, updated_at
		 FROM preview_deployment WHERE app_id = $1 ORDER BY created_at DESC`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var previews []PreviewDeployment
	for rows.Next() {
		var pd PreviewDeployment
		if err := rows.Scan(&pd.ID, &pd.AppID, &pd.Branch, &pd.PRNumber, &pd.Domain, &pd.Status, &pd.ServiceName, &pd.CreatedAt, &pd.UpdatedAt); err != nil {
			return nil, err
		}
		previews = append(previews, pd)
	}
	return previews, nil
}

func (s *Store) GetPreviewDeployment(ctx context.Context, id string) (*PreviewDeployment, error) {
	pd := &PreviewDeployment{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, branch, pr_number, domain, status, service_name, created_at, updated_at
		 FROM preview_deployment WHERE id = $1`, id,
	).Scan(&pd.ID, &pd.AppID, &pd.Branch, &pd.PRNumber, &pd.Domain, &pd.Status, &pd.ServiceName, &pd.CreatedAt, &pd.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return pd, nil
}

func (s *Store) UpdatePreviewDeploymentStatus(ctx context.Context, id string, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE preview_deployment SET status = $1, updated_at = NOW() WHERE id = $2`, status, id)
	return err
}

func (s *Store) DeletePreviewDeployment(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM preview_deployment WHERE id = $1`, id)
	return err
}

// --- Service Links ---

func (s *Store) CreateServiceLink(ctx context.Context, sl *ServiceLink) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO service_link (source_app_id, target_app_id, target_database_id, env_prefix)
		 VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4) RETURNING id, created_at`,
		sl.SourceAppID, sl.TargetAppID, sl.TargetDatabaseID, sl.EnvPrefix,
	).Scan(&sl.ID, &sl.CreatedAt)
}

func (s *Store) ListServiceLinks(ctx context.Context, appID string) ([]ServiceLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_app_id, COALESCE(target_app_id,''), COALESCE(target_database_id,''), env_prefix, created_at
		 FROM service_link WHERE source_app_id = $1 ORDER BY created_at DESC`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []ServiceLink
	for rows.Next() {
		var sl ServiceLink
		if err := rows.Scan(&sl.ID, &sl.SourceAppID, &sl.TargetAppID, &sl.TargetDatabaseID, &sl.EnvPrefix, &sl.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, sl)
	}
	return links, nil
}

func (s *Store) DeleteServiceLink(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM service_link WHERE id = $1`, id)
	return err
}

// --- Log Entry ---

func (s *Store) InsertLogEntry(ctx context.Context, le *LogEntry) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO log_entry (app_id, service_name, node_id, stream, message, level, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		le.AppID, le.ServiceName, le.NodeID, le.Stream, le.Message, le.Level, le.Timestamp,
	).Scan(&le.ID)
}

func (s *Store) InsertLogEntries(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO log_entry (app_id, service_name, node_id, stream, message, level, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := range entries {
		_, err := stmt.ExecContext(ctx,
			entries[i].AppID, entries[i].ServiceName, entries[i].NodeID, entries[i].Stream,
			entries[i].Message, entries[i].Level, entries[i].Timestamp,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) QueryLogEntries(ctx context.Context, appID string, since, until time.Time, search, level string, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT id, app_id, service_name, node_id, stream, message, level, timestamp
		FROM log_entry WHERE app_id = $1`
	args := []interface{}{appID}
	argNum := 2
	if !since.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argNum)
		args = append(args, since)
		argNum++
	}
	if !until.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argNum)
		args = append(args, until)
		argNum++
	}
	if search != "" {
		query += fmt.Sprintf(" AND message ILIKE $%d", argNum)
		args = append(args, "%"+search+"%")
		argNum++
	}
	if level != "" {
		query += fmt.Sprintf(" AND level = $%d", argNum)
		args = append(args, level)
		argNum++
	}
	query += fmt.Sprintf(" ORDER BY timestamp DESC LIMIT $%d", argNum)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []LogEntry
	for rows.Next() {
		var le LogEntry
		if err := rows.Scan(&le.ID, &le.AppID, &le.ServiceName, &le.NodeID, &le.Stream, &le.Message, &le.Level, &le.Timestamp); err != nil {
			return nil, err
		}
		entries = append(entries, le)
	}
	return entries, nil
}

func (s *Store) PurgeOldLogs(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	res, err := s.db.ExecContext(ctx, `DELETE FROM log_entry WHERE timestamp < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Log Forward Config ---

func (s *Store) CreateLogForwardConfig(ctx context.Context, lfc *LogForwardConfig) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO log_forward_config (org_id, name, type, config_encrypted, enabled)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		lfc.OrgID, lfc.Name, lfc.Type, lfc.ConfigEncrypted, lfc.Enabled,
	).Scan(&lfc.ID, &lfc.CreatedAt)
}

func (s *Store) ListLogForwardConfigs(ctx context.Context, orgID string) ([]LogForwardConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, type, enabled, created_at
		 FROM log_forward_config WHERE org_id = $1 ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []LogForwardConfig
	for rows.Next() {
		var lfc LogForwardConfig
		if err := rows.Scan(&lfc.ID, &lfc.OrgID, &lfc.Name, &lfc.Type, &lfc.Enabled, &lfc.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, lfc)
	}
	return configs, nil
}

func (s *Store) DeleteLogForwardConfig(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM log_forward_config WHERE id = $1`, id)
	return err
}

// --- App Env Vars ---

func (s *Store) CreateAppEnvVar(ctx context.Context, ev *AppEnvVar) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO app_env_var (app_id, key, value_encrypted, is_secret, source) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at, updated_at`,
		ev.AppID, ev.Key, ev.ValueEncrypted, ev.IsSecret, ev.Source,
	).Scan(&ev.ID, &ev.CreatedAt, &ev.UpdatedAt)
}

func (s *Store) ListAppEnvVars(ctx context.Context, appID string) ([]AppEnvVar, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, key, value_encrypted, is_secret, source, created_at, updated_at FROM app_env_var WHERE app_id = $1 ORDER BY key`,
		appID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vars []AppEnvVar
	for rows.Next() {
		var ev AppEnvVar
		if err := rows.Scan(&ev.ID, &ev.AppID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret, &ev.Source, &ev.CreatedAt, &ev.UpdatedAt); err != nil {
			return nil, err
		}
		vars = append(vars, ev)
	}
	return vars, nil
}

func (s *Store) GetAppEnvVar(ctx context.Context, id string) (*AppEnvVar, error) {
	ev := &AppEnvVar{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, key, value_encrypted, is_secret, source, created_at, updated_at FROM app_env_var WHERE id = $1`, id,
	).Scan(&ev.ID, &ev.AppID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret, &ev.Source, &ev.CreatedAt, &ev.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (s *Store) GetAppEnvVarByKey(ctx context.Context, appID, key string) (*AppEnvVar, error) {
	ev := &AppEnvVar{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, app_id, key, value_encrypted, is_secret, source, created_at, updated_at FROM app_env_var WHERE app_id = $1 AND key = $2`,
		appID, key,
	).Scan(&ev.ID, &ev.AppID, &ev.Key, &ev.ValueEncrypted, &ev.IsSecret, &ev.Source, &ev.CreatedAt, &ev.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (s *Store) UpdateAppEnvVar(ctx context.Context, ev *AppEnvVar) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_env_var SET value_encrypted = $1, is_secret = $2, updated_at = NOW() WHERE id = $3`,
		ev.ValueEncrypted, ev.IsSecret, ev.ID,
	)
	return err
}

func (s *Store) DeleteAppEnvVar(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_env_var WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteAppEnvVarByKey(ctx context.Context, appID, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM app_env_var WHERE app_id = $1 AND key = $2`, appID, key)
	return err
}

func (s *Store) BulkUpsertAppEnvVars(ctx context.Context, appID string, vars []AppEnvVar) error {
	for _, ev := range vars {
		ev.AppID = appID
		ev.Source = "user"
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO app_env_var (app_id, key, value_encrypted, is_secret, source) VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (app_id, key) DO UPDATE SET value_encrypted = EXCLUDED.value_encrypted, is_secret = EXCLUDED.is_secret, updated_at = NOW()`,
			ev.AppID, ev.Key, ev.ValueEncrypted, ev.IsSecret, ev.Source,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// --- Template Sources ---

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
	defer rows.Close()

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

// --- Custom Templates ---

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
	defer rows.Close()

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

func (s *Store) ListAppsByTemplate(ctx context.Context, templateName string) ([]App, error) {
	if templateName == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, deploy_type, image, git_repo, git_branch, dockerfile_path, domain, port, replicas, env_encrypted, status,
		 cpu_limit, memory_limit, health_check_path, health_check_interval,
		 homepage_labels, extra_labels, placement_constraints, placement_preferences,
		 update_strategy, update_parallelism, update_delay, update_failure_action, update_order,
		 template_name, template_version, created_at, updated_at
		 FROM app WHERE template_name = $1 ORDER BY created_at DESC`, templateName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []App
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Name, &a.DeployType, &a.Image, &a.GitRepo, &a.GitBranch, &a.DockerfilePath, &a.Domain, &a.Port, &a.Replicas, &a.EnvEncrypted, &a.Status,
			&a.CPULimit, &a.MemoryLimit, &a.HealthCheckPath, &a.HealthCheckInterval,
			&a.HomepageLabels, &a.ExtraLabels, &a.PlacementConstraints, &a.PlacementPreferences,
			&a.UpdateStrategy, &a.UpdateParallelism, &a.UpdateDelay, &a.UpdateFailureAction, &a.UpdateOrder,
			&a.TemplateName, &a.TemplateVersion, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, nil
}

// --- Ceph Clusters ---

func (s *Store) CreateCephCluster(ctx context.Context, c *CephCluster) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO ceph_cluster (name, fsid, status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 ceph_conf_encrypted, admin_keyring_encrypted, replication_size, storage_host_id)
		 VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11,''))
		 RETURNING id, created_at, updated_at`,
		c.Name, c.FSID, c.Status, c.BootstrapNodeID, pqStringArray(c.MonHosts), c.PublicNetwork, c.ClusterNetwork,
		c.CephConfEncrypted, c.AdminKeyringEncrypted, c.ReplicationSize, c.StorageHostID,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (s *Store) GetCephCluster(ctx context.Context, id string) (*CephCluster, error) {
	c := &CephCluster{}
	var monHosts []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(fsid,''), status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 ceph_conf_encrypted, admin_keyring_encrypted, replication_size, COALESCE(storage_host_id,''), created_at, updated_at
		 FROM ceph_cluster WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &c.FSID, &c.Status, &c.BootstrapNodeID, &monHosts, &c.PublicNetwork, &c.ClusterNetwork,
		&c.CephConfEncrypted, &c.AdminKeyringEncrypted, &c.ReplicationSize, &c.StorageHostID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.MonHosts = parsePqStringArray(monHosts)
	return c, nil
}

func (s *Store) GetCephClusterByFSID(ctx context.Context, fsid string) (*CephCluster, error) {
	c := &CephCluster{}
	var monHosts []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, COALESCE(fsid,''), status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 ceph_conf_encrypted, admin_keyring_encrypted, replication_size, COALESCE(storage_host_id,''), created_at, updated_at
		 FROM ceph_cluster WHERE fsid = $1`, fsid,
	).Scan(&c.ID, &c.Name, &c.FSID, &c.Status, &c.BootstrapNodeID, &monHosts, &c.PublicNetwork, &c.ClusterNetwork,
		&c.CephConfEncrypted, &c.AdminKeyringEncrypted, &c.ReplicationSize, &c.StorageHostID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	c.MonHosts = parsePqStringArray(monHosts)
	return c, nil
}

func (s *Store) ListCephClusters(ctx context.Context) ([]CephCluster, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, COALESCE(fsid,''), status, bootstrap_node_id, mon_hosts, public_network, cluster_network,
		 replication_size, COALESCE(storage_host_id,''), created_at, updated_at
		 FROM ceph_cluster ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clusters []CephCluster
	for rows.Next() {
		var c CephCluster
		var monHosts []byte
		if err := rows.Scan(&c.ID, &c.Name, &c.FSID, &c.Status, &c.BootstrapNodeID, &monHosts, &c.PublicNetwork, &c.ClusterNetwork,
			&c.ReplicationSize, &c.StorageHostID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.MonHosts = parsePqStringArray(monHosts)
		clusters = append(clusters, c)
	}
	return clusters, nil
}

func (s *Store) UpdateCephCluster(ctx context.Context, c *CephCluster) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ceph_cluster SET name=$1, fsid=NULLIF($2,''), status=$3, mon_hosts=$4, public_network=$5, cluster_network=$6,
		 ceph_conf_encrypted=$7, admin_keyring_encrypted=$8, replication_size=$9, storage_host_id=NULLIF($10,''), updated_at=now()
		 WHERE id=$11`,
		c.Name, c.FSID, c.Status, pqStringArray(c.MonHosts), c.PublicNetwork, c.ClusterNetwork,
		c.CephConfEncrypted, c.AdminKeyringEncrypted, c.ReplicationSize, c.StorageHostID, c.ID,
	)
	return err
}

func (s *Store) UpdateCephClusterStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ceph_cluster SET status=$1, updated_at=now() WHERE id=$2`, status, id)
	return err
}

func (s *Store) DeleteCephCluster(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ceph_cluster WHERE id = $1`, id)
	return err
}

// --- Ceph OSDs ---

func (s *Store) CreateCephOSD(ctx context.Context, o *CephOSD) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO ceph_osd (cluster_id, node_id, hostname, osd_id, device_path, device_size, device_type, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		o.ClusterID, o.NodeID, o.Hostname, o.OsdID, o.DevicePath, o.DeviceSize, o.DeviceType, o.Status,
	).Scan(&o.ID, &o.CreatedAt)
}

func (s *Store) ListCephOSDs(ctx context.Context, clusterID string) ([]CephOSD, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, cluster_id, node_id, hostname, osd_id, device_path, device_size, device_type, status, created_at
		 FROM ceph_osd WHERE cluster_id = $1 ORDER BY created_at`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var osds []CephOSD
	for rows.Next() {
		var o CephOSD
		if err := rows.Scan(&o.ID, &o.ClusterID, &o.NodeID, &o.Hostname, &o.OsdID, &o.DevicePath, &o.DeviceSize, &o.DeviceType, &o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		osds = append(osds, o)
	}
	return osds, nil
}

func (s *Store) UpdateCephOSDStatus(ctx context.Context, id, status string, osdID *int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ceph_osd SET status=$1, osd_id=$2 WHERE id=$3`, status, osdID, id)
	return err
}

func (s *Store) DeleteCephOSD(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ceph_osd WHERE id = $1`, id)
	return err
}

// --- Ceph Pools ---

func (s *Store) CreateCephPool(ctx context.Context, p *CephPool) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO ceph_pool (cluster_id, name, pool_id, pg_num, size, type, application)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		p.ClusterID, p.Name, p.PoolID, p.PGNum, p.Size, p.Type, p.Application,
	).Scan(&p.ID, &p.CreatedAt)
}

func (s *Store) ListCephPools(ctx context.Context, clusterID string) ([]CephPool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, cluster_id, name, pool_id, pg_num, size, type, application, created_at
		 FROM ceph_pool WHERE cluster_id = $1 ORDER BY created_at`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pools []CephPool
	for rows.Next() {
		var p CephPool
		if err := rows.Scan(&p.ID, &p.ClusterID, &p.Name, &p.PoolID, &p.PGNum, &p.Size, &p.Type, &p.Application, &p.CreatedAt); err != nil {
			return nil, err
		}
		pools = append(pools, p)
	}
	return pools, nil
}

func (s *Store) DeleteCephPool(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ceph_pool WHERE id = $1`, id)
	return err
}

func pqStringArray(arr []string) string {
	if len(arr) == 0 {
		return "{}"
	}
	elements := make([]string, len(arr))
	for i, v := range arr {
		elements[i] = `"` + v + `"`
	}
	result := elements[0]
	for _, e := range elements[1:] {
		result += "," + e
	}
	return "{" + result + "}"
}

func parsePqStringArray(data []byte) []string {
	raw := string(data)
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, `"`)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

