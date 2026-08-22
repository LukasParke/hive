package ca

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
)

// Secret names used to persist the CA material in the secrets store.
const (
	CAKeyName  = "hive_ca_key"
	CACertName = "hive_ca_cert"
	// CAKeyType and CACertType are secret_type enum values.
	CAKeyType  = "ca_key"
	CACertType = "ca_cert"
)

// SecretStore persists and retrieves secret material. It is implemented by
// *secrets.Store; a nil store means ephemeral (in-memory only) material.
type SecretStore interface {
	Get(ctx context.Context, name, typ string) ([]byte, error)
	Put(ctx context.Context, name, typ string, plain []byte) error
}

// Authority is the internal hive certificate authority. All replicas converge
// on the same CA because the key and certificate are persisted in the shared
// secrets store and loaded on boot.
type Authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// generateKey creates the CA's ECDSA P-256 key. A package variable so tests
// can inject failures.
var generateKey = func() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// randomInt draws a random serial in [0, max). A package variable so tests
// can inject failures or oversized serials.
var randomInt = func(max *big.Int) (*big.Int, error) {
	return rand.Int(rand.Reader, max)
}

// createCert and parseCert wrap x509 self-signing and parsing. Package
// variables so tests can inject failures.
var createCert = x509.CreateCertificate
var parseCert = x509.ParseCertificate

// LoadOrCreate returns the CA persisted in store, generating and persisting a
// new ECDSA P-256 self-signed CA (10y validity) when none exists. A nil store
// yields an ephemeral CA that is not persisted. All replicas calling this
// against the same store converge on the same CA.
func LoadOrCreate(ctx context.Context, store SecretStore) (*Authority, error) {
	if store != nil {
		keyPEM, keyErr := store.Get(ctx, CAKeyName, CAKeyType)
		certPEM, certErr := store.Get(ctx, CACertName, CACertType)
		switch {
		case keyErr == nil && certErr == nil:
			a, err := fromPEM(keyPEM, certPEM)
			if err != nil {
				return nil, fmt.Errorf("load persisted CA: %w", err)
			}
			return a, nil
		case errors.Is(keyErr, pgx.ErrNoRows) && errors.Is(certErr, pgx.ErrNoRows):
			// No CA yet: fall through and create one.
		default:
			return nil, fmt.Errorf("inconsistent CA material in secrets store (key err: %v, cert err: %v)", keyErr, certErr)
		}
	}

	key, err := generateKey()
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := randomInt(big.NewInt(1 << 62))
	if err != nil {
		return nil, fmt.Errorf("generate CA serial: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "hive-internal-ca",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	raw, err := createCert(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("self-sign CA certificate: %w", err)
	}
	cert, err := parseCert(raw)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	a := &Authority{cert: cert, key: key}

	if store != nil {
		if err := store.Put(ctx, CAKeyName, CAKeyType, a.KeyPEM()); err != nil {
			return nil, fmt.Errorf("persist CA key: %w", err)
		}
		if err := store.Put(ctx, CACertName, CACertType, a.CertPEM()); err != nil {
			return nil, fmt.Errorf("persist CA certificate: %w", err)
		}
	}
	return a, nil
}

// CertPEM returns the CA certificate in PEM form.
func (a *Authority) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw})
}

// KeyPEM returns the CA private key in PEM form (PKCS#8).
func (a *Authority) KeyPEM() []byte {
	der, err := x509.MarshalPKCS8PrivateKey(a.key)
	if err != nil {
		// MarshalPKCS8PrivateKey only fails for unsupported key types; an
		// ECDSA key is always supported.
		panic(fmt.Sprintf("marshal CA key: %v", err))
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// IssueCertificate signs a leaf certificate for cn over pub with client and
// server auth extended key usage, returning the certificate in PEM form.
func (a *Authority) IssueCertificate(cn string, pub crypto.PublicKey, ttl time.Duration) ([]byte, error) {
	serial, err := randomInt(big.NewInt(1 << 62))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(ttl),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	raw, err := x509.CreateCertificate(rand.Reader, tpl, a.cert, pub, a.key)
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), nil
}

// SignAgentCSR validates the CSR and returns a signed certificate PEM with
// CN "agent-<nodeID>".
func (a *Authority) SignAgentCSR(csr *x509.CertificateRequest, nodeID string, ttl time.Duration) ([]byte, error) {
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	return a.IssueCertificate(fmt.Sprintf("agent-%s", nodeID), csr.PublicKey, ttl)
}

// fromPEM parses a PKCS#8 (or EC) private key and certificate PEM pair.
func fromPEM(keyPEM, certPEM []byte) (*Authority, error) {
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("no PEM block in CA key")
	}
	genericKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		ecKey, ecErr := x509.ParseECPrivateKey(keyBlock.Bytes)
		if ecErr != nil {
			return nil, fmt.Errorf("parse CA key: %w", err)
		}
		genericKey = ecKey
	}
	key, ok := genericKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("CA key is not ECDSA")
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("no PEM block in CA certificate")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	return &Authority{cert: cert, key: key}, nil
}
