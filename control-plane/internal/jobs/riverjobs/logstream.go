package riverjobs

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

// ThrottledLogWriter is an io.Writer that bridges streamed build output
// (e.g. buildkit SolveStatus lines) into build_jobs.logs. Chunks are
// buffered and flushed to the database only when at least maxLines are
// pending or minInterval has elapsed since the previous flush — whichever
// comes first. Close performs a final flush.
type ThrottledLogWriter struct {
	queries     *dbgen.Queries
	buildID     string
	minInterval time.Duration
	maxLines    int

	mu        sync.Mutex
	buf       []string
	lastFlush time.Time
}

// NewThrottledLogWriter returns a writer that rate-limits log persistence.
func NewThrottledLogWriter(queries *dbgen.Queries, buildID string) *ThrottledLogWriter {
	return &ThrottledLogWriter{
		queries:     queries,
		buildID:     buildID,
		minInterval: 500 * time.Millisecond,
		maxLines:    20,
		lastFlush:   time.Now(),
	}
}

// Write implements io.Writer; input may contain partial or multiple lines.
func (w *ThrottledLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, line := range strings.Split(strings.TrimSuffix(string(p), "\n"), "\n") {
		if line == "" {
			continue
		}
		w.buf = append(w.buf, line)
	}
	return len(p), nil
}

// shouldFlushLocked reports whether the buffer must be persisted now.
func (w *ThrottledLogWriter) shouldFlushLocked(now time.Time) bool {
	if len(w.buf) == 0 {
		return false
	}
	return len(w.buf) >= w.maxLines || now.Sub(w.lastFlush) >= w.minInterval
}

// Flush persists buffered output if the throttle window allows it. now is
// injected so callers (and tests) control clock progression.
func (w *ThrottledLogWriter) Flush(ctx context.Context, now time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.shouldFlushLocked(now) {
		return nil
	}
	chunk := strings.Join(w.buf, "\n") + "\n"
	w.buf = nil
	w.lastFlush = now
	return w.queries.AppendBuildLog(ctx, dbgen.AppendBuildLogParams{BuildID: uuidOrNil(w.buildID), Chunk: chunk})
}

// Close flushes any buffered output regardless of throttle windows.
func (w *ThrottledLogWriter) Close(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) == 0 {
		return nil
	}
	chunk := strings.Join(w.buf, "\n") + "\n"
	w.buf = nil
	w.lastFlush = time.Now()
	return w.queries.AppendBuildLog(ctx, dbgen.AppendBuildLogParams{BuildID: uuidOrNil(w.buildID), Chunk: chunk})
}

func uuidOrNil(s string) pgtype.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}
