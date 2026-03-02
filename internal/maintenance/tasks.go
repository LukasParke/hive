package maintenance

import (
	"context"
	"fmt"
	"os/exec"

	"go.uber.org/zap"
)

func RunImagePrune(ctx context.Context, log *zap.SugaredLogger) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("image prune failed: %w", err)
	}
	return string(out), nil
}

func RunDBVacuum(ctx context.Context, dbURL string, log *zap.SugaredLogger) (string, error) {
	if dbURL == "" {
		return "", fmt.Errorf("database URL required for vacuum")
	}
	// Use psql to run VACUUM ANALYZE
	cmd := exec.CommandContext(ctx, "psql", dbURL, "-c", "VACUUM ANALYZE;")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("db vacuum failed: %w", err)
	}
	return string(out), nil
}

func RunSelfBackup(ctx context.Context, cfg map[string]string, log *zap.SugaredLogger) (string, error) {
	// Stub: would execute backup of Hive's own data
	_ = cfg
	log.Infof("self backup stub called")
	return "self backup not yet implemented", nil
}
