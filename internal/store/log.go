package store

import (
	"context"
	"fmt"
	"time"
)

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
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO log_entry (app_id, service_name, node_id, stream, message, level, timestamp)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
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
	defer func() { _ = rows.Close() }()
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

func (s *Store) QuerySystemLogEntries(ctx context.Context, since, until time.Time, search, level string, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT id, app_id, service_name, node_id, stream, message, level, timestamp
		FROM log_entry WHERE 1=1`
	var args []interface{}
	argNum := 1
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
