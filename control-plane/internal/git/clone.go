package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options controls a shallow clone performed for a build.
type Options struct {
	RepositoryURL string
	Ref           string
	// SSHKeyPath is an optional path to a private key (0600) used for
	// private repositories. When set, git is instructed to use it via
	// core.sshCommand.
	SSHKeyPath string
}

// Clone shallow-clones the repository at opts.Ref and returns the checkout
// directory plus the short commit SHA of HEAD.
func Clone(ctx context.Context, opts Options) (string, string, error) {
	if opts.RepositoryURL == "" {
		return "", "", fmt.Errorf("repository URL is required")
	}
	if opts.Ref == "" {
		opts.Ref = "main"
	}
	workdir, err := os.MkdirTemp("", "hive-build-*")
	if err != nil {
		return "", "", err
	}
	repoDir := filepath.Join(workdir, "repo")

	args := []string{"clone", "--depth", "1"}
	if opts.SSHKeyPath != "" {
		args = append(args, "-c", fmt.Sprintf(
			"core.sshCommand=ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new",
			opts.SSHKeyPath,
		))
	}
	args = append(args, "--branch", opts.Ref, opts.RepositoryURL, repoDir)
	if err := run(ctx, "git", args...); err != nil {
		_ = os.RemoveAll(workdir)
		return "", "", err
	}

	sha, err := runOutput(ctx, repoDir, "git", "rev-parse", "--short", "HEAD")
	if err != nil {
		_ = os.RemoveAll(workdir)
		return "", "", fmt.Errorf("resolve commit sha: %w", err)
	}
	return repoDir, sha, nil
}

// Cleanup removes a cloned repository directory, ignoring an empty path.
func Cleanup(path string) {
	if path == "" {
		return
	}
	_ = os.RemoveAll(path)
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // deliberate git invocation with server-constructed args
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v failed: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // deliberate git invocation with server-constructed args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %v failed: %w: %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
