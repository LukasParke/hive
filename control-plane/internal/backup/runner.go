package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/notify"
)

type Runner struct {
	pool     *pgxpool.Pool
	notifier *notify.Dispatcher
}

func NewRunner(pool *pgxpool.Pool, notifier *notify.Dispatcher) *Runner {
	return &Runner{pool: pool, notifier: notifier}
}

func (r *Runner) Run(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.processOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *Runner) processOnce(ctx context.Context) error {
	var id string
	var targetType string
	var targetID string
	var destinationType string
	var destinationConfig []byte
	err := r.pool.QueryRow(ctx, `
		select id::text, target_type, target_id
		from backup_runs
		where status = 'queued'
		order by created_at asc
		limit 1
	`).Scan(&id, &targetType, &targetID)
	if err == nil {
		_ = r.pool.QueryRow(ctx, `
			select coalesce(bd.type,''), coalesce(bd.config::text,'{}')::bytea
			from backup_runs br
			left join backup_destinations bd on bd.id = br.destination_id
			where br.id = $1::uuid
		`, id).Scan(&destinationType, &destinationConfig)
	}
	if err != nil {
		return r.processRestoreOnce(ctx)
	}
	if _, err := r.pool.Exec(ctx, `update backup_runs set status='running', started_at=now() where id=$1::uuid`, id); err != nil {
		return err
	}
	artifact, runErr := r.executeBackup(ctx, targetType, targetID, destinationType, destinationConfig)
	if runErr != nil {
		_, _ = r.pool.Exec(ctx, `
			update backup_runs
			set status='failed', error_message=$2, completed_at=now()
			where id=$1::uuid
		`, id, runErr.Error())
		if r.notifier != nil {
			r.notifier.Notify(ctx, "backup.failed", map[string]any{"backupId": id, "error": runErr.Error()})
		}
		return nil
	}
	if _, err := r.pool.Exec(ctx, `
		update backup_runs
		set status='complete', artifact_path=$2, completed_at=now(), error_message=null
		where id=$1::uuid
	`, id, artifact); err != nil {
		return err
	}
	if r.notifier != nil {
		r.notifier.Notify(ctx, "backup.succeeded", map[string]any{"backupId": id, "artifactPath": artifact})
	}
	return nil
}

func (r *Runner) processRestoreOnce(ctx context.Context) error {
	var id string
	var restoreTarget string
	var artifactPath string
	var targetType string
	var targetID string
	err := r.pool.QueryRow(ctx, `
		select id::text, coalesce(restore_target,''), coalesce(artifact_path,''), target_type, target_id
		from backup_runs
		where status = 'restore-queued'
		order by created_at asc
		limit 1
	`).Scan(&id, &restoreTarget, &artifactPath, &targetType, &targetID)
	if err != nil {
		return nil
	}
	if _, err := r.pool.Exec(ctx, `update backup_runs set status='restore-running', started_at=now() where id=$1::uuid`, id); err != nil {
		return err
	}
	if restoreTarget == "" || artifactPath == "" {
		_, _ = r.pool.Exec(ctx, `update backup_runs set status='restore-failed', error_message='missing restore target or artifact', completed_at=now() where id=$1::uuid`, id)
		return nil
	}
	if _, statErr := os.Stat(artifactPath); statErr != nil {
		_, _ = r.pool.Exec(ctx, `update backup_runs set status='restore-failed', error_message=$2, completed_at=now() where id=$1::uuid`, id, fmt.Sprintf("artifact not accessible: %v", statErr))
		return nil
	}
	if targetType == "database" {
		if err := r.executeDatabaseRestore(ctx, targetID, artifactPath); err != nil {
			_, _ = r.pool.Exec(ctx, `update backup_runs set status='restore-failed', error_message=$2, completed_at=now() where id=$1::uuid`, id, err.Error())
			return nil
		}
	} else {
		cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("test -f %q", artifactPath))
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			_, _ = r.pool.Exec(ctx, `update backup_runs set status='restore-failed', error_message=$2, completed_at=now() where id=$1::uuid`, id, fmt.Sprintf("restore validation failed: %v: %s", cmdErr, string(out)))
			return nil
		}
	}
	_, _ = r.pool.Exec(ctx, `update backup_runs set status='restore-complete', completed_at=now(), error_message=null where id=$1::uuid`, id)
	return nil
}

func (r *Runner) executeDatabaseRestore(ctx context.Context, targetID, artifactPath string) error {
	engine, host, username, password, databaseName, err := r.loadDatabaseTarget(ctx, targetID)
	if err != nil {
		return err
	}
	switch strings.ToLower(engine) {
	case "postgres":
		if _, err := exec.LookPath("psql"); err != nil {
			return errors.New("psql not available for restore")
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("gunzip -c %q | psql -h %q -U %q -d %q", artifactPath, host, username, databaseName))
		cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			return fmt.Errorf("postgres restore failed: %v: %s", cmdErr, strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return fmt.Errorf("restore not supported for engine %s", engine)
	}
}

func (r *Runner) loadDatabaseTarget(ctx context.Context, targetID string) (engine, host, username, password, databaseName string, err error) {
	err = r.pool.QueryRow(ctx, `
		select engine, service_name, coalesce(username,'hive'), coalesce(password_secret_name,''), coalesce(database_name,'hive')
		from database_services
		where id=$1::uuid
	`, targetID).Scan(&engine, &host, &username, &password, &databaseName)
	return
}

func (r *Runner) executeBackup(ctx context.Context, targetType, targetID, destinationType string, destinationConfigRaw []byte) (string, error) {
	backupRoot := strings.TrimSpace(os.Getenv("HIVE_BACKUP_ROOT"))
	if backupRoot == "" {
		backupRoot = "/tmp/hive-backups"
	}
	targetDir := filepath.Join(backupRoot, targetType)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	artifactPath := filepath.Join(targetDir, fmt.Sprintf("%s-%d.sql.gz", targetID, time.Now().Unix()))
	if targetType != "database" {
		return "", errors.New("only database backup target is currently supported")
	}
	engine, host, username, password, databaseName, err := r.loadDatabaseTarget(ctx, targetID)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(engine) {
	case "postgres":
		if _, err := exec.LookPath("pg_dump"); err != nil {
			return "", errors.New("pg_dump not available for postgres backups")
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("pg_dump -h %q -U %q -d %q | gzip > %q", host, username, databaseName, artifactPath))
		cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			return "", fmt.Errorf("postgres backup failed: %v: %s", cmdErr, strings.TrimSpace(string(out)))
		}
	default:
		return "", fmt.Errorf("backup not supported for engine %s", engine)
	}
	if destinationType == "" || destinationType == "local" {
		return artifactPath, nil
	}
	var cfg map[string]any
	_ = json.Unmarshal(destinationConfigRaw, &cfg)
	switch destinationType {
	case "shared":
		dir, _ := cfg["path"].(string)
		if dir == "" {
			dir = filepath.Join(backupRoot, "shared")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		dest := filepath.Join(dir, filepath.Base(artifactPath))
		content, err := os.ReadFile(artifactPath)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(dest, content, 0o600); err != nil {
			return "", err
		}
		return dest, nil
	case "s3":
		// Keep local artifact path until object storage integration is wired.
		return artifactPath, nil
	default:
		return "", fmt.Errorf("unsupported backup destination type: %s", destinationType)
	}
}
