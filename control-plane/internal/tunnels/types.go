// Package tunnels orchestrates Cloudflare tunnel lifecycle: creating the
// upstream tunnel through the Cloudflare API, storing its credentials
// encrypted, deploying a cloudflared connector service on the swarm,
// publishing DNS CNAME routes and tracking live connector health.
package tunnels

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Tunnel lifecycle statuses stored in tunnels.status.
const (
	StatusCreating = "creating"
	StatusDeployed = "deployed"
	StatusError    = "error"
	StatusDeleting = "deleting"
)

// Sentinel errors mapped onto HTTP statuses by the API layer.
var (
	// ErrNotFound means no tunnel row exists for the given identifier.
	ErrNotFound = errors.New("tunnel not found")
	// ErrConflict means the tunnel name is already taken.
	ErrConflict = errors.New("tunnel already exists")
	// ErrInvalidInput means request payloads failed semantic validation;
	// the message is safe to show to callers.
	ErrInvalidInput = errors.New("invalid tunnel input")
	// ErrNoCredentials means the encrypted Cloudflare API token for an
	// existing tunnel could not be decrypted (e.g. rotated master key).
	ErrNoCredentials = errors.New("tunnel api token unavailable")
)

// InvalidInput builds a validation error carrying a caller-safe message.
func InvalidInput(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// IngressRule mirrors the frozen openapi TunnelIngressRule schema.
type IngressRule struct {
	Hostname string `json:"hostname"`
	Path     string `json:"path,omitempty"`
	Service  string `json:"service"`
}

// ConnectorStatus is live connector health surfaced under
// Tunnel.connector in the API.
type ConnectorStatus struct {
	DesiredReplicas  uint64
	RunningReplicas  int
	CloudflareStatus string
}

// Row is the persistence view of a managed tunnel.
type Row struct {
	ID                   string
	Name                 string
	CfTunnelID           string
	AccountID            string
	ZoneID               string
	CredentialSecretName string
	Ingress              []IngressRule
	DNSRecords           map[string]string
	Status               string
	ErrorMessage         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// View pairs a tunnel row with its live connector health.
type View struct {
	Row       *Row
	Connector ConnectorStatus
}

// CreateParams carries the validated create-tunnel request.
type CreateParams struct {
	Name      string
	AccountID string
	ZoneID    string
	APIToken  string
	Ingress   []IngressRule
}

// Repository abstracts tunnel persistence. The production implementation
// sits on the generated sqlc queries; tests supply an in-memory double.
type Repository interface {
	Create(ctx context.Context, row *Row) error
	Get(ctx context.Context, id string) (*Row, error)
	GetByName(ctx context.Context, name string) (*Row, error)
	List(ctx context.Context) ([]*Row, error)
	UpdateIngress(ctx context.Context, id string, rules []IngressRule) error
	UpdateDNSRecords(ctx context.Context, id string, records map[string]string) error
	SetStatus(ctx context.Context, id string, status, errorMessage string) error
	Delete(ctx context.Context, id string) error
	// ForgetSecrets removes encrypted secrets_store entries by name; it
	// backs credential teardown on tunnel deletion.
	ForgetSecrets(ctx context.Context, names []string) error
}

// CredentialStore persists opaque blobs (credentials JSON, API tokens)
// encrypted at rest. *secrets.Store satisfies it.
type CredentialStore interface {
	Put(ctx context.Context, name, typ string, plain []byte) error
	Get(ctx context.Context, name, typ string) ([]byte, error)
}

// SecretType is the secrets_store type used for tunnel credential payloads.
const SecretType = "tunnel_credential" //nolint:gosec // symbolic name, not a credential

// Names of the two encrypted secrets kept per tunnel.
func credentialSecretKey(cfTunnelID string) string { return "tunnel:" + cfTunnelID }
func apiTokenSecretKey(cfTunnelID string) string   { return "tunnel-api-token:" + cfTunnelID }
