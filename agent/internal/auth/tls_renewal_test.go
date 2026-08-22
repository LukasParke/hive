package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newRegisterHandlerWithValidity behaves like newRegisterHandler but signs
// agent certificates with the given validity window.
func newRegisterHandlerWithValidity(token string, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, caPEM []byte, validity time.Duration, hits *atomic.Int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		var req struct {
			NodeID         string `json:"nodeId"`
			BootstrapToken string `json:"bootstrapToken"`
			CSR            string `json:"csr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.BootstrapToken != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		csrBlock, _ := pem.Decode([]byte(req.CSR))
		if csrBlock == nil {
			http.Error(w, "invalid csr", http.StatusBadRequest)
			return
		}
		csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		template := &x509.Certificate{
			Subject:     pkix.Name{CommonName: "agent-" + req.NodeID},
			NotBefore:   time.Now().Add(-time.Minute),
			NotAfter:    time.Now().Add(validity),
			KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		}
		certRaw, err := x509.CreateCertificate(rand.Reader, template, caCert, csr.PublicKey, caKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certRaw})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"cert":   string(certPEM),
			"caCert": string(caPEM),
		})
	}
}

// fixedResponseHandler returns an arbitrary status/body, for error-path tests.
func fixedResponseHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// jsonString JSON-encodes a Go string for embedding in fixture bodies.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestBootstrapRequestPayload(t *testing.T) {
	var gotNode, gotToken string
	caKey, caCert, caPEM := generateTestCA(t)
	handler := newRegisterHandler("test-token", caKey, caCert, caPEM)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/agent/register" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %q", ct)
		}
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body)) // let the wrapped handler decode too
		if err != nil {
			t.Errorf("read request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var req struct {
			NodeID         string `json:"nodeId"`
			BootstrapToken string `json:"bootstrapToken"`
			CSR            string `json:"csr"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		gotNode, gotToken = req.NodeID, req.BootstrapToken
		block, _ := pem.Decode([]byte(req.CSR))
		if block == nil || block.Type != "CERTIFICATE REQUEST" {
			t.Errorf("body must carry a CERTIFICATE REQUEST pem")
		} else if _, err := x509.ParseCertificateRequest(block.Bytes); err != nil {
			t.Errorf("CSR does not parse: %v", err)
		}
		handler(w, r)
	}))
	defer server.Close()

	cm := NewCertManager("payload-node", server.URL, "test-token", t.TempDir(), "")
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	if gotNode != "payload-node" || gotToken != "test-token" {
		t.Errorf("nodeId=%q bootstrapToken=%q", gotNode, gotToken)
	}
}

func TestBootstrapErrorBranches(t *testing.T) {
	validPEMCert := "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"
	cases := []struct {
		name    string
		url     string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name:    "connection refused",
			url:     "http://127.0.0.1:1",
			handler: nil,
			wantErr: "register request",
		},
		{
			name:    "http 500",
			url:     "",
			handler: fixedResponseHandler(http.StatusInternalServerError, "boom"),
			wantErr: "register failed: status 500",
		},
		{
			name:    "http 401",
			url:     "",
			handler: fixedResponseHandler(http.StatusUnauthorized, "nope"),
			wantErr: "register failed: status 401",
		},
		{
			name:    "invalid json",
			url:     "",
			handler: fixedResponseHandler(http.StatusOK, "{not-json"),
			wantErr: "decode response",
		},
		{
			name:    "empty cert",
			url:     "",
			handler: fixedResponseHandler(http.StatusOK, `{"cert":"","caCert":"x"}`),
			wantErr: "empty certificate",
		},
		{
			name:    "empty ca cert",
			url:     "",
			handler: fixedResponseHandler(http.StatusOK, `{"cert":"x","caCert":""}`),
			wantErr: "empty CA certificate",
		},
		{
			name:    "invalid cert pem",
			url:     "",
			handler: fixedResponseHandler(http.StatusOK, `{"cert":"garbage","caCert":`+jsonString(validPEMCert)+`}`),
			wantErr: "invalid cert PEM",
		},
		{
			name: "unparsable ca pem",
			url:  "",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"cert":` + jsonString(validPEMCert) + `,"caCert":"not a cert"}`))
			},
			wantErr: "failed to parse CA cert",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := tc.url
			if tc.handler != nil {
				srv := httptest.NewServer(tc.handler)
				defer srv.Close()
				url = srv.URL
			}
			cm := NewCertManager("node", url, "tok", t.TempDir(), "")
			err := cm.Bootstrap(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
			}
			if cm.TLSConfig() == nil {
				t.Error("TLSConfig should still return a config")
			}
		})
	}
}

func TestBootstrapRequestCreationError(t *testing.T) {
	// A URL that cannot be parsed into a request fails before any I/O.
	cm := NewCertManager("node", "ht\x74p://bad url with space", "tok", t.TempDir(), "")
	err := cm.Bootstrap(context.Background())
	if err == nil || !strings.Contains(err.Error(), "create request") {
		t.Fatalf("error = %v, want create-request failure", err)
	}
}

func TestBootstrapWriteFailures(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandler("tok", caKey, caCert, caPEM))
	defer server.Close()

	base := t.TempDir()

	t.Run("cert dir not creatable", func(t *testing.T) {
		blocker := filepath.Join(base, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		cm := NewCertManager("node", server.URL, "tok", filepath.Join(blocker, "certs"), "")
		err := cm.Bootstrap(context.Background())
		if err == nil || !strings.Contains(err.Error(), "create cert dir") {
			t.Fatalf("error = %v, want mkdir failure", err)
		}
	})

	t.Run("key write fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "agent.key"), 0o700); err != nil {
			t.Fatal(err)
		}
		cm := NewCertManager("node", server.URL, "tok", dir, "")
		err := cm.Bootstrap(context.Background())
		if err == nil || !strings.Contains(err.Error(), "write key") {
			t.Fatalf("error = %v, want key write failure", err)
		}
	})

	t.Run("crt write fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "agent.crt"), 0o700); err != nil {
			t.Fatal(err)
		}
		cm := NewCertManager("node", server.URL, "tok", dir, "")
		err := cm.Bootstrap(context.Background())
		if err == nil || !strings.Contains(err.Error(), "write cert") {
			t.Fatalf("error = %v, want crt write failure", err)
		}
	})

	t.Run("ca write fails", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "ca.crt"), 0o700); err != nil {
			t.Fatal(err)
		}
		cm := NewCertManager("node", server.URL, "tok", dir, "")
		err := cm.Bootstrap(context.Background())
		if err == nil || !strings.Contains(err.Error(), "write ca cert") {
			t.Fatalf("error = %v, want ca write failure", err)
		}
	})
}

func TestRunRenewalLoopDefaultInterval(t *testing.T) {
	cm := NewCertManager("node", "", "", t.TempDir(), "")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cm.RunRenewalLoop(ctx)
		close(done)
	}()
	cancel() // default hourly ticker must not block cancellation
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit with default interval")
	}
}
func TestBootstrapPinnedCAFileUnreadableFallsBack(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandler("tok", caKey, caCert, caPEM))
	defer server.Close()

	certDir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "does-not-exist.pem")
	cm := NewCertManager("node", server.URL, "tok", certDir, missing)
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(certDir, "ca.crt")) //nolint:gosec // test-only path from t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(caPEM) {
		t.Error("expected fallback to control-plane CA when pinned file unreadable")
	}
}

func TestLoadExistingExpiringSoonRejects(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandlerWithValidity("tok", caKey, caCert, caPEM, time.Hour, nil))
	defer server.Close()

	certDir := t.TempDir()
	cm := NewCertManager("node", server.URL, "tok", certDir, "")
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}
	cm2 := NewCertManager("node", server.URL, "tok", certDir, "")
	if cm2.LoadExisting() {
		t.Error("LoadExisting must reject certs expiring within 24h")
	}
}

func TestLoadExistingCorruptOrMissingFiles(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandler("tok", caKey, caCert, caPEM))
	defer server.Close()
	ctx := context.Background()

	t.Run("corrupt key", func(t *testing.T) {
		dir := t.TempDir()
		cm := NewCertManager("n", server.URL, "tok", dir, "")
		if err := cm.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "agent.key"), []byte("junk"), 0o600); err != nil {
			t.Fatal(err)
		}
		if cm.LoadExisting() {
			t.Error("expected failure with corrupt key")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		dir := t.TempDir()
		cm := NewCertManager("n", server.URL, "tok", dir, "")
		if err := cm.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "agent.key")); err != nil {
			t.Fatal(err)
		}
		if cm.LoadExisting() {
			t.Error("expected failure with missing key")
		}
	})

	t.Run("corrupt ca", func(t *testing.T) {
		dir := t.TempDir()
		cm := NewCertManager("n", server.URL, "tok", dir, "")
		if err := cm.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("junk"), 0o600); err != nil {
			t.Fatal(err)
		}
		if cm.LoadExisting() {
			t.Error("expected failure with corrupt CA")
		}
	})

	t.Run("no ca anywhere", func(t *testing.T) {
		dir := t.TempDir()
		cm := NewCertManager("n", server.URL, "tok", dir, "")
		if err := cm.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "ca.crt")); err != nil {
			t.Fatal(err)
		}
		if cm.LoadExisting() {
			t.Error("expected failure with no cached or pinned CA")
		}
	})
}

func TestTLSConfigGetCertificate(t *testing.T) {
	cm := NewCertManager("node", "", "", t.TempDir(), "")

	cfg := cm.TLSConfig()
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Error("expected RequireAndVerifyClientCert")
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Error("expected TLS 1.3 minimum")
	}

	if _, err := cfg.GetCertificate(nil); err == nil || !strings.Contains(err.Error(), "no certificate loaded") {
		t.Fatalf("GetCertificate = %v, want no-certificate error", err)
	}
	if cm.CAPool() != nil {
		t.Error("CAPool should be nil before any certificate material is loaded")
	}
	if !cm.CertExpiresAt().IsZero() {
		t.Error("CertExpiresAt should be zero before any certificate is loaded")
	}

	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandler("tok", caKey, caCert, caPEM))
	defer server.Close()
	cm2 := NewCertManager("node", server.URL, "tok", t.TempDir(), "")
	if err := cm2.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg2 := cm2.TLSConfig()
	cert, err := cfg2.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if len(cert.Certificate) != 1 {
		t.Errorf("expected leaf chain of 1, got %d", len(cert.Certificate))
	}
	if cfg2.ClientCAs != cm2.CAPool() {
		t.Error("ClientCAs should be wired to CAPool()")
	}
	if cm2.CertExpiresAt().IsZero() {
		t.Error("CertExpiresAt should report the loaded cert's NotAfter")
	}
}

func TestCertExpiresAtCorruptedCert(t *testing.T) {
	cm := &CertManager{}
	cm.cert = &tls.Certificate{Certificate: [][]byte{{0x00, 0x01}}}
	if !cm.CertExpiresAt().IsZero() {
		t.Error("unparsable certificate bytes should yield zero time")
	}
	cm2 := &CertManager{cert: &tls.Certificate{}}
	if !cm2.CertExpiresAt().IsZero() {
		t.Error("empty chain should yield zero time")
	}
}

func TestRunRenewalLoopRenewsExpiringCert(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	var hits atomic.Int64
	server := httptest.NewServer(newRegisterHandlerWithValidity("tok", caKey, caCert, caPEM, time.Hour, &hits))
	defer server.Close()

	cm := NewCertManager("node", server.URL, "tok", t.TempDir(), "")
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	initialHits := hits.Load()

	cm.tickInterval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		cm.RunRenewalLoop(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	renewed := false
	for time.Now().Before(deadline) {
		if hits.Load() > initialHits {
			renewed = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("renewal loop did not exit after context cancel")
	}
	if !renewed {
		t.Fatal("expected at least one renewal attempt against the registration endpoint")
	}
}

func TestRunRenewalLoopRetriesThenHonorsCancel(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandlerWithValidity("tok", caKey, caCert, caPEM, time.Hour, nil))
	cm := NewCertManager("node", server.URL, "tok", t.TempDir(), "")
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.Close() // renewal attempts will now fail

	cm.tickInterval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cm.RunRenewalLoop(ctx)
		close(done)
	}()

	time.Sleep(1300 * time.Millisecond) // allow two failing attempts so the backoff doubles
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("renewal loop did not honor context cancellation during backoff")
	}
}

func TestRunRenewalLoopWithoutCertKeepsPolling(t *testing.T) {
	cm := NewCertManager("node", "http://127.0.0.1:1", "tok", t.TempDir(), "")
	cm.tickInterval = time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cm.RunRenewalLoop(ctx)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not exit")
	}
}
