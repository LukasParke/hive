package store

import (
	"context"
	"database/sql"
)

func (s *Store) CreateGitHubApp(ctx context.Context, app *GitHubApp) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO github_app (org_id, app_id, app_slug, pem_encrypted, webhook_secret, client_id, client_secret_encrypted, installation_id, html_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, created_at`,
		app.OrgID, app.AppID, app.AppSlug, app.PemEncrypted, app.WebhookSecret,
		app.ClientID, app.ClientSecretEncrypted, app.InstallationID, app.HTMLURL,
	).Scan(&app.ID, &app.CreatedAt)
}

func (s *Store) GetGitHubApp(ctx context.Context, id string) (*GitHubApp, error) {
	app := &GitHubApp{}
	var installID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, app_id, app_slug, pem_encrypted, webhook_secret, client_id, client_secret_encrypted, COALESCE(installation_id,0), html_url, created_at
		 FROM github_app WHERE id = $1`, id,
	).Scan(&app.ID, &app.OrgID, &app.AppID, &app.AppSlug, &app.PemEncrypted, &app.WebhookSecret,
		&app.ClientID, &app.ClientSecretEncrypted, &installID, &app.HTMLURL, &app.CreatedAt)
	if err != nil {
		return nil, err
	}
	app.InstallationID = installID.Int64
	return app, nil
}

func (s *Store) GetGitHubAppByOrg(ctx context.Context, orgID string) (*GitHubApp, error) {
	app := &GitHubApp{}
	var installID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, app_id, app_slug, pem_encrypted, webhook_secret, client_id, client_secret_encrypted, COALESCE(installation_id,0), html_url, created_at
		 FROM github_app WHERE org_id = $1 ORDER BY created_at DESC LIMIT 1`, orgID,
	).Scan(&app.ID, &app.OrgID, &app.AppID, &app.AppSlug, &app.PemEncrypted, &app.WebhookSecret,
		&app.ClientID, &app.ClientSecretEncrypted, &installID, &app.HTMLURL, &app.CreatedAt)
	if err != nil {
		return nil, err
	}
	app.InstallationID = installID.Int64
	return app, nil
}

func (s *Store) GetGitHubAppByAppID(ctx context.Context, appID int) (*GitHubApp, error) {
	app := &GitHubApp{}
	var installID sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, app_id, app_slug, pem_encrypted, webhook_secret, client_id, client_secret_encrypted, COALESCE(installation_id,0), html_url, created_at
		 FROM github_app WHERE app_id = $1 LIMIT 1`, appID,
	).Scan(&app.ID, &app.OrgID, &app.AppID, &app.AppSlug, &app.PemEncrypted, &app.WebhookSecret,
		&app.ClientID, &app.ClientSecretEncrypted, &installID, &app.HTMLURL, &app.CreatedAt)
	if err != nil {
		return nil, err
	}
	app.InstallationID = installID.Int64
	return app, nil
}

func (s *Store) UpdateGitHubAppInstallation(ctx context.Context, id string, installationID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE github_app SET installation_id = $1 WHERE id = $2`,
		installationID, id,
	)
	return err
}

func (s *Store) DeleteGitHubApp(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM github_app WHERE id = $1`, id)
	return err
}

func (s *Store) ListGitHubApps(ctx context.Context) ([]GitHubApp, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, app_id, app_slug, webhook_secret, client_id, COALESCE(installation_id,0), html_url, created_at
		 FROM github_app ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var apps []GitHubApp
	for rows.Next() {
		var a GitHubApp
		var installID sql.NullInt64
		if err := rows.Scan(&a.ID, &a.OrgID, &a.AppID, &a.AppSlug, &a.WebhookSecret,
			&a.ClientID, &installID, &a.HTMLURL, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.InstallationID = installID.Int64
		apps = append(apps, a)
	}
	return apps, nil
}
