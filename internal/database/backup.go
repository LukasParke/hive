package database

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

type BackupRunner struct {
	log *zap.SugaredLogger
}

func NewBackupRunner(log *zap.SugaredLogger) *BackupRunner {
	return &BackupRunner{log: log}
}

func (br *BackupRunner) BackupDatabase(ctx context.Context, dbType, host, name, user, password, outputDir string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s-%s.sql", name, dbType, timestamp)
	outputPath := filepath.Join(outputDir, filename)
	os.MkdirAll(outputDir, 0755)

	var cmd *exec.Cmd
	switch dbType {
	case "postgres":
		cmd = exec.CommandContext(ctx, "pg_dump",
			"-h", host,
			"-U", user,
			"-d", name,
			"-f", outputPath,
		)
		cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", password))

	case "mysql":
		cmd = exec.CommandContext(ctx, "mysqldump",
			"-h", host,
			"-u", user,
			fmt.Sprintf("-p%s", password),
			name,
			"--result-file", outputPath,
		)

	case "mongo":
		dumpDir := filepath.Join(outputDir, name+"-dump")
		cmd = exec.CommandContext(ctx, "mongodump",
			"--host", host,
			"--username", user,
			"--password", password,
			"--db", name,
			"--out", dumpDir,
		)
		outputPath = dumpDir

	case "redis":
		// Redis uses BGSAVE; we copy the dump.rdb
		cmd = exec.CommandContext(ctx, "redis-cli", "-h", host, "BGSAVE")
		outputPath = filepath.Join(outputDir, fmt.Sprintf("redis-%s-%s.rdb", name, timestamp))

	default:
		return "", fmt.Errorf("backup not supported for db type: %s", dbType)
	}

	br.log.Infof("running backup for %s database %s", dbType, name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("backup failed: %w: %s", err, string(output))
	}

	br.log.Infof("backup complete: %s", outputPath)
	return outputPath, nil
}
