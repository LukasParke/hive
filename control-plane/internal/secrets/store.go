package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/sha3"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool      *pgxpool.Pool
	masterKey []byte
}

func NewStore(pool *pgxpool.Pool, masterKey []byte) (*Store, error) {
	if len(masterKey) < 32 {
		return nil, errors.New("master key must be at least 32 bytes")
	}
	return &Store{pool: pool, masterKey: masterKey}, nil
}

func (s *Store) Put(ctx context.Context, name, typ string, plain []byte) error {
	key := s.deriveKey(typ)
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plain, nil)
	payload := append(nonce, ciphertext...)
	_, err = s.pool.Exec(ctx, `
		insert into secrets_store(name, type, encrypted_value, updated_at)
		values ($1, $2::secret_type, $3, now())
		on conflict (name)
		do update set encrypted_value = excluded.encrypted_value, updated_at = now()
	`, name, typ, payload)
	return err
}

func (s *Store) Get(ctx context.Context, name, typ string) ([]byte, error) {
	var payload []byte
	if err := s.pool.QueryRow(ctx, "select encrypted_value from secrets_store where name=$1 and type=$2::secret_type", name, typ).Scan(&payload); err != nil {
		return nil, err
	}
	key := s.deriveKey(typ)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
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
