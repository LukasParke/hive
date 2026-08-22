package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// raiseTrigger installs a BEFORE UPDATE trigger on backup_runs that aborts
// every update, optionally only for a specific status transition.
func raiseTrigger(t *testing.T, name string, onlyTransition bool) {
	t.Helper()
	body := "begin raise exception 'injected update fault'; end"
	if onlyTransition {
		body = "begin if OLD.status='running' and NEW.status='complete' then raise exception 'injected completion fault'; end if; return new; end"
	}
	if _, err := testdb.Get(t).Exec(context.Background(), `
		create or replace function test_block_backup_update() returns trigger as $f$ `+body+` $f$ language plpgsql
	`); err != nil {
		t.Fatalf("create trigger function: %v", err)
	}
	if _, err := testdb.Get(t).Exec(context.Background(),
		`create trigger `+name+` before update on backup_runs for each row execute function test_block_backup_update()`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testdb.Get(t).Exec(context.Background(), `drop trigger if exists `+name+` on backup_runs`)
		_, _ = testdb.Get(t).Exec(context.Background(), `drop function if exists test_block_backup_update()`)
	})
}

func TestProcessOnceRecordsFailureWithNotification(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "mysql") // unsupported engine

	runID := seedRun(t, "database", serviceID, nil, "queued")
	if err := runner.processOnce(context.Background()); err != nil {
		t.Fatalf("processOnce: %v", err)
	}
	status, errMsg, artifact := runState(t, runID)
	if status != "failed" || !strings.Contains(errMsg, "backup not supported for engine mysql") || artifact != "" {
		t.Fatalf("run state = (%q, %q, %q), want failed with engine error", status, errMsg, artifact)
	}
}

func TestProcessOnceSuccessNotifies(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	// Default runner keeps its non-nil dispatcher; notifications table has no
	// enabled targets so Notify fans out to nobody but must be exercised.

	runID := seedRun(t, "database", serviceID, nil, "queued")
	if err := runner.processOnce(context.Background()); err != nil {
		t.Fatalf("processOnce: %v", err)
	}
	if status, _, _ := runState(t, runID); status != "complete" {
		t.Fatalf("status = %q, want complete", status)
	}
}

func TestRunReturnsPollerErrorAfterFirstTick(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	seedRun(t, "database", serviceID, nil, "queued")

	// Any update aborts, so the first poller tick (~10s) surfaces an error
	// out of Run instead of looping forever.
	raiseTrigger(t, "test_block_backup_updates", false)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := runner.Run(ctx)
	if err == nil || !strings.Contains(err.Error(), "injected update fault") {
		t.Fatalf("Run err = %v, want injected poller failure", err)
	}
}

func TestExecuteQueuedClaimPhaseFailures(t *testing.T) {
	pool := testdb.Get(t)

	t.Run("claim query fails", func(t *testing.T) {
		runner, serviceID := newBackupEnv(t, "postgres")
		seedRun(t, "database", serviceID, nil, "queued")
		if _, err := pool.Exec(context.Background(),
			`alter table backup_runs rename column created_at to created_at_gone`); err != nil {
			t.Fatalf("rename column: %v", err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(),
				`alter table backup_runs rename column created_at_gone to created_at`)
		})
		err := runner.ExecuteQueued(context.Background(), "database", serviceID)
		if err == nil || !strings.Contains(err.Error(), "claim backup run") {
			t.Fatalf("ExecuteQueued err = %v, want claim failure", err)
		}
	})

	t.Run("mark running fails", func(t *testing.T) {
		runner, serviceID := newBackupEnv(t, "postgres")
		seedRun(t, "database", serviceID, nil, "queued")
		raiseTrigger(t, "test_block_backup_updates", false)

		err := runner.ExecuteQueued(context.Background(), "database", serviceID)
		if err == nil || !strings.Contains(err.Error(), "mark backup running") {
			t.Fatalf("ExecuteQueued err = %v, want mark-running failure", err)
		}
	})
}

func TestExecuteQueuedCompletionUpdateFailureSurfaces(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	runID := seedRun(t, "database", serviceID, nil, "queued")

	// Only the running→complete transition aborts, so claiming succeeds and
	// the failure surfaces from runClaimed's completion update.
	raiseTrigger(t, "test_block_backup_completion", true)

	err := runner.ExecuteQueued(context.Background(), "database", serviceID)
	if err == nil || !strings.Contains(err.Error(), "injected completion fault") {
		t.Fatalf("ExecuteQueued err = %v, want completion-update failure", err)
	}
	if status, errMsg, _ := runState(t, runID); status != "running" || errMsg != "" {
		t.Fatalf("run state = (%q, %q), want stuck running", status, errMsg)
	}
}

func TestExecuteQueuedExecuteBackupEdgePaths(t *testing.T) {
	cases := []struct {
		name       string
		targetID   string
		stub       func(t *testing.T)
		destType   string // "" = none
		destConfig string
		wantErr    string
	}{
		{
			name:     "load target with malformed uuid",
			targetID: "not-a-uuid",
			wantErr:  "uuid",
		},
		{
			name:     "pg_dump exits nonzero",
			targetID: "",
			stub: func(t *testing.T) {
				// A failing gzip fails the whole shell pipeline (no pipefail).
				stubOnPath(t, "pg_dump", "echo 'select 1;'\n")
				stubOnPath(t, "gzip", "exit 1\n")
			},
			wantErr: "postgres backup failed",
		},
		{
			name:       "s3 destination falls back to local artifact",
			targetID:   "",
			stub:       stubPgDump,
			destType:   "s3",
			destConfig: `{}`,
		},
		{
			name:       "shared destination without configured path uses default root",
			targetID:   "",
			stub:       stubPgDump,
			destType:   "shared",
			destConfig: `{}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner, serviceID := newBackupEnv(t, "postgres")
			targetID := tc.targetID
			if targetID == "" {
				targetID = serviceID
			}
			if tc.stub != nil {
				tc.stub(t)
			}
			var destID *string
			if tc.destType != "" {
				id := seedDestination(t, tc.destType, tc.destConfig)
				destID = &id
			}
			runID := seedRun(t, "database", targetID, destID, "queued")

			err := runner.ExecuteQueued(context.Background(), "database", targetID)
			if err != nil {
				t.Fatalf("ExecuteQueued returned err: %v", err)
			}
			status, errMsg, artifact := runState(t, runID)
			if tc.wantErr != "" {
				if status != "failed" || !strings.Contains(errMsg, tc.wantErr) {
					t.Fatalf("run state = (%q, %q), want failed with %q", status, errMsg, tc.wantErr)
				}
				return
			}
			if status != "complete" {
				t.Fatalf("status = %q (%s), want complete", status, errMsg)
			}
			switch tc.destType {
			case "s3":
				// Until object storage lands, s3 keeps the local artifact path.
				if !strings.Contains(artifact, "database/") && !strings.Contains(artifact, "database"+string(filepath.Separator)) {
					t.Fatalf("s3 artifact %q should stay under the local backup root", artifact)
				}
			case "shared":
				want := filepath.Join(os.Getenv("HIVE_BACKUP_ROOT"), "shared")
				if !strings.HasPrefix(artifact, want) {
					t.Fatalf("shared artifact %q not under default root %q", artifact, want)
				}
			}
		})
	}
}

func TestExecuteQueuedSharedDestinationWriteFailure(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	sharedDir := t.TempDir()
	if err := os.Chmod(sharedDir, 0o500); err != nil { //nolint:gosec // test fixture
		t.Fatalf("chmod shared dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(sharedDir, 0o700) }) //nolint:gosec // test fixture

	destID := seedDestination(t, "shared", `{"path":"`+sharedDir+`"}`)
	runID := seedRun(t, "database", serviceID, &destID, "queued")

	if err := runner.ExecuteQueued(context.Background(), "database", serviceID); err != nil {
		t.Fatalf("ExecuteQueued: %v", err)
	}
	status, errMsg, _ := runState(t, runID)
	if status != "failed" || !strings.Contains(errMsg, "permission denied") {
		t.Fatalf("run state = (%q, %q), want failed with permission error", status, errMsg)
	}
}

func TestProcessRestoreOnceEdgePaths(t *testing.T) {
	t.Run("restore-running update fails", func(t *testing.T) {
		runner, serviceID := newBackupEnv(t, "postgres")
		runID := seedRestoreRun(t, "primary", "/tmp/whatever.sql.gz", "database", serviceID)
		raiseTrigger(t, "test_block_backup_updates", false)

		if err := runner.processRestoreOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "injected update fault") {
			t.Fatalf("processRestoreOnce err = %v, want restore-running failure", err)
		}
		_ = runID
	})

	t.Run("file validation command fails", func(t *testing.T) {
		runner, _ := newBackupEnv(t, "postgres")
		artifact := filepath.Join(t.TempDir(), "bundle.tar")
		if err := os.WriteFile(artifact, []byte("payload"), 0o600); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		// Without sh on PATH the validation command itself errors.
		t.Setenv("PATH", t.TempDir())
		runID := seedRestoreRun(t, "files", artifact, "files", uuid.NewString())

		if err := runner.processRestoreOnce(context.Background()); err != nil {
			t.Fatalf("processRestoreOnce: %v", err)
		}
		status, errMsg, _ := runState(t, runID)
		if status != "restore-failed" || !strings.Contains(errMsg, "restore validation failed") {
			t.Fatalf("run state = (%q, %q), want validation failure", status, errMsg)
		}
	})

	t.Run("database restore with malformed target id", func(t *testing.T) {
		runner, _ := newBackupEnv(t, "postgres")
		artifact := writeArtifact(t, gzipped(t, "select 1;"))
		runID := seedRestoreRun(t, "primary", artifact, "database", "not-a-uuid")

		if err := runner.processRestoreOnce(context.Background()); err != nil {
			t.Fatalf("processRestoreOnce: %v", err)
		}
		status, errMsg, _ := runState(t, runID)
		if status != "restore-failed" || !strings.Contains(errMsg, "uuid") {
			t.Fatalf("run state = (%q, %q), want uuid failure", status, errMsg)
		}
	})

	t.Run("psql restore pipeline fails", func(t *testing.T) {
		runner, serviceID := newBackupEnv(t, "postgres")
		stubOnPath(t, "psql", "echo boom >&2; exit 1\n")
		artifact := writeArtifact(t, gzipped(t, "select 1;"))
		runID := seedRestoreRun(t, "primary", artifact, "database", serviceID)

		if err := runner.processRestoreOnce(context.Background()); err != nil {
			t.Fatalf("processRestoreOnce: %v", err)
		}
		status, errMsg, _ := runState(t, runID)
		if status != "restore-failed" || !strings.Contains(errMsg, "postgres restore failed") {
			t.Fatalf("run state = (%q, %q), want restore pipeline failure", status, errMsg)
		}
	})
}

func TestProcessOnceCompletionUpdateFailureSurfaces(t *testing.T) {
	runner, serviceID := newBackupEnv(t, "postgres")
	stubPgDump(t)
	seedRun(t, "database", serviceID, nil, "queued")

	// Only the running→complete transition aborts; the earlier claim-style
	// update inside processOnce passes and the completion write fails.
	raiseTrigger(t, "test_block_backup_completion", true)

	err := runner.processOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "injected completion fault") {
		t.Fatalf("processOnce err = %v, want completion-update failure", err)
	}
}
