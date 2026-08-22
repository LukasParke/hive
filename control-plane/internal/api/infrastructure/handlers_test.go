package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"
	"github.com/moby/moby/api/types/network"
	swarm "github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/secrets"
)

// fakeSwarm is an in-memory SwarmAPI double recording mutations.
type fakeSwarm struct {
	secretsByID map[string]swarm.Secret
	configs     map[string]swarm.Config
	networks    map[string]network.Inspect
	services    []swarm.Service

	removedSecrets  []string
	createdSecrets  []swarm.SecretSpec
	updatedServices []serviceUpdate
	removeSecretErr error
	removeNetErr    error
	updateSvcErr    error
	nextID          int
}

type serviceUpdate struct {
	serviceID string
	version   uint64
	spec      swarm.ServiceSpec
}

func (f *fakeSwarm) ListSecrets(context.Context) ([]swarm.Secret, error) {
	out := make([]swarm.Secret, 0, len(f.secretsByID))
	for _, s := range f.secretsByID {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSwarm) CreateSecret(_ context.Context, spec swarm.SecretSpec) (string, error) {
	f.nextID++
	id := spec.Name + "-new" + string(rune('0'+f.nextID)) //nolint:gosec // test-only digit suffix
	f.createdSecrets = append(f.createdSecrets, spec)
	if f.secretsByID == nil {
		f.secretsByID = map[string]swarm.Secret{}
	}
	f.secretsByID[id] = swarm.Secret{ID: id, Spec: spec}
	return id, nil
}

func (f *fakeSwarm) GetSecret(_ context.Context, id string) (swarm.Secret, error) {
	s, ok := f.secretsByID[id]
	if !ok {
		return swarm.Secret{}, cerrdefs.ErrNotFound
	}
	return s, nil
}

func (f *fakeSwarm) RemoveSecret(_ context.Context, id string) error {
	if f.removeSecretErr != nil {
		return f.removeSecretErr
	}
	f.removedSecrets = append(f.removedSecrets, id)
	delete(f.secretsByID, id)
	return nil
}

func (f *fakeSwarm) ListConfigs(context.Context) ([]swarm.Config, error) { return nil, nil }

func (f *fakeSwarm) CreateConfig(_ context.Context, spec swarm.ConfigSpec) (string, error) {
	f.nextID++
	id := spec.Name + "-new" + string(rune('0'+f.nextID)) //nolint:gosec // test-only digit suffix
	if f.configs == nil {
		f.configs = map[string]swarm.Config{}
	}
	f.configs[id] = swarm.Config{ID: id, Spec: spec}
	return id, nil
}

func (f *fakeSwarm) GetConfig(_ context.Context, id string) (swarm.Config, error) {
	c, ok := f.configs[id]
	if !ok {
		return swarm.Config{}, cerrdefs.ErrNotFound
	}
	return c, nil
}

func (f *fakeSwarm) RemoveConfig(_ context.Context, id string) error {
	delete(f.configs, id)
	return nil
}

func (f *fakeSwarm) ListNetworks(context.Context) ([]network.Summary, error) { return nil, nil }

func (f *fakeSwarm) InspectNetwork(_ context.Context, id string) (network.Inspect, error) {
	nw, ok := f.networks[id]
	if !ok {
		return network.Inspect{}, cerrdefs.ErrNotFound
	}
	return nw, nil
}

func (f *fakeSwarm) CreateNetwork(context.Context, string) (string, error) { return "", nil }

func (f *fakeSwarm) RemoveNetwork(_ context.Context, id string) error {
	if f.removeNetErr != nil {
		return f.removeNetErr
	}
	delete(f.networks, id)
	return nil
}

func (f *fakeSwarm) ListServices(context.Context) ([]swarm.Service, error) {
	return f.services, nil
}

func (f *fakeSwarm) UpdateService(_ context.Context, serviceID string, version uint64, spec swarm.ServiceSpec) error {
	if f.updateSvcErr != nil {
		return f.updateSvcErr
	}
	f.updatedServices = append(f.updatedServices, serviceUpdate{serviceID: serviceID, version: version, spec: spec})
	return nil
}

// secretRef builds a container secret reference for the given secret ID.
func secretRef(secretID, targetName string) *swarm.SecretReference {
	return &swarm.SecretReference{
		SecretID:   secretID,
		SecretName: "db-password",
		File:       &swarm.SecretReferenceFileTarget{Name: targetName},
	}
}

// serviceWithSecrets builds a service referencing the given refs/networks.
func serviceWithSecrets(id, name string, refs []*swarm.SecretReference, networks []swarm.NetworkAttachmentConfig) swarm.Service {
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: name},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Secrets: refs},
			Networks:      networks,
		},
	}
	return swarm.Service{
		Meta: swarm.Meta{Version: swarm.Version{Index: 3}},
		Spec: spec,
	}
}

func newTestHandler(fake *fakeSwarm) *Handler {
	h := NewHandler(nil, fake)
	h.authorizeOverride = func(http.ResponseWriter, *http.Request) bool { return true }
	return h
}

func serve(t *testing.T, h *Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Put("/api/v1/secrets/{id}", h.UpdateSecret)
	r.Delete("/api/v1/secrets/{id}", h.DeleteSecret)
	r.Post("/api/v1/secrets/{id}/rotate", h.RotateSecret)
	r.Put("/api/v1/configs/{id}", h.UpdateConfig)
	r.Delete("/api/v1/configs/{id}", h.DeleteConfig)
	r.Delete("/api/v1/networks/{id}", h.DeleteNetwork)

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON body %q: %v", rec.Body.String(), err)
	}
	return out
}

// seedRotateFixture builds one old secret plus three services: two that
// reference it and one bystander that does not.
func seedRotateFixture() (*fakeSwarm, swarm.Secret) {
	old := swarm.Secret{
		ID:   "secret-old",
		Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "db-password"}},
	}
	fake := &fakeSwarm{
		secretsByID: map[string]swarm.Secret{"secret-old": old},
		services: []swarm.Service{
			serviceWithSecrets("svc-1", "api", []*swarm.SecretReference{secretRef("secret-old", "/run/secrets/db-password")}, nil),
			serviceWithSecrets("svc-2", "worker", []*swarm.SecretReference{
				{SecretID: "unrelated", SecretName: "tls"},
				secretRef("secret-old", "/run/secrets/db-password"),
			}, nil),
			serviceWithSecrets("svc-3", "bystander", []*swarm.SecretReference{{SecretID: "other"}}, nil),
		},
	}
	return fake, old
}

func TestRotateSecretRepointsReferencingServices(t *testing.T) {
	fake, old := seedRotateFixture()
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPost, "/api/v1/secrets/secret-old/rotate",
		map[string]any{"data": "brand-new-value"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["status"] != "ok" {
		t.Fatalf("body = %v, want status ok", body)
	}

	// Exactly the two referencing services are updated; the bystander is not.
	if len(fake.updatedServices) != 2 {
		t.Fatalf("service updates = %d, want 2 (%+v)", len(fake.updatedServices), fake.updatedServices)
	}
	newIDs := map[string]bool{}
	for _, upd := range fake.updatedServices {
		if upd.serviceID == "svc-3" {
			t.Fatal("bystander service must not be touched")
		}
		if upd.version != 3 {
			t.Fatalf("service %s updated at version %d, want 3", upd.serviceID, upd.version)
		}
		for _, ref := range upd.spec.TaskTemplate.ContainerSpec.Secrets {
			if ref != nil && ref.File != nil && ref.File.Name != "/run/secrets/db-password" {
				t.Fatalf("container target changed to %q; it must stay stable", ref.File.Name)
			}
			if ref.SecretID != "unrelated" && ref.SecretID != "secret-old" {
				newIDs[ref.SecretID] = true
			}
		}
	}
	if len(newIDs) != 1 {
		t.Fatalf("referencing services must be re-pointed at exactly one new secret, got %v", newIDs)
	}
	var newID string
	for id := range newIDs {
		newID = id
	}

	// The versioned successor carries the new payload and the old secret is gone.
	created, ok := fake.secretsByID[newID]
	if !ok {
		t.Fatalf("rotated secret %q missing from store", newID)
	}
	if string(created.Spec.Data) != "brand-new-value" {
		t.Fatalf("rotated data = %q, want brand-new-value", created.Spec.Data)
	}
	if !strings.HasPrefix(created.Spec.Name, old.Spec.Name+"-r") {
		t.Fatalf("successor name %q should be a versioned form of %q", created.Spec.Name, old.Spec.Name)
	}
	if _, stillThere := fake.secretsByID["secret-old"]; stillThere {
		t.Fatal("old secret was not removed after rotation")
	}
}

func TestRotateSecretRollsBackOnServiceUpdateFailure(t *testing.T) {
	fake, _ := seedRotateFixture()
	fake.updateSvcErr = cerrdefs.ErrInternal
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPost, "/api/v1/secrets/secret-old/rotate",
		map[string]any{"data": "x"})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body.String())
	}
	if _, stillThere := fake.secretsByID["secret-old"]; !stillThere {
		t.Fatal("old secret must survive a failed rotation")
	}
	rolledBack := false
	for _, removed := range fake.removedSecrets {
		if strings.HasPrefix(removed, "db-password-r") {
			rolledBack = true
		}
	}
	if !rolledBack {
		t.Fatalf("failed rotation must remove the orphaned successor; removed: %v", fake.removedSecrets)
	}
}

func TestDeleteConfigSuccessAndNotFound(t *testing.T) {
	fake := &fakeSwarm{configs: map[string]swarm.Config{
		"c1": {ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx"}}},
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/configs/c1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if _, gone := fake.configs["c1"]; gone {
		t.Fatal("config should have been removed")
	}

	rec = serve(t, h, http.MethodDelete, "/api/v1/configs/c1", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for missing config", rec.Code)
	}
}
func TestUpdateSecretReplaceFlow(t *testing.T) {
	fake := &fakeSwarm{secretsByID: map[string]swarm.Secret{
		"s1": {ID: "s1", Spec: swarm.SecretSpec{Annotations: swarm.Annotations{Name: "cfg"}}},
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPut, "/api/v1/secrets/s1", map[string]any{"data": "v2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(fake.createdSecrets) != 1 || string(fake.createdSecrets[0].Data) != "v2" {
		t.Fatalf("replacement secrets = %+v, want one with v2 payload", fake.createdSecrets)
	}
	if fake.createdSecrets[0].Name != "cfg" {
		t.Fatalf("replacement name = %q, want cfg preserved", fake.createdSecrets[0].Name)
	}
	if len(fake.removedSecrets) != 1 || fake.removedSecrets[0] != "s1" {
		t.Fatalf("removed secrets = %v, want [s1]", fake.removedSecrets)
	}
}

func TestDeleteSecretInUseConflicts(t *testing.T) {
	fake := &fakeSwarm{secretsByID: map[string]swarm.Secret{"s1": {ID: "s1"}}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/secrets/s1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for free secret: %s", rec.Code, rec.Body.String())
	}

	inUse := &fakeSwarm{
		secretsByID:     map[string]swarm.Secret{"s1": {ID: "s1"}},
		removeSecretErr: errInUse{},
	}
	rec = serve(t, newTestHandler(inUse), http.MethodDelete, "/api/v1/secrets/s1", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for in-use secret: %s", rec.Code, rec.Body.String())
	}
}

type errInUse struct{}

func (errInUse) Error() string { return "rpc error: secret s1 is in use by service api" }

func TestDeleteNetworkBlockedWhenServicesAttached(t *testing.T) {
	nw := network.Inspect{Network: network.Network{ID: "net-1", Name: "app_net"}}
	fake := &fakeSwarm{
		networks: map[string]network.Inspect{"net-1": nw},
		services: []swarm.Service{
			serviceWithSecrets("svc-1", "web", nil, []swarm.NetworkAttachmentConfig{{Target: "net-1"}}),
		},
	}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/networks/net-1", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if _, stillThere := fake.networks["net-1"]; !stillThere {
		t.Fatal("attached network must not be removed")
	}

	// With no attachments left the delete succeeds.
	fake.services = nil
	rec = serve(t, h, http.MethodDelete, "/api/v1/networks/net-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if _, gone := fake.networks["net-1"]; gone {
		t.Fatal("network should have been removed")
	}
}

func TestDeleteNetworkMatchesByNameToo(t *testing.T) {
	nw := network.Inspect{Network: network.Network{ID: "net-9", Name: "app_net"}}
	fake := &fakeSwarm{
		networks: map[string]network.Inspect{"net-9": nw},
		services: []swarm.Service{
			serviceWithSecrets("svc-1", "web", nil, []swarm.NetworkAttachmentConfig{{Target: "app_net"}}),
		},
	}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodDelete, "/api/v1/networks/net-9", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when attachment targets the network name", rec.Code)
	}
}

func TestUpdateConfigRemoveCreatePreservesName(t *testing.T) {
	fake := &fakeSwarm{configs: map[string]swarm.Config{
		"c1": {ID: "c1", Spec: swarm.ConfigSpec{Annotations: swarm.Annotations{Name: "nginx"}}},
	}}
	h := newTestHandler(fake)

	rec := serve(t, h, http.MethodPut, "/api/v1/configs/c1", map[string]any{"data": "listen 80;"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var replacement *swarm.Config
	for _, cfg := range fake.configs {
		if cfg.ID != "c1" {
			cfgCopy := cfg
			replacement = &cfgCopy
		}
	}
	if replacement == nil {
		t.Fatal("no replacement config created")
	}
	if replacement.Spec.Name != "nginx" || string(replacement.Spec.Data) != "listen 80;" {
		t.Fatalf("replacement = %+v, want nginx config with new data", replacement.Spec)
	}
	if _, gone := fake.configs["c1"]; gone {
		t.Fatal("old config was not removed")
	}
}

func TestSSHKeyEncryptOnWrite(t *testing.T) {
	const masterKey = "0123456789abcdef0123456789abcdef" // 32 bytes
	store, err := secrets.NewValueStore([]byte(masterKey))
	if err != nil {
		t.Fatal(err)
	}
	secrets.SetRuntime(store)

	plaintext := "-----BEGIN OPENSSH PRIVATE KEY-----b3BlbnNzaC1rZXktdjEAAAAA-----END OPENSSH PRIVATE KEY-----"
	sealed, sealErr := sealSensitive("ssh_key", plaintext)
	if sealErr != nil {
		t.Fatalf("seal: %v", sealErr)
	}
	if sealed == plaintext {
		t.Fatal("private key stored verbatim; encrypt-on-write failed")
	}
	if !strings.HasPrefix(sealed, secrets.EncryptedPrefix) {
		t.Fatalf("sealed value lacks the encrypted marker prefix: %.20s...", sealed)
	}
	opened, openErr := secrets.OpenValue("ssh_key", sealed)
	if openErr != nil {
		t.Fatalf("open: %v", openErr)
	}
	if string(opened) != plaintext {
		t.Fatal("round-trip through seal/open does not restore the private key")
	}

	// Legacy plaintext rows pass through untouched so pre-encryption keys
	// keep materializing.
	legacy, legacyErr := secrets.OpenValue("ssh_key", plaintext)
	if legacyErr != nil {
		t.Fatalf("legacy open: %v", legacyErr)
	}
	if string(legacy) != plaintext {
		t.Fatal("legacy plaintext value was altered on read")
	}
}
