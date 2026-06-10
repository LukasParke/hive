package config

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
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
	cfg := Config{
		DatabaseURL:       getenvOrFile("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/hive?sslmode=disable"),
		HTTPAddr:          getenv("HTTP_ADDR", ":3000"),
		DockerHost:        getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		BuildkitAddr:      getenv("BUILDKIT_ADDR", "tcp://buildkit:1234"),
		RegistryAddr:      getenv("REGISTRY_ADDR", "registry:5000"),
		MasterKeyFile:     getenv("MASTER_KEY_FILE", "/run/secrets/hive-master-key"),
		AgentBootstrapKey: getenvOrFile("AGENT_BOOTSTRAP_TOKEN", ""),
		JWTSecret:         getenvOrFile("JWT_SECRET", "dev-jwt-secret-change-me"),
	}
	if pw := fileValue("DATABASE_PASSWORD_FILE"); pw != "" {
		cfg.DatabaseURL = urlWithPassword(cfg.DatabaseURL, pw)
	}
	return cfg
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

// getenvOrFile resolves a value from <key>_FILE (a path to a file holding the
// value, e.g. a Docker secret mount) if set, falling back to the plain env var.
func getenvOrFile(key, fallback string) string {
	if v := fileValue(key + "_FILE"); v != "" {
		return v
	}
	return getenv(key, fallback)
}

// fileValue reads and trims the file named by the env var key, if set.
// A configured-but-unreadable file is fatal: silently falling back would
// start the server with wrong or default credentials.
func fileValue(key string) string {
	path := os.Getenv(key)
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("config: reading %s (%s): %v", key, path, err)
	}
	return strings.TrimSpace(string(b))
}

// urlWithPassword returns dbURL with its password replaced.
func urlWithPassword(dbURL, password string) string {
	u, err := url.Parse(dbURL)
	if err != nil {
		return dbURL
	}
	user := "postgres"
	if u.User != nil {
		user = u.User.Username()
	}
	u.User = url.UserPassword(user, password)
	return u.String()
}
