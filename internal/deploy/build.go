package deploy

import (
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"
)

type BuildMethod string

const (
	BuildMethodDockerfile BuildMethod = "dockerfile"
	BuildMethodNixpacks   BuildMethod = "nixpacks"
)

type Builder struct {
	log *zap.SugaredLogger
}

func NewBuilder(log *zap.SugaredLogger) *Builder {
	return &Builder{log: log}
}

func (b *Builder) Build(method BuildMethod, contextDir, imageName string, cacheEnabled bool) error {
	switch method {
	case BuildMethodDockerfile:
		return b.buildDockerfile(contextDir, imageName, cacheEnabled)
	case BuildMethodNixpacks:
		return b.buildNixpacks(contextDir, imageName, cacheEnabled)
	default:
		return fmt.Errorf("unknown build method: %s", method)
	}
}

func (b *Builder) buildDockerfile(contextDir, imageName string, cacheEnabled bool) error {
	args := []string{"build", "-t", imageName}
	if cacheEnabled {
		args = append(args, "--build-arg", "BUILDKIT_INLINE_CACHE=1")
	}
	args = append(args, contextDir)
	cmd := exec.Command("docker", args...)
	if cacheEnabled {
		cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (b *Builder) buildNixpacks(contextDir, imageName string, cacheEnabled bool) error {
	args := []string{"build", contextDir, "--name", imageName}
	if cacheEnabled {
		args = append(args, "--build-arg", "BUILDKIT_INLINE_CACHE=1")
	}
	cmd := exec.Command("nixpacks", args...)
	if cacheEnabled {
		cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nixpacks build: %w", err)
	}
	return nil
}
