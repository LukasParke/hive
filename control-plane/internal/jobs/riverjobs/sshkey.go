package riverjobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/secrets"
)

// materializeAppSSHKey resolves the application's linked SSH key
// (applications.ssh_key_id) to a temporary private-key file (mode 0600)
// inside a fresh temp directory. Returns "" when the application has no key
// linked; an error when a key is linked but cannot be materialized. The
// caller owns removal of the returned file's parent directory.
func materializeAppSSHKey(ctx context.Context, pool *pgxpool.Pool, appID string) (string, error) {
	keyName, privateKey, err := lookupAppSSHKey(ctx, pool, appID)
	if err != nil {
		return "", err
	}
	if keyName == "" {
		return "", nil
	}
	return writeSSHKeyFile(ctx, keyName, privateKey)
}

// lookupAppSSHKey fetches the linked key. A missing link (no row) is not an
// error: it returns empty strings.
func lookupAppSSHKey(ctx context.Context, pool *pgxpool.Pool, appID string) (string, string, error) {
	appUUID, parseErr := uuid.Parse(appID)
	if parseErr != nil {
		return "", "", fmt.Errorf("invalid application id %q: %w", appID, parseErr)
	}
	q := dbgen.New(pool)
	key, err := q.GetApplicationSSHKey(ctx, pgtype.UUID{Bytes: appUUID, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load ssh key for app %s: %w", appID, err)
	}
	// Column values written by the control-plane are sealed with the
	// runtime secrets store; legacy plaintext rows pass through unchanged.
	if plain, openErr := secrets.OpenValue("ssh_key", key.PrivateKey); openErr == nil {
		key.PrivateKey = string(plain)
	}
	return key.Name, key.PrivateKey, nil
}

// writeSSHKeyFile prefers the encrypted-at-rest copy from the secrets store;
// it falls back to the ssh_keys column value for keys provisioned before the
// store existed.
func writeSSHKeyFile(ctx context.Context, keyName, privateKey string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "hive-sshkey-*")
	if err != nil {
		return "", fmt.Errorf("create ssh key temp dir: %w", err)
	}

	store := secrets.Runtime()
	if store != nil {
		if plain, getErr := store.Get(ctx, keyName, "ssh_key"); getErr == nil && len(plain) > 0 {
			path, matErr := store.MaterializeToFile(ctx, keyName, "ssh_key", tmpDir)
			if matErr != nil {
				_ = os.RemoveAll(tmpDir)
				return "", fmt.Errorf("materialize ssh key %q: %w", keyName, matErr)
			}
			_ = os.Chmod(path, 0o600)
			return path, nil
		}
	}

	if privateKey == "" {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("ssh key %q has no private key material", keyName)
	}
	path := filepath.Join(tmpDir, "id_key")
	if err := os.WriteFile(path, []byte(privateKey), 0o600); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("write ssh key %q: %w", keyName, err)
	}
	return path, nil
}
