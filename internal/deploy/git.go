package deploy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"
)

type GitBuilder struct {
	workDir string
	log     *zap.SugaredLogger
}

func NewGitBuilder(workDir string, log *zap.SugaredLogger) *GitBuilder {
	return &GitBuilder{workDir: workDir, log: log}
}

func (g *GitBuilder) CloneAndBuild(repo, branch, dockerfile, imageName string) error {
	cloneDir := filepath.Join(g.workDir, "build", imageName)
	os.MkdirAll(cloneDir, 0755)

	g.log.Infof("cloning %s branch %s", repo, branch)
	cmd := exec.Command("git", "clone", "--depth=1", "--branch", branch, repo, cloneDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	g.log.Infof("building image %s from %s", imageName, dockerfile)
	buildCmd := exec.Command("docker", "build", "-t", imageName, "-f", filepath.Join(cloneDir, dockerfile), cloneDir)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	os.RemoveAll(cloneDir)
	return nil
}
