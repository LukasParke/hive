package secrets

import (
	"strings"
	"testing"
)

// TestSealOpenValueLifecycle runs in phases because SetRuntime is
// process-global and sticky: first the no-master-key passthrough, then the
// encrypted round-trip, then legacy plaintext tolerance.
func TestSealOpenValueLifecycle(t *testing.T) {
	if Runtime() != nil {
		t.Skip("runtime store already installed by another test binary state")
	}

	// Without a runtime store values pass through unmodified so deployments
	// without a master key keep working.
	plain := "s3cr3t"
	passthrough, err := SealValue("ssh_key", []byte(plain))
	if err != nil {
		t.Fatalf("seal without runtime: %v", err)
	}
	if passthrough != plain {
		t.Fatalf("seal without runtime = %q, want verbatim %q", passthrough, plain)
	}

	// Install a value store and verify the encrypted round-trip.
	store, err := NewValueStore([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	SetRuntime(store)

	sealed, sealErr := SealValue("ssh_key", []byte(plain))
	if sealErr != nil {
		t.Fatalf("seal: %v", sealErr)
	}
	if sealed == plain || !strings.HasPrefix(sealed, EncryptedPrefix) {
		t.Fatalf("sealed = %.20q..., want prefixed ciphertext", sealed)
	}
	opened, openErr := OpenValue("ssh_key", sealed)
	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}
	if string(opened) != plain {
		t.Fatalf("round-trip = %q, want %q", opened, plain)
	}

	// Context types derive different keys: a value sealed under one context
	// must not open under another.
	wrongCtx, wrongErr := OpenValue("certificate_key", sealed)
	if wrongErr == nil && string(wrongCtx) == plain {
		t.Fatal("value sealed for ssh_key opened under certificate_key context")
	}

	// Legacy plaintext values are returned unchanged.
	legacy, legacyErr := OpenValue("ssh_key", plain)
	if legacyErr != nil {
		t.Fatalf("legacy open: %v", legacyErr)
	}
	if string(legacy) != plain {
		t.Fatalf("legacy value = %q, want unchanged %q", legacy, plain)
	}

	// Tampered ciphertext must fail loudly instead of yielding garbage.
	if _, tamperErr := OpenValue("ssh_key", sealed+"x"); tamperErr == nil {
		t.Fatal("tampered sealed value opened successfully")
	}

	// Master key validation.
	if _, shortErr := NewValueStore([]byte("too-short")); shortErr == nil {
		t.Fatal("NewValueStore accepted a short master key")
	}

	// SetRuntime is sticky: later installs are ignored.
	first := Runtime()
	SetRuntime(&Store{masterKey: []byte("other-key-other-key-other-key!!")})
	if Runtime() != first {
		t.Fatal("SetRuntime replaced an existing runtime store")
	}

	// OpenValue with a prefixed value and an installed runtime decrypts;
	// with the prefix absent it returns plaintext unchanged (covered above).
	if _, prefixedErr := OpenValue("ssh_key", EncryptedPrefix+"!!!bad-b64!!!"); prefixedErr == nil {
		t.Error("OpenValue accepted undecodable sealed payload")
	}
}
