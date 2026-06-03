package webhooks

import "testing"

func TestMatchesWatchPaths(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		files    []string
		want     bool
	}{
		{
			name:     "empty patterns matches",
			patterns: nil,
			files:    []string{"README.md"},
			want:     true,
		},
		{
			name:     "exact file match",
			patterns: []string{"apps/api/**"},
			files:    []string{"apps/api/main.go"},
			want:     true,
		},
		{
			name:     "no match",
			patterns: []string{"ui/**"},
			files:    []string{"control-plane/internal/api/server.go"},
			want:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchesWatchPaths(c.patterns, c.files)
			if got != c.want {
				t.Fatalf("matchesWatchPaths(%v,%v)=%v want %v", c.patterns, c.files, got, c.want)
			}
		})
	}
}
