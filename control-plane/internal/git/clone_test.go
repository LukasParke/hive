package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initLocalRepo creates a throwaway git repository with one commit so
// Clone can be exercised without network access.
func initLocalRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...) //nolint:gosec // test fixture git setup
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "init")
	return dir
}

func TestCloneLocalRepository(t *testing.T) {
	repo := initLocalRepo(t)

	repoDir, sha, err := Clone(context.Background(), Options{RepositoryURL: repo, Ref: "main"})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	defer Cleanup(repoDir)

	data, err := os.ReadFile(filepath.Join(repoDir, "Dockerfile")) //nolint:gosec // test fixture path
	if err != nil || string(data) != "FROM scratch\n" {
		t.Fatalf("checked-out Dockerfile wrong: %q err=%v", data, err)
	}
	if sha == "" || len(sha) > 12 {
		t.Fatalf("unexpected short sha %q", sha)
	}
}

func TestCloneDefaultsRefToMain(t *testing.T) {
	repo := initLocalRepo(t)

	repoDir, sha, err := Clone(context.Background(), Options{RepositoryURL: repo})
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	defer Cleanup(repoDir)
	if sha == "" {
		t.Fatal("expected non-empty sha")
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "Dockerfile")) //nolint:gosec // test fixture path
	if err != nil || string(data) != "FROM scratch\n" {
		t.Fatalf("default-branch checkout wrong: %q err=%v", data, err)
	}
}

func TestCloneWithSSHKeyPath(t *testing.T) {
	repo := initLocalRepo(t)
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyFile, []byte("dummy-private-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	repoDir, _, err := Clone(context.Background(), Options{
		RepositoryURL: repo,
		Ref:           "main",
		SSHKeyPath:    keyFile,
	})
	if err != nil {
		t.Fatalf("clone with SSHKeyPath: %v", err)
	}
	defer Cleanup(repoDir)

	got, err := os.ReadFile(keyFile) //nolint:gosec // test fixture
	if err != nil || string(got) != "dummy-private-key\n" {
		t.Fatalf("key file changed or unreadable: %q err=%v", got, err)
	}
}

// GIT_DIR is ignored by `git clone` (documented in git-clone(1)) but honored
// by the follow-up rev-parse, deterministically exercising the
// "resolve commit sha" failure path after a successful clone.
func TestCloneRevParseFailure(t *testing.T) {
	repo := initLocalRepo(t)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "nonexistent-gitdir"))

	repoDir, sha, err := Clone(context.Background(), Options{RepositoryURL: repo, Ref: "main"})
	if err == nil {
		defer Cleanup(repoDir)
		t.Fatal("expected rev-parse failure")
	}
	if !strings.Contains(err.Error(), "resolve commit sha") {
		t.Fatalf("unexpected error: %v", err)
	}
	if repoDir != "" || sha != "" {
		t.Fatalf("expected empty dir/sha on failure, got %q/%q", repoDir, sha)
	}
}

func TestCloneFailuresCleanUpWorkdir(t *testing.T) {
	repo := initLocalRepo(t)

	cases := []struct {
		name string
		opts Options
	}{
		{"nonexistent ref", Options{RepositoryURL: repo, Ref: "does-not-exist"}},
		{"bad url", Options{RepositoryURL: filepath.Join(t.TempDir(), "not-a-repo"), Ref: "main"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := hiveBuildDirs()
			repoDir, _, err := Clone(context.Background(), tc.opts)
			if err == nil {
				Cleanup(repoDir)
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "git [clone") {
				t.Fatalf("unexpected error: %v", err)
			}
			if repoDir != "" {
				t.Fatalf("expected empty dir on failure, got %q", repoDir)
			}
			if after := hiveBuildDirs(); after != before {
				t.Fatalf("workdir leak: before=%d after=%d (%v)", before, after, after)
			}
		})
	}
}

// hiveBuildDirs counts leftover hive-build-* directories in the temp dir.
func hiveBuildDirs() int {
	matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "hive-build-*"))
	return len(matches)
}

func TestCleanupEmptyPathNoop(t *testing.T) {
	Cleanup("")
}

func TestCloneMkdirTempFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-tmpdir")
	t.Setenv("TMPDIR", missing)
	repoDir, _, err := Clone(context.Background(), Options{RepositoryURL: "/tmp/x"})
	if err == nil {
		Cleanup(repoDir)
		t.Fatal("expected error when temp dir creation fails")
	}
	if repoDir != "" {
		t.Fatalf("expected empty dir on failure, got %q", repoDir)
	}
}

func TestCloneRequiresURL(t *testing.T) {
	if _, _, err := Clone(context.Background(), Options{}); err == nil {
		t.Fatal("expected error for empty repository URL")
	} else if !strings.Contains(err.Error(), "repository URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}
