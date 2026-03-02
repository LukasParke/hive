package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HIVE_ROLE", "HIVE_DEV", "HIVE_DATA_DIR", "HIVE_API_PORT",
		"HIVE_UI_PORT", "HIVE_UI_DIR", "HIVE_NATS_PORT", "DATABASE_URL",
		"HIVE_NATS_URL", "HIVE_AUTH_URL", "DOCKER_HOST",
	} {
		os.Unsetenv(key)
	}
}

func TestLoadDefaults(t *testing.T) {
	clearEnv(t)
	cfg := Load()

	assert.Equal(t, RoleManager, cfg.Role)
	assert.False(t, cfg.DevMode)
	assert.Equal(t, "/data", cfg.DataDir)
	assert.Equal(t, 8080, cfg.APIPort)
	assert.Equal(t, 3000, cfg.UIPort)
	assert.Equal(t, "/app/ui", cfg.UIDir)
	assert.Equal(t, 4222, cfg.NATSPort)
	assert.Equal(t, "", cfg.DatabaseURL)
	assert.Equal(t, "", cfg.NATSManagerURL)
	assert.Equal(t, "http://127.0.0.1:3000", cfg.AuthBaseURL)
	assert.Equal(t, "unix:///var/run/docker.sock", cfg.DockerSocket)
	assert.False(t, cfg.MultiNode)
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("HIVE_ROLE", "worker")
	t.Setenv("HIVE_DEV", "1")
	t.Setenv("HIVE_DATA_DIR", "/custom/data")
	t.Setenv("HIVE_API_PORT", "9090")
	t.Setenv("HIVE_UI_PORT", "4000")
	t.Setenv("HIVE_UI_DIR", "/custom/ui")
	t.Setenv("HIVE_NATS_PORT", "5222")
	t.Setenv("DATABASE_URL", "postgres://localhost/hive")
	t.Setenv("HIVE_NATS_URL", "nats://manager:4222")
	t.Setenv("HIVE_AUTH_URL", "http://auth:3000")
	t.Setenv("DOCKER_HOST", "tcp://docker:2375")

	cfg := Load()

	assert.Equal(t, RoleWorker, cfg.Role)
	assert.True(t, cfg.DevMode)
	assert.Equal(t, "/custom/data", cfg.DataDir)
	assert.Equal(t, 9090, cfg.APIPort)
	assert.Equal(t, 4000, cfg.UIPort)
	assert.Equal(t, "/custom/ui", cfg.UIDir)
	assert.Equal(t, 5222, cfg.NATSPort)
	assert.Equal(t, "postgres://localhost/hive", cfg.DatabaseURL)
	assert.Equal(t, "nats://manager:4222", cfg.NATSManagerURL)
	assert.Equal(t, "http://auth:3000", cfg.AuthBaseURL)
	assert.Equal(t, "tcp://docker:2375", cfg.DockerSocket)
}

func TestLoadPartialEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("HIVE_API_PORT", "7070")

	cfg := Load()

	assert.Equal(t, 7070, cfg.APIPort)
	assert.Equal(t, RoleManager, cfg.Role)
	assert.Equal(t, "/data", cfg.DataDir)
}

func TestLoadInvalidIntFallsBack(t *testing.T) {
	clearEnv(t)
	t.Setenv("HIVE_API_PORT", "not-a-number")

	cfg := Load()

	assert.Equal(t, 8080, cfg.APIPort)
}
