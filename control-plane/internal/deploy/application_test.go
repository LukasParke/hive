package deploy

import (
	"strings"
	"testing"
)

func TestNormalizeServiceName(t *testing.T) {
	tests := []struct {
		name  string
		appID string
		want  string
	}{
		{name: "app-API Server", appID: "12345678-abcd", want: "app-api-server-12345678"},
		{name: "!!!", appID: "abcdef", want: "app-abcdef"},
		{name: "UPPER_and.dots", appID: "", want: "upper-and-dots"},
	}
	for _, tt := range tests {
		if got := normalizeServiceName(tt.name, tt.appID); got != tt.want {
			t.Fatalf("normalizeServiceName(%q, %q) = %q, want %q", tt.name, tt.appID, got, tt.want)
		}
	}
}

func TestNormalizeServiceNameCapsLength(t *testing.T) {
	got := normalizeServiceName("app-"+strings.Repeat("a", 120), "1234567890")
	if len(got) > 63 {
		t.Fatalf("service name length = %d, want <= 63", len(got))
	}
}
