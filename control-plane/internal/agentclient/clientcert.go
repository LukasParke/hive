package agentclient

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/luke/hive/control-plane/internal/ca"
)

// Secret names under which the control-plane client certificate material is
// persisted in the secrets store.
const (
	ClientKeyName  = "hive_cp_client_key"
	ClientCertName = "hive_cp_client_cert"
	// ClientKeyCertType and ClientCertType are secret_type enum values.
	ClientKeyType  = "tls_key"
	ClientCertType = "tls_cert"
)

// clientCertTTL matches the agent certificate lifetime so a control-plane
// restart within the renewal window always yields a fresh certificate.
const clientCertTTL = 72 * time.Hour

// clientCertCN is the common name of the control-plane client certificate.
const clientCertCN = "hive-control-plane"

// DefaultMinValidity is the renew window used when a caller does not specify
// one: a persisted certificate expiring within 24h is re-issued on boot.
const DefaultMinValidity = 24 * time.Hour

// CertRenewalMinValidity is the force-reissue threshold used by the periodic
// renewal job: re-issue when less than 72h of validity remains.
const CertRenewalMinValidity = 72 * time.Hour

// Seams over crypto and CA calls so tests can inject failures.
var (
	generateClientKey = func() (*ecdsa.PrivateKey, error) {
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	issueCert = func(authority *ca.Authority, cn string, pub crypto.PublicKey, ttl time.Duration) ([]byte, error) {
		return authority.IssueCertificate(cn, pub, ttl)
	}
)

// LoadOrCreateClientCert returns the control-plane client certificate
// persisted in store, issuing and persisting a new one (CN=hive-control-plane,
// signed by authority, 72h TTL) when none exists or the stored one expires
// within DefaultMinValidity. A nil store yields an ephemeral certificate
// that is not persisted. The registration/bootstrap flow for agents is
// unaffected.
func LoadOrCreateClientCert(ctx context.Context, authority *ca.Authority, store ca.SecretStore) (tls.Certificate, error) {
	return LoadOrCreateClientCertWithMinValidity(ctx, authority, store, DefaultMinValidity)
}

// LoadOrCreateClientCertWithMinValidity is LoadOrCreateClientCert with an
// explicit force-reissue threshold: a stored certificate with less than
// minValidity of remaining life is re-issued and re-persisted.
func LoadOrCreateClientCertWithMinValidity(ctx context.Context, authority *ca.Authority, store ca.SecretStore, minValidity time.Duration) (tls.Certificate, error) {
	if store != nil {
		keyPEM, keyErr := store.Get(ctx, ClientKeyName, ClientKeyType)
		certPEM, certErr := store.Get(ctx, ClientCertName, ClientCertType)
		if keyErr == nil && certErr == nil {
			cert, err := tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				return tls.Certificate{}, fmt.Errorf("parse stored client keypair: %w", err)
			}
			leaf, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				return tls.Certificate{}, fmt.Errorf("parse stored client certificate: %w", err)
			}
			if time.Until(leaf.NotAfter) > minValidity {
				return cert, nil
			}
			// Expiring soon: fall through and re-issue.
		} else if !errors.Is(keyErr, pgx.ErrNoRows) || !errors.Is(certErr, pgx.ErrNoRows) {
			return tls.Certificate{}, fmt.Errorf("inconsistent client cert material in secrets store (key err: %v, cert err: %v)", keyErr, certErr)
		}
	}

	key, err := generateClientKey()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate client key: %w", err)
	}
	certPEM, err := issueCert(authority, clientCertCN, &key.PublicKey, clientCertTTL)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("issue client certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal client key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("build client keypair: %w", err)
	}

	if store != nil {
		if err := store.Put(ctx, ClientKeyName, ClientKeyType, keyPEM); err != nil {
			return tls.Certificate{}, fmt.Errorf("persist client key: %w", err)
		}
		if err := store.Put(ctx, ClientCertName, ClientCertType, certPEM); err != nil {
			return tls.Certificate{}, fmt.Errorf("persist client certificate: %w", err)
		}
	}
	return cert, nil
}
