package nodeops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"
	swarm "github.com/moby/moby/api/types/swarm"
)

// fakeSwarm is an in-memory SwarmAPI double recording mutations.
type fakeSwarm struct {
	nodes       map[string]swarm.Node
	updateCalls []updateCall
	removeCalls []removeCall
	getErr      error
	// Additive failure seams for the error-branch tests below.
	updateErr error
	removeErr error
	listErr   error
	// getErrs is consumed one entry per GetNode call; a nil entry falls
	// through to the node map (lets tests fail only the second fetch).
	getErrs []error
}

type updateCall struct {
	nodeID  string
	version uint64
	spec    swarm.NodeSpec
}

type removeCall struct {
	nodeID string
	force  bool
}

func (f *fakeSwarm) GetNode(_ context.Context, nodeID string) (swarm.Node, error) {
	if len(f.getErrs) > 0 {
		err := f.getErrs[0]
		f.getErrs = f.getErrs[1:]
		if err != nil {
			return swarm.Node{}, err
		}
	}
	if f.getErr != nil {
		return swarm.Node{}, f.getErr
	}
	node, ok := f.nodes[nodeID]
	if !ok {
		return swarm.Node{}, cerrdefs.ErrNotFound
	}
	return node, nil
}

func (f *fakeSwarm) UpdateNode(_ context.Context, nodeID string, version uint64, spec swarm.NodeSpec) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updateCalls = append(f.updateCalls, updateCall{nodeID: nodeID, version: version, spec: spec})
	node := f.nodes[nodeID]
	node.Spec = spec
	node.Version.Index = version + 1
	f.nodes[nodeID] = node
	return nil
}

func (f *fakeSwarm) RemoveNode(_ context.Context, nodeID string, force bool) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removeCalls = append(f.removeCalls, removeCall{nodeID: nodeID, force: force})
	delete(f.nodes, nodeID)
	return nil
}

func (f *fakeSwarm) ListNodes(_ context.Context) ([]swarm.Node, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]swarm.Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		out = append(out, n)
	}
	return out, nil
}

func newNode(id, role, availability string, labels map[string]string) swarm.Node {
	node := swarm.Node{
		ID:          id,
		Meta:        swarm.Meta{Version: swarm.Version{Index: 7}},
		Description: swarm.NodeDescription{Hostname: "host-" + id},
		Status:      swarm.NodeStatus{State: swarm.NodeStateReady},
		Spec: swarm.NodeSpec{
			Annotations:  swarm.Annotations{Labels: labels},
			Role:         swarm.NodeRole(role),
			Availability: swarm.NodeAvailability(availability),
		},
	}
	return node
}

// newTestHandler builds a handler over the fake with the RBAC gate disabled.
func newTestHandler(fake *fakeSwarm) *Handler {
	h := NewHandler(nil, fake)
	h.authorizeOverride = func(http.ResponseWriter, *http.Request) bool { return true }
	return h
}

func serve(t *testing.T, h *Handler, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Put("/api/v1/nodes/{id}/labels", h.UpdateNodeLabels)
	r.Post("/api/v1/nodes/{id}/drain", h.DrainNode)
	r.Post("/api/v1/nodes/{id}/promote", h.PromoteNode)
	r.Post("/api/v1/nodes/{id}/demote", h.DemoteNode)
	r.Delete("/api/v1/nodes/{id}", h.RemoveNode)

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader([]byte(mustJSON(t, body)))
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON body %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestUpdateNodeLabelsMerges(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"w1": newNode("w1", "worker", "active", map[string]string{"env": "prod"}),
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPut, "/api/v1/nodes/w1/labels",
		map[string]any{"labels": map[string]string{"role": "edge", "env": "staging"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	if len(fake.updateCalls) != 1 {
		t.Fatalf("update calls = %d, want 1", len(fake.updateCalls))
	}
	call := fake.updateCalls[0]
	if call.nodeID != "w1" || call.version != 7 {
		t.Fatalf("update called with node=%s version=%d, want w1@7", call.nodeID, call.version)
	}
	want := map[string]string{"env": "staging", "role": "edge"}
	for k, v := range want {
		if got := call.spec.Labels[k]; got != v {
			t.Fatalf("merged labels[%q] = %q, want %q (full: %v)", k, got, v, call.spec.Labels)
		}
	}

	body := decodeBody(t, rec)
	labels, _ := body["labels"].(map[string]any)
	if labels["env"] != "staging" || labels["role"] != "edge" {
		t.Fatalf("response labels = %v, want merged set", labels)
	}
	if body["hostname"] != "host-w1" || body["status"] != "ready" || body["role"] != "worker" {
		t.Fatalf("response node fields = %v", body)
	}
}

func TestUpdateNodeLabelsRequiresLabelsObject(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{"w1": newNode("w1", "worker", "active", nil)}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPut, "/api/v1/nodes/w1/labels", map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(fake.updateCalls) != 0 {
		t.Fatal("no update must happen on invalid payload")
	}
}

func TestDrainNodeSetsAvailability(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"w1": newNode("w1", "worker", "active", nil),
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPost, "/api/v1/nodes/w1/drain", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := fake.nodes["w1"].Spec.Availability; got != swarm.NodeAvailabilityDrain {
		t.Fatalf("availability = %q, want drain", got)
	}
	body := decodeBody(t, rec)
	if body["status"] != "ok" {
		t.Fatalf("body = %v, want status ok", body)
	}
}

func TestPromoteAndDemoteTransitionRole(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"w1": newNode("w1", "worker", "active", nil),
		"m1": newNode("m1", "manager", "active", nil),
	}}
	h := newTestHandler(fake)

	if rec := serve(t, h, http.MethodPost, "/api/v1/nodes/w1/promote", nil); rec.Code != http.StatusOK {
		t.Fatalf("promote status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.nodes["w1"].Spec.Role; got != swarm.NodeRoleManager {
		t.Fatalf("role after promote = %q, want manager", got)
	}
	if rec := serve(t, h, http.MethodPost, "/api/v1/nodes/m1/demote", nil); rec.Code != http.StatusOK {
		t.Fatalf("demote status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := fake.nodes["m1"].Spec.Role; got != swarm.NodeRoleWorker {
		t.Fatalf("role after demote = %q, want worker", got)
	}
}

func TestRemoveNodeRefusesLastManager(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"m1": newNode("m1", "manager", "active", nil),
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/nodes/m1?force=true", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if len(fake.removeCalls) != 0 {
		t.Fatal("last manager must never reach RemoveNode")
	}
}

func TestRemoveNodeForwardsForce(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"m1": newNode("m1", "manager", "active", nil),
		"w1": newNode("w1", "worker", "drain", nil),
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/nodes/w1?force=true", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(fake.removeCalls) != 1 || !fake.removeCalls[0].force || fake.removeCalls[0].nodeID != "w1" {
		t.Fatalf("remove calls = %+v, want forced removal of w1", fake.removeCalls)
	}
}

func TestRemoveNodeAllowsSecondManager(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{
		"m1": newNode("m1", "manager", "active", nil),
		"m2": newNode("m2", "manager", "active", nil),
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/nodes/m2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if _, stillThere := fake.nodes["m2"]; stillThere {
		t.Fatal("node m2 should have been removed")
	}
}

func TestNodeNotFoundMapsTo404(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{}}
	h := newTestHandler(fake)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/nodes/missing/labels"},
		{http.MethodPost, "/api/v1/nodes/missing/drain"},
		{http.MethodPost, "/api/v1/nodes/missing/promote"},
		{http.MethodPost, "/api/v1/nodes/missing/demote"},
	} {
		var body map[string]any
		if tc.method == http.MethodPut {
			body = map[string]any{"labels": map[string]string{}}
		}
		rec := serve(t, h, tc.method, tc.path, body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

func TestAuthorizeBlocksWhenGateDenies(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{"w1": newNode("w1", "worker", "active", nil)}}
	h := NewHandler(nil, fake)
	h.authorizeOverride = func(w http.ResponseWriter, _ *http.Request) bool {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return false
	}

	rec := serve(t, h, http.MethodPost, "/api/v1/nodes/w1/drain", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if len(fake.updateCalls) != 0 {
		t.Fatal("denied request must not reach the swarm")
	}
}

// serveRaw issues a request with a verbatim body so malformed JSON payloads
// can be exercised.
func serveRaw(t *testing.T, h *Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Put("/api/v1/nodes/{id}/labels", h.UpdateNodeLabels)
	r.Post("/api/v1/nodes/{id}/drain", h.DrainNode)
	r.Post("/api/v1/nodes/{id}/promote", h.PromoteNode)
	r.Post("/api/v1/nodes/{id}/demote", h.DemoteNode)
	r.Delete("/api/v1/nodes/{id}", h.RemoveNode)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestHandlerSwarmFailuresMapToStatusCodes walks the swarm-error branches of
// every node operation: update failures surface as 502, list failures as 502,
// and remove failures reuse writeNodeError's not-found vs runtime split.
func TestHandlerSwarmFailuresMapToStatusCodes(t *testing.T) {
	boom := errors.New("swarm boom")
	labels := map[string]any{"labels": map[string]string{}}
	cases := []struct {
		name         string
		method, path string
		body         map[string]any
		mutate       func(*fakeSwarm)
		want         int
		wantBody     string
	}{
		{
			name: "labels update fails", method: http.MethodPut, path: "/api/v1/nodes/w1/labels", body: labels,
			mutate: func(f *fakeSwarm) { f.updateErr = boom }, want: http.StatusBadGateway, wantBody: "failed to update node w1",
		},
		{
			name: "drain update fails", method: http.MethodPost, path: "/api/v1/nodes/w1/drain",
			mutate: func(f *fakeSwarm) { f.updateErr = boom }, want: http.StatusBadGateway, wantBody: "failed to update node w1",
		},
		{
			name: "promote update fails", method: http.MethodPost, path: "/api/v1/nodes/w1/promote",
			mutate: func(f *fakeSwarm) { f.updateErr = boom }, want: http.StatusBadGateway, wantBody: "failed to update node w1",
		},
		{
			name: "demote update fails", method: http.MethodPost, path: "/api/v1/nodes/w1/demote",
			mutate: func(f *fakeSwarm) { f.updateErr = boom }, want: http.StatusBadGateway, wantBody: "failed to update node w1",
		},
		{
			name: "labels refetch after update fails", method: http.MethodPut, path: "/api/v1/nodes/w1/labels", body: labels,
			mutate: func(f *fakeSwarm) { f.getErrs = []error{nil, boom} }, want: http.StatusBadGateway, wantBody: "swarm request for node w1 failed",
		},
		{
			name: "remove list fails", method: http.MethodDelete, path: "/api/v1/nodes/w1",
			mutate: func(f *fakeSwarm) { f.listErr = boom }, want: http.StatusBadGateway, wantBody: "failed to list cluster nodes",
		},
		{
			name: "remove fails at the daemon", method: http.MethodDelete, path: "/api/v1/nodes/w1",
			mutate: func(f *fakeSwarm) { f.removeErr = boom }, want: http.StatusBadGateway, wantBody: "swarm request for node w1 failed",
		},
		{
			name: "remove of vanished node is a 404", method: http.MethodDelete, path: "/api/v1/nodes/w1",
			mutate: func(f *fakeSwarm) { f.removeErr = cerrdefs.ErrNotFound }, want: http.StatusNotFound, wantBody: "not found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSwarm{nodes: map[string]swarm.Node{
				"w1": newNode("w1", "worker", "active", nil),
			}}
			tc.mutate(fake)
			h := newTestHandler(fake)
			rec := serve(t, h, tc.method, tc.path, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body %q does not contain %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestUpdateNodeLabelsRejectsMalformedJSON(t *testing.T) {
	h := newTestHandler(&fakeSwarm{nodes: map[string]swarm.Node{"w1": newNode("w1", "worker", "active", nil)}})
	rec := serveRaw(t, h, http.MethodPut, "/api/v1/nodes/w1/labels", "{not json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestEveryHandlerDeniesWhenUnauthorized covers the authorize short-circuit
// of each endpoint (drain was already covered; the rest are exercised here).
func TestEveryHandlerDeniesWhenUnauthorized(t *testing.T) {
	fake := &fakeSwarm{nodes: map[string]swarm.Node{"w1": newNode("w1", "worker", "active", nil)}}
	h := NewHandler(nil, fake)
	h.authorizeOverride = func(w http.ResponseWriter, _ *http.Request) bool {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return false
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/nodes/w1/labels"},
		{http.MethodPost, "/api/v1/nodes/w1/promote"},
		{http.MethodPost, "/api/v1/nodes/w1/demote"},
		{http.MethodDelete, "/api/v1/nodes/w1"},
	} {
		var body map[string]any
		if route.method == http.MethodPut {
			body = map[string]any{"labels": map[string]string{}}
		}
		rec := serve(t, h, route.method, route.path, body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403", route.method, route.path, rec.Code)
		}
	}
	if len(fake.updateCalls)+len(fake.removeCalls) != 0 {
		t.Fatal("denied requests must never reach the swarm")
	}
}
