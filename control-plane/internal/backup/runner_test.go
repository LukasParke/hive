package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/luke/hive/control-plane/internal/notify"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func stubOnPath(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, name)
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o750); err != nil { //nolint:gosec // test fixture
		t.Fatalf("write stub %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubPgDump installs a pg_dump that emits trivial SQL so the dump pipeline
// succeeds without touching a real server.
func stubPgDump(t *testing.T) {
	t.Helper()
	stubOnPath(t, "pg_dump", "echo 'select 1;'\n")
}

// newBackupEnv seeds an org-backed database service of the given engine and
// returns a Runner plus that service's id.
func newBackupEnv(t *testing.T, engine string) (*Runner, string) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	t.Setenv("HIVE_BACKUP_ROOT", t.TempDir())

	org := testdb.SeedOrg(t)
	var serviceID string
	if err := pool.QueryRow(context.Background(), `
		insert into database_services(project_id, engine, name, service_name, username, password_secret_name, database_name, port)
		values ($1::uuid, $2, 'svc', 'svc-host', 'dbuser', 'db-secret', 'appdb', 5432)
		returning id::text
	`, org.ProjectID, engine).Scan(&serviceID); err != nil {
		t.Fatalf("seed database service: %v", err)
	}

	return NewRunner(pool, notify.NewDispatcher(pool)), serviceID
}

func seedRun(t *testing.T, targetType, targetID string, destinationID *string, status string) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into backup_runs(target_type, target_id, destination_id, status)
		values ($1, $2, $3::uuid, $4) returning id::text
	`, targetType, targetID, destinationID, status).Scan(&id); err != nil {
		t.Fatalf("seed backup run: %v", err)
	}
	return id
}

func seedDestination(t *testing.T, typ, config string) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into backup_destinations(name, type, config) values ($1, $2, $3::jsonb) returning id::text
	`, "dest-"+uuid.NewString()[:8], typ, config).Scan(&id); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	return id
}

// runState reads status/error_message/artifact_path for a backup run.
func runState(t *testing.T, id string) (status, errMsg, artifact string) {
	t.Helper()
	err := testdb.Get(t).QueryRow(context.Background(), `
		select status, coalesce(error_message,''), coalesce(artifact_path,'')
		from backup_runs where id=$1::uuid
	`, id).Scan(&status, &errMsg, &artifact)
	if err != nil {
		t.Fatalf("read run %s: %v", id, err)
	}
	return status, errMsg, artifact
}

func TestExecuteQueuedWithNoQueuedRunIsNoop(t *testing.T) {
	runner, _ := newBackupEnv(t, "postgres")
	if err := runner.ExecuteQueued(context.Background(), "database", uuid.NewString()); err != nil {
		t.Fatalf("ExecuteQueued with empty queue: %v", err)
	}
	if n := testdb.QueryCount(t, `select count(*) from backup_runs`); n != 0 {
		t.Fatalf("backup_runs rows = %d, want 0", n)
	}
}

func TestExecuteQueuedHappyPathCompletesWithArtifact(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)

	runID := seedRun(t, "database", serviceID, nil, "queued")
	if err := runner.ExecuteQueued(context.Background(), "database", serviceID); err != nil {
		t.Fatalf("ExecuteQueued: %v", err)
	}

	status, errMsg, artifact := runState(t, runID)
	if status != "complete" || errMsg != "" {
		t.Fatalf("run state = (%q, %q), want complete", status, errMsg)
	}
	info, err := os.Stat(artifact)
	if err != nil {
		t.Fatalf("artifact missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("artifact is empty; stubbed pg_dump output was not captured")
	}
	if !strings.HasSuffix(artifact, ".sql.gz") || !strings.Contains(filepath.Base(artifact), serviceID) {
		t.Fatalf("artifact path %q has unexpected shape", artifact)
	}
}

func TestExecuteQueuedClaimsOldestRunFirst(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)

	newer := seedRun(t, "database", serviceID, nil, "queued")
	var older string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into backup_runs(target_type, target_id, status, created_at)
		values ('database', $1, 'queued', now() - interval '1 hour') returning id::text
	`, serviceID).Scan(&older); err != nil {
		t.Fatalf("seed older run: %v", err)
	}

	if err := runner.ExecuteQueued(context.Background(), "database", serviceID); err != nil {
		t.Fatalf("ExecuteQueued: %v", err)
	}

	if status, errMsg, artifact := runState(t, older); status != "complete" || artifact == "" || errMsg != "" {
		t.Fatalf("older run state = (%q, %q, %q), want completed first", status, errMsg, artifact)
	}
	if status, _, _ := runState(t, newer); status != "queued" {
		t.Fatalf("newer run status = %q, want still queued", status)
	}
}

func TestExecuteQueuedSharedDestinationCopiesArtifact(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	sharedDir := t.TempDir()
	destID := seedDestination(t, "shared", fmt.Sprintf(`{"path":%q}`, sharedDir))

	runID := seedRun(t, "database", serviceID, &destID, "queued")
	if err := runner.ExecuteQueued(context.Background(), "database", serviceID); err != nil {
		t.Fatalf("ExecuteQueued: %v", err)
	}

	status, errMsg, artifact := runState(t, runID)
	if status != "complete" || errMsg != "" {
		t.Fatalf("run state = (%q, %q), want complete", status, errMsg)
	}
	if !strings.HasPrefix(artifact, sharedDir) {
		t.Fatalf("artifact %q not copied into shared dir %q", artifact, sharedDir)
	}
	if info, err := os.Stat(artifact); err != nil || info.Size() == 0 {
		t.Fatalf("shared copy missing or empty: %v", err)
	}
}

func TestExecuteQueuedFailurePathsMarkRunFailed(t *testing.T) {
	cases := []struct {
		name     string
		engine   string
		stubDump bool
		target   string // backup target_type
		destType string // "" = no destination row
		wantErr  string
	}{
		{
			name:    "unsupported engine",
			engine:  "mysql",
			target:  "database",
			wantErr: "backup not supported for engine mysql",
		},
		{
			name:    "missing pg_dump binary",
			engine:  "postgres",
			target:  "database",
			wantErr: "pg_dump not available",
		},
		{
			name:     "unsupported destination",
			engine:   "postgres",
			stubDump: true,
			target:   "database",
			destType: "ftp",
			wantErr:  "unsupported backup destination type: ftp",
		},
		{
			name:     "non-database target unsupported",
			engine:   "postgres",
			stubDump: true,
			target:   "volume",
			wantErr:  "only database backup target is currently supported",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner, serviceID := newBackupEnv(t, tc.engine)
			if tc.stubDump {
				stubPgDump(t)
			} else if tc.engine == "postgres" && tc.target == "database" {
				// Force the missing-pg_dump path with a PATH that has nothing.
				t.Setenv("PATH", t.TempDir())
			}
			var destID *string
			if tc.destType != "" {
				id := seedDestination(t, tc.destType, `{}`)
				destID = &id
			}
			runID := seedRun(t, tc.target, serviceID, destID, "queued")

			// Failures are recorded on the row, not returned to the caller.
			if err := runner.ExecuteQueued(context.Background(), tc.target, serviceID); err != nil {
				t.Fatalf("ExecuteQueued returned err: %v", err)
			}
			status, errMsg, artifact := runState(t, runID)
			if status != "failed" {
				t.Fatalf("status = %q (%s), want failed", status, errMsg)
			}
			if !strings.Contains(errMsg, tc.wantErr) {
				t.Fatalf("error_message = %q, want it to contain %q", errMsg, tc.wantErr)
			}
			if artifact != "" {
				t.Fatalf("failed run recorded artifact %q", artifact)
			}
		})
	}
}

func TestRunStopsOnCancelledContext(t *testing.T) {
	newBackupEnv(t, "postgres")
	runner := NewRunner(testdb.Get(t), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.Run(ctx); err == nil {
		t.Fatal("Run with cancelled context err = nil, want context error")
	}
}

func TestProcessOnceExecutesQueuedBackupWithoutNotifier(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	runner.notifier = nil // nil-notifier branch must not panic

	runID := seedRun(t, "database", serviceID, nil, "queued")
	if err := runner.processOnce(context.Background()); err != nil {
		t.Fatalf("processOnce: %v", err)
	}
	if status, errMsg, artifact := runState(t, runID); status != "complete" || artifact == "" || errMsg != "" {
		t.Fatalf("run state = (%q, %q, %q), want complete", status, errMsg, artifact)
	}
}

func TestProcessOnceIdleFallsThroughToRestore(t *testing.T) {
	runner, _ := newBackupEnv(t, "postgres")

	// Nothing queued anywhere: both pollers must return nil.
	if err := runner.processOnce(context.Background()); err != nil {
		t.Fatalf("idle processOnce: %v", err)
	}
}

func seedRestoreRun(t *testing.T, restoreTarget, artifactPath, targetType, targetID string) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into backup_runs(target_type, target_id, status, restore_target, artifact_path)
		values ($1, $2, 'restore-queued', $3, $4) returning id::text
	`, targetType, targetID, restoreTarget, artifactPath).Scan(&id); err != nil {
		t.Fatalf("seed restore run: %v", err)
	}
	return id
}

// gzipped returns genuine gzip content so the restore pipeline's gunzip step
// succeeds against real bytes.
func gzipped(t *testing.T, sql string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(sql)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func writeArtifact(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dump.sql.gz")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return p
}

func TestProcessRestoreOncePaths(t *testing.T) {
	cases := []struct {
		name        string
		runTarget   string // backup_runs.target_type
		restoreTrgt string
		artifact    func(t *testing.T) string
		dbEngine    string // "" = no database service lookup needed
		noPsql      bool
		stubPsql    bool
		wantStatus  string
		wantErrPart string
	}{
		{
			name:        "missing fields fails fast",
			runTarget:   "database",
			restoreTrgt: "",
			artifact:    func(*testing.T) string { return "" },
			wantStatus:  "restore-failed",
			wantErrPart: "missing restore target or artifact",
		},
		{
			name:        "inaccessible artifact fails",
			restoreTrgt: "somewhere",
			artifact: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist.sql.gz")
			},
			wantStatus:  "restore-failed",
			wantErrPart: "artifact not accessible",
		},
		{
			name:        "file target validates and completes",
			runTarget:   "files",
			restoreTrgt: "files",
			artifact: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "bundle.tar")
				if err := os.WriteFile(p, []byte("payload"), 0o600); err != nil {
					t.Fatalf("write artifact: %v", err)
				}
				return p
			},
			wantStatus: "restore-complete",
		},
		{
			name:        "database restore rejects unsupported engine",
			runTarget:   "database",
			restoreTrgt: "primary",
			artifact:    func(t *testing.T) string { return writeArtifact(t, []byte("\x1f\x8bfake")) },
			dbEngine:    "mysql",
			wantStatus:  "restore-failed",
			wantErrPart: "restore not supported for engine mysql",
		},
		{
			name:        "postgres restore without psql fails",
			runTarget:   "database",
			restoreTrgt: "primary",
			artifact:    func(t *testing.T) string { return writeArtifact(t, gzipped(t, "select 1;")) },
			dbEngine:    "postgres",
			noPsql:      true,
			wantStatus:  "restore-failed",
			wantErrPart: "psql not available",
		},
		{
			name:        "postgres restore with psql completes",
			runTarget:   "database",
			restoreTrgt: "primary",
			artifact:    func(t *testing.T) string { return writeArtifact(t, gzipped(t, "select 1;")) },
			dbEngine:    "postgres",
			stubPsql:    true,
			wantStatus:  "restore-complete",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := tc.dbEngine
			if engine == "" {
				engine = "postgres"
			}
			runner, serviceID := newBackupEnv(t, engine)
			switch {
			case tc.stubPsql:
				stubOnPath(t, "psql", "cat > /dev/null\n")
			case tc.noPsql:
				t.Setenv("PATH", t.TempDir()) // no psql anywhere
			}

			runID := seedRestoreRun(t, tc.restoreTrgt, tc.artifact(t), tc.runTarget, serviceID)
			if err := runner.processRestoreOnce(context.Background()); err != nil {
				t.Fatalf("processRestoreOnce: %v", err)
			}
			status, errMsg, _ := runState(t, runID)
			if status != tc.wantStatus {
				t.Fatalf("status = %q (%s), want %q", status, errMsg, tc.wantStatus)
			}
			if tc.wantErrPart != "" && !strings.Contains(errMsg, tc.wantErrPart) {
				t.Fatalf("error_message = %q, want it to contain %q", errMsg, tc.wantErrPart)
			}
		})
	}
}
