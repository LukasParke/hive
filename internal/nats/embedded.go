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

func resilientOpts(log *zap.SugaredLogger) []nats.Option {
	return []nats.Option{
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
		nats.ReconnectBufSize(16 * 1024 * 1024),
		nats.PingInterval(10 * time.Second),
		nats.MaxPingsOutstanding(3),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warnf("NATS disconnected: %v", err)
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Infof("NATS reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			log.Errorf("NATS async error: %v", err)
		}),
	}
}

func Connect(ns *natsserver.Server, cfg *config.Config, log *zap.SugaredLogger) (*nats.Conn, error) {
	var nc *nats.Conn
	var err error

	opts := resilientOpts(log)

	if cfg.MultiNode {
		nc, err = nats.Connect(fmt.Sprintf("nats://127.0.0.1:%d", cfg.NATSPort), opts...)
	} else {
		opts = append(opts, nats.InProcessServer(ns))
		nc, err = nats.Connect(nats.DefaultURL, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect to embedded NATS: %w", err)
	}
	return nc, nil
}

func ConnectExternal(cfg *config.Config, logs ...*zap.SugaredLogger) (*nats.Conn, error) {
	if cfg.NATSManagerURL == "" {
		return nil, fmt.Errorf("HIVE_NATS_URL is required in worker mode")
	}

	var log *zap.SugaredLogger
	if len(logs) > 0 && logs[0] != nil {
		log = logs[0]
	}

	var opts []nats.Option
	if log != nil {
		opts = resilientOpts(log)
	} else {
		opts = []nats.Option{
			nats.RetryOnFailedConnect(true),
			nats.MaxReconnects(-1),
			nats.ReconnectWait(2 * time.Second),
			nats.ReconnectBufSize(16 * 1024 * 1024),
			nats.PingInterval(10 * time.Second),
			nats.MaxPingsOutstanding(3),
		}
	}

	nc, err := nats.Connect(cfg.NATSManagerURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to manager NATS at %s: %w", cfg.NATSManagerURL, err)
	}
	return nc, nil
}
