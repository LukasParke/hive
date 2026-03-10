package store

import (
	"context"
	"time"
)

type Cluster struct {
	ID                 string    `json:"id"`
	OrgID              string    `json:"org_id"`
	Name               string    `json:"name"`
	APIEndpoint        string    `json:"api_endpoint"`
	AuthTokenEncrypted string    `json:"-"`
	TLSCA              string    `json:"-"`
	IsLocal            bool      `json:"is_local"`
	Status             string    `json:"status"`
	NodeCount          int       `json:"node_count"`
	CreatedAt          time.Time `json:"created_at"`
}

func (s *Store) CreateCluster(ctx context.Context, orgID, name, endpoint, token, tlsCA string, isLocal bool) (*Cluster, error) {
	c := &Cluster{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO cluster (org_id, name, api_endpoint, auth_token_encrypted, tls_ca, is_local)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, org_id, name, api_endpoint, is_local, status, node_count, created_at`,
		orgID, name, endpoint, token, tlsCA, isLocal,
	).Scan(&c.ID, &c.OrgID, &c.Name, &c.APIEndpoint, &c.IsLocal, &c.Status, &c.NodeCount, &c.CreatedAt)
	return c, err
}

func (s *Store) ListClusters(ctx context.Context, orgID string) ([]Cluster, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, api_endpoint, is_local, status, node_count, created_at
		 FROM cluster WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.OrgID, &c.Name, &c.APIEndpoint, &c.IsLocal, &c.Status, &c.NodeCount, &c.CreatedAt); err != nil {
			return nil, err
		}
		clusters = append(clusters, c)
	}
	return clusters, nil
}

func (s *Store) DeleteCluster(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cluster WHERE id = $1`, id)
	return err
}

func (s *Store) UpdateClusterStatus(ctx context.Context, id, status string, nodeCount int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE cluster SET status=$2, node_count=$3 WHERE id=$1`, id, status, nodeCount)
	return err
}
