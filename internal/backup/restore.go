package backup

import (
	"context"
	"fmt"
	"os/exec"

	"go.uber.org/zap"
)

type RestoreRunner struct {
	log *zap.SugaredLogger
}

func NewRestoreRunner(log *zap.SugaredLogger) *RestoreRunner {
	return &RestoreRunner{log: log}
}

func (r *RestoreRunner) RestoreDatabase(ctx context.Context, dbType, host, dbName, user, password, backupPath string) error {
	var cmd *exec.Cmd

	switch dbType {
	case "postgres":
		cmd = exec.CommandContext(ctx, "pg_restore",
			"-h", host, "-U", user, "-d", dbName, "--clean", "--if-exists", backupPath)
		cmd.Env = append(cmd.Environ(), "PGPASSWORD="+password)
	case "mysql":
		cmd = exec.CommandContext(ctx, "sh", "-c",
			fmt.Sprintf("mysql -h %s -u %s -p%s %s < %s", host, user, password, dbName, backupPath))
	case "mongo":
		cmd = exec.CommandContext(ctx, "mongorestore",
			"--host", host, "--username", user, "--password", password,
			"--db", dbName, "--drop", backupPath)
	default:
		return fmt.Errorf("unsupported database type for restore: %s", dbType)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore failed: %w: %s", err, string(output))
	}

	r.log.Infof("database restored: type=%s db=%s from=%s", dbType, dbName, backupPath)
	return nil
}

func (r *RestoreRunner) RestoreVolume(ctx context.Context, volumeName, backupPath string) error {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", volumeName+":/data",
		"-v", backupPath+":/backup/restore.tar.gz:ro",
		"alpine",
		"sh", "-c", "rm -rf /data/* && tar xzf /backup/restore.tar.gz -C /data",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("volume restore failed: %w: %s", err, string(output))
	}

	r.log.Infof("volume restored: %s from %s", volumeName, backupPath)
	return nil
}
