package store

import (
	"context"
	"encoding/json"
)

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
	defer func() { _ = rows.Close() }()

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
