package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBootstrapAndLoadExisting(t *testing.T) {
	// Create a mock CA
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caRaw, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caRaw)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caRaw})

	// Mock control plane
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			NodeID         string `json:"nodeId"`
			BootstrapToken string `json:"bootstrapToken"`
			CSR            string `json:"csr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.BootstrapToken != "test-token" {
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

		serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		template := &x509.Certificate{
			SerialNumber: serial,
			Subject:      pkix.Name{CommonName: "agent-" + req.NodeID},
			NotBefore:    time.Now().Add(-time.Minute),
			NotAfter:     time.Now().Add(72 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
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
	}))
	defer server.Close()

	certDir := t.TempDir()

	cm := NewCertManager("test-node-1", server.URL, "test-token", certDir, "")

	// Test bootstrap
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	// Verify cert files were written
	if _, err := os.Stat(filepath.Join(certDir, "agent.crt")); err != nil {
		t.Error("agent.crt not found")
	}
	if _, err := os.Stat(filepath.Join(certDir, "agent.key")); err != nil {
		t.Error("agent.key not found")
	}
	if _, err := os.Stat(filepath.Join(certDir, "ca.crt")); err != nil {
		t.Error("ca.crt not found")
	}

	// Test TLS config
	tlsCfg := cm.TLSConfig()
	if tlsCfg.MinVersion != tls.VersionTLS13 {
		t.Error("expected TLS 1.3 minimum")
	}
	if tlsCfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Error("expected RequireAndVerifyClientCert")
	}

	// Test cert expiry
	expires := cm.CertExpiresAt()
	if expires.IsZero() {
		t.Error("cert expiry should not be zero")
	}
	if time.Until(expires) < 71*time.Hour {
		t.Errorf("cert should expire in ~72h, got %v", time.Until(expires))
	}

	// Test LoadExisting
	cm2 := NewCertManager("test-node-1", server.URL, "test-token", certDir, "")
	if !cm2.LoadExisting() {
		t.Error("LoadExisting should succeed with valid certs")
	}

	// Test LoadExisting with empty dir
	cm3 := NewCertManager("test-node-1", server.URL, "test-token", t.TempDir(), "")
	if cm3.LoadExisting() {
		t.Error("LoadExisting should fail with no certs")
	}
}

// generateTestCA creates a throwaway self-signed CA for tests.
func generateTestCA(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
	return caKey, cert, pemBytes
}

// TestBootstrapPrefersPinnedCAFile verifies that when AGENT_CA_FILE points at
// a readable CA, bootstrap pins that CA instead of trusting the one returned
// by the control plane.
func TestBootstrapPrefersPinnedCAFile(t *testing.T) {
	_, _, pinnedPEM := generateTestCA(t)
	responseKey, responseCert, responsePEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandler("test-token", responseKey, responseCert, responsePEM))
	defer server.Close()

	certDir := t.TempDir()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pinnedPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	cm := NewCertManager("test-node-1", server.URL, "test-token", certDir, caFile)
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(certDir, "ca.crt")) //nolint:gosec // test-only path from t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(pinnedPEM) {
		t.Error("bootstrap should pin the CA from AGENT_CA_FILE, not the control-plane response")
	}
	pool := cm.CAPool()
	if pool == nil {
		t.Fatal("expected non-nil CA pool after bootstrap")
	}
}

// newRegisterHandler returns a mock control-plane registration endpoint that
// signs agent CSRs with the given CA.
func newRegisterHandler(token string, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, caPEM []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
		template := &x509.Certificate{
			SerialNumber: serial,
			Subject:      pkix.Name{CommonName: "agent-" + req.NodeID},
			NotBefore:    time.Now().Add(-time.Minute),
			NotAfter:     time.Now().Add(72 * time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
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

// TestLoadExistingFallsBackToPinnedCAFile verifies LoadExisting still works
// when the cached ca.crt is missing but AGENT_CA_FILE is readable.
func TestLoadExistingFallsBackToPinnedCAFile(t *testing.T) {
	caKey, caCert, caPEM := generateTestCA(t)
	server := httptest.NewServer(newRegisterHandler("test-token", caKey, caCert, caPEM))
	defer server.Close()

	certDir := t.TempDir()
	cm := NewCertManager("test-node-1", server.URL, "test-token", certDir, "")
	if err := cm.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap failed: %v", err)
	}

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(certDir, "ca.crt")); err != nil {
		t.Fatal(err)
	}
	cm2 := NewCertManager("test-node-1", server.URL, "test-token", certDir, caFile)
	if !cm2.LoadExisting() {
		t.Error("LoadExisting should succeed via pinned CA file fallback")
	}
}
