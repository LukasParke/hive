package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func Clone(ctx context.Context, repositoryURL, ref string) (string, error) {
	if repositoryURL == "" {
		return "", fmt.Errorf("repository URL is required")
	}
	if ref == "" {
		ref = "main"
	}
	workdir, err := os.MkdirTemp("", "hive-build-*")
	if err != nil {
		return "", err
	}
	repoDir := filepath.Join(workdir, "repo")
	if err := run(ctx, "git", "clone", "--depth", "1", "--branch", ref, repositoryURL, repoDir); err != nil {
		_ = os.RemoveAll(workdir)
		return "", err
	}
	return repoDir, nil
}

func Cleanup(path string) {
	if path == "" {
		return
	}
	_ = os.RemoveAll(path)
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}
