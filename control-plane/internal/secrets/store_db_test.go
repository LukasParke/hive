package secrets

import (
	"context"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/luke/hive/control-plane/internal/testdb"
)

var masterKey = []byte("0123456789abcdef0123456789abcdef")

// TestPutGetRoundtripPersistsEncrypted proves a secret survives a Put/Get
// cycle against the real secrets_store table, is decryptable only with the
// same master key + type context, and that the stored bytes are ciphertext
// rather than plaintext.
func TestPutGetRoundtripPersistsEncrypted(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "secrets_store")
	ctx := context.Background()

	store, err := NewStore(pool, masterKey)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	plain := []byte("super-secret-material")
	if err := store.Put(ctx, "roundtrip-key", "tls_cert", plain); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(ctx, "roundtrip-key", "tls_cert")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("round-trip = %q, want %q", got, plain)
	}

	var raw []byte
	if err := pool.QueryRow(ctx, "select encrypted_value from secrets_store where name='roundtrip-key'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) == string(plain) {
		t.Fatal("secret persisted as plaintext")
	}

	// Upsert: same name replaces the value.
	if err := store.Put(ctx, "roundtrip-key", "tls_cert", []byte("rotated")); err != nil {
		t.Fatalf("Put rotate: %v", err)
	}
	got, err = store.Get(ctx, "roundtrip-key", "tls_cert")
	if err != nil || string(got) != "rotated" {
		t.Fatalf("rotated Get = %q, err %v; want rotated", got, err)
	}
}

func TestNewStoreRejectsShortMasterKey(t *testing.T) {
	if _, err := NewStore(nil, []byte("short")); err == nil {
		t.Error("NewStore accepted a short master key")
	}
	exactly32 := make([]byte, 32)
	if _, err := NewStore(nil, exactly32); err != nil {
		t.Errorf("NewStore rejected a 32-byte key: %v", err)
	}
}

func TestGetMissingAndWrongType(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "secrets_store")
	ctx := context.Background()
	store, err := NewStore(pool, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "nope", "tls_key"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("missing name: want pgx.ErrNoRows, got %v", err)
	}
	if err := store.Put(ctx, "typed", "ssh_key", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "typed", "signing_key"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("wrong type: want pgx.ErrNoRows, got %v", err)
	}
}

// TestGetTamperedAndShortPayloads seeds malformed ciphertext directly into the
// table so the decryption guards fire.
func TestGetTamperedAndShortPayloads(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "secrets_store")
	ctx := context.Background()
	store, err := NewStore(pool, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "victim", "tls_key", []byte("payload")); err != nil {
		t.Fatal(err)
	}

	// Flip a ciphertext byte: GCM authentication must fail.
	var raw []byte
	if err := pool.QueryRow(ctx, "select encrypted_value from secrets_store where name='victim'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, err := pool.Exec(ctx, "update secrets_store set encrypted_value=$1 where name='victim'", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "victim", "tls_key"); err == nil {
		t.Error("tampered ciphertext decrypted without error")
	}

	// Payload shorter than the nonce size.
	if _, err := pool.Exec(ctx, "update secrets_store set encrypted_value=$1 where name='victim'", []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "victim", "tls_key"); err == nil || err.Error() != "secret payload is invalid" {
		t.Errorf("short payload: want invalid-payload error, got %v", err)
	}
}

func TestMaterializeToFile(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "secrets_store")
	ctx := context.Background()
	store, err := NewStore(pool, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if err := store.Put(ctx, "materialize-me", "ca_key", []byte("key-bytes")); err != nil {
		t.Fatal(err)
	}

	path, err := store.MaterializeToFile(ctx, "materialize-me", "ca_key", outDir)
	if err != nil {
		t.Fatalf("MaterializeToFile: %v", err)
	}
	b, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "key-bytes" {
		t.Errorf("file content = %q, want key-bytes", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}

	// Missing secret surfaces the Get error.
	if _, err := store.MaterializeToFile(ctx, "absent", "ca_key", outDir); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("absent secret: want pgx.ErrNoRows, got %v", err)
	}
}

// TestValueStoreEdges covers open/seal failure branches that need crafted
// payloads: invalid base64 and a well-formed prefix with a too-short body.
func TestValueStoreEdges(t *testing.T) {
	store, err := NewValueStore(masterKey)
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := store.seal("ssh_key", []byte("data"))
	if err != nil {
		t.Fatal(err)
	}

	// Invalid base64 after the prefix.
	if _, err := store.open("ssh_key", EncryptedPrefix+"!!!not-base64!!!"); err == nil || !strings.Contains(err.Error(), "decode sealed value") {
		t.Errorf("invalid base64: want decode error, got %v", err)
	}

	// Valid base64 but shorter than the nonce.
	short := EncryptedPrefix + base64.StdEncoding.EncodeToString([]byte{1})
	if _, err := store.open("ssh_key", short); err == nil || !strings.Contains(err.Error(), "sealed value is invalid") {
		t.Errorf("short sealed payload: want invalid error, got %v", err)
	}

	// Wrong context cannot recover the plaintext.
	if wrong, err := store.open("signing_key", sealed); err == nil && string(wrong) == "data" {
		t.Error("value opened under the wrong context")
	}
}

// TestMaterializeToFileWriteError covers the write-failure branch: outDir is
// a regular file, so creating the target file must fail.
func TestMaterializeToFileWriteError(t *testing.T) {
	pool := testdb.Get(t)
	testdb.Truncate(t, "secrets_store")
	ctx := context.Background()
	store, err := NewStore(pool, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "blocked", "tls_key", []byte("v")); err != nil {
		t.Fatal(err)
	}
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MaterializeToFile(ctx, "blocked", "tls_key", notADir); err == nil {
		t.Error("expected write failure when outDir is a file")
	}
}

// TestOpenPlaintextPassthrough covers open's legacy branch directly.
func TestOpenPlaintextPassthrough(t *testing.T) {
	store, err := NewValueStore(masterKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.open("ssh_key", "legacy-plaintext")
	if err != nil || string(got) != "legacy-plaintext" {
		t.Fatalf("open(plaintext) = %q, %v; want verbatim", got, err)
	}
}

// TestAEADConstructionFailureInjected swaps the AEAD builder to prove every
// call site propagates construction errors instead of panicking.
func TestAEADConstructionFailureInjected(t *testing.T) {
	testdb.Get(t)
	testdb.Truncate(t, "secrets_store")
	ctx := context.Background()

	orig := newGCM
	newGCM = func([]byte) (cipher.AEAD, error) { return nil, errors.New("no aes-ni") }
	defer func() { newGCM = orig }()

	store, err := NewStore(testdb.Get(t), masterKey)
	if err != nil {
		t.Fatal(err)
	}
	testdb.Truncate(t, "secrets_store")
	// Seed a row directly so Get reaches decryption with a broken builder.
	if _, err := store.pool.Exec(ctx,
		`insert into secrets_store(name, type, encrypted_value) values ('k', 'tls_key', $1)`,
		[]byte{0, 1, 2}); err != nil {
		t.Fatal(err)
	}

	vs, err := NewValueStore(masterKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "k2", "tls_key", []byte("v")); err == nil {
		t.Error("Put did not surface AEAD failure")
	}
	if _, err := store.Get(ctx, "k", "tls_key"); err == nil {
		t.Error("Get did not surface AEAD failure")
	}
	if _, err := vs.seal("ssh_key", []byte("v")); err == nil {
		t.Error("seal did not surface AEAD failure")
	}
	if _, err := vs.open("ssh_key", EncryptedPrefix+"AAAA"); err == nil {
		t.Error("open did not surface AEAD failure")
	}
}
