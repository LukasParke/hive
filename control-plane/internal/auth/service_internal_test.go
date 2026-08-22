package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/luke/hive/control-plane/internal/db"
)

// The white-box tests below cannot import internal/testdb (it imports this
// package, which would be an import cycle in tests), so they spin up their
// own throwaway Postgres with the same migrations applied.
const internalTestPassword = "sup3rsecret!"

var (
	fixtureOnce sync.Once
	fixturePool *pgxpool.Pool
	fixtureSvc  *Service
	fixtureErr  error
)

func internalUniqueEmail(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12] + "@test.local"
}

func mustRegisterInternal(t *testing.T, email string) string {
	t.Helper()
	userID, err := fixture(t).Register(context.Background(), email, internalTestPassword, "Internal User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return userID
}

// fixture returns an auth Service backed by a dedicated test database.
func fixture(t *testing.T) *Service {
	t.Helper()
	fixtureOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("hive"),
			tcpostgres.WithUsername("hive"),
			tcpostgres.WithPassword("hive"),
		)
		if err != nil {
			fixtureErr = fmt.Errorf("start postgres container: %w", err)
			return
		}

		dsn, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			fixtureErr = fmt.Errorf("container connection string: %w", err)
			return
		}
		p, err := db.NewPool(ctx, dsn)
		if err != nil {
			fixtureErr = fmt.Errorf("connect pool: %w", err)
			return
		}
		// The container may still be finishing its init sequence; retry until
		// Postgres actually accepts queries.
		if err := waitReady(ctx, p); err != nil {
			fixtureErr = fmt.Errorf("ping pool: %w", err)
			return
		}
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			fixtureErr = errors.New("cannot locate source file")
			return
		}
		migrations := filepath.Join(filepath.Dir(thisFile), "..", "db", "migrations")
		if err := db.ApplyMigrations(ctx, p, os.DirFS(migrations)); err != nil {
			fixtureErr = fmt.Errorf("apply migrations: %w", err)
			return
		}
		fixturePool = p
		fixtureSvc = NewService(p, "internal-whitebox-secret")
	})
	if fixtureErr != nil {
		t.Skipf("white-box database unavailable: %v", fixtureErr)
	}
	return fixtureSvc
}

// waitReady polls the pool until Postgres answers queries or ctx expires.
func waitReady(ctx context.Context, p *pgxpool.Pool) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := p.Ping(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// withMintFailures points both token-minting seams at failing stubs for the
// duration of fn, restoring the production implementations afterward.
func withMintFailures(t *testing.T, mintErr error, fn func(t *testing.T)) {
	t.Helper()
	prevIssue, prevRandom := issueAccessTokenFn, randomTokenFn
	issueAccessTokenFn = func(*Service, string, string) (string, error) { return "", mintErr }
	randomTokenFn = func(int) (string, error) { return "", mintErr }
	t.Cleanup(func() {
		issueAccessTokenFn, randomTokenFn = prevIssue, prevRandom
	})
	fn(t)
}

func TestLoginPropagatesAccessTokenFailure(t *testing.T) {
	svc := fixture(t)
	email := internalUniqueEmail("mintfail")

	withMintFailures(t, errors.New("signing exploded"), func(t *testing.T) {
		if _, err := svc.Register(context.Background(), email, internalTestPassword, "Mint Fail"); err != nil {
			t.Fatalf("register: %v", err)
		}
		access, refresh, err := svc.Login(context.Background(), email, internalTestPassword)
		if err == nil || !strings.Contains(err.Error(), "signing exploded") {
			t.Fatalf("login err = %v, want signing failure", err)
		}
		if access != "" || refresh != "" {
			t.Fatalf("failed login returned tokens (%q, %q)", access, refresh)
		}
	})
}

func TestLoginPropagatesRefreshTokenFailure(t *testing.T) {
	svc := fixture(t)
	email := internalUniqueEmail("randfail")
	mustRegisterInternal(t, email)

	prevIssue, prevRandom := issueAccessTokenFn, randomTokenFn
	issueAccessTokenFn = func(s *Service, userID, email string) (string, error) {
		return prevIssue(s, userID, email)
	}
	randomTokenFn = func(int) (string, error) { return "", errors.New("entropy exhausted") }
	t.Cleanup(func() { issueAccessTokenFn, randomTokenFn = prevIssue, prevRandom })

	if _, _, err := svc.Login(context.Background(), email, internalTestPassword); err == nil || !strings.Contains(err.Error(), "entropy") {
		t.Fatalf("login err = %v, want random-token failure", err)
	}
}

func TestRefreshPropagatesMintFailures(t *testing.T) {
	svc := fixture(t)
	email := internalUniqueEmail("refmint")
	mustRegisterInternal(t, email)
	_, refresh, err := svc.Login(context.Background(), email, internalTestPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	t.Run("access token failure", func(t *testing.T) {
		withMintFailures(t, errors.New("access signing failed"), func(t *testing.T) {
			if _, _, err := svc.Refresh(context.Background(), refresh); err == nil || !strings.Contains(err.Error(), "access signing failed") {
				t.Fatalf("refresh err = %v, want access-mint failure", err)
			}
		})
	})

	t.Run("refresh token failure", func(t *testing.T) {
		prevIssue, prevRandom := issueAccessTokenFn, randomTokenFn
		issueAccessTokenFn = func(s *Service, userID, email string) (string, error) {
			return prevIssue(s, userID, email)
		}
		randomTokenFn = func(int) (string, error) { return "", errors.New("no randomness") }
		t.Cleanup(func() { issueAccessTokenFn, randomTokenFn = prevIssue, prevRandom })
		if _, _, err := svc.Refresh(context.Background(), refresh); err == nil || !strings.Contains(err.Error(), "no randomness") {
			t.Fatalf("refresh err = %v, want refresh-mint failure", err)
		}
	})
}

func TestLoginAndRefreshSessionWriteFailures(t *testing.T) {
	fixture(t)
	pool := fixturePool
	svc := fixtureSvc
	email := internalUniqueEmail("sesswrite")
	mustRegisterInternal(t, email)

	rename := func(from, to string) error {
		_, err := pool.Exec(context.Background(),
			`alter table sessions rename column `+from+` to `+to)
		return err
	}

	t.Run("login insert fails", func(t *testing.T) {
		// Break session persistence by renaming the column the insert targets.
		if err := rename("refresh_token_hash", "refresh_token_hash_gone"); err != nil {
			t.Fatalf("rename sessions column: %v", err)
		}
		t.Cleanup(func() {
			_ = rename("refresh_token_hash_gone", "refresh_token_hash")
		})
		if _, _, err := svc.Login(context.Background(), email, internalTestPassword); err == nil || !strings.Contains(err.Error(), "42703") {
			t.Fatalf("login err = %v, want session-insert failure", err)
		}
	})

	_, refresh, err := svc.Login(context.Background(), email, internalTestPassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	t.Run("refresh update fails", func(t *testing.T) {
		// A BEFORE UPDATE trigger fails only the rotation UPDATE while the
		// session lookup keeps working.
		if _, err := pool.Exec(context.Background(), `
			create or replace function test_block_session_update() returns trigger as $f$
			begin raise exception 'injected session update fault'; end
			$f$ language plpgsql`); err != nil {
			t.Fatalf("create trigger function: %v", err)
		}
		if _, err := pool.Exec(context.Background(),
			`create trigger test_block_sessions_upd before update on sessions
			 for each row execute function test_block_session_update()`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `drop trigger if exists test_block_sessions_upd on sessions`)
			_, _ = pool.Exec(context.Background(), `drop function if exists test_block_session_update()`)
		})

		if _, _, err := svc.Refresh(context.Background(), refresh); err == nil || !strings.Contains(err.Error(), "injected session update fault") {
			t.Fatalf("refresh err = %v, want session-update failure", err)
		}
	})
}
