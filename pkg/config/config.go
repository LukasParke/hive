package config

import (
	"os"
	"strconv"
)

type Role string

const (
	RoleManager Role = "manager"
	RoleWorker  Role = "worker"
	RoleAgent   Role = "agent"
)

type Config struct {
	Role     Role
	DevMode  bool
	DataDir  string
	APIPort  int
	UIPort   int // SvelteKit server port (internal, proxied by Go)
	UIDir    string // pre-built frontend assets directory
	NATSPort int

	// Postgres (set after bootstrap or by env for external DB)
	DatabaseURL string

	// Manager NATS address (used by workers)
	NATSManagerURL string

	// BetterAuth base URL for session validation
	AuthBaseURL string

	// Docker socket path
	DockerSocket string

	// Multi-node: number of nodes detected triggers registry deploy
	MultiNode bool

	// Cloudflare integration
	CFAPIToken   string
	CFTunnelToken string
	CFZoneID     string
	IngressMode  string // "port_forward", "cloudflare_tunnel", "both"

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
}

func Load() *Config {
	cfg := &Config{
		Role:           Role(getEnv("HIVE_ROLE", "manager")),
		DevMode:        getEnv("HIVE_DEV", "") != "",
		DataDir:        getEnv("HIVE_DATA_DIR", "/data"),
		APIPort:        getEnvInt("HIVE_API_PORT", 8080),
		UIPort:         getEnvInt("HIVE_UI_PORT", 3000),
		UIDir:          getEnv("HIVE_UI_DIR", "/app/ui"),
		NATSPort:       getEnvInt("HIVE_NATS_PORT", 4222),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
		NATSManagerURL: getEnv("HIVE_NATS_URL", ""),
		AuthBaseURL:    getEnv("HIVE_AUTH_URL", "http://127.0.0.1:3000"),
		DockerSocket:    getEnv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		MultiNode:       false,
		CFAPIToken:      getEnv("HIVE_CF_API_TOKEN", ""),
		CFTunnelToken:   getEnv("HIVE_CF_TUNNEL_TOKEN", ""),
		CFZoneID:        getEnv("HIVE_CF_ZONE_ID", ""),
		IngressMode:     getEnv("HIVE_INGRESS_MODE", "port_forward"),
		RegistryDomain:   getEnv("HIVE_REGISTRY_DOMAIN", "registry.hive.local"),
		RegistryInsecure: getEnv("HIVE_REGISTRY_INSECURE", "true") == "true",
		AgentInterval:   getEnvInt("HIVE_AGENT_INTERVAL", 10),
		AllowedOrigins:  getEnv("HIVE_ALLOWED_ORIGINS", ""),
		WebhookBaseURL:  getEnv("HIVE_WEBHOOK_BASE_URL", "http://localhost:8080"),
		ManagedService:  getEnv("HIVE_MANAGED", "") == "true",
		HiveImage:       getEnv("HIVE_IMAGE", "ghcr.io/lholliger/hive:latest"),
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
