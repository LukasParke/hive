package build

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/secrets"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// secretsMu serializes tests that touch the process-wide secrets runtime.
// SetRuntime is install-once (a later call is a no-op when a store is
// already installed), so the subtests below are ordered to observe the
// unconfigured state before installing the store.
var secretsMu sync.Mutex

// seedRegistry inserts one registries row and returns its id. An empty
// secretName stores NULL so the row reads as credential-less.
func seedRegistry(t *testing.T, pool *pgxpool.Pool, name, url, username, secretName string, isDefault bool) string {
	t.Helper()
	var secret any
	if secretName != "" {
		secret = secretName
	}
	var id string
	err := pool.QueryRow(context.Background(),
		`insert into registries(name, url, username, secret_name, is_default)
		 values ($1, $2, $3, $4, $5) returning id`,
		name, url, username, secret, isDefault).Scan(&id)
	if err != nil {
		t.Fatalf("seed registry %q: %v", name, err)
	}
	return id
}

func TestResolveRegistry(t *testing.T) {
	pool := testdb.Get(t)
	ctx := context.Background()

	t.Run("pinned registry wins", func(t *testing.T) {
		testdb.TruncateAll(t)
		id := seedRegistry(t, pool, "pinned", "reg-pinned.example.com", "pinuser", "", false)
		auth, err := ResolveRegistry(ctx, pool, &id, "internal:5000")
		if err != nil {
			t.Fatalf("ResolveRegistry: %v", err)
		}
		if auth.Host != "reg-pinned.example.com" || auth.Username != "pinuser" || auth.Password != "" {
			t.Fatalf("auth = %+v, want pinned host/user without password", auth)
		}
	})

	t.Run("nil id falls back to default registry", func(t *testing.T) {
		testdb.TruncateAll(t)
		seedRegistry(t, pool, "plain", "reg-plain.example.com", "nobody", "", false)
		defID := seedRegistry(t, pool, "default", "reg-default.example.com", "defuser", "", true)
		auth, err := ResolveRegistry(ctx, pool, nil, "internal:5000")
		if err != nil {
			t.Fatalf("ResolveRegistry: %v", err)
		}
		if auth.Host != "reg-default.example.com" || auth.Username != "defuser" {
			t.Fatalf("auth = %+v, want default registry host/user", auth)
		}
		_ = defID
	})

	t.Run("empty pinned id behaves like nil", func(t *testing.T) {
		testdb.TruncateAll(t)
		seedRegistry(t, pool, "default", "reg-default.example.com", "defuser", "", true)
		empty := ""
		auth, err := ResolveRegistry(ctx, pool, &empty, "internal:5000")
		if err != nil {
			t.Fatalf("ResolveRegistry: %v", err)
		}
		if auth.Host != "reg-default.example.com" {
			t.Fatalf("host = %q, want default registry fallback", auth.Host)
		}
	})

	t.Run("no registries uses internal address", func(t *testing.T) {
		testdb.TruncateAll(t)
		auth, err := ResolveRegistry(ctx, pool, nil, "registry.internal:5000")
		if err != nil {
			t.Fatalf("ResolveRegistry: %v", err)
		}
		if auth.Host != "registry.internal:5000" || auth.Username != "" {
			t.Fatalf("auth = %+v, want internal addr fallback", auth)
		}
	})

	t.Run("missing pinned registry errors", func(t *testing.T) {
		testdb.TruncateAll(t)
		missing := uuid.NewString()
		_, err := ResolveRegistry(ctx, pool, &missing, "internal:5000")
		if err == nil || !strings.Contains(err.Error(), "load application registry "+missing) {
			t.Fatalf("err = %v, want load application registry failure", err)
		}
	})

	// The remaining cases mutate process-global state; hold the package
	// mutex so no sibling test observes a half-installed runtime.
	secretsMu.Lock()
	defer secretsMu.Unlock()

	t.Run("secret without configured store errors", func(t *testing.T) {
		testdb.TruncateAll(t)
		id := seedRegistry(t, pool, "secreted", "reg-secret.example.com", "secuser", "regcred", false)
		_, err := ResolveRegistry(ctx, pool, &id, "internal:5000")
		if err == nil || !strings.Contains(err.Error(), "secrets store is not configured") {
			t.Fatalf("err = %v, want secrets store not configured", err)
		}
	})

	t.Run("secret-backed registry decrypts password", func(t *testing.T) {
		testdb.TruncateAll(t)
		store, err := secrets.NewStore(pool, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		if err := store.Put(ctx, "regcred", "registry_password", []byte("hunter2")); err != nil {
			t.Fatalf("store.Put: %v", err)
		}
		secrets.SetRuntime(store)
		defer secrets.SetRuntime(nil)

		id := seedRegistry(t, pool, "secreted", "reg-secret.example.com", "secuser", "regcred", true)
		auth, err := ResolveRegistry(ctx, pool, &id, "internal:5000")
		if err != nil {
			t.Fatalf("ResolveRegistry: %v", err)
		}
		if auth.Password != "hunter2" || auth.Username != "secuser" || auth.Host != "reg-secret.example.com" {
			t.Fatalf("auth = %+v, want decrypted hunter2 credentials", auth)
		}

		// Default fallback also resolves credentials for secret-backed rows.
		auth, err = ResolveRegistry(ctx, pool, nil, "internal:5000")
		if err != nil {
			t.Fatalf("ResolveRegistry(default): %v", err)
		}
		if auth.Password != "hunter2" {
			t.Fatalf("default-fallback password = %q, want hunter2", auth.Password)
		}
	})

	t.Run("missing secret errors", func(t *testing.T) {
		testdb.TruncateAll(t)
		if secrets.Runtime() == nil {
			t.Fatal("secrets runtime unexpectedly unset")
		}
		id := seedRegistry(t, pool, "secreted", "reg-secret.example.com", "secuser", "no-such-secret", false)
		_, err := ResolveRegistry(ctx, pool, &id, "internal:5000")
		if err == nil || !strings.Contains(err.Error(), "decrypt registry credentials") {
			t.Fatalf("err = %v, want decrypt registry credentials failure", err)
		}
	})
}
