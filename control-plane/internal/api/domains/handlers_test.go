package domains

import "testing"

func TestNormalizeHostname(t *testing.T) {
	cases := map[string]string{
		" HTTPS://App.Example.COM/path ": "app.example.com",
		"api.example.com:8443":           "api.example.com",
		"example.com.":                   "example.com",
	}
	for input, want := range cases {
		if got := normalizeHostname(input); got != want {
			t.Fatalf("normalizeHostname(%q)=%q want %q", input, got, want)
		}
	}
}

func TestValidHostname(t *testing.T) {
	valid := []string{"app.example.com", "a-b.example.co", "localhost"}
	for _, host := range valid {
		if !validHostname(host) {
			t.Fatalf("expected %q to be valid", host)
		}
	}
	invalid := []string{"", "no-tld", "-bad.example.com", "bad_.example.com", "bad..example.com"}
	for _, host := range invalid {
		if validHostname(host) {
			t.Fatalf("expected %q to be invalid", host)
		}
	}
}
