package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL       string
	HTTPAddr          string
	DockerHost        string
	BuildkitAddr      string
	RegistryAddr      string
	MasterKeyFile     string
	AgentBootstrapKey string
	JWTSecret         string
}

func Load() Config {
	return Config{
		DatabaseURL:       getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/hive?sslmode=disable"),
		HTTPAddr:          getenv("HTTP_ADDR", ":3000"),
		DockerHost:        getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		BuildkitAddr:      getenv("BUILDKIT_ADDR", "tcp://buildkit:1234"),
		RegistryAddr:      getenv("REGISTRY_ADDR", "registry:5000"),
		MasterKeyFile:     getenv("MASTER_KEY_FILE", "/run/secrets/dokploy-master-key"),
		AgentBootstrapKey: getenv("AGENT_BOOTSTRAP_TOKEN", ""),
		JWTSecret:         getenv("JWT_SECRET", "dev-jwt-secret-change-me"),
	}
}

// Validate returns an error if required production configuration is missing or insecure.
func (c Config) Validate() error {
	var errs []error
	if c.JWTSecret == "" || c.JWTSecret == "dev-jwt-secret-change-me" {
		errs = append(errs, errors.New("JWT_SECRET must be set to a strong secret (min 32 chars)"))
	}
	if c.AgentBootstrapKey == "" {
		errs = append(errs, errors.New("AGENT_BOOTSTRAP_TOKEN must be set"))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL must be set"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %w", errors.Join(errs...))
	}
	return nil
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
