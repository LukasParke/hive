package agentclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"

	"github.com/luke/hive/control-plane/internal/ca"
	"github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
)

// Dialer creates ConnectRPC clients for communicating with agents. When
// mtls is enabled it dials with a client certificate and verifies agent
// server certificates against the CA; otherwise it dials plaintext (for use
// on the encrypted hive_internal overlay in dev setups without certs).
type Dialer struct {
	authority *ca.Authority
	tlsCert   tls.Certificate
	caPool    *x509.CertPool
	mtls      bool
	clients   map[string]agentv1connect.AgentServiceClient
	mu        sync.Mutex
}

// appendCertToPool is a seam so tests can inject trust-root failures.
var appendCertToPool = func(pool *x509.CertPool, pemCerts []byte) bool {
	return pool.AppendCertsFromPEM(pemCerts)
}

// NewDialer creates a new agent dialer that authenticates with the given
// control-plane client certificate (see LoadOrCreateClientCert) and, when
// mtls is true, verifies agent server certificates against the CA pool built
// from authority.
func NewDialer(authority *ca.Authority, tlsCert tls.Certificate, mtls bool) (*Dialer, error) {
	d := &Dialer{
		authority: authority,
		tlsCert:   tlsCert,
		mtls:      mtls,
		clients:   make(map[string]agentv1connect.AgentServiceClient),
	}
	if mtls {
		caPool := x509.NewCertPool()
		if !appendCertToPool(caPool, authority.CertPEM()) {
			return nil, fmt.Errorf("failed to add CA cert to pool")
		}
		d.caPool = caPool
	}
	return d, nil
}

// agentServerName returns the TLS ServerName for an agent's certificate,
// matching the "agent-<nodeID>" CN pattern issued by the CA.
func agentServerName(nodeID string) string {
	return "agent-" + nodeID
}

// TLSConfigFor returns the mTLS configuration used when dialing the agent
// for nodeID: our client certificate, the CA pool as trust root, and a
// ServerName matching the agent certificate pattern "agent-<nodeID>".
func (d *Dialer) TLSConfigFor(nodeID string) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{d.tlsCert},
		RootCAs:      d.caPool,
		MinVersion:   tls.VersionTLS13,
		ServerName:   agentServerName(nodeID),
	}
}

// Client returns (or creates) a ConnectRPC client for the given agent,
// honoring the dialer's mtls setting. Clients are rare to create and never
// touched per-request, so a plain exclusive lock is simpler than an
// RWMutex double-check dance.
func (d *Dialer) Client(nodeID, addr string) agentv1connect.AgentServiceClient {
	if !d.mtls {
		return d.ClientPlaintext(nodeID, addr)
	}
	key := nodeID + "|" + addr

	d.mu.Lock()
	defer d.mu.Unlock()

	if c, ok := d.clients[key]; ok {
		return c
	}

	tlsConfig := d.TLSConfigFor(nodeID)

	transport := &http2.Transport{
		TLSClientConfig: tlsConfig,
		DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
			return tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, network, address, tlsConfig)
		},
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	baseURL := fmt.Sprintf("https://%s", addr)
	client := agentv1connect.NewAgentServiceClient(httpClient, baseURL, connect.WithGRPC())
	d.clients[key] = client
	return client
}

// ClientPlaintext returns a ConnectRPC client without mTLS (for use on encrypted overlay networks).
func (d *Dialer) ClientPlaintext(nodeID, addr string) agentv1connect.AgentServiceClient {
	key := "plaintext|" + nodeID + "|" + addr

	d.mu.Lock()
	defer d.mu.Unlock()

	if c, ok := d.clients[key]; ok {
		return c
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	baseURL := fmt.Sprintf("http://%s", addr)
	client := agentv1connect.NewAgentServiceClient(httpClient, baseURL)
	d.clients[key] = client
	return client
}
