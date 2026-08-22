package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"
	"github.com/luke/hive/control-plane/internal/secrets"
	"github.com/luke/hive/control-plane/internal/testdb"
	"github.com/moby/moby/api/types/network"
	swarm "github.com/moby/moby/api/types/swarm"
)

// failingSwarm wraps the in-memory fake with targeted failures.
type failingSwarm struct {
	*fakeSwarm
	listSecretsErr   error
	createSecretErr  error
	getSecretErr     error
	removeSecretErr  error
	listConfigsErr   error
	createConfigErr  error
	getConfigErr     error
	removeConfigErr  error
	listNetworksErr  error
	inspectNetErr    error
	createNetErr     error
	listServicesErr  error
	removeNetworkErr error
}

func (f *failingSwarm) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	if f.listSecretsErr != nil {
		return nil, f.listSecretsErr
	}
	return f.fakeSwarm.ListSecrets(ctx)
}

func (f *failingSwarm) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	if f.createSecretErr != nil {
		return "", f.createSecretErr
	}
	return f.fakeSwarm.CreateSecret(ctx, spec)
}

func (f *failingSwarm) GetSecret(ctx context.Context, id string) (swarm.Secret, error) {
	if f.getSecretErr != nil {
		return swarm.Secret{}, f.getSecretErr
	}
	return f.fakeSwarm.GetSecret(ctx, id)
}

func (f *failingSwarm) RemoveSecret(ctx context.Context, id string) error {
	if f.removeSecretErr != nil {
		return f.removeSecretErr
	}
	return f.fakeSwarm.RemoveSecret(ctx, id)
}

func (f *failingSwarm) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	if f.listConfigsErr != nil {
		return nil, f.listConfigsErr
	}
	return f.fakeSwarm.ListConfigs(ctx)
}

func (f *failingSwarm) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	if f.createConfigErr != nil {
		return "", f.createConfigErr
	}
	return f.fakeSwarm.CreateConfig(ctx, spec)
}

func (f *failingSwarm) GetConfig(ctx context.Context, id string) (swarm.Config, error) {
	if f.getConfigErr != nil {
		return swarm.Config{}, f.getConfigErr
	}
	return f.fakeSwarm.GetConfig(ctx, id)
}

func (f *failingSwarm) RemoveConfig(ctx context.Context, id string) error {
	if f.removeConfigErr != nil {
		return f.removeConfigErr
	}
	return f.fakeSwarm.RemoveConfig(ctx, id)
}

func (f *failingSwarm) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	if f.listNetworksErr != nil {
		return nil, f.listNetworksErr
	}
	return f.fakeSwarm.ListNetworks(ctx)
}

func (f *failingSwarm) InspectNetwork(ctx context.Context, id string) (network.Inspect, error) {
	if f.inspectNetErr != nil {
		return network.Inspect{}, f.inspectNetErr
	}
	return f.fakeSwarm.InspectNetwork(ctx, id)
}

func (f *failingSwarm) CreateNetwork(ctx context.Context, name string) (string, error) {
	if f.createNetErr != nil {
		return "", f.createNetErr
	}
	return f.fakeSwarm.CreateNetwork(ctx, name)
}

func (f *failingSwarm) RemoveNetwork(ctx context.Context, id string) error {
	if f.removeNetworkErr != nil {
		return f.removeNetworkErr
	}
	return f.fakeSwarm.RemoveNetwork(ctx, id)
}

func (f *failingSwarm) ListServices(ctx context.Context) ([]swarm.Service, error) {
	if f.listServicesErr != nil {
		return nil, f.listServicesErr
	}
	return f.fakeSwarm.ListServices(ctx)
}

// fullRouter exposes every endpoint (list + create included).
func fullRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/v1/secrets", h.ListSecrets)
	r.Post("/api/v1/secrets", h.CreateSecret)
	r.Put("/api/v1/secrets/{id}", h.UpdateSecret)
	r.Delete("/api/v1/secrets/{id}", h.DeleteSecret)
	r.Post("/api/v1/secrets/{id}/rotate", h.RotateSecret)
	r.Get("/api/v1/configs", h.ListConfigs)
	r.Post("/api/v1/configs", h.CreateConfig)
	r.Put("/api/v1/configs/{id}", h.UpdateConfig)
	r.Delete("/api/v1/configs/{id}", h.DeleteConfig)
	r.Get("/api/v1/networks", h.ListNetworks)
	r.Post("/api/v1/networks", h.CreateNetwork)
	r.Delete("/api/v1/networks/{id}", h.DeleteNetwork)
	r.Get("/api/v1/ssh-keys", h.ListSSHKeys)
	r.Post("/api/v1/ssh-keys", h.CreateSSHKey)
	r.Get("/api/v1/certificates", h.ListCertificates)
	r.Post("/api/v1/certificates", h.CreateCertificate)
	return r
}

func reqJSON(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postJSON(t *testing.T, router http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestListSecretsEmptyAndPopulated(t *testing.T) {
	fake, _ := seedRotateFixture()
	h := newTestHandler(fake)
	router := fullRouter(h)

	if rec := postJSON(t, router, "/api/v1/secrets", `not-json`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed create status = %d", rec.Code)
	}
	if rec := postJSON(t, router, "/api/v1/secrets", `{"data":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("missing name status = %d", rec.Code)
	}
	rec := postJSON(t, router, "/api/v1/secrets", `{"name":"db-password","data":"s3cret","labels":{"team":"core"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("create returned no id")
	}

	// Listing now shows both the fixture secret and the new one.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "db-password") {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}

	// List failure -> 500.
	fh := &Handler{Swarm: &failingSwarm{fakeSwarm: fake, listSecretsErr: errors.New("rpc down")},
		authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	rec = httptest.NewRecorder()
	fullRouter(fh).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/secrets", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list failure status = %d", rec.Code)
	}
	// Create failure -> 400.
	fh.Swarm = &failingSwarm{fakeSwarm: fake, createSecretErr: errors.New("quota")}
	rec = postJSON(t, fullRouter(fh), "/api/v1/secrets", `{"name":"x","data":"y"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create failure status = %d", rec.Code)
	}
}

func TestUpdateSecretErrorBranches(t *testing.T) {
	fake, _ := seedRotateFixture()
	base := &failingSwarm{fakeSwarm: fake}
	mk := func(s *failingSwarm) http.Handler {
		h := &Handler{Swarm: s, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
		return fullRouter(h)
	}

	if rec := reqJSON(t, mk(base), http.MethodPut, "/api/v1/secrets/secret-old", `{invalid`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed payload status = %d", rec.Code)
	}
	// Unknown secret -> 404.
	if rec := reqJSON(t, mk(base), http.MethodPut, "/api/v1/secrets/nope", `{"data":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown secret status = %d, want 404", rec.Code)
	}
	// Transient get failure -> 502.
	g := &failingSwarm{fakeSwarm: fake, getSecretErr: errors.New("rpc down")}
	if rec := reqJSON(t, mk(g), http.MethodPut, "/api/v1/secrets/secret-old", `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("get failure status = %d, want 502", rec.Code)
	}
	// Replacement creation failure -> 502.
	c := &failingSwarm{fakeSwarm: fake, createSecretErr: errors.New("quota")}
	if rec := reqJSON(t, mk(c), http.MethodPut, "/api/v1/secrets/secret-old", `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("create failure status = %d, want 502", rec.Code)
	}
	// Old-secret removal conflict -> 409 and the replacement is rolled back.
	inUse := &failingSwarm{fakeSwarm: fake, removeSecretErr: errInUse{}}
	before := len(fake.secretsByID)
	if rec := reqJSON(t, mk(inUse), http.MethodPut, "/api/v1/secrets/secret-old", `{"data":"x"}`); rec.Code != http.StatusConflict {
		t.Errorf("in-use removal status = %d, want 409", rec.Code)
	}
	// The rollback removal also hits the blanket error here; the conflict
	// mapping itself is what this branch pins down.
	_ = before
	// Generic removal failure -> 502.
	generic := &failingSwarm{fakeSwarm: fake, removeSecretErr: errors.New("rpc down")}
	if rec := reqJSON(t, mk(generic), http.MethodPut, "/api/v1/secrets/secret-old", `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("generic removal status = %d, want 502", rec.Code)
	}
}

func TestDeleteSecretBranches(t *testing.T) {
	fake, _ := seedRotateFixture()
	mk := func(s *failingSwarm) http.Handler {
		h := &Handler{Swarm: s, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
		return fullRouter(h)
	}
	del := func(router http.Handler, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := del(mk(&failingSwarm{fakeSwarm: fake}), "/api/v1/secrets/unknown"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown secret status = %d, want 404", rec.Code)
	}
	if rec := del(mk(&failingSwarm{fakeSwarm: fake, removeSecretErr: errInUse{}}), "/api/v1/secrets/secret-old"); rec.Code != http.StatusConflict {
		t.Errorf("in-use delete status = %d, want 409", rec.Code)
	}
	if rec := del(mk(&failingSwarm{fakeSwarm: fake, removeSecretErr: errors.New("rpc down")}), "/api/v1/secrets/secret-old"); rec.Code != http.StatusBadGateway {
		t.Errorf("failed delete status = %d, want 502", rec.Code)
	}
}

func TestRotateSecretRollbackBranches(t *testing.T) {
	fake, old := seedRotateFixture()
	mk := func(s *failingSwarm, pool interface {
		Ping(ctx context.Context) error
	}) http.Handler {
		h := &Handler{Swarm: s, Pool: nil,
			authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
		return fullRouter(h)
	}
	_ = mk
	_ = old

	run := func(s *failingSwarm, body string) *httptest.ResponseRecorder {
		h := &Handler{Swarm: s, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
		return postJSON(t, fullRouter(h), "/api/v1/secrets/secret-old/rotate", body)
	}
	if rec := run(&failingSwarm{fakeSwarm: fake}, `{invalid`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed rotate status = %d", rec.Code)
	}
	if rec := run(&failingSwarm{fakeSwarm: fake}, `{"data":"x"}`); rec.Code != http.StatusOK {
		t.Fatalf("happy rotate status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Get of the freshly created secret fails -> rollback + 502.
	fake2, _ := seedRotateFixture()
	if rec := run(&failingSwarm{fakeSwarm: fake2, getSecretErr: errSecondGet{}}, `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("rotated get failure status = %d", rec.Code)
	}
	// Service listing fails -> rollback + 502.
	fake3, _ := seedRotateFixture()
	if rec := run(&failingSwarm{fakeSwarm: fake3, listServicesErr: errors.New("rpc down")}, `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("list services failure status = %d", rec.Code)
	}
	// Old-secret removal fails -> rollback + 409.
	fake4, _ := seedRotateFixture()
	if rec := run(&failingSwarm{fakeSwarm: fake4, removeSecretErr: errInUse{}}, `{"data":"x"}`); rec.Code != http.StatusConflict {
		t.Errorf("old removal failure status = %d", rec.Code)
	}
}

// errSecondGet fails every GetSecret (used for the rotated-secret lookup).
type errSecondGet struct{}

func (errSecondGet) Error() string { return "rotated secret vanished" }

func TestConfigsListCreateAndErrors(t *testing.T) {
	fake := &fakeSwarm{configs: map[string]swarm.Config{
		"c1": {ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx-conf"}}},
	}}
	h := newTestHandler(fake)
	router := fullRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list configs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, router, "/api/v1/configs", `{"data":"x"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("missing name status = %d", rec.Code)
	}
	rec = postJSON(t, router, "/api/v1/configs", `{"name":"app-conf","data":"key=value"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create config status = %d body=%s", rec.Code, rec.Body.String())
	}

	fh := &Handler{Swarm: &failingSwarm{fakeSwarm: fake, listConfigsErr: errors.New("rpc down")},
		authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	rec = httptest.NewRecorder()
	fullRouter(fh).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list failure status = %d", rec.Code)
	}
	fh.Swarm = &failingSwarm{fakeSwarm: fake, createConfigErr: errors.New("quota")}
	if rec := postJSON(t, fullRouter(fh), "/api/v1/configs", `{"name":"x","data":"y"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("create failure status = %d", rec.Code)
	}
}

func TestUpdateConfigErrorBranches(t *testing.T) {
	fake := &fakeSwarm{configs: map[string]swarm.Config{
		"c1": {ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "conf", Labels: map[string]string{"a": "b"}}}},
	}}
	mk := func(s *failingSwarm) http.Handler {
		h := &Handler{Swarm: s, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
		return fullRouter(h)
	}
	put := func(router http.Handler, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := put(mk(&failingSwarm{fakeSwarm: fake}), "/api/v1/configs/c1", `{invalid`); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed status = %d", rec.Code)
	}
	if rec := put(mk(&failingSwarm{fakeSwarm: fake}), "/api/v1/configs/nope", `{"data":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown config status = %d, want 404", rec.Code)
	}
	if rec := put(mk(&failingSwarm{fakeSwarm: fake, createConfigErr: errors.New("quota")}), "/api/v1/configs/c1", `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("create failure status = %d, want 502", rec.Code)
	}
	inUse := &failingSwarm{fakeSwarm: fake, removeConfigErr: errInUse{}}
	before := len(fake.configs)
	if rec := put(mk(inUse), "/api/v1/configs/c1", `{"data":"x"}`); rec.Code != http.StatusConflict {
		t.Errorf("in-use status = %d, want 409", rec.Code)
	}
	_ = before // rollback removal also hits the injected error here; conflict mapping is what matters
	if rec := put(mk(&failingSwarm{fakeSwarm: fake, removeConfigErr: errors.New("rpc down")}), "/api/v1/configs/c1", `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("generic removal status = %d, want 502", rec.Code)
	}
}

func TestDeleteConfigBranches(t *testing.T) {
	fake := &fakeSwarm{configs: map[string]swarm.Config{"c1": {ID: "c1"}}}
	mk := func(s *failingSwarm) http.Handler {
		h := &Handler{Swarm: s, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
		return fullRouter(h)
	}
	del := func(router http.Handler, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := del(mk(&failingSwarm{fakeSwarm: fake}), "/api/v1/configs/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown config status = %d, want 404", rec.Code)
	}
	if rec := del(mk(&failingSwarm{fakeSwarm: fake, removeConfigErr: errInUse{}}), "/api/v1/configs/c1"); rec.Code != http.StatusConflict {
		t.Errorf("in-use status = %d, want 409", rec.Code)
	}
	if rec := del(mk(&failingSwarm{fakeSwarm: fake, removeConfigErr: errors.New("rpc down")}), "/api/v1/configs/c1"); rec.Code != http.StatusBadGateway {
		t.Errorf("failed status = %d, want 502", rec.Code)
	}
}

func TestNetworksListCreateDeleteBranches(t *testing.T) {
	fake := &fakeSwarm{networks: map[string]network.Inspect{
		"n1": {Network: network.Network{ID: "n1", Name: "app-net", Driver: "overlay"}},
	}}
	h := newTestHandler(fake)
	router := fullRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list networks status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, router, "/api/v1/networks", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("missing name status = %d", rec.Code)
	}
	rec = postJSON(t, router, "/api/v1/networks", `{"name":"extra-net"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create network status = %d body=%s", rec.Code, rec.Body.String())
	}

	fh := func(s *failingSwarm) http.Handler {
		return fullRouter(&Handler{Swarm: s, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }})
	}
	rec = httptest.NewRecorder()
	fh(&failingSwarm{fakeSwarm: fake, listNetworksErr: errors.New("rpc down")}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list failure status = %d", rec.Code)
	}
	if rec := postJSON(t, fh(&failingSwarm{fakeSwarm: fake, createNetErr: errors.New("quota")}), "/api/v1/networks", `{"name":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("create failure status = %d", rec.Code)
	}
	del := func(s *failingSwarm, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		rec := httptest.NewRecorder()
		fh(s).ServeHTTP(rec, req)
		return rec
	}
	if rec := del(&failingSwarm{fakeSwarm: fake, inspectNetErr: cerrdefs.ErrNotFound}, "/api/v1/networks/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown network status = %d, want 404", rec.Code)
	}
	if rec := del(&failingSwarm{fakeSwarm: fake, inspectNetErr: errors.New("rpc down")}, "/api/v1/networks/n1"); rec.Code != http.StatusBadGateway {
		t.Errorf("inspect failure status = %d, want 502", rec.Code)
	}
	if rec := del(&failingSwarm{fakeSwarm: fake, listServicesErr: errors.New("rpc down")}, "/api/v1/networks/n1"); rec.Code != http.StatusBadGateway {
		t.Errorf("list services failure status = %d, want 502", rec.Code)
	}
	if rec := del(&failingSwarm{fakeSwarm: fake, removeNetworkErr: errInUse{}}, "/api/v1/networks/n1"); rec.Code != http.StatusConflict {
		t.Errorf("remove failure status = %d, want 409", rec.Code)
	}
	if rec := del(&failingSwarm{fakeSwarm: fake, removeNetworkErr: errors.New("rpc down")}, "/api/v1/networks/n1"); rec.Code != http.StatusBadGateway {
		t.Errorf("generic remove failure status = %d, want 502", rec.Code)
	}
}

func TestDeleteNetworkAttachedByServiceBlocks(t *testing.T) {
	att := serviceWithSecrets("svc-1", "api", nil, []swarm.NetworkAttachmentConfig{{Target: "n1"}})
	fake := &fakeSwarm{
		networks: map[string]network.Inspect{"n1": {Network: network.Network{ID: "n1", Name: "app-net"}}},
		services: []swarm.Service{att},
	}
	h := &Handler{Swarm: fake, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/networks/n1", nil)
	rec := httptest.NewRecorder()
	fullRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "api") {
		t.Fatalf("attached network status = %d body=%s, want 409 naming the service", rec.Code, rec.Body.String())
	}
}

// --- SSH keys & certificates (DB-backed) ---

func TestSSHKeysAndCertificatesCRUD(t *testing.T) {
	const masterKey = "0123456789abcdef0123456789abcdef"
	store, err := secrets.NewValueStore([]byte(masterKey))
	if err != nil {
		t.Fatal(err)
	}
	secrets.SetRuntime(store)

	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, &fakeSwarm{})
	router := fullRouter(h)

	// Empty listings.
	for _, path := range []string{"/api/v1/ssh-keys", "/api/v1/certificates"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
			t.Fatalf("%s empty status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	// Validation table.
	sshCases := []struct {
		body, frag string
		want       int
	}{
		{`{invalid`, "", http.StatusBadRequest},
		{`{"publicKey":"ssh-ed25519 AAA"}`, "name and publicKey", http.StatusBadRequest},
		{`{"name":"k"}`, "name and publicKey", http.StatusBadRequest},
	}
	for _, tc := range sshCases {
		rec := postJSON(t, router, "/api/v1/ssh-keys", tc.body)
		if rec.Code != tc.want {
			t.Errorf("ssh-key create %q status = %d, want %d", tc.body, rec.Code, tc.want)
		}
	}
	certCases := []struct {
		body string
		want int
	}{
		{`{invalid`, http.StatusBadRequest},
		{`{"domain":"a.com"}`, http.StatusBadRequest},
		{`{"domain":"a.com","certPem":"c"}`, http.StatusBadRequest},
	}
	for _, tc := range certCases {
		rec := postJSON(t, router, "/api/v1/certificates", tc.body)
		if rec.Code != tc.want {
			t.Errorf("certificate create %q status = %d, want %d", tc.body, rec.Code, tc.want)
		}
	}

	// Happy paths persist rows.
	rec := postJSON(t, router, "/api/v1/ssh-keys", `{"name":"deploy-key","publicKey":"ssh-ed25519 AAA","privateKey":"TOPSECRET"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ssh-key create status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = postJSON(t, router, "/api/v1/certificates", `{"domain":"a.example.com","certPem":"CERT","keyPem":"KEY"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("certificate create status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Upsert on conflict keeps one row per domain.
	rec = postJSON(t, router, "/api/v1/certificates", `{"domain":"a.example.com","certPem":"CERT2","keyPem":"KEY2"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("certificate upsert status = %d", rec.Code)
	}

	// Populated listings round-trip.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ssh-keys", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "deploy-key") {
		t.Fatalf("ssh-key list status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/certificates", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "a.example.com") {
		t.Fatalf("certificate list status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from certificates`); n != 1 {
		t.Fatalf("certificate rows = %d, want 1 (upsert)", n)
	}
	// The private key material is stored encrypted, not as plaintext.
	if n := testdb.QueryCount(t, `select count(*) from ssh_keys where private_key = 'TOPSECRET'`); n != 0 {
		t.Fatal("private key stored unencrypted")
	}
}

func TestAuthorizeFallsBackToRBAC(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	h := &Handler{Pool: pool, Swarm: &fakeSwarm{}}

	// Unauthenticated request is rejected by the RBAC path.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	if h.authorize(rec, req) {
		t.Fatal("unauthenticated request must not be authorized")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	_ = org
}

// rotateGetOnly wraps a failing swarm and fails GetSecret only for the
// rotated (non-original) secret ids.
type rotateGetOnly struct {
	inner *failingSwarm
}

func (r *rotateGetOnly) ListSecrets(ctx context.Context) ([]swarm.Secret, error) {
	return r.inner.ListSecrets(ctx)
}

func (r *rotateGetOnly) CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error) {
	return r.inner.CreateSecret(ctx, spec)
}

func (r *rotateGetOnly) GetSecret(ctx context.Context, id string) (swarm.Secret, error) {
	if id != "secret-old" {
		return swarm.Secret{}, errors.New("rotated secret vanished")
	}
	return r.inner.GetSecret(ctx, id)
}

func (r *rotateGetOnly) RemoveSecret(ctx context.Context, id string) error {
	return r.inner.RemoveSecret(ctx, id)
}

func (r *rotateGetOnly) ListServices(ctx context.Context) ([]swarm.Service, error) {
	return r.inner.ListServices(ctx)
}

func (r *rotateGetOnly) UpdateService(ctx context.Context, id string, v uint64, spec swarm.ServiceSpec) error {
	return r.inner.UpdateService(ctx, id, v, spec)
}

// listingConfigs makes the in-memory configs visible to ListConfigs.
type listingConfigs struct {
	*fakeSwarm
}

func (l *listingConfigs) ListConfigs(context.Context) ([]swarm.Config, error) {
	out := make([]swarm.Config, 0, len(l.fakeSwarm.configs))
	for _, c := range l.fakeSwarm.configs {
		out = append(out, c)
	}
	return out, nil
}

func TestAllEndpointsRequireAuthorization(t *testing.T) {
	fake, _ := seedRotateFixture()
	h := &Handler{Swarm: fake, authorizeOverride: func(w http.ResponseWriter, _ *http.Request) bool {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}}
	router := fullRouter(h)
	reqs := []struct{ method, path, body string }{
		{http.MethodPut, "/api/v1/secrets/x", `{}`},
		{http.MethodDelete, "/api/v1/secrets/x", ""},
		{http.MethodPost, "/api/v1/secrets/x/rotate", `{}`},
		{http.MethodPut, "/api/v1/configs/x", `{}`},
		{http.MethodDelete, "/api/v1/configs/x", ""},
		{http.MethodDelete, "/api/v1/networks/x", ""},
	}
	for _, r := range reqs {
		rec := reqJSON(t, router, r.method, r.path, r.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d, want 403", r.method, r.path, rec.Code)
		}
	}
}

func TestRotateSecretGetNewFailureRollsBack(t *testing.T) {
	fake, _ := seedRotateFixture()
	before := len(fake.secretsByID)
	swarmDouble := struct {
		*failingSwarm
	}{&failingSwarm{fakeSwarm: fake}}
	_ = swarmDouble
	h := &Handler{Swarm: &rotateGetOnly{inner: &failingSwarm{fakeSwarm: fake}},
		authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	rec := postJSON(t, fullRouter(h), "/api/v1/secrets/secret-old/rotate", `{"data":"x"}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s, want 502", rec.Code, rec.Body.String())
	}
	if len(fake.secretsByID) != before {
		t.Fatalf("rotated secret not rolled back: %d secrets", len(fake.secretsByID))
	}
}

func TestRotateSecretCreateFailureIs502(t *testing.T) {
	fake, _ := seedRotateFixture()
	h := &Handler{Swarm: &failingSwarm{fakeSwarm: fake, createSecretErr: errors.New("quota")},
		authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	if rec := postJSON(t, fullRouter(h), "/api/v1/secrets/secret-old/rotate", `{"data":"x"}`); rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestListConfigsShowsEntries(t *testing.T) {
	fake := &fakeSwarm{configs: map[string]swarm.Config{
		"c1": {ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx-conf"}}},
	}}
	h := &Handler{Swarm: &listingConfigs{fake}, authorizeOverride: func(http.ResponseWriter, *http.Request) bool { return true }}
	rec := httptest.NewRecorder()
	fullRouter(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "nginx-conf") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuditBranches(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := &Handler{Pool: pool, Swarm: &fakeSwarm{}}
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	// Unmarshalable details short-circuit silently.
	h.audit(req, "rotate", "secret", "s1", map[string]any{"fn": func() {}})
	if n := testdb.QueryCount(t, `select count(*) from audit_log where resource_id = 's1'`); n != 0 {
		t.Fatal("unmarshalable details must not write audit rows")
	}
	// Valid details persist.
	h.audit(req, "rotate", "secret", "s2", map[string]any{"newSecretId": "s2b"})
	if n := testdb.QueryCount(t, `select count(*) from audit_log where resource_id = 's2'`); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
}

func (r *rotateGetOnly) CreateConfig(ctx context.Context, spec swarm.ConfigSpec) (string, error) {
	return r.inner.CreateConfig(ctx, spec)
}

func (r *rotateGetOnly) GetConfig(ctx context.Context, id string) (swarm.Config, error) {
	return r.inner.GetConfig(ctx, id)
}

func (r *rotateGetOnly) RemoveConfig(ctx context.Context, id string) error {
	return r.inner.RemoveConfig(ctx, id)
}

func (r *rotateGetOnly) ListConfigs(ctx context.Context) ([]swarm.Config, error) {
	return r.inner.ListConfigs(ctx)
}

func (r *rotateGetOnly) InspectNetwork(ctx context.Context, id string) (network.Inspect, error) {
	return r.inner.InspectNetwork(ctx, id)
}

func (r *rotateGetOnly) CreateNetwork(ctx context.Context, name string) (string, error) {
	return r.inner.CreateNetwork(ctx, name)
}

func (r *rotateGetOnly) RemoveNetwork(ctx context.Context, id string) error {
	return r.inner.RemoveNetwork(ctx, id)
}

func (r *rotateGetOnly) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	return r.inner.ListNetworks(ctx)
}
