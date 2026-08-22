package agentclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/luke/hive/control-plane/internal/ca"
)

// storedLeafNotAfter parses the NotAfter of the certificate currently
// persisted in store.
func storedLeafNotAfter(t *testing.T, store *memStore) time.Time {
	t.Helper()
	certPEM, err := store.Get(context.Background(), ClientCertName, ClientCertType)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block in stored certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return leaf.NotAfter
}

func issueAndStore(t *testing.T, authority *ca.Authority, store *memStore, ttl time.Duration) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := authority.IssueCertificate(clientCertCN, &key.PublicKey, ttl)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	ctx := context.Background()
	if err := store.Put(ctx, ClientKeyName, ClientKeyType, keyPEM); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, ClientCertName, ClientCertType, certPEM); err != nil {
		t.Fatal(err)
	}
	return certPEM
}

// TestRenewControlPlaneCertReissuesBelowThreshold proves the renewer forces
// a re-issue when the persisted certificate has less than 72h of validity
// remaining (which, given the 72h TTL, is always) and persists the new one.
func TestRenewControlPlaneCertReissuesBelowThreshold(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()

	oldPEM := issueAndStore(t, authority, store, CertRenewalMinValidity-time.Hour)

	renewer := &ControlPlaneCertRenewer{Authority: authority, Store: store}
	if err := renewer.RenewControlPlaneCert(ctx); err != nil {
		t.Fatalf("RenewControlPlaneCert: %v", err)
	}

	persisted, err := store.Get(ctx, ClientCertName, ClientCertType)
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) == string(oldPEM) {
		t.Error("certificate below validity threshold was not re-issued")
	}
	if notAfter := storedLeafNotAfter(t, store); time.Until(notAfter) < clientCertTTL-time.Minute {
		t.Errorf("renewed certificate should have fresh %v TTL, got %v", clientCertTTL, time.Until(notAfter))
	}
}

// TestLoadOrCreateClientCertWithMinValidityKeepsFreshCert proves a large
// minValidity threshold does not touch a certificate that still has plenty
// of validity left (e.g. one issued with a longer TTL).
func TestLoadOrCreateClientCertWithMinValidityKeepsFreshCert(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()

	longTTL := 30 * 24 * time.Hour
	oldPEM := issueAndStore(t, authority, store, longTTL)

	if _, err := LoadOrCreateClientCertWithMinValidity(ctx, authority, store, CertRenewalMinValidity); err != nil {
		t.Fatalf("LoadOrCreateClientCertWithMinValidity: %v", err)
	}

	persisted, err := store.Get(ctx, ClientCertName, ClientCertType)
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) != string(oldPEM) {
		t.Error("fresh certificate was re-issued despite sufficient validity")
	}
}

// TestRenewControlPlaneCertRequiresAuthority proves the nil-authority guard.
func TestRenewControlPlaneCertRequiresAuthority(t *testing.T) {
	renewer := &ControlPlaneCertRenewer{Store: newMemStore()}
	if err := renewer.RenewControlPlaneCert(context.Background()); err == nil {
		t.Error("expected error without an authority")
	}
}

// TestRenewControlPlaneCertPropagatesStoreErrors proves a renewal against an
// inconsistent store fails instead of silently succeeding.
func TestRenewControlPlaneCertPropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()
	if err := store.Put(ctx, ClientKeyName, ClientKeyType, []byte("orphan")); err != nil {
		t.Fatal(err)
	}
	renewer := &ControlPlaneCertRenewer{Authority: authority, Store: store}
	err := renewer.RenewControlPlaneCert(ctx)
	if err == nil || !strings.Contains(err.Error(), "cert renewal") {
		t.Errorf("want wrapped renewal error, got %v", err)
	}
}

// TestLoadOrCreateClientCertBoundaryWindow pins the reissue decision at the
// DefaultMinValidity window: remaining validity just below forces re-issue,
// just above keeps the stored certificate.
func TestLoadOrCreateClientCertBoundaryWindow(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)

	for _, tc := range []struct {
		name     string
		ttl      time.Duration
		minValid time.Duration
		reissue  bool
	}{
		{"just below window reissues", DefaultMinValidity - 30*time.Second, DefaultMinValidity, true},
		{"just above window kept", DefaultMinValidity + time.Minute, DefaultMinValidity, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemStore()
			oldPEM := issueAndStore(t, authority, store, tc.ttl)

			cert, err := LoadOrCreateClientCertWithMinValidity(ctx, authority, store, tc.minValid)
			if err != nil {
				t.Fatalf("LoadOrCreateClientCertWithMinValidity: %v", err)
			}
			persisted, err := store.Get(ctx, ClientCertName, ClientCertType)
			if err != nil {
				t.Fatal(err)
			}
			wasReissued := string(persisted) != string(oldPEM)
			if wasReissued != tc.reissue {
				t.Fatalf("reissued = %v, want %v", wasReissued, tc.reissue)
			}
			leaf, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				t.Fatal(err)
			}
			if !tc.reissue && leaf.NotAfter.Equal(time.Time{}) {
				t.Error("kept certificate has no expiry")
			}
		})
	}
}

// TestLoadOrCreateClientCertCorruptPair proves stored material that parses as
// rows but not as a keypair surfaces an error rather than silent re-issue.
func TestLoadOrCreateClientCertCorruptPair(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)

	// Garbage PEM for both rows.
	store := newMemStore()
	if err := store.Put(ctx, ClientKeyName, ClientKeyType, []byte("junk")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, ClientCertName, ClientCertType, []byte("junk")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateClientCert(ctx, authority, store); err == nil || !strings.Contains(err.Error(), "parse stored client keypair") {
		t.Errorf("garbage pair: want parse error, got %v", err)
	}

	// Valid PEMs but mismatched key/cert pair.
	mismatched := newMemStore()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := authority.IssueCertificate(clientCertCN, &key.PublicKey, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherDER, err := x509.MarshalECPrivateKey(otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := mismatched.Put(ctx, ClientKeyName, ClientKeyType, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: otherDER})); err != nil {
		t.Fatal(err)
	}
	if err := mismatched.Put(ctx, ClientCertName, ClientCertType, certPEM); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateClientCert(ctx, authority, mismatched); err == nil || !strings.Contains(err.Error(), "parse stored client keypair") {
		t.Errorf("mismatched pair: want parse error, got %v", err)
	}

	// Corrupt certificate DER inside a well-formed PEM: tls.X509KeyPair
	// rejects it while parsing the pair.
	badDER := newMemStore()
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := badDER.Put(ctx, ClientKeyName, ClientKeyType, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})); err != nil {
		t.Fatal(err)
	}
	if err := badDER.Put(ctx, ClientCertName, ClientCertType, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{0x00}})); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateClientCert(ctx, authority, badDER); err == nil || !strings.Contains(err.Error(), "parse stored client keypair") {
		t.Errorf("bad cert DER: want keypair parse error, got %v", err)
	}
}
