package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CertManager handles mTLS certificate lifecycle for the agent.
type CertManager struct {
	mu              sync.RWMutex
	cert            *tls.Certificate
	caPool          *x509.CertPool
	nodeID          string
	controlPlaneURL string
	bootstrapToken  string
	certDir         string
	key             *ecdsa.PrivateKey
}

// NewCertManager creates a new certificate manager.
func NewCertManager(nodeID, controlPlaneURL, bootstrapToken, certDir string) *CertManager {
	return &CertManager{
		nodeID:          nodeID,
		controlPlaneURL: controlPlaneURL,
		bootstrapToken:  bootstrapToken,
		certDir:         certDir,
	}
}

// Bootstrap generates a new key pair, creates a CSR, sends it to the control plane,
// and stores the signed certificate and CA cert to disk.
func (cm *CertManager) Bootstrap(ctx context.Context) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	csrTemplate := &x509.CertificateRequest{
		// Subject is set by the CA based on nodeID
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, key)
	if err != nil {
		return fmt.Errorf("create csr: %w", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	body, err := json.Marshal(map[string]string{
		"nodeId":         cm.nodeID,
		"bootstrapToken": cm.bootstrapToken,
		"csr":            string(csrPEM),
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := cm.controlPlaneURL + "/internal/agent/register"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("register failed: status %d", resp.StatusCode)
	}

	var result struct {
		Cert   string `json:"cert"`
		CACert string `json:"caCert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if result.Cert == "" {
		return fmt.Errorf("empty certificate in response")
	}
	if result.CACert == "" {
		return fmt.Errorf("empty CA certificate in response")
	}

	// Parse and build the TLS certificate
	certBlock, _ := pem.Decode([]byte(result.Cert))
	if certBlock == nil {
		return fmt.Errorf("invalid cert PEM")
	}
	tlsCert := tls.Certificate{
		Certificate: [][]byte{certBlock.Bytes},
		PrivateKey:  key,
	}

	// Parse CA cert
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM([]byte(result.CACert)) {
		return fmt.Errorf("failed to parse CA cert")
	}

	// Write to disk
	if err := os.MkdirAll(cm.certDir, 0o700); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(cm.certDir, "agent.key"), keyPEM, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cm.certDir, "agent.crt"), []byte(result.Cert), 0o644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cm.certDir, "ca.crt"), []byte(result.CACert), 0o644); err != nil {
		return fmt.Errorf("write ca cert: %w", err)
	}

	cm.mu.Lock()
	cm.cert = &tlsCert
	cm.caPool = caPool
	cm.key = key
	cm.mu.Unlock()

	log.Printf("mTLS bootstrap complete for node %s", cm.nodeID)
	return nil
}

// LoadExisting loads certificates from disk. Returns false if certs don't exist
// or are expiring within 24 hours.
func (cm *CertManager) LoadExisting() bool {
	certPath := filepath.Join(cm.certDir, "agent.crt")
	keyPath := filepath.Join(cm.certDir, "agent.key")
	caPath := filepath.Join(cm.certDir, "ca.crt")

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return false
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return false
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return false
	}

	// Check expiry
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return false
	}
	if time.Until(leaf.NotAfter) < 24*time.Hour {
		log.Printf("cert expires in less than 24h (%s), will re-bootstrap", leaf.NotAfter)
		return false
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		return false
	}

	cm.mu.Lock()
	cm.cert = &tlsCert
	cm.caPool = caPool
	cm.mu.Unlock()

	log.Printf("loaded existing certs (expires %s)", leaf.NotAfter.Format(time.RFC3339))
	return true
}

// TLSConfig returns a *tls.Config for the agent's mTLS server.
func (cm *CertManager) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			cm.mu.RLock()
			defer cm.mu.RUnlock()
			if cm.cert == nil {
				return nil, fmt.Errorf("no certificate loaded")
			}
			return cm.cert, nil
		},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  cm.CAPool(),
		MinVersion: tls.VersionTLS13,
	}
}

// CAPool returns the current CA certificate pool.
func (cm *CertManager) CAPool() *x509.CertPool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.caPool
}

// CertExpiresAt returns the expiry time of the current certificate.
func (cm *CertManager) CertExpiresAt() time.Time {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	if cm.cert == nil || len(cm.cert.Certificate) == 0 {
		return time.Time{}
	}
	leaf, err := x509.ParseCertificate(cm.cert.Certificate[0])
	if err != nil {
		return time.Time{}
	}
	return leaf.NotAfter
}

// RunRenewalLoop checks for certificate expiry and re-bootstraps when needed.
// For a 72h cert, renewal triggers at NotAfter - 24h (i.e., 48h into the cert's life).
func (cm *CertManager) RunRenewalLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expires := cm.CertExpiresAt()
			if expires.IsZero() {
				continue
			}
			if time.Until(expires) < 24*time.Hour {
				log.Printf("cert expires at %s, renewing...", expires.Format(time.RFC3339))
				backoff := time.Second
				for attempt := 0; attempt < 5; attempt++ {
					if err := cm.Bootstrap(ctx); err != nil {
						log.Printf("renewal attempt %d failed: %v", attempt+1, err)
						select {
						case <-ctx.Done():
							return
						case <-time.After(backoff):
						}
						backoff *= 2
						continue
					}
					log.Printf("cert renewed successfully, new expiry: %s", cm.CertExpiresAt().Format(time.RFC3339))
					break
				}
			}
		}
	}
}
