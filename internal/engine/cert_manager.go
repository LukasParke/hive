package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
	"gopkg.in/yaml.v3"
)

type tlsConfig struct {
	TLS tlsSection `yaml:"tls"`
}

type tlsSection struct {
	Certificates []tlsCertEntry `yaml:"certificates"`
}

type tlsCertEntry struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

func (s *Server) writeCertFiles(ctx context.Context, cert *store.CustomCertificate) error {
	certsDir := filepath.Join(s.cfg.DataDir, "traefik", "certs")
	if err := os.MkdirAll(certsDir, 0755); err != nil {
		return fmt.Errorf("create certs dir: %w", err)
	}

	certFile := filepath.Join(certsDir, cert.ID+".crt")
	keyFile := filepath.Join(certsDir, cert.ID+".key")

	if err := os.WriteFile(certFile, []byte(cert.CertPEM), 0644); err != nil {
		return fmt.Errorf("write cert PEM: %w", err)
	}

	keyPEM, err := encryption.Decrypt(cert.KeyPEMEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt key PEM: %w", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key PEM: %w", err)
	}

	return s.regenerateTLSConfig(ctx)
}

func (s *Server) removeCertFiles(ctx context.Context, certID string) {
	certsDir := filepath.Join(s.cfg.DataDir, "traefik", "certs")
	_ = os.Remove(filepath.Join(certsDir, certID+".crt"))
	_ = os.Remove(filepath.Join(certsDir, certID+".key"))
	_ = s.regenerateTLSConfig(ctx)
}

func (s *Server) regenerateTLSConfig(ctx context.Context) error {
	certsDir := filepath.Join(s.cfg.DataDir, "traefik", "certs")
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		return err
	}

	var certs []tlsCertEntry
	seen := make(map[string]bool)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".crt" {
			base := e.Name()[:len(e.Name())-4]
			if seen[base] {
				continue
			}
			seen[base] = true
			keyPath := filepath.Join(certsDir, base+".key")
			if _, err := os.Stat(keyPath); err == nil {
				certs = append(certs, tlsCertEntry{
					CertFile: filepath.Join("/certs", base+".crt"),
					KeyFile:  filepath.Join("/certs", base+".key"),
				})
			}
		}
	}

	cfg := tlsConfig{TLS: tlsSection{Certificates: certs}}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	configDir := filepath.Join(s.cfg.DataDir, "traefik")
	return os.WriteFile(filepath.Join(configDir, "tls.yml"), data, 0644)
}
