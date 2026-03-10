package store

import (
	"context"
	"time"
)

type VPNServer struct {
	ID                  string    `json:"id"`
	OrgID               string    `json:"org_id"`
	Name                string    `json:"name"`
	NodeID              string    `json:"node_id"`
	ListenPort          int       `json:"listen_port"`
	AddressRange        string    `json:"address_range"`
	DNS                 string    `json:"dns"`
	PrivateKeyEncrypted string    `json:"-"`
	PublicKey           string    `json:"public_key"`
	Endpoint            string    `json:"endpoint"`
	Enabled             bool      `json:"enabled"`
	CreatedAt           time.Time `json:"created_at"`
}

type VPNPeer struct {
	ID                    string     `json:"id"`
	ServerID              string     `json:"server_id"`
	Name                  string     `json:"name"`
	PublicKey             string     `json:"public_key"`
	PresharedKeyEncrypted string     `json:"-"`
	AllowedIPs            string     `json:"allowed_ips"`
	AssignedIP            string     `json:"assigned_ip"`
	LastHandshake         *time.Time `json:"last_handshake"`
	TransferRX            int64      `json:"transfer_rx"`
	TransferTX            int64      `json:"transfer_tx"`
	CreatedAt             time.Time  `json:"created_at"`
}

func (s *Store) CreateVPNServer(ctx context.Context, orgID, name, nodeID string, port int, addrRange, dns, privKey, pubKey, endpoint string) (*VPNServer, error) {
	v := &VPNServer{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO vpn_server (org_id, name, node_id, listen_port, address_range, dns, private_key_encrypted, public_key, endpoint)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING id, org_id, name, node_id, listen_port, address_range, dns, public_key, endpoint, enabled, created_at`,
		orgID, name, nodeID, port, addrRange, dns, privKey, pubKey, endpoint,
	).Scan(&v.ID, &v.OrgID, &v.Name, &v.NodeID, &v.ListenPort, &v.AddressRange, &v.DNS, &v.PublicKey, &v.Endpoint, &v.Enabled, &v.CreatedAt)
	return v, err
}

func (s *Store) ListVPNServers(ctx context.Context, orgID string) ([]VPNServer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, name, node_id, listen_port, address_range, dns, public_key, endpoint, enabled, created_at
		 FROM vpn_server WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var servers []VPNServer
	for rows.Next() {
		var v VPNServer
		if err := rows.Scan(&v.ID, &v.OrgID, &v.Name, &v.NodeID, &v.ListenPort, &v.AddressRange, &v.DNS, &v.PublicKey, &v.Endpoint, &v.Enabled, &v.CreatedAt); err != nil {
			return nil, err
		}
		servers = append(servers, v)
	}
	return servers, nil
}

func (s *Store) GetVPNServer(ctx context.Context, id string) (*VPNServer, error) {
	v := &VPNServer{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, name, node_id, listen_port, address_range, dns, private_key_encrypted, public_key, endpoint, enabled, created_at
		 FROM vpn_server WHERE id = $1`, id,
	).Scan(&v.ID, &v.OrgID, &v.Name, &v.NodeID, &v.ListenPort, &v.AddressRange, &v.DNS, &v.PrivateKeyEncrypted, &v.PublicKey, &v.Endpoint, &v.Enabled, &v.CreatedAt)
	return v, err
}

func (s *Store) DeleteVPNServer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM vpn_server WHERE id = $1`, id)
	return err
}

func (s *Store) CreateVPNPeer(ctx context.Context, serverID, name, pubKey, psk, allowedIPs, assignedIP string) (*VPNPeer, error) {
	p := &VPNPeer{}
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO vpn_peer (server_id, name, public_key, preshared_key_encrypted, allowed_ips, assigned_ip)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, server_id, name, public_key, allowed_ips, assigned_ip, last_handshake, transfer_rx, transfer_tx, created_at`,
		serverID, name, pubKey, psk, allowedIPs, assignedIP,
	).Scan(&p.ID, &p.ServerID, &p.Name, &p.PublicKey, &p.AllowedIPs, &p.AssignedIP, &p.LastHandshake, &p.TransferRX, &p.TransferTX, &p.CreatedAt)
	return p, err
}

func (s *Store) ListVPNPeers(ctx context.Context, serverID string) ([]VPNPeer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, server_id, name, public_key, allowed_ips, assigned_ip, last_handshake, transfer_rx, transfer_tx, created_at
		 FROM vpn_peer WHERE server_id = $1 ORDER BY name`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var peers []VPNPeer
	for rows.Next() {
		var p VPNPeer
		if err := rows.Scan(&p.ID, &p.ServerID, &p.Name, &p.PublicKey, &p.AllowedIPs, &p.AssignedIP, &p.LastHandshake, &p.TransferRX, &p.TransferTX, &p.CreatedAt); err != nil {
			return nil, err
		}
		peers = append(peers, p)
	}
	return peers, nil
}

func (s *Store) GetVPNPeer(ctx context.Context, id string) (*VPNPeer, error) {
	p := &VPNPeer{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, server_id, name, public_key, preshared_key_encrypted, allowed_ips, assigned_ip, last_handshake, transfer_rx, transfer_tx, created_at
		 FROM vpn_peer WHERE id = $1`, id,
	).Scan(&p.ID, &p.ServerID, &p.Name, &p.PublicKey, &p.PresharedKeyEncrypted, &p.AllowedIPs, &p.AssignedIP, &p.LastHandshake, &p.TransferRX, &p.TransferTX, &p.CreatedAt)
	return p, err
}

func (s *Store) DeleteVPNPeer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM vpn_peer WHERE id = $1`, id)
	return err
}

func (s *Store) CountVPNPeers(ctx context.Context, serverID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM vpn_peer WHERE server_id = $1`, serverID).Scan(&count)
	return count, err
}
