package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/luke/hive/agent/internal/auth"
	"github.com/luke/hive/agent/internal/docker"
	"github.com/luke/hive/agent/internal/hostmetrics"
	"github.com/luke/hive/agent/internal/server"
	"github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
)

var Version = "dev"

func main() {
	addr := getenv("AGENT_ADDR", ":9090")
	metricsAddr := getenv("AGENT_METRICS_ADDR", ":9091")
	dockerHost := getenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	hostRoot := getenv("HOST_ROOT", "")
	hostMgmt := os.Getenv("HOST_MGMT_ENABLED") == "true"
	controlPlaneURL := getenv("CONTROL_PLANE_URL", "http://control-plane:3000")
	certDir := getenv("AGENT_CERT_DIR", "/data/agent/certs")

	server.Version = Version

	// Docker client
	dockerOps, err := docker.NewClient(dockerHost)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = dockerOps.Close() }()

	// Get node ID from Docker Swarm info
	nodeID := os.Getenv("AGENT_NODE_ID")
	if nodeID == "" {
		infoCtx, infoCancel := context.WithTimeout(context.Background(), 10*time.Second)
		info, err := dockerOps.Info(infoCtx)
		infoCancel()
		if err != nil {
			log.Printf("warning: could not get docker info: %v", err)
			nodeID = "unknown"
		} else {
			nodeID = info.Swarm.NodeID
			if nodeID == "" {
				nodeID = info.ID
			}
		}
	}
	log.Printf("agent node ID: %s", safeLogValue(nodeID)) //nolint:gosec // nodeID is control-char-sanitized; sourced from env/Docker info, not attacker-controlled

	// Host metrics collector
	collector := hostmetrics.NewCollector(hostRoot, hostMgmt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(ctx)

	// Host operations executor
	executor := hostmetrics.NewExecutor(hostRoot, hostMgmt)

	// Prometheus metrics
	metrics := server.NewMetrics()

	// ConnectRPC service
	srv := server.New(dockerOps, collector, executor, metrics)

	mux := http.NewServeMux()
	path, handler := agentv1connect.NewAgentServiceHandler(srv)
	mux.Handle(path, handler)

	// By default the agent serves plaintext h2c on the encrypted hive_internal
	// overlay, which is how the control-plane dials it. mTLS is opt-in via
	// AGENT_MTLS_ENABLED=true; when enabled, a bootstrap failure is fatal
	// rather than a silent downgrade to unauthenticated plaintext.
	mtlsEnabled := os.Getenv("AGENT_MTLS_ENABLED") == "true"
	caFile := getenv("AGENT_CA_FILE", "/hive-ca/ca.pem")
	bootstrapToken := readSecret("/run/secrets/agent-bootstrap-token")
	if bootstrapToken == "" {
		bootstrapToken = os.Getenv("AGENT_BOOTSTRAP_TOKEN")
	}

	mainServer := startMainServer(ctx, addr, nodeID, controlPlaneURL, certDir, bootstrapToken, caFile, mtlsEnabled, mux, metrics)

	// Metrics + health server on separate port (always plaintext)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	metricsServer := &http.Server{
		Addr:              metricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("metrics listening on %s", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	log.Printf("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	_ = mainServer.Shutdown(shutdownCtx)
	_ = metricsServer.Shutdown(shutdownCtx)
	cancel()
}

// startMainServer serves plaintext h2c by default (intended for the encrypted
// hive_internal overlay). When mtlsEnabled is true it serves mTLS instead, and
// a bootstrap failure is fatal — it never silently downgrades to unauthenticated
func startMainServer(ctx context.Context, addr, nodeID, controlPlaneURL, certDir, bootstrapToken, caFile string, mtlsEnabled bool, handler http.Handler, metrics *server.Metrics) *http.Server {
	if mtlsEnabled {
		if bootstrapToken == "" || controlPlaneURL == "" {
			log.Fatal("AGENT_MTLS_ENABLED=true requires a bootstrap token and CONTROL_PLANE_URL")
		}
		cm := auth.NewCertManager(nodeID, controlPlaneURL, bootstrapToken, certDir, caFile)
		if !cm.LoadExisting() {
			log.Printf("bootstrapping mTLS certificates...")
			bootstrapCtx, bootstrapCancel := context.WithTimeout(ctx, 30*time.Second)
			err := cm.Bootstrap(bootstrapCtx)
			bootstrapCancel()
			if err != nil {
				log.Fatalf("mTLS bootstrap failed: %v", err)
			}
		}

		// Update cert expiry metric
		if !cm.CertExpiresAt().IsZero() {
			metrics.CertExpiryTimestamp.Set(float64(cm.CertExpiresAt().Unix()))
		}

		// Start renewal loop
		go cm.RunRenewalLoop(ctx)

		// Serve with mTLS
		srv := &http.Server{
			Addr:              addr,
			Handler:           handler,
			TLSConfig:         cm.TLSConfig(),
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			log.Printf("agent listening with mTLS on %s (node %s)", addr, safeLogValue(nodeID)) //nolint:gosec // nodeID is control-char-sanitized; sourced from env/Docker info, not attacker-controlled
			if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Printf("mTLS server error: %v", err)
			}
		}()
		return srv
	}

	return startPlaintext(addr, nodeID, handler)
}

// startPlaintext starts the ConnectRPC server with HTTP/2 cleartext (h2c) for streaming support.
func startPlaintext(addr, nodeID string, handler http.Handler) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Go 1.24+: h2c is expressed via Protocols instead of the
		// deprecated x/net/http2/h2c wrapper.
		Protocols: func() *http.Protocols {
			p := &http.Protocols{}
			p.SetHTTP1(true)
			p.SetUnencryptedHTTP2(true)
			return p
		}(),
	}
	go func() {
		log.Printf("agent listening on %s (node %s)", addr, safeLogValue(nodeID)) //nolint:gosec // nodeID is control-char-sanitized; sourced from env/Docker info, not attacker-controlled
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	return srv
}

func getenv(k, fallback string) string {
	v := os.Getenv(k)
	if v == "" {
		return fallback
	}
	return v
}

func readSecret(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec // path is a fixed Swarm secret mount path supplied by the caller
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// safeLogValue strips control characters so untrusted fields cannot forge log lines.
func safeLogValue(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
