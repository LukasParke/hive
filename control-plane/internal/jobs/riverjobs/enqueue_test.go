package riverjobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestIsUniqueViolation(t *testing.T) {
	if IsUniqueViolation(nil) {
		t.Fatal("nil must not be a unique violation")
	}
	if IsUniqueViolation(errors.New("plain")) {
		t.Fatal("plain error must not be a unique violation")
	}
	pgErr := &pgconn.PgError{Code: "23505"}
	if !IsUniqueViolation(pgErr) {
		t.Fatal("23505 PgError must be a unique violation")
	}
	wrapped := fmt.Errorf("insert build job: %w", pgErr)
	if !IsUniqueViolation(wrapped) {
		t.Fatal("wrapped 23505 must be detected via errors.As")
	}
	if IsUniqueViolation(&pgconn.PgError{Code: "23503"}) {
		t.Fatal("other SQLSTATEs are not unique violations")
	}
}

func TestEnqueueBuildHappyPath(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "enq", "", nil)

	client := testdb.RiverClient(t)
	buildID, err := EnqueueBuild(context.Background(), client, pool, appID, "manual", "")
	if err != nil {
		t.Fatalf("EnqueueBuild: %v", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(),
		`select status::text from build_jobs where id=$1::uuid`, buildID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		t.Fatalf("build status = %q, want queued", status)
	}
	n := testdb.QueryCount(t, `select count(*) from river_job where kind='build'`)
	if n != 1 {
		t.Fatalf("river build jobs = %d, want 1", n)
	}
}

func TestEnqueueBuildAlreadyQueued(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "dup", "", nil)
	seedBuildJob(t, appID, "queued", "manual")

	_, err := EnqueueBuild(context.Background(), nil, pool, appID, "manual", "")
	if !errors.Is(err, ErrBuildAlreadyQueued) {
		t.Fatalf("err = %v, want ErrBuildAlreadyQueued", err)
	}
}

func TestEnqueueBuildNilClientSkipsRiver(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "noclient", "", nil)

	buildID, err := EnqueueBuild(context.Background(), nil, pool, appID, "rollback", "img:1")
	if err != nil {
		t.Fatalf("EnqueueBuild: %v", err)
	}
	var status, imageTag string
	if err := pool.QueryRow(context.Background(),
		`select status::text, image_tag from build_jobs where id=$1::uuid`, buildID).Scan(&status, &imageTag); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || imageTag != "img:1" {
		t.Fatalf("status=%q image_tag=%q, want queued/img:1", status, imageTag)
	}
	if n := testdb.QueryCount(t, `select count(*) from river_job`); n != 0 {
		t.Fatalf("river_job rows = %d, want 0 with nil client", n)
	}
}

func TestEnqueueBuildInsertFailureMarksFailed(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "deadclient", "", nil)

	// A river client on a closed pool makes Insert fail while the shared
	// pool keeps serving the build row insert/update.
	deadPool := deadPoolForTest(t)
	client, err := river.NewClient(riverpgxv5.New(deadPool), &river.Config{})
	if err != nil {
		t.Fatalf("river.NewClient: %v", err)
	}

	_, err = EnqueueBuild(context.Background(), client, pool, appID, "manual", "")
	if err == nil || !strings.Contains(err.Error(), "enqueue build job") {
		t.Fatalf("expected enqueue failure, got %v", err)
	}
	var status, errMsg string
	if err := pool.QueryRow(context.Background(),
		`select status::text, coalesce(error_message,'') from build_jobs where application_id=$1::uuid`, appID).
		Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.HasPrefix(errMsg, "enqueue failed:") {
		t.Fatalf("status=%q error=%q, want failed with 'enqueue failed:' prefix", status, errMsg)
	}
}

// deadPoolForTest returns a pgxpool built from the shared pool's connection
// settings and immediately closed, so any acquire fails.
func deadPoolForTest(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := testdb.Get(t)
	cfg, err := pgxpool.ParseConfig(shared.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	p.Close()
	return p
}
