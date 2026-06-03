package agentclient

import (
	"testing"

	"github.com/luke/hive/control-plane/internal/ca"
)

func TestNewDialer(t *testing.T) {
	authority, err := ca.New()
	if err != nil {
		t.Fatal(err)
	}

	d, err := NewDialer(authority)
	if err != nil {
		t.Fatal(err)
	}

	// Test client creation
	client := d.ClientPlaintext("node-1", "localhost:9090")
	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Test caching
	client2 := d.ClientPlaintext("node-1", "localhost:9090")
	if client != client2 {
		t.Error("expected same cached client instance")
	}

	// Test different node returns different client
	client3 := d.ClientPlaintext("node-2", "localhost:9091")
	if client == client3 {
		t.Error("expected different client for different node")
	}
}
