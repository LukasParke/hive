package riverjobs

import (
	"testing"

	"github.com/luke/hive/control-plane/internal/backup"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestNewClientRegistersWorkers(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	client, err := NewClient(pool, "127.0.0.1:5000", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client == nil {
		t.Fatal("client = nil, want a configured river client")
	}
}

func TestNewClientWithOptions(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	fanout := &fakeFanout{}
	renewer := &fakeRenewer{}
	runner := backup.NewRunner(pool, nil) // explicit runner skips default construction

	client, err := NewClient(pool, "registry.test:5000", nil, nil, nil,
		WithFanout(fanout),
		WithCertRenewer(renewer),
		WithBackupRunner(runner),
	)
	if err != nil {
		t.Fatalf("NewClient with options: %v", err)
	}
	if client == nil {
		t.Fatal("client = nil, want a configured river client")
	}
}

func TestStartPeriodicJobs(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	client, err := NewClient(pool, "127.0.0.1:5000", nil, nil, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	handles := StartPeriodicJobs(client)
	if len(handles) != 3 {
		t.Fatalf("periodic jobs = %d, want 3 (preview cleanup, cleanup, cert renewal)", len(handles))
	}
}
