package backup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

type FileBackupRunner struct {
	log *zap.SugaredLogger
}

func NewFileBackupRunner(log *zap.SugaredLogger) *FileBackupRunner {
	return &FileBackupRunner{log: log}
}

func (r *FileBackupRunner) BackupVolume(ctx context.Context, volumeName, outputDir string) (string, error) {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	ts := time.Now().Format("20060102-150405")
	archiveName := fmt.Sprintf("%s-%s.tar.gz", volumeName, ts)
	archivePath := filepath.Join(outputDir, archiveName)

	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", volumeName+":/data:ro",
		"-v", outputDir+":/backup",
		"alpine",
		"tar", "czf", "/backup/"+archiveName, "-C", "/data", ".",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tar backup failed: %w: %s", err, string(output))
	}

	r.log.Infof("volume backup created: %s", archivePath)
	return archivePath, nil
}
