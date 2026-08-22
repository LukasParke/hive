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

func TestAppNetworkNames(t *testing.T) {
	tests := []struct {
		name    string
		spec    ApplicationSpec
		domains []string
		want    []string
	}{
		{
			name: "zero value attaches nothing",
			spec: ApplicationSpec{AppID: "app-1"},
			want: nil,
		},
		{
			name: "project slug attaches project network",
			spec: ApplicationSpec{AppID: "app-1", ProjectSlug: "My Shop"},
			want: []string{"hive_project_my-shop"},
		},
		{
			name:    "domains without slug attach proxy only",
			spec:    ApplicationSpec{AppID: "app-1"},
			domains: []string{"shop.example.com"},
			want:    []string{"hive_proxy"},
		},
		{
			name:    "slug and domains attach both",
			spec:    ApplicationSpec{AppID: "app-1", ProjectSlug: "shop"},
			domains: []string{"a.example.com", "b.example.com"},
			want:    []string{"hive_project_shop", "hive_proxy"},
		},
		{
			name:    "empty domain list attaches no proxy",
			spec:    ApplicationSpec{AppID: "app-1", ProjectSlug: "shop"},
			domains: []string{},
			want:    []string{"hive_project_shop"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appNetworkNames(tt.spec, tt.domains)
			if len(got) != len(tt.want) {
				t.Fatalf("appNetworkNames = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("appNetworkNames = %v, want %v", got, tt.want)
				}
			}
		})
	}
}
