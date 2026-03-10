package store

import (
	"context"
	"time"
)

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
	defer func() { _ = rows.Close() }()
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

func (s *Store) UpdateDNSProvider(ctx context.Context, p *DNSProvider) error {
	if p.IsDefault {
		_, _ = s.db.ExecContext(ctx, `UPDATE dns_provider SET is_default = false WHERE org_id = $1 AND id <> $2`, p.OrgID, p.ID)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE dns_provider
		 SET name = $1, type = $2, config_encrypted = $3, is_default = $4
		 WHERE id = $5`,
		p.Name, p.Type, p.ConfigEncrypted, p.IsDefault, p.ID,
	)
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

func (s *Store) CreateDNSRecord(ctx context.Context, r *DNSRecord) error {
	return s.db.QueryRowContext(ctx,
		`INSERT INTO dns_record (provider_id, app_id, domain, record_type, value, proxied, managed, external_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`,
		r.ProviderID, sqlNullIfEmpty(r.AppID), r.Domain, r.RecordType, r.Value, r.Proxied, r.Managed, r.ExternalID,
	).Scan(&r.ID, &r.CreatedAt)
}

func (s *Store) ListDNSRecords(ctx context.Context, providerID string) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider_id, COALESCE(app_id, ''), domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE provider_id = $1 ORDER BY created_at DESC`, providerID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func (s *Store) GetDNSRecord(ctx context.Context, id string) (*DNSRecord, error) {
	r := &DNSRecord{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, provider_id, COALESCE(app_id, ''), domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE id = $1`, id,
	).Scan(&r.ID, &r.ProviderID, &r.AppID, &r.Domain, &r.RecordType, &r.Value, &r.Proxied, &r.Managed, &r.ExternalID, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) GetDNSRecordByAppDomain(ctx context.Context, appID, domain string) (*DNSRecord, error) {
	r := &DNSRecord{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, provider_id, COALESCE(app_id, ''), domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE app_id = $1 AND domain = $2 LIMIT 1`, appID, domain,
	).Scan(&r.ID, &r.ProviderID, &r.AppID, &r.Domain, &r.RecordType, &r.Value, &r.Proxied, &r.Managed, &r.ExternalID, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) ListDNSRecordsByAppID(ctx context.Context, appID string) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider_id, COALESCE(app_id, ''), domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE app_id = $1 ORDER BY created_at DESC`, appID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func (s *Store) ListAllManagedDNSRecords(ctx context.Context) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, provider_id, COALESCE(app_id, ''), domain, record_type, value, proxied, managed, external_id, created_at
		 FROM dns_record WHERE managed = true ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
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

func sqlNullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *Store) UpsertDNSRecord(ctx context.Context, r *DNSRecord) error {
	existing, err := s.db.QueryContext(ctx,
		`SELECT id, created_at FROM dns_record WHERE provider_id = $1 AND COALESCE(app_id,'') = COALESCE($2,'') AND domain = $3 LIMIT 1`,
		r.ProviderID, r.AppID, r.Domain,
	)
	if err != nil {
		return err
	}
	defer func() { _ = existing.Close() }()

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
