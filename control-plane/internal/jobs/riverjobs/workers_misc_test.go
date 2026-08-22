package riverjobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"

	"github.com/luke/hive/control-plane/internal/backup"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestBackupWorkerNilRunner(t *testing.T) {
	w := &BackupWorker{}
	err := w.Work(context.Background(), &river.Job[BackupJobArgs]{
		Args: BackupJobArgs{TargetType: "database", TargetID: "abc"},
	})
	if err == nil || err.Error() != "backup runner is not configured" {
		t.Fatalf("err = %v, want backup runner is not configured", err)
	}
}

func TestBackupWorkerNothingQueued(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	w := &BackupWorker{Runner: backup.NewRunner(pool, nil)}
	err := w.Work(context.Background(), &river.Job[BackupJobArgs]{
		Args: BackupJobArgs{TargetType: "database", TargetID: "00000000-0000-0000-0000-000000000001"},
	})
	if err != nil {
		t.Fatalf("ExecuteQueued with nothing queued = %v, want nil", err)
	}
}

func TestBackupWorkerQueuedRunFailsNaturally(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	// A queued run whose target does not exist: the real runner claims it,
	// attempts the backup, and records the failure.
	if _, err := pool.Exec(context.Background(), `
		insert into backup_runs(target_type, target_id, status)
		values ('database', '00000000-0000-0000-0000-000000000002', 'queued')
	`); err != nil {
		t.Fatalf("seed backup run: %v", err)
	}

	t.Setenv("HIVE_BACKUP_ROOT", t.TempDir())
	w := &BackupWorker{Runner: backup.NewRunner(pool, nil)}
	err := w.Work(context.Background(), &river.Job[BackupJobArgs]{
		Args: BackupJobArgs{TargetType: "database", TargetID: "00000000-0000-0000-0000-000000000002"},
	})
	if err != nil {
		t.Fatalf("Work = %v, want nil (failure is recorded on the run)", err)
	}
	var status, errMsg string
	if err := pool.QueryRow(context.Background(),
		`select status, coalesce(error_message,'') from backup_runs where target_id=$1`,
		"00000000-0000-0000-0000-000000000002").Scan(&status, &errMsg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || errMsg == "" {
		t.Fatalf("backup run status=%q error=%q, want failed with an error message", status, errMsg)
	}
}

func TestCertRenewalWorker(t *testing.T) {
	t.Run("nil renewer is a no-op", func(t *testing.T) {
		w := &CertRenewalWorker{}
		if err := w.Work(context.Background(), &river.Job[CertRenewalJobArgs]{}); err != nil {
			t.Fatalf("Work = %v, want nil", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		renewer := &fakeRenewer{}
		w := &CertRenewalWorker{Renewer: renewer}
		if err := w.Work(context.Background(), &river.Job[CertRenewalJobArgs]{}); err != nil {
			t.Fatalf("Work = %v, want nil", err)
		}
		if renewer.calls != 1 {
			t.Fatalf("renewer calls = %d, want 1", renewer.calls)
		}
	})

	t.Run("error propagates", func(t *testing.T) {
		want := errors.New("ca unreachable")
		w := &CertRenewalWorker{Renewer: &fakeRenewer{err: want}}
		if err := w.Work(context.Background(), &river.Job[CertRenewalJobArgs]{}); !errors.Is(err, want) {
			t.Fatalf("Work = %v, want %v", err, want)
		}
	})
}

func TestJobArgsKindAndInsertOpts(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		queue       string
		maxAttempts int
		hasOpts     bool
		args        river.JobArgs
	}{
		{name: "build", kind: "build", queue: QueueBuild, maxAttempts: 3, hasOpts: true, args: BuildJobArgs{}},
		{name: "deploy", kind: "deploy", queue: QueueDeploy, maxAttempts: 4, hasOpts: true, args: DeployJobArgs{}},
		{name: "backup", kind: "backup", hasOpts: false, args: BackupJobArgs{}},
		{name: "cleanup", kind: "cleanup", hasOpts: false, args: CleanupJobArgs{}},
		{name: "cert_renewal", kind: "cert_renewal", hasOpts: false, args: CertRenewalJobArgs{}},
		{name: "preview_deploy", kind: "preview_deploy", queue: QueueBuild, maxAttempts: 3, hasOpts: true, args: PreviewDeployJobArgs{}},
		{name: "preview_cleanup", kind: "preview_cleanup", hasOpts: false, args: PreviewCleanupJobArgs{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.args.Kind(); got != tt.kind {
				t.Fatalf("Kind() = %q, want %q", got, tt.kind)
			}
			type optsGetter interface{ InsertOpts() river.InsertOpts }
			og, ok := tt.args.(optsGetter)
			if ok != tt.hasOpts {
				t.Fatalf("InsertOpts presence = %v, want %v", ok, tt.hasOpts)
			}
			if !tt.hasOpts {
				return
			}
			opts := og.InsertOpts()
			if opts.Queue != tt.queue || opts.MaxAttempts != tt.maxAttempts {
				t.Fatalf("InsertOpts = %+v, want queue %q maxAttempts %d", opts, tt.queue, tt.maxAttempts)
			}
		})
	}
}
