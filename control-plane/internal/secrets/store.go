package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store encrypts secrets at rest with AES-GCM keys derived from a master key.
type Store struct {
	pool      *pgxpool.Pool
	masterKey []byte
}

// NewStore returns a Store; the master key must be at least 32 bytes.
func NewStore(pool *pgxpool.Pool, masterKey []byte) (*Store, error) {
	if len(masterKey) < 32 {
		return nil, errors.New("master key must be at least 32 bytes")
	}
	return &Store{pool: pool, masterKey: masterKey}, nil
}

// runtime holds the process-wide store, set once during main initialization.
var runtime *Store

// SetRuntime installs the process-wide secrets store. Call once during
// startup; later calls are ignored.
func SetRuntime(s *Store) {
	if runtime == nil {
		runtime = s
	}
}

// Runtime returns the process-wide secrets store, or nil when no master key
// was configured. Callers must handle nil (no encryption at rest available).
func Runtime() *Store {
	return runtime
}

// newGCM derives the AES-GCM AEAD for a 32-byte key. A package variable so
// tests can inject construction failures.
var newGCM = func(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// aead builds the context-derived AEAD for this store.
func (s *Store) aead(contextType string) (cipher.AEAD, error) {
	return newGCM(s.deriveKey(contextType))
}

// Put encrypts plain and upserts it under the given name and type.
func (s *Store) Put(ctx context.Context, name, typ string, plain []byte) error {
	aead, err := s.aead(typ)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plain, nil)
	payload := slices.Concat(nonce, ciphertext)
	_, err = s.pool.Exec(ctx, `
		insert into secrets_store(name, type, encrypted_value, updated_at)
		values ($1, $2::secret_type, $3, now())
		on conflict (name)
		do update set encrypted_value = excluded.encrypted_value, updated_at = now()
	`, name, typ, payload)
	return err
}

// Get fetches and decrypts the secret stored under the given name and type.
func (s *Store) Get(ctx context.Context, name, typ string) ([]byte, error) {
	var payload []byte
	if err := s.pool.QueryRow(ctx, "select encrypted_value from secrets_store where name=$1 and type=$2::secret_type", name, typ).Scan(&payload); err != nil {
		return nil, err
	}
	aead, err := s.aead(typ)
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize() {
		return nil, errors.New("secret payload is invalid")
	}
	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}

// MaterializeToFile decrypts a secret and writes it to a 0o600 file in outDir,
// returning the file path.
func (s *Store) MaterializeToFile(ctx context.Context, name, typ, outDir string) (string, error) {
	plain, err := s.Get(ctx, name, typ)
	if err != nil {
		return "", err
	}
	file := filepath.Join(outDir, base64.RawURLEncoding.EncodeToString([]byte(name)))
	if err := os.WriteFile(file, plain, 0o600); err != nil {
		return "", err
	}
	return file, nil
}

func (s *Store) deriveKey(contextType string) []byte {
	h := hkdf.New(sha3.New256, s.masterKey, nil, []byte("hive:"+contextType))
	key := make([]byte, 32)
	_, _ = io.ReadFull(h, key)
	return key
}

// EncryptedPrefix marks values produced by SealValue; exported for tests
// and readers that need to distinguish sealed values from plaintext.
const EncryptedPrefix = encryptedPrefix

// encryptedPrefix marks values produced by SealValue. Values without the
// prefix are treated as legacy plaintext by OpenValue.
const encryptedPrefix = "enc1:"

// NewValueStore returns a store that can only seal and open values (no
// secrets_store persistence). It is used to encrypt sensitive columns and
// in tests; use NewStore when persistence is needed.
func NewValueStore(masterKey []byte) (*Store, error) {
	if len(masterKey) < 32 {
		return nil, errors.New("master key must be at least 32 bytes")
	}
	return &Store{masterKey: masterKey}, nil
}

// seal encrypts plain under the given context type, returning a prefixed,
// base64-encoded nonce+ciphertext blob safe to store in a text column.
func (s *Store) seal(contextType string, plain []byte) (string, error) {
	aead, err := s.aead(contextType)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, plain, nil)
	return encryptedPrefix + base64.StdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

// open reverses seal. Values without the encryptedPrefix are returned
// unchanged so plaintext rows written before encryption keep working.
func (s *Store) open(contextType string, sealed string) ([]byte, error) {
	if !strings.HasPrefix(sealed, encryptedPrefix) {
		return []byte(sealed), nil
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, encryptedPrefix))
	if err != nil {
		return nil, fmt.Errorf("decode sealed value: %w", err)
	}
	aead, err := s.aead(contextType)
	if err != nil {
		return nil, err
	}
	if len(payload) < aead.NonceSize() {
		return nil, errors.New("sealed value is invalid")
	}
	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("open sealed value: %w", err)
	}
	return plain, nil
}

// SealValue encrypts plain with the process-wide runtime store. When no
// master key is configured (Runtime() is nil) the value is returned
// unchanged so callers can store it as-is.
func SealValue(contextType string, plain []byte) (string, error) {
	store := Runtime()
	if store == nil {
		return string(plain), nil
	}
	return store.seal(contextType, plain)
}

// OpenValue reverses SealValue. Unmarked values are returned unchanged;
// marked values require the same master key that sealed them.
func OpenValue(contextType string, sealed string) ([]byte, error) {
	store := Runtime()
	if store == nil || !strings.HasPrefix(sealed, encryptedPrefix) {
		return []byte(sealed), nil
	}
	return store.open(contextType, sealed)
}
