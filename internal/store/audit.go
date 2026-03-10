package store

import (
	"context"
	"fmt"
)

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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
