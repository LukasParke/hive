package agentclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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

// Dialer creates mTLS ConnectRPC clients for communicating with agents.
type Dialer struct {
	authority *ca.Authority
	tlsCert   tls.Certificate
	caPool    *x509.CertPool
	clients   map[string]agentv1connect.AgentServiceClient
	mu        sync.RWMutex
}

// NewDialer creates a new agent dialer. It generates a client certificate
// signed by the CA for the control-plane to authenticate to agents.
func NewDialer(authority *ca.Authority) (*Dialer, error) {
	// Generate control-plane client key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	// Create CSR and have the CA sign it
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: "control-plane",
		},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return nil, fmt.Errorf("create csr: %w", err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("parse csr: %w", err)
	}

	// Sign with long TTL for the control-plane
	certPEM, err := authority.SignAgentCSR(csr, "control-plane", 365*24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("sign client cert: %w", err)
	}

	// Build tls.Certificate from the signed cert + our private key
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	tlsCert, err := tls.X509KeyPair(certPEM, pemEncode("EC PRIVATE KEY", keyDER))
	if err != nil {
		return nil, fmt.Errorf("x509 key pair: %w", err)
	}

	// Build CA pool
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(authority.CertPEM()) {
		return nil, fmt.Errorf("failed to add CA cert to pool")
	}

	return &Dialer{
		authority: authority,
		tlsCert:   tlsCert,
		caPool:    caPool,
		clients:   make(map[string]agentv1connect.AgentServiceClient),
	}, nil
}

// Client returns (or creates) a ConnectRPC client for the given agent.
func (d *Dialer) Client(nodeID, addr string) agentv1connect.AgentServiceClient {
	key := nodeID + "|" + addr

	d.mu.RLock()
	if c, ok := d.clients[key]; ok {
		d.mu.RUnlock()
		return c
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check after acquiring write lock
	if c, ok := d.clients[key]; ok {
		return c
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{d.tlsCert},
		RootCAs:      d.caPool,
		MinVersion:   tls.VersionTLS13,
		ServerName:   fmt.Sprintf("agent-%s", nodeID),
	}

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

	d.mu.RLock()
	if c, ok := d.clients[key]; ok {
		d.mu.RUnlock()
		return c
	}
	d.mu.RUnlock()

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

func pemEncode(blockType string, data []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data})
}
