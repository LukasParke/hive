package monitor

import (
	"context"
	"encoding/binary"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"

	"github.com/lholliger/hive/internal/store"

	"go.uber.org/zap"
)

type LogCollector struct {
	docker   *client.Client
	store    *store.Store
	log      *zap.SugaredLogger
	lastPoll map[string]time.Time
}

func NewLogCollector(db *store.Store, log *zap.SugaredLogger) (*LogCollector, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &LogCollector{
		docker:   docker,
		store:    db,
		log:      log,
		lastPoll: make(map[string]time.Time),
	}, nil
}

func (lc *LogCollector) Run(ctx context.Context, interval time.Duration) {
	lc.collect(ctx, interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lc.collect(ctx, interval)
		}
	}
}

func (lc *LogCollector) CollectOnce(ctx context.Context) error {
	lc.collect(ctx, 10*time.Second)
	return nil
}

func (lc *LogCollector) collect(ctx context.Context, defaultLookback time.Duration) {
	services, err := lc.docker.ServiceList(ctx, swarm.ServiceListOptions{})
	if err != nil {
		lc.log.Warnf("log collector: list services: %v", err)
		return
	}

	for _, svc := range services {
		if ctx.Err() != nil {
			return
		}

		svcName := svc.Spec.Name
		since := lc.lastPoll[svc.ID]
		if since.IsZero() {
			since = time.Now().Add(-defaultLookback)
		}

		reader, err := lc.docker.ServiceLogs(ctx, svc.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Timestamps: true,
			Since:      since.Format(time.RFC3339Nano),
		})
		if err != nil {
			lc.log.Debugf("log collector: %s: %v", svcName, err)
			continue
		}

		entries := parseLogStream(reader, svcName)
		_ = reader.Close()

		if len(entries) > 0 {
			if err := lc.store.InsertLogEntries(ctx, entries); err != nil {
				lc.log.Warnf("log collector: insert %d entries for %s: %v", len(entries), svcName, err)
			}
		}

		lc.lastPoll[svc.ID] = time.Now()
	}
}

func parseLogStream(reader io.Reader, serviceName string) []store.LogEntry {
	appID := "system"
	if !strings.HasPrefix(serviceName, "hive-") {
		appID = serviceName
	}

	var entries []store.LogEntry
	hdr := make([]byte, 8)

	for {
		if _, err := io.ReadFull(reader, hdr); err != nil {
			break
		}

		streamType := "stdout"
		if hdr[0] == 2 {
			streamType = "stderr"
		}

		size := binary.BigEndian.Uint32(hdr[4:8])
		if size == 0 {
			continue
		}
		if size > 1<<20 {
			break
		}

		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			break
		}

		for _, line := range strings.Split(strings.TrimRight(string(payload), "\n\r"), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" {
				continue
			}

			ts, msg := parseTimestampedLine(line)
			level := classifyLogLevel(msg, streamType)

			entries = append(entries, store.LogEntry{
				AppID:       appID,
				ServiceName: serviceName,
				Stream:      streamType,
				Message:     msg,
				Level:       level,
				Timestamp:   ts,
			})
		}
	}

	return entries
}

func parseTimestampedLine(line string) (time.Time, string) {
	if len(line) > 30 && line[4] == '-' && line[10] == 'T' {
		if spaceIdx := strings.IndexByte(line, ' '); spaceIdx > 0 {
			if ts, err := time.Parse(time.RFC3339Nano, line[:spaceIdx]); err == nil {
				return ts, line[spaceIdx+1:]
			}
		}
	}
	return time.Now(), line
}

func classifyLogLevel(msg, stream string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return "error"
	case strings.Contains(lower, "error"):
		return "error"
	case strings.Contains(lower, "warn"):
		return "warn"
	case strings.Contains(lower, "debug"):
		return "debug"
	case stream == "stderr":
		return "warn"
	default:
		return "info"
	}
}
