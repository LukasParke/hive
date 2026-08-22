package config

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds all control-plane runtime configuration from the environment.
type Config struct {
	DatabaseURL string
	// ListenDatabaseURL optionally points at Postgres DIRECTLY (bypassing
	// PgBouncer) for the dedicated LISTEN/NOTIFY connection: PgBouncer in
	// transaction pooling mode does not support session-level LISTEN.
	// Empty means "use DatabaseURL" (single-node/dev setups).
	ListenDatabaseURL string
	HTTPAddr          string
	DockerHost        string
	BuildkitAddr      string
	RegistryAddr      string
	MasterKeyFile     string
	AgentBootstrapKey string
	JWTSecret         string
	// AgentMTLSEnabled mirrors the agent's AGENT_MTLS_ENABLED: when true the
	// control-plane dials agents over mTLS instead of plaintext.
	AgentMTLSEnabled bool
	// Public-endpoint rate limits (requests/min/IP). Zero = built-in prod
	// defaults; CI/test deployments raise them via env.
	AuthRateLimitPerMin    int
	WebhookRateLimitPerMin int
}

// Load reads configuration from environment variables (with _FILE variants
// for secrets) and applies defaults.
func Load() Config {
	cfg := Config{
		DatabaseURL:            getenvOrFile("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/hive?sslmode=disable"),
		ListenDatabaseURL:      getenvOrFile("HIVE_LISTEN_URL", ""),
		HTTPAddr:               getenv("HTTP_ADDR", ":3000"),
		DockerHost:             getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		BuildkitAddr:           getenv("BUILDKIT_ADDR", "tcp://buildkit:1234"),
		RegistryAddr:           getenv("REGISTRY_ADDR", "registry:5000"),
		MasterKeyFile:          getenv("MASTER_KEY_FILE", "/run/secrets/hive-master-key"),
		AgentBootstrapKey:      getenvOrFile("AGENT_BOOTSTRAP_TOKEN", ""),
		JWTSecret:              getenvOrFile("JWT_SECRET", "dev-jwt-secret-change-me"),
		AgentMTLSEnabled:       os.Getenv("AGENT_MTLS_ENABLED") == "true",
		AuthRateLimitPerMin:    atoiDefault(os.Getenv("AUTH_RATE_LIMIT_PER_MIN"), 0),
		WebhookRateLimitPerMin: atoiDefault(os.Getenv("WEBHOOK_RATE_LIMIT_PER_MIN"), 0),
	}
	if pw := fileValue("DATABASE_PASSWORD_FILE"); pw != "" {
		cfg.DatabaseURL = urlWithPassword(cfg.DatabaseURL, pw)
		cfg.ListenDatabaseURL = urlWithPassword(cfg.ListenDatabaseURL, pw)
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
	b, err := os.ReadFile(path) //nolint:gosec // path comes from the operator's own env var by design
	if err != nil {
		log.Fatalf("config: reading %s (%s): %v", key, path, err) //nolint:gosec // operator-supplied env path echoed in a fatal startup log
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

// atoiDefault parses s as an int, returning def when empty or malformed.
func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
