package agentclient

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/luke/hive/control-plane/internal/ca"
)

// memStore is an in-memory ca.SecretStore for tests.
type memStore struct {
	rows map[string][]byte
}

func newMemStore() *memStore {
	return &memStore{rows: make(map[string][]byte)}
}

func (m *memStore) Get(_ context.Context, name, typ string) ([]byte, error) {
	v, ok := m.rows[typ+"/"+name]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return v, nil
}

func (m *memStore) Put(_ context.Context, name, typ string, plain []byte) error {
	cp := make([]byte, len(plain))
	copy(cp, plain)
	m.rows[typ+"/"+name] = cp
	return nil
}

// newTestAuthority creates a CA without touching any store.
func newTestAuthority(t *testing.T) *ca.Authority {
	t.Helper()
	a, err := ca.LoadOrCreate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func storedCertCN(t *testing.T, certPEM []byte) string {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("no PEM block in stored certificate")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c.Subject.CommonName
}

func TestLoadOrCreateClientCertIssuesAndPersists(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()

	cert, err := LoadOrCreateClientCert(ctx, authority, store)
	if err != nil {
		t.Fatalf("LoadOrCreateClientCert: %v", err)
	}
	if len(store.rows) != 2 {
		t.Errorf("expected key and cert persisted, got %d rows", len(store.rows))
	}
	storedCert, err := store.Get(ctx, ClientCertName, ClientCertType)
	if err != nil {
		t.Fatal(err)
	}
	if cn := storedCertCN(t, storedCert); cn != "hive-control-plane" {
		t.Errorf("stored cert CN = %q, want hive-control-plane", cn)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if ttl := time.Until(leaf.NotAfter); ttl > 72*time.Hour || ttl < 71*time.Hour {
		t.Errorf("cert TTL should be ~72h, got %v", ttl)
	}
}

func TestLoadOrCreateClientCertIdempotent(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()

	first, err := LoadOrCreateClientCert(ctx, authority, store)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateClientCert(ctx, authority, store)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Certificate[0]) != string(second.Certificate[0]) {
		t.Error("expected the same certificate to be loaded on second call")
	}
}

func TestLoadOrCreateClientCertReissuesNearExpiry(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()

	// Seed a certificate that is about to expire.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldPEM, err := authority.IssueCertificate("hive-control-plane", &key.PublicKey, DefaultMinValidity-time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := store.Put(ctx, ClientKeyName, ClientKeyType, keyPEM); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, ClientCertName, ClientCertType, oldPEM); err != nil {
		t.Fatal(err)
	}

	renewed, err := LoadOrCreateClientCert(ctx, authority, store)
	if err != nil {
		t.Fatalf("LoadOrCreateClientCert near expiry: %v", err)
	}
	leaf, err := x509.ParseCertificate(renewed.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(leaf.NotAfter) < DefaultMinValidity {
		t.Errorf("renewed certificate should have a fresh TTL, got %v", time.Until(leaf.NotAfter))
	}
	persisted, err := store.Get(ctx, ClientCertName, ClientCertType)
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) == string(oldPEM) {
		t.Error("renewed certificate was not persisted")
	}
}

func TestLoadOrCreateClientCertInconsistentMaterial(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := newMemStore()
	if err := store.Put(ctx, ClientKeyName, ClientKeyType, []byte("orphan")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateClientCert(ctx, authority, store); err == nil {
		t.Error("expected error when only the client key is present")
	}
}

// failPutStore fails every Put to exercise the persistence error branches.
type failPutStore struct {
	memStore
}

func (f *failPutStore) Put(context.Context, string, string, []byte) error {
	return errors.New("store offline")
}

// TestLoadOrCreateClientCertPersistFailures proves issuance aborts with a
// wrapped error when the secrets store cannot persist the fresh material.
func TestLoadOrCreateClientCertPersistFailures(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	if _, err := LoadOrCreateClientCert(ctx, authority, &failPutStore{memStore: memStore{rows: map[string][]byte{}}}); err == nil || !strings.Contains(err.Error(), "persist client key") {
		t.Errorf("want persist-key error, got %v", err)
	}
}

// TestLoadOrCreateClientCertPersistCertFailure fails the SECOND Put (the key
// succeeds) to cover the persist-certificate error branch.
func TestLoadOrCreateClientCertPersistCertFailure(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)
	store := &countingFailStore{memStore: memStore{rows: map[string][]byte{}}, failOn: 1}
	if _, err := LoadOrCreateClientCert(ctx, authority, store); err == nil || !strings.Contains(err.Error(), "persist client certificate") {
		t.Errorf("want persist-certificate error, got %v", err)
	}
}

type countingFailStore struct {
	memStore
	failOn int
	calls  int
}

func (f *countingFailStore) Put(ctx context.Context, name, typ string, plain []byte) error {
	defer func() { f.calls++ }()
	if f.calls == f.failOn {
		return errors.New("store offline")
	}
	return f.memStore.Put(ctx, name, typ, plain)
}

// TestLoadOrCreateClientCertInjectedFailures covers the defensive issuance
// branches via seams.
func TestLoadOrCreateClientCertInjectedFailures(t *testing.T) {
	ctx := context.Background()
	authority := newTestAuthority(t)

	origKey, origIssue := generateClientKey, issueCert
	defer func() { generateClientKey, issueCert = origKey, origIssue }()

	generateClientKey = func() (*ecdsa.PrivateKey, error) { return nil, errors.New("entropy starved") }
	if _, err := LoadOrCreateClientCert(ctx, authority, nil); err == nil || !strings.Contains(err.Error(), "generate client key") {
		t.Errorf("want key-generation error, got %v", err)
	}

	generateClientKey = origKey
	issueCert = func(*ca.Authority, string, crypto.PublicKey, time.Duration) ([]byte, error) {
		return nil, errors.New("signer offline")
	}
	if _, err := LoadOrCreateClientCert(ctx, authority, nil); err == nil || !strings.Contains(err.Error(), "issue client certificate") {
		t.Errorf("want issue error, got %v", err)
	}
}
