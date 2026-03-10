package store

import (
	"context"
	"time"
)

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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
