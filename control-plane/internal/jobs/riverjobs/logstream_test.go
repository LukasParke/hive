package riverjobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

// fakeDBTX records Exec calls so the real ThrottledLogWriter can be
// exercised without a database.
type fakeDBTX struct {
	execStatements []string
	execArgs       [][]any
}

func (f *fakeDBTX) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execStatements = append(f.execStatements, sql)
	f.execArgs = append(f.execArgs, args)
	return pgconn.CommandTag{}, nil
}

func (f *fakeDBTX) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeDBTX) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

// chunks extracts the appended chunk from the recorded AppendBuildLog
// exec calls (the chunk is the first argument).
func (f *fakeDBTX) chunks() []string {
	var out []string
	for _, args := range f.execArgs {
		if len(args) > 0 {
			if s, ok := args[0].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func TestThrottledLogWriterFlushesOnLineThreshold(t *testing.T) {
	fake := &fakeDBTX{}
	w := NewThrottledLogWriter(dbgen.New(fake), "00000000-0000-0000-0000-000000000001")
	base := time.Unix(0, 0)

	for range 20 { // maxLines threshold reached → flush despite frozen clock
		if _, err := w.Write([]byte("line\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Flush(context.Background(), base); err != nil {
		t.Fatalf("flush: %v", err)
	}
	chunks := fake.chunks()
	if len(chunks) != 1 {
		t.Fatalf("expected exactly 1 flush at line threshold, got %d", len(chunks))
	}
	if got := strings.Count(chunks[0], "line"); got != 20 {
		t.Fatalf("flushed chunk should contain 20 lines, got %d", got)
	}
}

func TestThrottledLogWriterHoldsFlushInsideWindow(t *testing.T) {
	fake := &fakeDBTX{}
	w := NewThrottledLogWriter(dbgen.New(fake), "00000000-0000-0000-0000-000000000001")
	start := time.Now()
	w.lastFlush = start

	for range 5 { // below the 20-line threshold
		if _, err := w.Write([]byte("early\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Flush(context.Background(), start.Add(100*time.Millisecond)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := fake.chunks(); len(got) != 0 {
		t.Fatalf("flush fired inside 500ms window with %d lines buffered", 5)
	}
}
func TestThrottledLogWriterFlushesWhenIntervalElapses(t *testing.T) {
	fake := &fakeDBTX{}
	w := NewThrottledLogWriter(dbgen.New(fake), "00000000-0000-0000-0000-000000000001")
	start := time.Now()
	w.lastFlush = start

	for range 5 {
		if _, err := w.Write([]byte("early\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Flush(context.Background(), start.Add(500*time.Millisecond)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	chunks := fake.chunks()
	if len(chunks) != 1 || !strings.Contains(chunks[0], "early") {
		t.Fatalf("interval flush missing buffered output: %#v", chunks)
	}
}
func TestThrottledLogWriterCloseFlushesRemaining(t *testing.T) {
	fake := &fakeDBTX{}
	w := NewThrottledLogWriter(dbgen.New(fake), "00000000-0000-0000-0000-000000000001")

	if _, err := w.Write([]byte("tail\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	chunks := fake.chunks()
	if len(chunks) != 1 || !strings.Contains(chunks[0], "tail") {
		t.Fatalf("close did not persist buffered tail: %#v", chunks)
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if got := len(fake.chunks()); got != 1 {
		t.Fatalf("close flushed twice: %d chunk(s)", got)
	}
}
