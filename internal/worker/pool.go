package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/config"

	"go.uber.org/zap"
)

const maxJobRetries = 3

type Pool struct {
	nc    *nats.Conn
	cfg   *config.Config
	log   *zap.SugaredLogger
	store *store.Store
}

func NewPool(nc *nats.Conn, cfg *config.Config, db *store.Store, log *zap.SugaredLogger) *Pool {
	return &Pool{nc: nc, cfg: cfg, store: db, log: log}
}

func (p *Pool) Start(ctx context.Context) {
	p.log.Info("starting worker pool")

	p.subscribe("hive.build", p.withRetry("build", p.handleBuild))
	p.subscribe("hive.deploy", p.withRetry("deploy", p.handleDeploy))
	p.subscribe("hive.backup", p.withRetry("backup", p.handleBackup))
	p.subscribe("hive.cleanup", p.handleCleanup)
	p.subscribe("hive.health", p.handleHealth)
	p.subscribe("hive.maintenance", p.withRetry("maintenance", p.handleMaintenance))

	p.log.Info("worker pool subscribed to all subjects")
}

// withRetry wraps a NATS handler with bounded retry + exponential backoff.
// Non-retryable panics are caught and logged to a dead-letter subject.
func (p *Pool) withRetry(jobType string, handler nats.MsgHandler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		var payload map[string]interface{}
		_ = json.Unmarshal(msg.Data, &payload)

		attempt := 0
		if a, ok := payload["_attempt"]; ok {
			if af, ok := a.(float64); ok {
				attempt = int(af)
			}
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					p.log.Errorf("job %s panic (attempt %d/%d): %v\n%s", jobType, attempt+1, maxJobRetries, r, debug.Stack())
					p.deadLetter(jobType, msg.Data, fmt.Sprintf("panic: %v", r))
				}
			}()
			handler(msg)
		}()

		if attempt < maxJobRetries-1 {
			if errField, ok := payload["_error"]; ok && errField != nil {
				attempt++
				payload["_attempt"] = attempt
				retryData, _ := json.Marshal(payload)
				backoff := time.Duration(attempt) * 5 * time.Second
				p.log.Warnf("job %s failed (attempt %d/%d), retrying in %s", jobType, attempt, maxJobRetries, backoff)
				time.AfterFunc(backoff, func() {
					if err := p.nc.Publish(msg.Subject, retryData); err != nil {
						p.log.Errorf("job %s retry publish: %v", jobType, err)
					}
				})
			}
		}
	}
}

func (p *Pool) deadLetter(jobType string, data []byte, reason string) {
	dlq := fmt.Sprintf("hive.deadletter.%s", jobType)
	envelope := map[string]interface{}{
		"reason":    reason,
		"payload":   string(data),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(envelope)
	if err := p.nc.Publish(dlq, b); err != nil {
		p.log.Errorf("dead-letter publish %s: %v", dlq, err)
	}
	p.log.Warnf("job sent to dead-letter queue: %s (reason: %s)", dlq, reason)
}

func (p *Pool) subscribe(subject string, handler nats.MsgHandler) {
	_, err := p.nc.QueueSubscribe(subject, "hive-workers", handler)
	if err != nil {
		p.log.Errorf("failed to subscribe to %s: %v", subject, err)
	}
}
