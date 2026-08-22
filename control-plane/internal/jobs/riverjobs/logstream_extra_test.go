package riverjobs

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

func TestUUIDOrNil(t *testing.T) {
	if got := uuidOrNil("not-a-uuid"); got.Valid {
		t.Fatalf("uuidOrNil(invalid) = %+v, want invalid", got)
	}
	id := uuid.New()
	got := uuidOrNil(id.String())
	if !got.Valid || uuid.UUID(got.Bytes) != id {
		t.Fatalf("uuidOrNil(valid) = %+v, want %s", got, id)
	}
}

func TestPgText(t *testing.T) {
	got := pgText("hello")
	if !got.Valid || got.String != "hello" {
		t.Fatalf("pgText = %+v, want valid hello", got)
	}
}

// TestThrottledLogWriterEmptyBufferNeverFlushes covers the remaining
// shouldFlushLocked branch: an empty buffer must not flush even when the
// throttle interval has elapsed.
func TestThrottledLogWriterEmptyBufferNeverFlushes(t *testing.T) {
	fake := &fakeDBTX{}
	lw := &ThrottledLogWriter{
		queries:     dbgen.New(fake),
		buildID:     uuid.NewString(),
		minInterval: time.Nanosecond,
		maxLines:    20,
		lastFlush:   time.Now().Add(-time.Hour),
	}
	if err := lw.Flush(context.Background(), time.Now()); err != nil {
		t.Fatalf("Flush with empty buffer = %v, want nil", err)
	}
	if len(fake.execStatements) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(fake.execStatements))
	}
}

// TestThrottledLogWriterWriteSplitsAndSkipsBlankLines covers the multi-line
// Write path, including blank-line suppression.
func TestThrottledLogWriterWriteSplitsAndSkipsBlankLines(t *testing.T) {
	fake := &fakeDBTX{}
	lw := &ThrottledLogWriter{
		queries:     dbgen.New(fake),
		buildID:     uuid.NewString(),
		minInterval: time.Hour,
		maxLines:    3,
		lastFlush:   time.Now(),
	}
	if n, err := lw.Write([]byte("one\n\ntwo\n")); err != nil || n != 9 {
		t.Fatalf("Write = (%d, %v), want (9, nil)", n, err)
	}
	// Third line crosses the threshold; Flush persists all kept lines.
	if n, err := lw.Write([]byte("three\n")); err != nil || n != 6 {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
	if err := lw.Flush(context.Background(), time.Now()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	chunks := fake.chunks()
	if len(chunks) != 1 || chunks[0] != "one\ntwo\nthree\n" {
		t.Fatalf("chunks = %q, want [one\\ntwo\\nthree\\n]", chunks)
	}
}
