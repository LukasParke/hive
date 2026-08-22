// Package cloudflare provides a small client for the subset of the
// Cloudflare REST API v4 that Hive needs to manage named tunnels
// (cfd_tunnel) and their DNS CNAME routes.
package cloudflare

import "context"

// TunnelRef is the result of creating a Cloudflare tunnel: its UUID, the
// base64 connector token (also embedded at the front of the credentials
// file) and the full credentials JSON blob cloudflared consumes.
type TunnelRef struct {
	ID              string
	Token           string
	CredentialsJSON []byte
}

// API is the Cloudflare surface the tunnel manager depends on. The real
// implementation talks to the REST v4 API with a bearer token; tests
// supply a fake or an httptest-backed client.
type API interface {
	// CreateTunnel provisions a named tunnel on the given account.
	CreateTunnel(ctx context.Context, accountID, name string) (TunnelRef, error)
	// DeleteTunnel removes a tunnel from the account.
	DeleteTunnel(ctx context.Context, accountID, id string) error
	// GetTunnel returns the Cloudflare-reported tunnel status
	// (e.g. "healthy", "down", "degraded").
	GetTunnel(ctx context.Context, accountID, id string) (string, error)
	// CreateDNSRoute publishes a proxied CNAME <hostname> ->
	// <tunnelID>.cfargotunnel.com in the given zone and returns the
	// created DNS record ID.
	CreateDNSRoute(ctx context.Context, zoneID, hostname, tunnelID string) (string, error)
	// DeleteDNSRecord removes a previously created DNS record.
	DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error
}
