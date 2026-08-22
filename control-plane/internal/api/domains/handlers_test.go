package domains

import (
	"errors"
	"testing"

	"github.com/luke/hive/control-plane/internal/proxy"
)

func TestNormalizeHostname(t *testing.T) {
	cases := map[string]string{
		" HTTPS://App.Example.COM/path ": "app.example.com", //nolint:gocritic // whitespace intentionally exercises trimming/normalization
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
	valid := []string{
		"app.example.com",
		"a-b.example.co",
		"localhost",
		// A single leading "*." marks a wildcard domain.
		"*.example.com",
		"*.apps.example.com",
	}
	for _, host := range valid {
		if !validHostname(host) {
			t.Fatalf("expected %q to be valid", host)
		}
	}
	invalid := []string{
		"",
		"no-tld",
		"-bad.example.com",
		"bad_.example.com",
		"bad..example.com",
		// Bare or misplaced asterisks are rejected.
		"*",
		"*.",
		"a.*.example.com",
		"sub.*.example.com",
		"*example.com",
		"example.*",
		"*.*.example.com",
		"*.exa_mple.com",
	}
	for _, host := range invalid {
		if validHostname(host) {
			t.Fatalf("expected %q to be invalid", host)
		}
	}
}

func TestNormalizeRoute(t *testing.T) {
	cases := []struct {
		name        string
		host        string
		routeType   string
		pathPrefix  string
		stripPrefix bool
		priority    int
		want        proxy.Route
	}{
		{
			name:      "empty type defaults to host",
			host:      "app.example.com",
			routeType: "",
			want:      proxy.Route{Host: "app.example.com", RouteType: proxy.RouteTypeHost},
		},
		{
			name:      "wildcard kept",
			host:      "*.example.com",
			routeType: proxy.RouteTypeWildcard,
			want:      proxy.Route{Host: "*.example.com", RouteType: proxy.RouteTypeWildcard},
		},
		{
			name:        "path with prefix and strip",
			host:        "api.example.com",
			routeType:   proxy.RouteTypePath,
			pathPrefix:  "/api",
			stripPrefix: true,
			priority:    10,
			want:        proxy.Route{Host: "api.example.com", RouteType: proxy.RouteTypePath, PathPrefix: "/api", StripPrefix: true, Priority: 10},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeRoute(tc.host, tc.routeType, tc.pathPrefix, tc.stripPrefix, tc.priority)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("normalizeRoute()=%+v want %+v", got, tc.want)
			}
		})
	}
}

func TestNormalizeRouteRejectsInvalid(t *testing.T) {
	cases := []struct {
		name                        string
		host, routeType, pathPrefix string
		priority                    int
	}{
		{"unknown type", "app.example.com", "regex", "", 0},
		{"wildcard without *. host", "app.example.com", proxy.RouteTypeWildcard, "", 0},
		{"path without prefix", "api.example.com", proxy.RouteTypePath, "  ", 0},
		{"negative priority", "api.example.com", proxy.RouteTypeHost, "", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeRoute(tc.host, tc.routeType, tc.pathPrefix, false, tc.priority); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestNormalizeRouteStripOnlyAppliesToPath(t *testing.T) {
	got, err := normalizeRoute("app.example.com", proxy.RouteTypeHost, "", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.StripPrefix {
		t.Fatal("StripPrefix must be forced off for non-path routes")
	}
}

func TestNullablePriority(t *testing.T) {
	if v := nullablePriority(0); v != nil {
		t.Fatalf("nullablePriority(0)=%v want nil", v)
	}
	if v := nullablePriority(7); v != 7 {
		t.Fatalf("nullablePriority(7)=%v want 7", v)
	}
}

// TestCreateRequestRoundTrip exercises the payload→route mapping CreateDomain
// and UpdateDomain perform before persisting.
func TestCreateRequestRoundTrip(t *testing.T) {
	type req struct {
		hostname    string
		routeType   string
		pathPrefix  string
		stripPrefix bool
		priority    int
	}
	cases := []struct {
		name string
		in   req
		want proxy.Route
	}{
		{
			name: "legacy host-only payload",
			in:   req{hostname: "app.example.com"},
			want: proxy.Route{Host: "app.example.com", RouteType: proxy.RouteTypeHost},
		},
		{
			name: "wildcard payload",
			in:   req{hostname: "*.example.com", routeType: proxy.RouteTypeWildcard},
			want: proxy.Route{Host: "*.example.com", RouteType: proxy.RouteTypeWildcard},
		},
		{
			name: "path payload adds leading slash",
			in:   req{hostname: "api.example.com", routeType: proxy.RouteTypePath, pathPrefix: "v1", stripPrefix: true, priority: 25},
			want: proxy.Route{Host: "api.example.com", RouteType: proxy.RouteTypePath, PathPrefix: "/v1", StripPrefix: true, Priority: 25},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := normalizeHostname(tc.in.hostname)
			if !validHostname(host) {
				t.Fatalf("hostname %q rejected", host)
			}
			got, err := normalizeRoute(host, tc.in.routeType, tc.in.pathPrefix, tc.in.stripPrefix, tc.in.priority)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("round trip=%+v want %+v", got, tc.want)
			}
		})
	}
}

// errRow is a pgx.Row stand-in that always fails its scan.
type errRow struct{ err error }

func (r errRow) Scan(_ ...any) error { return r.err }

func TestScanRoutePropagatesScanErrors(t *testing.T) {
	if _, err := scanRoute(errRow{err: errors.New("scan boom")}); err == nil {
		t.Fatal("expected scan error to propagate")
	}
}
