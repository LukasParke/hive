package store

import (
	"context"
	"time"
)

type NodeConfig struct {
	ID                   string    `json:"id"`
	NodeID               string    `json:"node_id"`
	Hostname             string    `json:"hostname"`
	MACAddress           string    `json:"mac_address"`
	BMCAddress           string    `json:"bmc_address"`
	BMCUsername          string    `json:"bmc_username"`
	BMCPasswordEncrypted string    `json:"-"`
	WoLEnabled           bool      `json:"wol_enabled"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (s *Store) GetNodeConfig(ctx context.Context, nodeID string) (*NodeConfig, error) {
	n := &NodeConfig{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, node_id, hostname, mac_address, bmc_address, bmc_username, bmc_password_encrypted, wol_enabled, created_at, updated_at
		 FROM node_config WHERE node_id = $1`, nodeID,
	).Scan(&n.ID, &n.NodeID, &n.Hostname, &n.MACAddress, &n.BMCAddress, &n.BMCUsername, &n.BMCPasswordEncrypted, &n.WoLEnabled, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

func (s *Store) UpsertNodeConfig(ctx context.Context, nodeID, hostname, mac, bmc, bmcUser, bmcPass string, wol bool) (*NodeConfig, error) {
	n := &NodeConfig{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO node_config (node_id, hostname, mac_address, bmc_address, bmc_username, bmc_password_encrypted, wol_enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (node_id) DO UPDATE SET hostname=$2, mac_address=$3, bmc_address=$4, bmc_username=$5, bmc_password_encrypted=$6, wol_enabled=$7, updated_at=NOW()
		 RETURNING id, node_id, hostname, mac_address, bmc_address, bmc_username, bmc_password_encrypted, wol_enabled, created_at, updated_at`,
		nodeID, hostname, mac, bmc, bmcUser, bmcPass, wol,
	).Scan(&n.ID, &n.NodeID, &n.Hostname, &n.MACAddress, &n.BMCAddress, &n.BMCUsername, &n.BMCPasswordEncrypted, &n.WoLEnabled, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}
