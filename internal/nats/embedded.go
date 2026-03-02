package nats

import (
	"fmt"
	"path/filepath"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

func StartEmbedded(cfg *config.Config, log *zap.SugaredLogger) (*natsserver.Server, error) {
	storeDir := filepath.Join(cfg.DataDir, "nats")

	opts := &natsserver.Options{
		JetStream: true,
		StoreDir:  storeDir,
		NoLog:     !cfg.DevMode,
	}

	if cfg.MultiNode {
		opts.Host = "0.0.0.0"
		opts.Port = cfg.NATSPort
		log.Infof("NATS listening on 0.0.0.0:%d (multi-node)", cfg.NATSPort)
	} else {
		opts.DontListen = true
		log.Info("NATS running in-process (single-node)")
	}

	ns, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS server: %w", err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(10 * time.Second) {
		return nil, fmt.Errorf("NATS server not ready after 10s")
	}

	log.Info("embedded NATS server started with JetStream")
	return ns, nil
}

func Connect(ns *natsserver.Server, cfg *config.Config) (*nats.Conn, error) {
	var nc *nats.Conn
	var err error

	if cfg.MultiNode {
		nc, err = nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", cfg.NATSPort))
	} else {
		nc, err = nats.Connect(nats.DefaultURL, nats.InProcessServer(ns))
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to embedded NATS: %w", err)
	}
	return nc, nil
}

func ConnectExternal(cfg *config.Config) (*nats.Conn, error) {
	if cfg.NATSManagerURL == "" {
		return nil, fmt.Errorf("HIVE_NATS_URL is required in worker mode")
	}
	nc, err := nats.Connect(cfg.NATSManagerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to manager NATS at %s: %w", cfg.NATSManagerURL, err)
	}
	return nc, nil
}
