package store

import (
	"context"
)

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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
