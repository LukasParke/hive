package riverjobs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSSHKeyFilePlaintextFallback(t *testing.T) {
	// No runtime secrets store in tests: writeSSHKeyFile must fall back to
	// the (legacy plaintext) column value.
	path, err := writeSSHKeyFile(context.Background(), "test-key", "TESTMATERIAL")
	if err != nil {
		t.Fatalf("writeSSHKeyFile: %v", err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(path)) }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}
	if string(data) != "TESTMATERIAL" {
		t.Fatalf("key file content = %q", string(data))
	}
}

func TestWriteSSHKeyFileNoMaterial(t *testing.T) {
	_, err := writeSSHKeyFile(context.Background(), "empty-key", "")
	if err == nil || !strings.Contains(err.Error(), "no private key material") {
		t.Fatalf("expected no-material error, got %v", err)
	}
}
