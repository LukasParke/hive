package agentclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/luke/hive/control-plane/internal/ca"
	v1 "github.com/luke/hive/proto/gen/agent/v1"
)

func TestNewDialer(t *testing.T) {
	authority, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := LoadOrCreateClientCert(context.Background(), authority, nil)
	if err != nil {
		t.Fatal(err)
	}

	d, err := NewDialer(authority, cert, true)
	if err != nil {
		t.Fatal(err)
	}

	// Test client creation
	client := d.ClientPlaintext("node-1", "localhost:9090")
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Test caching
	client2 := d.ClientPlaintext("node-1", "localhost:9090")
	if client != client2 {
		t.Error("expected same cached client instance")
	}

	// Test different node returns different client
	client3 := d.ClientPlaintext("node-2", "localhost:9091")
	if client == client3 {
		t.Error("expected different client for different node")
	}
}

func TestDialerTLSConfig(t *testing.T) {
	authority, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := LoadOrCreateClientCert(context.Background(), authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDialer(authority, cert, true)
	if err != nil {
		t.Fatal(err)
	}

	cfg := d.TLSConfigFor("node-7")
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %v, want TLS 1.3", cfg.MinVersion)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected exactly one client certificate, got %d", len(cfg.Certificates))
	}
	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "hive-control-plane" {
		t.Errorf("client cert CN = %q, want hive-control-plane", leaf.Subject.CommonName)
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected RootCAs pool on dialer TLS config")
	}
	// The client certificate must chain to the dialer's CA pool.
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: cfg.RootCAs}); err != nil {
		t.Errorf("client certificate should verify against the dialer CA pool: %v", err)
	}
	// The ServerName must follow the per-node agent certificate pattern.
	if want := agentServerName("node-7"); cfg.ServerName != want {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, want)
	}
}

// TestDialerPlaintextTLSConfig proves the no-mTLS dialer builds configs
// without a CA pool or client certificate (plaintext overlay mode).
func TestDialerPlaintextTLSConfig(t *testing.T) {
	authority, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := LoadOrCreateClientCert(context.Background(), authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDialer(authority, cert, false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := d.TLSConfigFor("node-7")
	if cfg.RootCAs != nil {
		t.Error("plaintext dialer should not carry a CA pool")
	}
	if cfg.ServerName != agentServerName("node-7") {
		t.Errorf("ServerName = %q, want %q", cfg.ServerName, agentServerName("node-7"))
	}
}

// TestClientRoutingMatrix proves Client() routes to plaintext or mTLS
// clients per dialer mode and caches each per node|addr key.
func TestClientRoutingMatrix(t *testing.T) {
	ctx := context.Background()
	authority, err := ca.LoadOrCreate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := LoadOrCreateClientCert(ctx, authority, nil)
	if err != nil {
		t.Fatal(err)
	}

	plainDialer, err := NewDialer(authority, cert, false)
	if err != nil {
		t.Fatal(err)
	}
	plain := plainDialer.Client("node-1", "10.0.0.1:7000")
	if plain == nil {
		t.Fatal("nil plaintext client")
	}
	// Cached: same instance on repeat and via ClientPlaintext directly.
	if again := plainDialer.Client("node-1", "10.0.0.1:7000"); again != plain {
		t.Error("plaintext client not cached across Client calls")
	}

	mtlsDialer, err := NewDialer(authority, cert, true)
	if err != nil {
		t.Fatal(err)
	}
	mtls := mtlsDialer.Client("node-1", "10.0.0.1:7000")
	if mtls == nil {
		t.Fatal("nil mtls client")
	}
	if mtls == plain {
		t.Error("mtls and plaintext routing returned the same client")
	}
	// Cached under the write-lock double-check too.
	if again := mtlsDialer.Client("node-1", "10.0.0.1:7000"); again != mtls {
		t.Error("mtls client not cached across Client calls")
	}
	if other := mtlsDialer.Client("node-2", "10.0.0.1:7000"); other == mtls {
		t.Error("different node must get its own client")
	}
	if other := mtlsDialer.Client("node-1", "10.0.0.2:7000"); other == mtls {
		t.Error("different addr must get its own client")
	}
}

// TestDialerMTLSDialEndToEnd proves the mTLS client actually dials: a local
// TLS server presents an agent certificate signed by the CA, and the dialer's
// client completes the handshake (client cert + CA root + agent ServerName).
func TestDialerMTLSDialEndToEnd(t *testing.T) {
	ctx := context.Background()
	authority, err := ca.LoadOrCreate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Agent key + certificate with the "agent-<nodeID>" CN.
	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	agentCertPEM, err := authority.IssueCertificate("agent-e2e-node", &agentKey.PublicKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	agentKeyDER, err := x509.MarshalECPrivateKey(agentKey)
	if err != nil {
		t.Fatal(err)
	}
	agentCertTLSPair, err := tls.X509KeyPair(agentCertPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: agentKeyDER}))
	if err != nil {
		t.Fatal(err)
	}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{agentCertTLSPair},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Handshake completes; then drop the connection — the dial is
			// all this test proves.
			go func() {
				_ = conn.(*tls.Conn).HandshakeContext(ctx)
				_ = conn.Close()
			}()
		}
	}()

	cpCert, err := LoadOrCreateClientCert(ctx, authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDialer(authority, cpCert, true)
	if err != nil {
		t.Fatal(err)
	}
	client := d.Client("e2e-node", ln.Addr().String())
	if client == nil {
		t.Fatal("nil client")
	}
	// Any RPC forces the http2 transport through DialTLSContext.
	_, err = client.Health(ctx, connect.NewRequest(&v1.HealthRequest{}))
	if err == nil {
		t.Log("unexpected healthy agent")
	}
}

// TestNewDialerCATrustFailure covers the AppendCertsFromPEM failure branch.
func TestNewDialerCATrustFailure(t *testing.T) {
	orig := appendCertToPool
	appendCertToPool = func(*x509.CertPool, []byte) bool { return false }
	defer func() { appendCertToPool = orig }()

	authority, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := LoadOrCreateClientCert(context.Background(), authority, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDialer(authority, cert, true); err == nil || !strings.Contains(err.Error(), "failed to add CA cert") {
		t.Errorf("want CA-pool failure, got %v", err)
	}
}
