package ctx

import (
	"context"
	"testing"

	"github.com/luke/hive/control-plane/internal/auth"
)

// TestWithClaimsRoundtrip verifies claims survive a context roundtrip and
// that a bare context reports false.
func TestWithClaimsRoundtrip(t *testing.T) {
	claims := &auth.Claims{UserID: "u1", Email: "u1@test.local"}

	got, ok := ClaimsFromContext(WithClaims(context.Background(), claims))
	if !ok {
		t.Fatal("ClaimsFromContext = false, want true")
	}
	if got.UserID != "u1" || got.Email != "u1@test.local" {
		t.Fatalf("claims = %+v, want u1/u1@test.local", got)
	}

	if _, ok := ClaimsFromContext(context.Background()); ok {
		t.Fatal("ClaimsFromContext(bare ctx) = true, want false")
	}

	if _, ok := ClaimsFromContext(context.WithValue(context.Background(), claimsKey, "not-claims")); ok {
		t.Fatal("ClaimsFromContext(wrong value type) = true, want false")
	}
}
