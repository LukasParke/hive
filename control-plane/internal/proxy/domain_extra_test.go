package proxy

import (
	"context"
	"errors"
	"testing"

	"github.com/moby/moby/api/types/swarm"
)

// --- RemoveDomain ---

func newLabeledManager(labels map[string]string) (*DomainManager, *fakeServiceStore) {
	store := &fakeServiceStore{services: []swarm.Service{{
		ID:   "svc-1",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Labels: labels}},
	}}}
	return NewDomainManager(store), store
}

func TestRemoveDomainStripsRouterLabels(t *testing.T) {
	router := "traefik.http.routers.app-app-example-com."
	m, _ := newLabeledManager(map[string]string{
		"traefik.enable":            "true",
		router + "rule":             "Host(`app.example.com`)",
		router + "entrypoints":      "websecure",
		router + "tls":              "true",
		router + "tls.certresolver": "letsencrypt",
		router + "priority":         "5",
		"traefik.http.middlewares.app-app-example-com-strip.stripprefix.prefixes": "/api",
		router + "middlewares": "app-app-example-com-strip",
		"traefik.http.services.app-app-example-com.loadbalancer.server.port": "3000",
		"unrelated.label": "keep-me",
	})
	if err := m.RemoveDomain(context.Background(), "svc-1", "app-app-example-com"); err != nil {
		t.Fatalf("RemoveDomain: %v", err)
	}
	labels := m.client.(*fakeServiceStore).services[0].Spec.Labels
	// RemoveDomain clears router-scoped labels; traefik.enable itself stays.
	for _, key := range []string{
		router + "rule",
		router + "entrypoints",
		router + "tls",
		router + "tls.certresolver",
		router + "priority",
		"traefik.http.middlewares.app-app-example-com-strip.stripprefix.prefixes",
		router + "middlewares",
		"traefik.http.services.app-app-example-com.loadbalancer.server.port",
	} {
		if _, ok := labels[key]; ok {
			t.Errorf("label %q should have been removed", key)
		}
	}
	if labels["unrelated.label"] != "keep-me" {
		t.Errorf("foreign label disturbed: %v", labels)
	}
}

func TestRemoveDomainUnknownServiceIsNoop(t *testing.T) {
	m, _ := newLabeledManager(map[string]string{"traefik.enable": "true"})
	if err := m.RemoveDomain(context.Background(), "missing-svc", "app-app-example-com"); err != nil {
		t.Fatalf("expected nil for unknown service, got %v", err)
	}
	if got := m.client.(*fakeServiceStore).updates; got != 0 {
		t.Fatalf("expected no UpdateService calls, got %d", got)
	}
}

func TestApplyDomainListErrorPropagates(t *testing.T) {
	m := NewDomainManager(&errStore{err: errors.New("swarm down")})
	err := m.ApplyDomain(context.Background(), "svc-1", "r", Route{}, 3000)
	if err == nil || err.Error() != "swarm down" {
		t.Fatalf("expected list error passthrough, got %v", err)
	}
	if err := m.RemoveDomain(context.Background(), "svc-1", "r"); err == nil || err.Error() != "swarm down" {
		t.Fatalf("expected list error passthrough, got %v", err)
	}
}

func TestApplyDomainNilLabelsInitialized(t *testing.T) {
	m, _ := newLabeledManager(nil)
	err := m.ApplyDomain(context.Background(), "svc-1", "app-app-example-com",
		Route{Host: "app.example.com"}, 3000)
	if err != nil {
		t.Fatalf("ApplyDomain on nil labels: %v", err)
	}
	labels := m.client.(*fakeServiceStore).services[0].Spec.Labels
	if labels["traefik.enable"] != "true" {
		t.Fatalf("expected traefik.enable set, got %v", labels)
	}
}

// --- priority handling ---

func TestApplyDomainPriorityZeroClearsStaleLabel(t *testing.T) {
	router := "traefik.http.routers.app-app-example-com."
	m, _ := newLabeledManager(map[string]string{router + "priority": "9"})
	route := Route{Host: "app.example.com", Priority: 0}
	if err := m.ApplyDomain(context.Background(), "svc-1", "app-app-example-com", route, 3000); err != nil {
		t.Fatalf("ApplyDomain: %v", err)
	}
	labels := m.client.(*fakeServiceStore).services[0].Spec.Labels
	if _, ok := labels[router+"priority"]; ok {
		t.Fatal("priority=0 must clear a stale priority label")
	}
}

func TestApplyDomainTLSToggleCleansWebEntrypointLabels(t *testing.T) {
	router := "traefik.http.routers.app-app-example-com."
	// Start from a TLS-enabled router, then re-apply without TLS.
	m, _ := newLabeledManager(map[string]string{
		router + "tls":              "true",
		router + "tls.certresolver": "letsencrypt",
	})
	route := Route{Host: "app.example.com", TLSEnabled: false}
	if err := m.ApplyDomain(context.Background(), "svc-1", "app-app-example-com", route, 3000); err != nil {
		t.Fatalf("ApplyDomain: %v", err)
	}
	labels := m.client.(*fakeServiceStore).services[0].Spec.Labels
	if labels[router+"entrypoints"] != "web" {
		t.Fatalf("entrypoints = %q, want web", labels[router+"entrypoints"])
	}
	if _, ok := labels[router+"tls"]; ok {
		t.Fatal("stale tls label must be removed")
	}
	if _, ok := labels[router+"tls.certresolver"]; ok {
		t.Fatal("stale certresolver label must be removed")
	}
}

// --- AttachNetworks ---

func TestAttachNetworksReplacesAttachments(t *testing.T) {
	spec := &swarm.ServiceSpec{}
	spec.TaskTemplate.Networks = []swarm.NetworkAttachmentConfig{{Target: "old"}}
	AttachNetworks(spec)
	if len(spec.TaskTemplate.Networks) != 0 {
		t.Fatalf("AttachNetworks() with no ids must clear attachments, got %v", spec.TaskTemplate.Networks)
	}
	AttachNetworks(spec, "net-a", "net-b")
	got := spec.TaskTemplate.Networks
	if len(got) != 2 || got[0].Target != "net-a" || got[1].Target != "net-b" {
		t.Fatalf("attachments = %v, want [net-a net-b]", got)
	}
}
