package config

import (
	"os"
	"strconv"
)

type Role string

const (
	RoleAgent Role = "agent"
)

type Config struct {
	Role    Role
	DevMode bool
	DataDir string
	APIPort int
	NATSPort int

	// Durable backup storage directory (should be a persistent volume mount)
	BackupDir string

	// Postgres (set after bootstrap or by env for external DB)
	DatabaseURL string

	// Manager NATS address (used by workers)
	NATSManagerURL string

	// Docker socket path
	DockerSocket string

	// Multi-node: number of nodes detected triggers registry deploy
	MultiNode bool

	// Cloudflare integration
	CFAPIToken    string
	CFTunnelToken string
	CFZoneID      string
	IngressMode   string // "port_forward", "cloudflare_tunnel", "both"

	// Registry
	RegistryDomain   string
	RegistryInsecure bool

	// Agent
	AgentInterval int // seconds between metrics collections

	// CORS
	AllowedOrigins string

	// Webhook base URL for git provider callbacks (e.g. https://hive.example.com)
	WebhookBaseURL string

	// When true, Hive is running as a Swarm-managed service (not the initial launcher)
	ManagedService bool

	// Docker image reference for self-deployment and agent deployment
	HiveImage string

	// Prometheus URL for metrics queries
	PrometheusURL string

	// Log level override: debug, info, warn, error (default: info, dev mode forces debug)
	LogLevel string

	// When true, session cookies are set with the Secure flag (HTTPS only).
	// Defaults to true in production, false in dev mode.
	SecureCookies bool
}

func Load() *Config {
	dataDir := getEnv("HIVE_DATA_DIR", "/data")
	cfg := &Config{
		Role:             Role(getEnv("HIVE_ROLE", "manager")),
		DevMode:          getEnv("HIVE_DEV", "") != "",
		DataDir:          dataDir,
		BackupDir:        getEnv("HIVE_BACKUP_DIR", dataDir+"/backups"),
		APIPort:          getEnvInt("HIVE_API_PORT", 8080),
		NATSPort:         getEnvInt("HIVE_NATS_PORT", 4222),
		DatabaseURL:      getEnv("DATABASE_URL", ""),
		NATSManagerURL:   getEnv("HIVE_NATS_URL", "nats://hive-nats:4222"),
		DockerSocket:     getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		MultiNode:        false,
		CFAPIToken:       getEnv("HIVE_CF_API_TOKEN", ""),
		CFTunnelToken:    getEnv("HIVE_CF_TUNNEL_TOKEN", ""),
		CFZoneID:         getEnv("HIVE_CF_ZONE_ID", ""),
		IngressMode:      getEnv("HIVE_INGRESS_MODE", "port_forward"),
		RegistryDomain:   getEnv("HIVE_REGISTRY_DOMAIN", "registry.hive.local"),
		RegistryInsecure: getEnv("HIVE_REGISTRY_INSECURE", "true") == "true",
		AgentInterval:    getEnvInt("HIVE_AGENT_INTERVAL", 2),
		AllowedOrigins:   getEnv("HIVE_ALLOWED_ORIGINS", ""),
		WebhookBaseURL:   getEnv("HIVE_WEBHOOK_BASE_URL", "http://localhost:8080"),
		ManagedService:   getEnv("HIVE_MANAGED", "") == "true",
		HiveImage:        getEnv("HIVE_IMAGE", "127.0.0.1:5000/hive:latest"),
		PrometheusURL:    getEnv("PROMETHEUS_URL", "http://hive-prometheus:9090"),
		LogLevel:         getEnv("HIVE_LOG_LEVEL", "info"),
		SecureCookies:    getEnv("HIVE_SECURE_COOKIES", "") != "false",
	}
	if cfg.DevMode {
		cfg.SecureCookies = false
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
