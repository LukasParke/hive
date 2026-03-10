package store

import (
	"context"
)

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
	defer func() { _ = rows.Close() }()
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
