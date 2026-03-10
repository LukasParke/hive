package store

import (
	"context"
	"database/sql"
	"time"
)

type APIToken struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"org_id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"-"`
	Scopes     string     `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Store) CreateAPIToken(ctx context.Context, orgID, userID, name, tokenHash, scopes string, expiresAt *time.Time) (*APIToken, error) {
	t := &APIToken{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		 RETURNING id, org_id, user_id, name, scopes::text, last_used_at, expires_at, created_at`,
		orgID, userID, name, tokenHash, scopes, expiresAt,
	).Scan(&t.ID, &t.OrgID, &t.UserID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt)
	return t, err
}

func (s *Store) ListAPITokens(ctx context.Context, orgID string) ([]APIToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, user_id, name, scopes::text, last_used_at, expires_at, created_at
		 FROM api_token WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.OrgID, &t.UserID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *Store) GetAPITokenByHash(ctx context.Context, hash string) (*APIToken, error) {
	t := &APIToken{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, user_id, name, scopes::text, last_used_at, expires_at, created_at
		 FROM api_token WHERE token_hash = $1`, hash,
	).Scan(&t.ID, &t.OrgID, &t.UserID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *Store) TouchAPIToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_token SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

func (s *Store) DeleteAPIToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api_token WHERE id = $1`, id)
	return err
}
