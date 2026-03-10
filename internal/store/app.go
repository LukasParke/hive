package store

import (
	"context"
)

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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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

func (s *Store) ListAllApps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.project_id, a.name, a.deploy_type, a.image, a.git_repo, a.git_branch, a.dockerfile_path, a.domain, a.port, a.replicas, a.env_encrypted, a.status,
		 a.cpu_limit, a.memory_limit, a.health_check_path, a.health_check_interval,
		 a.homepage_labels, a.extra_labels, a.placement_constraints, a.placement_preferences,
		 a.update_strategy, a.update_parallelism, a.update_delay, a.update_failure_action, a.update_order,
		 a.build_cache_enabled, a.auto_deploy_branch, a.preview_environments,
		 a.template_name, a.template_version, a.created_at, a.updated_at
		 FROM app a ORDER BY a.status = 'failed' DESC, a.updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

func (s *Store) ListAllAppsByOrg(ctx context.Context, orgID string) ([]App, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.project_id, a.name, a.deploy_type, a.image, a.git_repo, a.git_branch, a.dockerfile_path, a.domain, a.port, a.replicas, a.env_encrypted, a.status,
		 a.cpu_limit, a.memory_limit, a.health_check_path, a.health_check_interval,
		 a.homepage_labels, a.extra_labels, a.placement_constraints, a.placement_preferences,
		 a.update_strategy, a.update_parallelism, a.update_delay, a.update_failure_action, a.update_order,
		 a.build_cache_enabled, a.auto_deploy_branch, a.preview_environments,
		 a.template_name, a.template_version, a.created_at, a.updated_at
		 FROM app a
		 JOIN project p ON p.id = a.project_id
		 WHERE p.org_id = $1
		 ORDER BY a.status = 'failed' DESC, a.updated_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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

func (s *Store) UpdateAppPlacement(ctx context.Context, id string, constraints, preferences []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET placement_constraints = $1, placement_preferences = $2, updated_at = NOW() WHERE id = $3`,
		constraints, preferences, id,
	)
	return err
}

func (s *Store) UpdateAppUpdateStrategy(ctx context.Context, id string, strategy string, parallelism int, delay, failureAction, order string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET update_strategy=$1, update_parallelism=$2, update_delay=$3, update_failure_action=$4, update_order=$5, updated_at=NOW() WHERE id=$6`,
		strategy, parallelism, delay, failureAction, order, id,
	)
	return err
}

func (s *Store) UpdateAppLabels(ctx context.Context, id string, homepageLabels, extraLabels []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE app SET homepage_labels = $1, extra_labels = $2, updated_at = NOW() WHERE id = $3`,
		homepageLabels, extraLabels, id,
	)
	return err
}

func (s *Store) ListGitAppsWithAutoDeploy(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, deploy_type, image, git_repo, git_branch, dockerfile_path, domain, port, replicas, env_encrypted, status,
		 cpu_limit, memory_limit, health_check_path, health_check_interval,
		 homepage_labels, extra_labels, placement_constraints, placement_preferences,
		 update_strategy, update_parallelism, update_delay, update_failure_action, update_order,
		 build_cache_enabled, auto_deploy_branch, preview_environments,
		 template_name, template_version, created_at, updated_at
		 FROM app WHERE deploy_type = 'git' AND git_repo != '' AND auto_deploy_branch != ''
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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
