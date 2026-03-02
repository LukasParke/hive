package proxy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func WriteCertificateFiles(configDir string, cert *store.CustomCertificate) error {
	certDir := filepath.Join(configDir, "certs")
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}

	certFile := filepath.Join(certDir, cert.ID+".crt")
	if err := os.WriteFile(certFile, []byte(cert.CertPEM), 0644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}

	keyPEM, err := encryption.Decrypt(cert.KeyPEMEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt key: %w", err)
	}

	keyFile := filepath.Join(certDir, cert.ID+".key")
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	return nil
}

func RemoveCertificateFiles(configDir, certID string) error {
	certDir := filepath.Join(configDir, "certs")
	if err := os.Remove(filepath.Join(certDir, certID+".crt")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove cert file: %w", err)
	}
	if err := os.Remove(filepath.Join(certDir, certID+".key")); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove key file: %w", err)
	}
	return nil
}

func GenerateTLSConfig(configDir string, certs []store.CustomCertificate) error {
	if len(certs) == 0 {
		return nil
	}

	certDir := filepath.Join(configDir, "certs")
	var entries []string
	for _, c := range certs {
		entries = append(entries,
			fmt.Sprintf("[[tls.certificates]]\n  certFile = \"%s/%s.crt\"\n  keyFile = \"%s/%s.key\"",
				certDir, c.ID, certDir, c.ID))
	}

	content := ""
	for _, e := range entries {
		content += e + "\n\n"
	}

	return os.WriteFile(filepath.Join(configDir, "tls.toml"), []byte(content), 0644)
}
