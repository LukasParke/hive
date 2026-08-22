// Package testdb provides a shared, real Postgres-backed test database for
// control-plane integration-style unit tests. The container starts once per
// test binary via sync.Once; when Docker is unreachable every DB-backed test
// skips cleanly instead of failing.
package testdb

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/auth"
	"github.com/luke/hive/control-plane/internal/db"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	testDBUser     = "hive"
	testDBPassword = "hive"
	testDBName     = "hive"
	jwtSecret      = "test-jwt-secret"
)

var (
	testDSN     string
	once        sync.Once
	pool        *pgxpool.Pool
	startErr    error
	migrations  string
	sharedAuth  *auth.Service
	testContext = context.Background()
)

func init() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("testdb: cannot locate source file")
	}
	// internal/testdb/testdb.go -> internal/db/migrations
	migrations = filepath.Join(filepath.Dir(thisFile), "..", "db", "migrations")
}

// waitForDB polls the pool until Postgres answers or the context expires.
func waitForDB(ctx context.Context, p *pgxpool.Pool) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := p.Ping(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return lastErr
}

func start() {
	if !dockerReachable() {
		startErr = fmt.Errorf("docker daemon not reachable (set DOCKER_HOST or grant access to %s)", defaultSocket())
		return
	}

	ctx, cancel := context.WithTimeout(testContext, 2*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase(testDBName),
		tcpostgres.WithUsername(testDBUser),
		tcpostgres.WithPassword(testDBPassword),
	)
	if err != nil {
		startErr = fmt.Errorf("start postgres container: %w", err)
		return
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		startErr = fmt.Errorf("container connection string: %w", err)
		return
	}
	testDSN = dsn

	p, err := db.NewPool(ctx, dsn)
	if err != nil {
		startErr = fmt.Errorf("connect pool: %w", err)
		return
	}
	// The container may still be finishing its init sequence; retry until
	// Postgres actually accepts queries.
	if err := waitForDB(ctx, p); err != nil {
		startErr = fmt.Errorf("ping pool: %w", err)
		return
	}
	if err := db.ApplyMigrations(ctx, p, os.DirFS(migrations)); err != nil {
		startErr = fmt.Errorf("apply migrations from %s: %w", migrations, err)
		return
	}
	if err := db.MigrateRiver(ctx, p); err != nil {
		startErr = fmt.Errorf("apply river migrations: %w", err)
		return
	}
	pool = p
	sharedAuth = auth.NewService(p, jwtSecret)
}

func defaultSocket() string {
	if host := os.Getenv("DOCKER_HOST"); host != "" && !strings.HasPrefix(host, "tcp://") {
		return strings.TrimPrefix(host, "unix://")
	}
	return "/var/run/docker.sock"
}

// dockerReachable performs a cheap connectivity probe so that environments
// without a usable daemon skip in milliseconds instead of hanging on
// testcontainers' retry loop.
func dockerReachable() bool {
	host := os.Getenv("DOCKER_HOST")
	if host == "" || strings.HasPrefix(host, "unix://") {
		conn, err := net.DialTimeout("unix", defaultSocket(), time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}
	addr := strings.TrimPrefix(host, "tcp://")
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = net.JoinHostPort(addr, "2375")
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second) //nolint:gosec // test fixture
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// DSN returns the connection string of the shared test database container.
// Empty until Get has started the container.
func DSN() string { return testDSN }

// Get returns the shared test database pool, starting the container on first use.
// Get returns the shared pool backed by a real Postgres 16 container with all
// control-plane and River migrations applied. It skips the test when Docker is
// unavailable.
func Get(t *testing.T) *pgxpool.Pool {
	t.Helper()
	once.Do(start)
	if startErr != nil {
		t.Skipf("docker unavailable: %v", startErr)
	}
	if err := pool.Ping(testContext); err != nil {
		t.Skipf("database unreachable: %v", err)
	}
	return pool
}

// Auth returns an auth Service sharing the test JWT secret.
func Auth(t *testing.T) *auth.Service {
	t.Helper()
	Get(t)
	return sharedAuth
}

// Truncate empties the given tables in one CASCADE statement so FK references
// are honored without ordering concerns.
func Truncate(t *testing.T, tables ...string) {
	t.Helper()
	p := Get(t)
	if len(tables) == 0 {
		t.Fatal("testdb.Truncate: no tables given")
	}
	quoted := make([]string, 0, len(tables))
	for _, tbl := range tables {
		quoted = append(quoted, (&pgIdentifier{tbl}).String())
	}
	sql := "truncate table " + strings.Join(quoted, ", ") + " cascade"
	if _, err := p.Exec(testContext, sql); err != nil {
		t.Fatalf("testdb.Truncate %v: %v", tables, err)
	}
}

// TruncateAll resets every application table (including River's job table),
// leaving only migration bookkeeping intact. Use at test start for isolation.
func TruncateAll(t *testing.T) {
	t.Helper()
	p := Get(t)
	rows, err := p.Query(testContext, `
		select tablename from pg_tables
		where schemaname = 'public'
		  and tablename <> 'schema_migrations'
	`)
	if err != nil {
		t.Fatalf("testdb.TruncateAll: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("testdb.TruncateAll scan: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("testdb.TruncateAll rows: %v", err)
	}
	if len(tables) == 0 {
		return
	}
	Truncate(t, tables...)
}

type pgIdentifier struct{ name string }

func (i *pgIdentifier) String() string {
	return `"` + strings.ReplaceAll(i.name, `"`, `""`) + `"`
}

// OrgFixture carries everything needed to authenticate as an organization
// member: real DB rows plus headers ready to attach to requests.
type OrgFixture struct {
	OrgID     string
	UserID    string
	Email     string
	Token     string
	ProjectID string
	Headers   http.Header
}

// SeedOrg creates an organization, owner user (via the real auth service, so
// the token is a genuine signed JWT), membership, and a project. Headers
// include Authorization and X-Organization-Id.
func SeedOrg(t *testing.T) *OrgFixture {
	t.Helper()
	return SeedOrgWithRole(t, rbac.RoleOwner)
}

// SeedOrgWithRole behaves like SeedOrg but grants the given role.
func SeedOrgWithRole(t *testing.T, role rbac.Role) *OrgFixture {
	t.Helper()
	p := Get(t)

	email := fmt.Sprintf("user-%s@test.local", strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	userID, err := sharedAuth.Register(testContext, email, "sup3rsecret!", "Test User")
	if err != nil {
		t.Fatalf("testdb.SeedOrg register user: %v", err)
	}
	token, _, err := sharedAuth.Login(testContext, email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("testdb.SeedOrg login: %v", err)
	}

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var orgID string
	err = p.QueryRow(testContext, `
		insert into organizations(name, slug) values ($1, $2) returning id::text
	`, "org-"+suffix, "org-"+suffix).Scan(&orgID)
	if err != nil {
		t.Fatalf("testdb.SeedOrg insert organization: %v", err)
	}
	if _, err := p.Exec(testContext, `
		insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, $3)
	`, orgID, userID, string(role)); err != nil {
		t.Fatalf("testdb.SeedOrg insert membership: %v", err)
	}

	projectID := SeedProject(t, orgID)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	headers.Set("X-Organization-Id", orgID)

	return &OrgFixture{
		OrgID:     orgID,
		UserID:    userID,
		Email:     email,
		Token:     token,
		ProjectID: projectID,
		Headers:   headers,
	}
}

// AddMember adds another user to an existing organization with the given role,
// returning its fixture (same org/project, fresh credentials).
func (o *OrgFixture) AddMember(t *testing.T, role rbac.Role) *OrgFixture {
	t.Helper()
	p := Get(t)
	email := fmt.Sprintf("user-%s@test.local", strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	userID, err := sharedAuth.Register(testContext, email, "sup3rsecret!", "Test Member")
	if err != nil {
		t.Fatalf("testdb.AddMember register user: %v", err)
	}
	token, _, err := sharedAuth.Login(testContext, email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("testdb.AddMember login: %v", err)
	}
	if _, err := p.Exec(testContext, `
		insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, $3)
	`, o.OrgID, userID, string(role)); err != nil {
		t.Fatalf("testdb.AddMember insert membership: %v", err)
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+token)
	headers.Set("X-Organization-Id", o.OrgID)
	return &OrgFixture{
		OrgID:     o.OrgID,
		UserID:    userID,
		Email:     email,
		Token:     token,
		ProjectID: o.ProjectID,
		Headers:   headers,
	}
}

// SeedProject creates a project owned by the organization and returns its ID.
func SeedProject(t *testing.T, orgID string) string {
	t.Helper()
	p := Get(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var projectID string
	err := p.QueryRow(testContext, `
		insert into projects(name, organization_id) values ($1, $2::uuid) returning id::text
	`, "project-"+suffix, orgID).Scan(&projectID)
	if err != nil {
		t.Fatalf("testdb.SeedProject: %v", err)
	}
	return projectID
}

// SeedApplication inserts an application row for fixtures.
func SeedApplication(t *testing.T, projectID, name, repositoryURL string, watchPaths []string) string {
	t.Helper()
	p := Get(t)
	if name == "" {
		name = "app-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	autoDeploy := repositoryURL != ""
	var appID string
	err := p.QueryRow(testContext, `
		insert into applications(project_id, name, source_type, image, repository_url, git_ref, auto_deploy, watch_paths)
		values ($1::uuid, $2, 'git', nullif($3, ''), nullif($3, ''), 'main', $4, coalesce($5, '{}'::text[]))
		returning id::text
	`, projectID, name, repositoryURL, autoDeploy, watchPaths).Scan(&appID)
	if err != nil {
		t.Fatalf("testdb.SeedApplication: %v", err)
	}
	return appID
}

// SeedApplicationWithRef seeds an application pinned to a non-default branch.
func SeedApplicationWithRef(t *testing.T, projectID, name, repositoryURL, gitRef string) string {
	t.Helper()
	return seedApplication(t, projectID, name, repositoryURL, &gitRef, nil, true)
}

// SeedApplicationWatchPaths seeds an application limited to watch paths.
func SeedApplicationWatchPaths(t *testing.T, projectID, name, repositoryURL string, watchPaths []string) string {
	t.Helper()
	return seedApplication(t, projectID, name, repositoryURL, nil, watchPaths, true)
}

// SeedApplicationNoAutoDeploy seeds an application with auto-deploy off.
func SeedApplicationNoAutoDeploy(t *testing.T, projectID, name, repositoryURL string) string {
	t.Helper()
	return seedApplication(t, projectID, name, repositoryURL, nil, nil, false)
}

// seedApplication inserts one application row with explicit optional fields.
func seedApplication(t *testing.T, projectID, name, repositoryURL string, gitRef *string, watchPaths []string, autoDeploy bool) string {
	t.Helper()
	p := Get(t)
	if name == "" {
		name = "app-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	ref := "main"
	if gitRef != nil {
		ref = *gitRef
	}
	var appID string
	err := p.QueryRow(testContext, `
		insert into applications(project_id, name, source_type, repository_url, git_ref, auto_deploy, watch_paths)
		values ($1::uuid, $2, 'git', $3, $4, $5, coalesce($6, '{}'::text[]))
		returning id::text
	`, projectID, name, repositoryURL, ref, autoDeploy, watchPaths).Scan(&appID)
	if err != nil {
		t.Fatalf("testdb.seedApplication: %v", err)
	}
	return appID
}

// SeedGitProvider inserts an enabled git provider of the given type with a
// webhook secret, returning its ID.
func SeedGitProvider(t *testing.T, providerType, webhookSecret string) string {
	t.Helper()
	p := Get(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var id string
	err := p.QueryRow(testContext, `
		insert into git_providers(type, name, base_url, webhook_secret, enabled)
		values ($1, $2, 'https://example.test', $3, true)
		returning id::text
	`, providerType, providerType+"-"+suffix, webhookSecret).Scan(&id)
	if err != nil {
		t.Fatalf("testdb.SeedGitProvider(%s): %v", providerType, err)
	}
	return id
}

// RiverClient returns a River client suitable for enqueuing jobs against the
// shared pool. No workers are started; Insert writes straight to river_job.
func RiverClient(t *testing.T) *river.Client[pgx.Tx] {
	t.Helper()
	p := Get(t)
	client, err := river.NewClient(riverpgxv5.New(p), &river.Config{})
	if err != nil {
		t.Fatalf("testdb.RiverClient: %v", err)
	}
	return client
}

// QueryCount returns the number of rows matching an arbitrary scalar query;
// handy for asserting inserted rows.
func QueryCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	p := Get(t)
	var n int
	if err := p.QueryRow(testContext, query, args...).Scan(&n); err != nil {
		t.Fatalf("testdb.QueryCount(%s): %v", query, err)
	}
	return n
}
