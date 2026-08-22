package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// memStore is an in-memory SecretStore for tests.
type memStore struct {
	rows map[string][]byte
}

func newMemStore() *memStore {
	return &memStore{rows: make(map[string][]byte)}
}

func (m *memStore) key(name, typ string) string {
	return typ + "/" + name
}

func (m *memStore) Get(_ context.Context, name, typ string) ([]byte, error) {
	v, ok := m.rows[m.key(name, typ)]
	if !ok {
		return nil, fmt.Errorf("get %s: %w", name, pgx.ErrNoRows)
	}
	return v, nil
}

func (m *memStore) Put(_ context.Context, name, typ string, plain []byte) error {
	cp := make([]byte, len(plain))
	copy(cp, plain)
	m.rows[m.key(name, typ)] = cp
	return nil
}

func TestLoadOrCreateCreatesAndPersists(t *testing.T) {
	store := newMemStore()
	a, err := LoadOrCreate(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if a.cert == nil || a.key == nil {
		t.Fatal("authority missing cert or key")
	}
	if !a.cert.IsCA {
		t.Error("generated certificate is not a CA")
	}
	if time.Until(a.cert.NotAfter) < 9*365*24*time.Hour {
		t.Errorf("CA should be valid ~10y, got %v", time.Until(a.cert.NotAfter))
	}
	if err := a.cert.CheckSignatureFrom(a.cert); err != nil {
		t.Errorf("CA not self-signed: %v", err)
	}
	if len(store.rows) != 2 {
		t.Errorf("expected CA key and cert persisted, got %d rows", len(store.rows))
	}
	if _, err := store.Get(context.Background(), CAKeyName, CAKeyType); err != nil {
		t.Errorf("CA key not persisted: %v", err)
	}
}

func TestLoadOrCreateDeterministicAcrossInstances(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	first, err := LoadOrCreate(ctx, store)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	second, err := LoadOrCreate(ctx, store)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	third, err := LoadOrCreate(ctx, store)
	if err != nil {
		t.Fatalf("third LoadOrCreate: %v", err)
	}

	if string(first.CertPEM()) != string(second.CertPEM()) {
		t.Error("second LoadOrCreate returned a different certificate")
	}
	if string(second.CertPEM()) != string(third.CertPEM()) {
		t.Error("third LoadOrCreate returned a different certificate")
	}
}

func TestLoadOrCreateRoundtripPreservesKey(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()

	first, err := LoadOrCreate(ctx, store)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	second, err := LoadOrCreate(ctx, store)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if first.key.D.Cmp(second.key.D) == 0 {
		return // same private key loaded
	}
	t.Error("second LoadOrCreate did not reload the persisted private key")
}

func TestLoadOrCreateNilStoreEphemeral(t *testing.T) {
	a, err := LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatalf("LoadOrCreate(nil): %v", err)
	}
	if a.cert == nil || a.key == nil {
		t.Fatal("ephemeral authority missing material")
	}
}

func TestLoadOrCreateInconsistentMaterial(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()
	if _, err := LoadOrCreate(ctx, store); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	delete(store.rows, CACertType+"/"+CACertName)

	if _, err := LoadOrCreate(ctx, store); err == nil {
		t.Error("expected error when only the CA key is present")
	}
}

func TestLoadOrCreateCorruptMaterial(t *testing.T) {
	store := newMemStore()
	ctx := context.Background()
	if err := store.Put(ctx, CAKeyName, CAKeyType, []byte("not pem")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, CACertName, CACertType, []byte("not pem")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(ctx, store); err == nil {
		t.Error("expected error for corrupt persisted CA material")
	}
}

func TestIssueCertificate(t *testing.T) {
	a, err := LoadOrCreate(context.Background(), newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := a.IssueCertificate("hive-control-plane", &leafKey.PublicKey, 72*time.Hour)
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block in issued certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "hive-control-plane" {
		t.Errorf("CN = %q, want hive-control-plane", cert.Subject.CommonName)
	}
	if !certHasEKU(cert, x509.ExtKeyUsageClientAuth) || !certHasEKU(cert, x509.ExtKeyUsageServerAuth) {
		t.Error("issued certificate missing client/server auth EKUs")
	}
	if err := cert.CheckSignatureFrom(a.cert); err != nil {
		t.Errorf("certificate not signed by CA: %v", err)
	}
}

func TestSignAgentCSRRetainsBehavior(t *testing.T) {
	a, err := LoadOrCreate(context.Background(), newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.CertificateRequest{PublicKey: &key.PublicKey}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tpl, key)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	certPEM, err := a.SignAgentCSR(csr, "node-1", time.Hour)
	if err != nil {
		t.Fatalf("SignAgentCSR: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "agent-node-1" {
		t.Errorf("CN = %q, want agent-node-1", cert.Subject.CommonName)
	}
}

func certHasEKU(cert *x509.Certificate, eku x509.ExtKeyUsage) bool {
	for _, e := range cert.ExtKeyUsage {
		if e == eku {
			return true
		}
	}
	return false
}

// TestFromPEMBranches walks every parse failure and fallback branch of
// fromPEM by seeding crafted key/cert material into the store.
func TestFromPEMBranches(t *testing.T) {
	ctx := context.Background()

	validCertPEM := func(t *testing.T) []byte {
		t.Helper()
		a, err := LoadOrCreate(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return a.CertPEM()
	}

	// Valid key PEM but garbage certificate: no PEM block in cert.
	store := newMemStore()
	a, err := LoadOrCreate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, CAKeyName, CAKeyType, a.KeyPEM()); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, CACertName, CACertType, []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(ctx, store); err == nil || !strings.Contains(err.Error(), "no PEM block in CA certificate") {
		t.Errorf("cert without PEM block: want specific error, got %v", err)
	}

	// Certificate PEM block present but DER invalid.
	if err := store.Put(ctx, CACertName, CACertType, pemBlockFor("CERTIFICATE", []byte{0xde, 0xad})); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(ctx, store); err == nil || !strings.Contains(err.Error(), "parse CA certificate") {
		t.Errorf("corrupt cert DER: want parse error, got %v", err)
	}

	// Key PEM block present but neither PKCS#8 nor SEC1 parses.
	if err := store.Put(ctx, CAKeyName, CAKeyType, pemBlockFor("PRIVATE KEY", []byte{1, 2, 3})); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, CACertName, CACertType, validCertPEM(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(ctx, store); err == nil || !strings.Contains(err.Error(), "parse CA key") {
		t.Errorf("corrupt key DER: want parse error, got %v", err)
	}

	// PKCS#8 fails but SEC1 EC parsing succeeds: legacy EC key must load.
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecDER, err := x509.MarshalECPrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, CAKeyName, CAKeyType, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: ecDER})); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadOrCreate(ctx, store)
	if err != nil {
		t.Fatalf("SEC1 EC key fallback: %v", err)
	}
	if loaded.key.D.Cmp(ecKey.D) != 0 {
		t.Error("SEC1 fallback did not preserve the private key")
	}

	// Parsed key is not ECDSA (RSA): rejected.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaDER, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, CAKeyName, CAKeyType, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rsaDER})); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(ctx, store); err == nil || !strings.Contains(err.Error(), "not ECDSA") {
		t.Errorf("RSA CA key: want rejection, got %v", err)
	}

	// Key with no PEM block at all.
	if err := store.Put(ctx, CAKeyName, CAKeyType, []byte("still not pem")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(ctx, store); err == nil || !strings.Contains(err.Error(), "no PEM block in CA key") {
		t.Errorf("key without PEM block: want specific error, got %v", err)
	}
}

func pemBlockFor(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

// TestIssueCertificateExpiryMath proves the leaf validity window is exactly
// [now-1m, now+ttl] within tolerance.
func TestIssueCertificateExpiryMath(t *testing.T) {
	a, err := LoadOrCreate(context.Background(), newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ttl := 72 * time.Hour
	before := time.Now()
	certPEM, err := a.IssueCertificate("expiry-check", &key.PublicKey, ttl)
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now()

	// x509 marshals times with whole-second precision, so allow up to one
	// second of truncation on both bounds.
	const secSlack = time.Second
	if cert.NotBefore.Before(before.Add(-time.Minute - secSlack)) {
		t.Errorf("NotBefore = %v, want >= %v", cert.NotBefore, before.Add(-time.Minute))
	}
	wantNotAfter := after.Add(ttl)
	if cert.NotAfter.After(wantNotAfter) || cert.NotAfter.Before(before.Add(ttl-secSlack)) {
		t.Errorf("NotAfter = %v, want in [%v, %v]", cert.NotAfter, before.Add(ttl), wantNotAfter)
	}
}

// TestSignAgentCSRRejectsTamperedCSR proves the signature check runs.
func TestSignAgentCSRRejectsTamperedCSR(t *testing.T) {
	a, err := LoadOrCreate(context.Background(), newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		PublicKey: &key.PublicKey,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrDER[len(csrDER)-1] ^= 0xff // corrupt the signature bytes
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignAgentCSR(csr, "node-1", time.Hour); err == nil {
		t.Error("tampered CSR accepted")
	}
}

// failPutStore fails on the Nth Put (0-based) to exercise persistence error
// branches one at a time.
type failPutStore struct {
	memStore
	failOn int
	calls  int
}

func (f *failPutStore) Put(ctx context.Context, name, typ string, plain []byte) error {
	if f.calls == f.failOn {
		f.calls++
		return errors.New("store offline")
	}
	f.calls++
	return f.memStore.Put(ctx, name, typ, plain)
}

// TestLoadOrCreatePersistenceFailures proves a failed secrets write aborts
// creation instead of silently running unpersisted.
func TestLoadOrCreatePersistenceFailures(t *testing.T) {
	ctx := context.Background()
	for _, failOn := range []int{0, 1} {
		store := &failPutStore{memStore: memStore{rows: map[string][]byte{}}, failOn: failOn}
		_, err := LoadOrCreate(ctx, store)
		if err == nil || !strings.Contains(err.Error(), "persist CA ") {
			t.Errorf("failOn=%d: want persist error, got %v", failOn, err)
		}
	}
}

// TestRandomnessFailureBranches injects key/serial failures to cover the
// defensive error paths of CA issuance.
func TestRandomnessFailureBranches(t *testing.T) {
	ctx := context.Background()

	origKey, origInt := generateKey, randomInt
	defer func() { generateKey, randomInt = origKey, origInt }()

	generateKey = func() (*ecdsa.PrivateKey, error) { return nil, errors.New("entropy starved") }
	if _, err := LoadOrCreate(ctx, newMemStore()); err == nil || !strings.Contains(err.Error(), "generate CA key") {
		t.Errorf("want key-generation error, got %v", err)
	}
	generateKey = origKey

	randomInt = func(*big.Int) (*big.Int, error) { return nil, errors.New("no entropy") }
	if _, err := LoadOrCreate(ctx, newMemStore()); err == nil || !strings.Contains(err.Error(), "generate CA serial") {
		t.Errorf("want serial error, got %v", err)
	}
	randomInt = origInt
	a, err := LoadOrCreate(ctx, newMemStore())
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	randomInt = func(*big.Int) (*big.Int, error) { return nil, errors.New("no entropy") }
	if _, err := a.IssueCertificate("x", &key.PublicKey, time.Hour); err == nil || !strings.Contains(err.Error(), "generate serial") {
		t.Errorf("want leaf serial error, got %v", err)
	}

	// An unsupported leaf public key type is rejected by x509 during
	// signing.
	randomInt = origInt
	ok, err := LoadOrCreate(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	type unsupportedKey struct{}
	if _, err := ok.IssueCertificate("x", unsupportedKey{}, time.Hour); err == nil || !strings.Contains(err.Error(), "sign certificate") {
		t.Errorf("want sign error for unsupported key, got %v", err)
	}
}

// TestLoadOrCreateSelfSignFailure injects a self-sign failure to cover the
// defensive error path between key generation and persistence.
func TestLoadOrCreateSelfSignFailure(t *testing.T) {
	orig := createCert
	createCert = func(rand io.Reader, template, parent *x509.Certificate, pub, priv any) ([]byte, error) {
		return nil, errors.New("self-sign failed")
	}
	defer func() { createCert = orig }()

	if _, err := LoadOrCreate(context.Background(), newMemStore()); err == nil {
		t.Fatal("expected error when self-signing fails")
	} else if !strings.Contains(err.Error(), "self-sign CA certificate") {
		t.Fatalf("error = %v, want self-sign CA certificate wrap", err)
	}
}

// TestLoadOrCreateParseFailure feeds junk DER through a successful self-sign
// so the certificate-parse error branch runs.
func TestLoadOrCreateParseFailure(t *testing.T) {
	orig := createCert
	createCert = func(rand io.Reader, template, parent *x509.Certificate, pub, priv any) ([]byte, error) {
		return []byte{0x30, 0x01, 0x00}, nil // well-formed SEQUENCE header, empty body
	}
	defer func() { createCert = orig }()

	if _, err := LoadOrCreate(context.Background(), newMemStore()); err == nil {
		t.Fatal("expected error when parsing the signed certificate fails")
	} else if !strings.Contains(err.Error(), "parse CA certificate") {
		t.Fatalf("error = %v, want parse CA certificate wrap", err)
	}
}
