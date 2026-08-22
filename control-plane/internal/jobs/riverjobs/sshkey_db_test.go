package riverjobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luke/hive/control-plane/internal/secrets"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// linkSSHKey inserts an ssh_keys row and points the application at it.
func linkSSHKey(t *testing.T, appID, name, privateKey string) {
	t.Helper()
	p := testdb.Get(t)
	if _, err := p.Exec(context.Background(), `
		with k as (
			insert into ssh_keys(name, public_key, private_key)
			values ($2, 'ssh-ed25519 AAAATEST', $3)
			returning id
		)
		update applications a set ssh_key_id = k.id from k where a.id = $1::uuid
	`, appID, name, privateKey); err != nil {
		t.Fatalf("linkSSHKey: %v", err)
	}
}

func TestMaterializeAppSSHKeyInvalidAppID(t *testing.T) {
	pool := testdb.Get(t)
	_, err := materializeAppSSHKey(context.Background(), pool, "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "invalid application id") {
		t.Fatalf("err = %v, want invalid application id", err)
	}
}

func TestMaterializeAppSSHKeyNoKeyLinked(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "nokey", "", nil)

	path, err := materializeAppSSHKey(context.Background(), pool, appID)
	if err != nil {
		t.Fatalf("materializeAppSSHKey: %v", err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty when no key is linked", path)
	}
}

func TestMaterializeAppSSHKeyPlaintextColumn(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "plainkey", "", nil)
	linkSSHKey(t, appID, "legacy-key", "PLAINMATERIAL")

	path, err := materializeAppSSHKey(context.Background(), pool, appID)
	if err != nil {
		t.Fatalf("materializeAppSSHKey: %v", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if string(data) != "PLAINMATERIAL" {
		t.Fatalf("key content = %q, want PLAINMATERIAL", data)
	}
}

func TestMaterializeAppSSHKeyFromSecretsStore(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "sealedkey", "", nil)

	store, err := secrets.NewStore(pool, []byte("0123456789abcdef0123456789abcdef0"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	secrets.SetRuntime(store) // sticky for this binary; plaintext column values still pass through
	if err := store.Put(context.Background(), "sealed-key", "ssh_key", []byte("SEALED-MATERIAL")); err != nil {
		t.Fatalf("store.Put: %v", err)
	}
	// The column copy is absent: materialization must prefer the store.
	linkSSHKey(t, appID, "sealed-key", "")

	path, err := materializeAppSSHKey(context.Background(), pool, appID)
	if err != nil {
		t.Fatalf("materializeAppSSHKey: %v", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if string(data) != "SEALED-MATERIAL" {
		t.Fatalf("key content = %q, want SEALED-MATERIAL", data)
	}
}

func TestMaterializeAppSSHKeyNoMaterial(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "ghostkey", "", nil)
	// Linked key with neither a store entry nor a column value.
	linkSSHKey(t, appID, "ghost-key", "")

	_, err := materializeAppSSHKey(context.Background(), pool, appID)
	if err == nil || !strings.Contains(err.Error(), "no private key material") {
		t.Fatalf("err = %v, want no private key material", err)
	}
}
