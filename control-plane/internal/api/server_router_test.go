package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	mswarm "github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"

	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// fakeSwarm embeds swarmclient.APIClient so only the slices the smoke test
// reaches (health ping, cluster counts) need overriding.
type fakeSwarm struct {
	swarmclient.APIClient

	pingErr error
}

func (f *fakeSwarm) Ping(ctx context.Context, opts dockerclient.PingOptions) (dockerclient.PingResult, error) {
	return dockerclient.PingResult{}, f.pingErr
}

func (f *fakeSwarm) ServiceList(ctx context.Context, opts dockerclient.ServiceListOptions) (dockerclient.ServiceListResult, error) {
	return dockerclient.ServiceListResult{Items: []mswarm.Service{}}, nil
}

func (f *fakeSwarm) NodeList(ctx context.Context, opts dockerclient.NodeListOptions) (dockerclient.NodeListResult, error) {
	return dockerclient.NodeListResult{Items: []mswarm.Node{}}, nil
}

func newSmokeServer(t *testing.T) *httptest.Server {
	t.Helper()
	pool := testdb.Get(t)
	srv := &Server{
		Pool:  pool,
		Swarm: swarmclient.NewWithAPI(&fakeSwarm{}),
	}
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return ts
}

func TestRouterHealth(t *testing.T) {
	ts := newSmokeServer(t)

	res, err := http.Get(ts.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", res.StatusCode)
	}
}

func TestRouterUnknownRoute404(t *testing.T) {
	ts := newSmokeServer(t)

	for _, path := range []string{"/api/v1/definitely-not-a-route", "/internal/nope"} {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = func() error { return res.Body.Close() }()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, res.StatusCode)
		}
	}
}

func TestRouterAuthedRouteWithoutToken401(t *testing.T) {
	ts := newSmokeServer(t)

	authed := []string{
		"/api/v1/projects",
		"/api/v1/profile",
		"/api/v1/nodes",
		"/api/v1/settings",
	}
	for _, path := range authed {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = func() error { return res.Body.Close() }()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s status = %d, want 401", path, res.StatusCode)
		}
	}
}

func TestRouterServesDocsAndSpec(t *testing.T) {
	ts := newSmokeServer(t)

	ok200 := []string{"/docs/api", "/api/v1/openapi.yaml"}
	for _, path := range ok200 {
		res, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = func() error { return res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, res.StatusCode)
		}
	}
}

func TestRouterMetricsEndpoint(t *testing.T) {
	ts := newSmokeServer(t)

	res, err := http.Get(ts.URL + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("GET metrics: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", res.StatusCode)
	}
}
