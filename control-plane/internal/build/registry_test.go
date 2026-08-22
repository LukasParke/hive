package build

import "testing"

func TestSameRegistryHost(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "registry.example.com", "registry.example.com", true},
		{"scheme and slash", "https://registry.example.com/", "registry.example.com", true},
		{"case", "Registry.Example.com", "registry.example.com", true},
		{"docker hub aliases", "docker.io", "index.docker.io", true},
		{"docker hub v1", "index.docker.io", "registry-1.docker.io", true},
		{"different hosts", "registry.example.com", "other.example.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameRegistryHost(tc.a, tc.b); got != tc.want {
				t.Fatalf("SameRegistryHost(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestImageRef(t *testing.T) {
	auth := RegistryAuth{Host: "registry.example.com/"}
	got := auth.ImageRef("proj", "app", "abc123")
	want := "registry.example.com/proj/app:abc123"
	if got != want {
		t.Fatalf("ImageRef()=%q want %q", got, want)
	}
}
