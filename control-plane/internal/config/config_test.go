package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadListenDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://postgres@pgbouncer:6432/hive?sslmode=disable")
	t.Setenv("HIVE_LISTEN_URL", "postgres://postgres@postgres:5432/hive?sslmode=disable")
	cfg := Load()
	if cfg.ListenDatabaseURL != "postgres://postgres@postgres:5432/hive?sslmode=disable" {
		t.Fatalf("ListenDatabaseURL = %q", cfg.ListenDatabaseURL)
	}
}

func TestLoadListenDatabaseURLDefaultsToEmpty(t *testing.T) {
	t.Setenv("HIVE_LISTEN_URL", "")
	cfg := Load()
	if cfg.ListenDatabaseURL != "" {
		t.Fatalf("expected empty ListenDatabaseURL, got %q", cfg.ListenDatabaseURL)
	}
}

func TestLoadPasswordFileAppliesToBothURLs(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://postgres@pgbouncer:6432/hive?sslmode=disable")
	t.Setenv("HIVE_LISTEN_URL", "postgres://postgres@postgres:5432/hive?sslmode=disable")
	t.Setenv("DATABASE_PASSWORD_FILE", pwFile)
	cfg := Load()
	if !strings.Contains(cfg.DatabaseURL, "s3cret") {
		t.Fatalf("pool URL missing password: %q", cfg.DatabaseURL)
	}
	if !strings.Contains(cfg.ListenDatabaseURL, "s3cret") {
		t.Fatalf("listen URL missing password: %q", cfg.ListenDatabaseURL)
	}
}

// unset clears an env var for the duration of the test so defaults are
// observable regardless of the host environment.
func unset(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	unset(t,
		"HTTP_ADDR", "DOCKER_HOST", "BUILDKIT_ADDR", "REGISTRY_ADDR",
		"MASTER_KEY_FILE", "AGENT_MTLS_ENABLED",
		"AUTH_RATE_LIMIT_PER_MIN", "WEBHOOK_RATE_LIMIT_PER_MIN",
		"JWT_SECRET_FILE", "JWT_SECRET", "AGENT_BOOTSTRAP_TOKEN_FILE", "AGENT_BOOTSTRAP_TOKEN",
		"DATABASE_URL_FILE", "DATABASE_PASSWORD_FILE",
	)
	cfg := Load()
	if cfg.HTTPAddr != ":3000" {
		t.Errorf("HTTPAddr = %q, want :3000", cfg.HTTPAddr)
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("DockerHost = %q", cfg.DockerHost)
	}
	if cfg.BuildkitAddr != "tcp://buildkit:1234" {
		t.Errorf("BuildkitAddr = %q", cfg.BuildkitAddr)
	}
	if cfg.RegistryAddr != "registry:5000" {
		t.Errorf("RegistryAddr = %q", cfg.RegistryAddr)
	}
	if cfg.MasterKeyFile != "/run/secrets/hive-master-key" {
		t.Errorf("MasterKeyFile = %q", cfg.MasterKeyFile)
	}
	if cfg.JWTSecret != "dev-jwt-secret-change-me" {
		t.Errorf("JWTSecret default = %q", cfg.JWTSecret)
	}
	if cfg.AgentMTLSEnabled {
		t.Error("AgentMTLSEnabled should default to false")
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("DOCKER_HOST", "tcp://docker:2375")
	t.Setenv("BUILDKIT_ADDR", "tcp://bk:1234")
	t.Setenv("REGISTRY_ADDR", "reg:5000")
	t.Setenv("MASTER_KEY_FILE", "/tmp/key")
	t.Setenv("AGENT_MTLS_ENABLED", "true")
	t.Setenv("AUTH_RATE_LIMIT_PER_MIN", " 42 ")
	t.Setenv("WEBHOOK_RATE_LIMIT_PER_MIN", "99")
	cfg := Load()
	if cfg.HTTPAddr != ":8080" || cfg.DockerHost != "tcp://docker:2375" ||
		cfg.BuildkitAddr != "tcp://bk:1234" || cfg.RegistryAddr != "reg:5000" ||
		cfg.MasterKeyFile != "/tmp/key" {
		t.Fatalf("override mismatch: %+v", cfg)
	}
	if !cfg.AgentMTLSEnabled {
		t.Error("AGENT_MTLS_ENABLED=true not honored")
	}
	if cfg.AuthRateLimitPerMin != 42 || cfg.WebhookRateLimitPerMin != 99 {
		t.Errorf("rate limits = %d/%d, want 42/99", cfg.AuthRateLimitPerMin, cfg.WebhookRateLimitPerMin)
	}
}

func TestLoadMTLSEnabledRequiresExactTrue(t *testing.T) {
	t.Setenv("AGENT_MTLS_ENABLED", "TRUE")
	if cfg := Load(); cfg.AgentMTLSEnabled {
		t.Error("\"TRUE\" must not enable mtls (comparison is case-sensitive)")
	}
	t.Setenv("AGENT_MTLS_ENABLED", "1")
	if cfg := Load(); cfg.AgentMTLSEnabled {
		t.Error("\"1\" must not enable mtls")
	}
}

func TestLoadRateLimitGarbageFallsBackToDefault(t *testing.T) {
	for _, garbage := range []string{"abc", "12x", "-  3", "999999999999999999999"} {
		t.Setenv("AUTH_RATE_LIMIT_PER_MIN", garbage)
		if cfg := Load(); cfg.AuthRateLimitPerMin != 0 {
			t.Errorf("atoiDefault(%q) = %d, want fallback 0", garbage, cfg.AuthRateLimitPerMin)
		}
	}
}

func TestLoadFileVariants(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	jwtFile := write("jwt", "jwt-from-file\n")
	tokenFile := write("token", "  token-from-file  \n")
	urlFile := write("url", "postgres://fileuser@from-file:5432/db\n")

	unset(t, "DATABASE_PASSWORD_FILE")
	t.Setenv("JWT_SECRET_FILE", jwtFile)
	t.Setenv("AGENT_BOOTSTRAP_TOKEN_FILE", tokenFile)
	t.Setenv("DATABASE_URL_FILE", urlFile)

	cfg := Load()
	if cfg.JWTSecret != "jwt-from-file" {
		t.Errorf("JWTSecret = %q, want file value", cfg.JWTSecret)
	}
	if cfg.AgentBootstrapKey != "token-from-file" {
		t.Errorf("AgentBootstrapKey = %q, want trimmed file value", cfg.AgentBootstrapKey)
	}
	if cfg.DatabaseURL != "postgres://fileuser@from-file:5432/db" {
		t.Errorf("DatabaseURL = %q, want file value", cfg.DatabaseURL)
	}

	// The plain env var loses to the _FILE variant.
	t.Setenv("JWT_SECRET", "plain-wins-not")
	if cfg := Load(); cfg.JWTSecret != "jwt-from-file" {
		t.Errorf("_FILE variant must take precedence over plain env var, got %q", cfg.JWTSecret)
	}
}

func TestURLWithPasswordEdges(t *testing.T) {
	got := urlWithPassword("postgres://bob@pg:5432/hive?sslmode=disable", "")
	if !strings.Contains(got, "bob:") {
		t.Errorf("existing username not preserved: %q", got)
	}
	if !strings.HasPrefix(got, "postgres://bob:@pg:5432") {
		t.Errorf("empty password not applied: %q", got)
	}

	// No userinfo at all: user defaults to postgres.
	got = urlWithPassword("postgres://pg:5432/hive", "pw")
	if !strings.HasPrefix(got, "postgres://postgres:pw@pg:5432") {
		t.Errorf("default user missing: %q", got)
	}

	// Unparseable URL is returned unchanged.
	bad := "postgres://%zz@pg:5432/hive"
	if got := urlWithPassword(bad, "pw"); got != bad {
		t.Errorf("unparseable URL modified: %q", got)
	}
}

func TestValidate(t *testing.T) {
	valid := Config{
		JWTSecret:         strings.Repeat("x", 32),
		AgentBootstrapKey: "boot",
		DatabaseURL:       "postgres://localhost/hive",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	// Default dev secret is rejected even though non-empty.
	weak := valid
	weak.JWTSecret = "dev-jwt-secret-change-me"
	if err := weak.Validate(); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("dev JWT secret accepted: %v", err)
	}

	// Each missing field alone produces its own error.
	for _, mutate := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"no jwt", func(c *Config) { c.JWTSecret = "" }, "JWT_SECRET"},
		{"no token", func(c *Config) { c.AgentBootstrapKey = "" }, "AGENT_BOOTSTRAP_TOKEN"},
		{"no db url", func(c *Config) { c.DatabaseURL = "" }, "DATABASE_URL"},
	} {
		bad := valid
		mutate.mutate(&bad)
		err := bad.Validate()
		if err == nil || !strings.Contains(err.Error(), mutate.want) {
			t.Errorf("%s: want error mentioning %s, got %v", mutate.name, mutate.want, err)
		}
	}

	// All failures aggregate into one error listing every problem.
	empty := Config{}
	err := empty.Validate()
	if err == nil {
		t.Fatal("empty config must fail validation")
	}
	for _, want := range []string{"JWT_SECRET", "AGENT_BOOTSTRAP_TOKEN", "DATABASE_URL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("aggregated error missing %s: %v", want, err)
		}
	}
}
