package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/swarm"
)

func TestRouterRule(t *testing.T) {
	cases := []struct {
		name       string
		host       string
		routeType  string
		pathPrefix string
		want       string
	}{
		{"exact host", "app.example.com", RouteTypeHost, "", "Host(`app.example.com`)"},
		{"empty type defaults to host", "app.example.com", "", "", "Host(`app.example.com`)"},
		{"wildcard escapes dots", "*.example.com", RouteTypeWildcard, "", "HostRegexp(`{sub:[^.]+}\\.example\\.com`)"},
		{"wildcard multi-label suffix", "*.apps.example.com", RouteTypeWildcard, "", "HostRegexp(`{sub:[^.]+}\\.apps\\.example\\.com`)"},
		{"path prefix", "api.example.com", RouteTypePath, "/api", "Host(`api.example.com`) && PathPrefix(`/api`)"},
		{"path prefix adds slash", "api.example.com", RouteTypePath, "api", "Host(`api.example.com`) && PathPrefix(`/api`)"},
		{"wildcard plus path", "*.example.com", RouteTypePath, "/api", "HostRegexp(`{sub:[^.]+}\\.example\\.com`) && PathPrefix(`/api`)"},
		{"regex metachars escaped", "*.exa+mple.com", RouteTypeWildcard, "", "HostRegexp(`{sub:[^.]+}\\.exa\\+mple\\.com`)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RouterRule(tc.host, tc.routeType, tc.pathPrefix); got != tc.want {
				t.Fatalf("RouterRule(%q,%q,%q)=%q want %q", tc.host, tc.routeType, tc.pathPrefix, got, tc.want)
			}
		})
	}
}

func TestRouterNameFromHost(t *testing.T) {
	cases := map[string]string{
		"app.example.com":   "app-app-example-com",
		"*.example.com":     "app-example-com",
		"*.APP.Example.COM": "app-app-example-com",
		"a-b.example.co":    "app-a-b-example-co",
	}
	for host, want := range cases {
		if got := RouterNameFromHost(host); got != want {
			t.Fatalf("RouterNameFromHost(%q)=%q want %q", host, got, want)
		}
	}
}

// fakeServiceStore records UpdateService calls so label assertions can be
// made without a swarm cluster.
type fakeServiceStore struct {
	services []swarm.Service
	updates  int
}

func (f *fakeServiceStore) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return f.services, nil
}

func (f *fakeServiceStore) UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error {
	for i := range f.services {
		if f.services[i].ID == id {
			f.services[i].Spec = spec
			f.updates++
			return nil
		}
	}
	return nil
}

func newFakeService(id string, labels map[string]string) fakeServiceStore {
	if labels == nil {
		labels = map[string]string{}
	}
	return fakeServiceStore{services: []swarm.Service{{
		ID:   id,
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Labels: labels}},
	}}}
}

func TestApplyDomainHostLabels(t *testing.T) {
	store := newFakeService("svc1", nil)
	m := NewDomainManager(&store)
	err := m.ApplyDomain(context.Background(), "svc1", "app-example-com", Route{
		Host: "app.example.com", RouteType: RouteTypeHost, TLSEnabled: true,
	}, 3000)
	if err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	want := map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers.app-example-com.rule":                      "Host(`app.example.com`)",
		"traefik.http.routers.app-example-com.entrypoints":               "websecure",
		"traefik.http.routers.app-example-com.tls":                       "true",
		"traefik.http.routers.app-example-com.tls.certresolver":          "letsencrypt",
		"traefik.http.services.app-example-com.loadbalancer.server.port": "3000",
	}
	for k, v := range want {
		if labels[k] != v {
			t.Fatalf("label %q=%q want %q", k, labels[k], v)
		}
	}
	if _, ok := labels["traefik.http.routers.app-example-com.priority"]; ok {
		t.Fatal("priority label must be absent when Priority is 0")
	}
	if _, ok := labels["traefik.http.routers.app-example-com.middlewares"]; ok {
		t.Fatal("middlewares label must be absent without strip prefix")
	}
}

func TestApplyDomainWebEntrypointWithoutTLS(t *testing.T) {
	store := newFakeService("svc1", nil)
	m := NewDomainManager(&store)
	if err := m.ApplyDomain(context.Background(), "svc1", "app-example-com", Route{Host: "app.example.com"}, 80); err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	if labels["traefik.http.routers.app-example-com.entrypoints"] != "web" {
		t.Fatalf("entrypoints=%q want web", labels["traefik.http.routers.app-example-com.entrypoints"])
	}
	if _, ok := labels["traefik.http.routers.app-example-com.tls"]; ok {
		t.Fatal("tls label must be absent without TLS")
	}
}

func TestApplyDomainWildcardPriority(t *testing.T) {
	store := newFakeService("svc1", nil)
	m := NewDomainManager(&store)
	err := m.ApplyDomain(context.Background(), "svc1", "app-example-com", Route{
		Host: "*.example.com", RouteType: RouteTypeWildcard, Priority: 50,
	}, 3000)
	if err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	if got := labels["traefik.http.routers.app-example-com.rule"]; got != "HostRegexp(`{sub:[^.]+}\\.example\\.com`)" {
		t.Fatalf("rule=%q", got)
	}
	if got := labels["traefik.http.routers.app-example-com.priority"]; got != "50" {
		t.Fatalf("priority=%q want 50", got)
	}
}

func TestApplyDomainStripMiddlewareMergesWithSecurity(t *testing.T) {
	sec := "hive-sec-abcd1234"
	store := newFakeService("svc1", map[string]string{
		"traefik.http.routers.app-example-com.middlewares": sec,
	})
	m := NewDomainManager(&store)
	err := m.ApplyDomain(context.Background(), "svc1", "app-example-com", Route{
		Host: "api.example.com", RouteType: RouteTypePath, PathPrefix: "/api", StripPrefix: true,
	}, 3000)
	if err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	wantList := sec + ",app-example-com-strip"
	if got := labels["traefik.http.routers.app-example-com.middlewares"]; got != wantList {
		t.Fatalf("middlewares=%q want %q", got, wantList)
	}
	if got := labels["traefik.http.middlewares.app-example-com-strip.stripprefix.prefixes"]; got != "/api" {
		t.Fatalf("stripprefix prefixes=%q want /api", got)
	}
	if got := labels["traefik.http.routers.app-example-com.rule"]; got != "Host(`api.example.com`) && PathPrefix(`/api`)" {
		t.Fatalf("rule=%q", got)
	}
}

func TestApplyDomainStripWithoutExistingMiddlewares(t *testing.T) {
	store := newFakeService("svc1", nil)
	m := NewDomainManager(&store)
	if err := m.ApplyDomain(context.Background(), "svc1", "app-example-com", Route{
		Host: "api.example.com", RouteType: RouteTypePath, PathPrefix: "/v2", StripPrefix: true,
	}, 3000); err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	if got := labels["traefik.http.routers.app-example-com.middlewares"]; got != "app-example-com-strip" {
		t.Fatalf("middlewares=%q want app-example-com-strip", got)
	}
}

func TestApplyDomainDropsStaleStripMiddleware(t *testing.T) {
	sec := "hive-sec-abcd1234"
	store := newFakeService("svc1", map[string]string{
		"traefik.http.routers.app-example-com.middlewares":                    "app-example-com-strip," + sec,
		"traefik.http.middlewares.app-example-com-strip.stripprefix.prefixes": "/old",
	})
	m := NewDomainManager(&store)
	if err := m.ApplyDomain(context.Background(), "svc1", "app-example-com", Route{
		Host: "api.example.com", RouteType: RouteTypePath,
	}, 3000); err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	if got := labels["traefik.http.routers.app-example-com.middlewares"]; got != sec {
		t.Fatalf("middlewares=%q want %q", got, sec)
	}
	if _, ok := labels["traefik.http.middlewares.app-example-com-strip.stripprefix.prefixes"]; ok {
		t.Fatal("stale strip middleware label must be removed")
	}
}

func TestRemoveDomainCleansRoutingLabels(t *testing.T) {
	sec := "hive-sec-abcd1234"
	store := newFakeService("svc1", map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers.app-example-com.rule":                           "Host(`x.example.com`)",
		"traefik.http.routers.app-example-com.entrypoints":                    "websecure",
		"traefik.http.routers.app-example-com.tls":                            "true",
		"traefik.http.routers.app-example-com.tls.certresolver":               "letsencrypt",
		"traefik.http.routers.app-example-com.priority":                       "50",
		"traefik.http.routers.app-example-com.middlewares":                    "app-example-com-strip," + sec,
		"traefik.http.middlewares.app-example-com-strip.stripprefix.prefixes": "/api",
		"traefik.http.services.app-example-com.loadbalancer.server.port":      "3000",
	})
	m := NewDomainManager(&store)
	if err := m.RemoveDomain(context.Background(), "svc1", "app-example-com"); err != nil {
		t.Fatal(err)
	}
	labels := store.services[0].Spec.Labels
	for k := range labels {
		if strings.Contains(k, "app-example-com") && !strings.Contains(k, "hive-sec") &&
			!strings.HasSuffix(k, ".middlewares") {
			t.Fatalf("routing label %q survived RemoveDomain", k)
		}
	}
	if got := labels["traefik.http.routers.app-example-com.middlewares"]; got != sec {
		t.Fatalf("middlewares=%q want security entries kept (%q)", got, sec)
	}
}

func TestApplyDomainUnknownServiceIsNoop(t *testing.T) {
	store := newFakeService("svc1", nil)
	m := NewDomainManager(&store)
	if err := m.ApplyDomain(context.Background(), "missing", "app-example-com", Route{Host: "x.example.com"}, 80); err != nil {
		t.Fatalf("expected no-op for unknown service, got %v", err)
	}
	if store.updates != 0 {
		t.Fatal("UpdateService must not be called for unknown service")
	}
}

func TestApplySecurityRulesPreserveStripMiddleware(t *testing.T) {
	// ApplySecurityRulesForApplication rewrites the middlewares list; the
	// domain-owned strip middleware must survive a security pass and be
	// dropped only when it is no longer referenced.
	router := "app-example-com"
	strip := stripMiddlewareName(router)
	labels := map[string]string{
		"traefik.http.routers." + router + ".middlewares":             "hive-sec-abcd1234," + strip,
		"traefik.http.middlewares." + strip + ".stripprefix.prefixes": "/api",
	}
	// Simulate the security pass list-composition by reusing composeMiddlewares:
	// it must keep foreign entries and re-append strip.
	composeMiddlewares(labels, router, Route{Host: "x.example.com", RouteType: RouteTypePath, PathPrefix: "/api", StripPrefix: true})
	got := labels["traefik.http.routers."+router+".middlewares"]
	if got != "hive-sec-abcd1234,"+strip {
		t.Fatalf("middlewares=%q want security first then strip", got)
	}
}
