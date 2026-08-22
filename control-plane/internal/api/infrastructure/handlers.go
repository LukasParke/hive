package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/network"
	dockerswarm "github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/secrets"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

// SwarmAPI is the subset of the swarm client the infrastructure handlers
// need. *swarmclient.Client satisfies it; tests supply a fake.
type SwarmAPI interface {
	ListSecrets(ctx context.Context) ([]dockerswarm.Secret, error)
	CreateSecret(ctx context.Context, spec dockerswarm.SecretSpec) (string, error)
	GetSecret(ctx context.Context, id string) (dockerswarm.Secret, error)
	RemoveSecret(ctx context.Context, id string) error

	ListConfigs(ctx context.Context) ([]dockerswarm.Config, error)
	CreateConfig(ctx context.Context, spec dockerswarm.ConfigSpec) (string, error)
	GetConfig(ctx context.Context, id string) (dockerswarm.Config, error)
	RemoveConfig(ctx context.Context, id string) error

	ListNetworks(ctx context.Context) ([]network.Summary, error)
	InspectNetwork(ctx context.Context, id string) (network.Inspect, error)
	CreateNetwork(ctx context.Context, name string) (string, error)
	RemoveNetwork(ctx context.Context, id string) error

	ListServices(ctx context.Context) ([]dockerswarm.Service, error)
	UpdateService(ctx context.Context, id string, version uint64, spec dockerswarm.ServiceSpec) error
}

var _ SwarmAPI = (*swarmclient.Client)(nil)

// Handler serves swarm infrastructure endpoints (secrets, configs, networks, SSH keys, certificates).
type Handler struct {
	Pool  *pgxpool.Pool
	Swarm SwarmAPI

	// authorizeOverride replaces the org-admin RBAC gate on destructive
	// endpoints. Nil in production; tests set it to exercise handlers
	// without a database.
	authorizeOverride func(w http.ResponseWriter, r *http.Request) bool
}

// NewHandler returns an infrastructure Handler wired to the given dependencies.
func NewHandler(pool *pgxpool.Pool, swarm SwarmAPI) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.authorizeOverride != nil {
		return h.authorizeOverride(w, r)
	}
	_, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	return ok
}

// writeSwarmError maps docker client errors to openapi responses.
func writeSwarmError(w http.ResponseWriter, kind, id string, err error) {
	switch {
	case cerrdefs.IsNotFound(err):
		common.WriteError(w, http.StatusNotFound, "not_found", fmt.Sprintf("%s %s not found", kind, id))
	case strings.Contains(err.Error(), "in use"):
		common.WriteError(w, http.StatusConflict, "conflict", fmt.Sprintf("%s %s is still in use", kind, id))
	default:
		common.WriteError(w, http.StatusBadGateway, "runtime_error", fmt.Sprintf("swarm request for %s %s failed", kind, id))
	}
}

// sealSensitive encrypts private key material with the runtime secrets
// store. Without a master key the value is stored as-is.
func sealSensitive(contextType, value string) (string, error) {
	return secrets.SealValue(contextType, []byte(value))
}

// ListSecrets lists swarm secrets for the organization.
func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	items, err := h.Swarm.ListSecrets(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, sec := range items {
		out = append(out, map[string]any{"id": sec.ID, "name": sec.Spec.Name, "createdAt": sec.CreatedAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateSecret stores a new swarm secret.
func (h *Handler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string            `json:"name"`
		Data   string            `json:"data"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	id, err := h.Swarm.CreateSecret(r.Context(), dockerswarm.SecretSpec{
		Annotations: dockerswarm.Annotations{Name: req.Name, Labels: req.Labels},
		Data:        []byte(req.Data),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// UpdateSecret replaces a secret's payload. Swarm secrets are immutable, so
// the update creates a replacement secret with the same name and removes the
// old one; removing fails while any service still references the old secret,
// which surfaces as a conflict. The secret's ID therefore changes — services
// that must survive rotation should use POST /secrets/{id}/rotate, which
// re-points them first.
func (h *Handler) UpdateSecret(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Data   string            `json:"data"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	old, err := h.Swarm.GetSecret(r.Context(), id)
	if err != nil {
		writeSwarmError(w, "secret", id, err)
		return
	}
	labels := req.Labels
	if labels == nil {
		labels = old.Spec.Labels
	}
	newID, err := h.Swarm.CreateSecret(r.Context(), dockerswarm.SecretSpec{
		Annotations: dockerswarm.Annotations{Name: old.Spec.Name, Labels: labels},
		Data:        []byte(req.Data),
	})
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to create replacement secret")
		return
	}
	if rmErr := h.Swarm.RemoveSecret(r.Context(), id); rmErr != nil {
		_ = h.Swarm.RemoveSecret(r.Context(), newID)
		writeSwarmError(w, "secret", id, rmErr)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteSecret removes a Swarm secret.
func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := h.Swarm.GetSecret(r.Context(), id); err != nil {
		writeSwarmError(w, "secret", id, err)
		return
	}
	if err := h.Swarm.RemoveSecret(r.Context(), id); err != nil {
		writeSwarmError(w, "secret", id, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RotateSecret replaces a secret's value without breaking its consumers:
// it creates a versioned successor secret, re-points every service that
// references the old secret at the new one, removes the old secret and
// records an audit_log entry.
func (h *Handler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Data   string            `json:"data"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	old, err := h.Swarm.GetSecret(r.Context(), id)
	if err != nil {
		writeSwarmError(w, "secret", id, err)
		return
	}
	labels := req.Labels
	if labels == nil {
		labels = old.Spec.Labels
	}
	newID, err := h.Swarm.CreateSecret(r.Context(), dockerswarm.SecretSpec{
		Annotations: dockerswarm.Annotations{
			Name:   fmt.Sprintf("%s-r%d", old.Spec.Name, time.Now().Unix()),
			Labels: labels,
		},
		Data: []byte(req.Data),
	})
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to create rotated secret")
		return
	}
	newSecret, err := h.Swarm.GetSecret(r.Context(), newID)
	if err != nil {
		_ = h.Swarm.RemoveSecret(r.Context(), newID)
		writeSwarmError(w, "secret", newID, err)
		return
	}
	services, err := h.Swarm.ListServices(r.Context())
	if err != nil {
		_ = h.Swarm.RemoveSecret(r.Context(), newID)
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to list services")
		return
	}
	for _, svc := range services {
		var refs []*dockerswarm.SecretReference
		if svc.Spec.TaskTemplate.ContainerSpec != nil {
			refs = svc.Spec.TaskTemplate.ContainerSpec.Secrets
		}
		if !referencesSecret(refs, id) {
			continue
		}
		spec := svc.Spec
		containerSpec := *spec.TaskTemplate.ContainerSpec
		containerSpec.Secrets = swapSecretRefs(refs, id, newID, newSecret.Spec.Name)
		spec.TaskTemplate.ContainerSpec = &containerSpec
		if err := h.Swarm.UpdateService(r.Context(), svc.ID, svc.Version.Index, spec); err != nil {
			_ = h.Swarm.RemoveSecret(r.Context(), newID)
			common.WriteError(w, http.StatusBadGateway, "runtime_error", fmt.Sprintf("failed to update service %s during rotation: %v", svc.Spec.Name, err))
			return
		}
	}
	if err := h.Swarm.RemoveSecret(r.Context(), id); err != nil {
		_ = h.Swarm.RemoveSecret(r.Context(), newID)
		writeSwarmError(w, "secret", id, err)
		return
	}
	h.audit(r, "rotate", "secret", id, map[string]any{"newSecretId": newID, "name": old.Spec.Name})
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// referencesSecret reports whether the container spec references secretID.
func referencesSecret(refs []*dockerswarm.SecretReference, secretID string) bool {
	for _, ref := range refs {
		if ref != nil && ref.SecretID == secretID {
			return true
		}
	}
	return false
}

// swapSecretRefs returns copies of the refs pointing at newSecretID,
// preserving each reference's container target so in-container paths stay
// stable across the rotation.
func swapSecretRefs(refs []*dockerswarm.SecretReference, oldID, newID, newName string) []*dockerswarm.SecretReference {
	out := make([]*dockerswarm.SecretReference, len(refs))
	copy(out, refs)
	for _, ref := range out {
		if ref != nil && ref.SecretID == oldID {
			ref.SecretID = newID
			ref.SecretName = newName
		}
	}
	return out
}

// audit records an action in the audit_log table. Logging failures never
// fail the handler.
func (h *Handler) audit(r *http.Request, action, resourceType, resourceID string, details map[string]any) {
	if h.Pool == nil {
		return
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return
	}
	_, _ = h.Pool.Exec(r.Context(),
		`insert into audit_log(action, resource_type, resource_id, details) values ($1, $2, $3, $4::jsonb)`,
		action, resourceType, resourceID, string(payload))
}

// ListConfigs lists swarm configs for the organization.
func (h *Handler) ListConfigs(w http.ResponseWriter, r *http.Request) {
	items, err := h.Swarm.ListConfigs(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, cfg := range items {
		out = append(out, map[string]any{"id": cfg.ID, "name": cfg.Spec.Name, "createdAt": cfg.CreatedAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateConfig stores a new swarm config.
func (h *Handler) CreateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string            `json:"name"`
		Data   string            `json:"data"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	id, err := h.Swarm.CreateConfig(r.Context(), dockerswarm.ConfigSpec{
		Annotations: dockerswarm.Annotations{Name: req.Name, Labels: req.Labels},
		Data:        []byte(req.Data),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// UpdateConfig replaces a config's data. Swarm configs are immutable, so the
// update creates a replacement config with the same name and removes the old
// one; removing fails while any service still references the old config,
// which surfaces as a conflict. The config's ID changes as a result.
func (h *Handler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Data   string            `json:"data"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	old, err := h.Swarm.GetConfig(r.Context(), id)
	if err != nil {
		writeSwarmError(w, "config", id, err)
		return
	}
	labels := req.Labels
	if labels == nil {
		labels = old.Spec.Labels
	}
	newID, err := h.Swarm.CreateConfig(r.Context(), dockerswarm.ConfigSpec{
		Annotations: dockerswarm.Annotations{Name: old.Spec.Name, Labels: labels},
		Data:        []byte(req.Data),
		Templating:  old.Spec.Templating,
	})
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to create replacement config")
		return
	}
	if rmErr := h.Swarm.RemoveConfig(r.Context(), id); rmErr != nil {
		_ = h.Swarm.RemoveConfig(r.Context(), newID)
		writeSwarmError(w, "config", id, rmErr)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteConfig removes a Swarm config.
func (h *Handler) DeleteConfig(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := h.Swarm.GetConfig(r.Context(), id); err != nil {
		writeSwarmError(w, "config", id, err)
		return
	}
	if err := h.Swarm.RemoveConfig(r.Context(), id); err != nil {
		writeSwarmError(w, "config", id, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListNetworks lists overlay networks for the organization.
func (h *Handler) ListNetworks(w http.ResponseWriter, r *http.Request) {
	items, err := h.Swarm.ListNetworks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, nw := range items {
		out = append(out, map[string]any{"id": nw.ID, "name": nw.Name, "driver": nw.Driver})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateNetwork creates a new overlay network.
func (h *Handler) CreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		Internal   bool              `json:"internal"`
		Attachable bool              `json:"attachable"`
		Labels     map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	id, err := h.Swarm.CreateNetwork(r.Context(), req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// DeleteNetwork removes a Swarm network after verifying no service still
// attaches to it.
func (h *Handler) DeleteNetwork(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	nw, err := h.Swarm.InspectNetwork(r.Context(), id)
	if err != nil {
		writeSwarmError(w, "network", id, err)
		return
	}
	services, err := h.Swarm.ListServices(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to list services")
		return
	}
	for _, svc := range services {
		for _, attachment := range svc.Spec.TaskTemplate.Networks {
			if attachment.Target == nw.ID || attachment.Target == nw.Name {
				common.WriteError(w, http.StatusConflict, "conflict",
					fmt.Sprintf("network %s is still attached to service %s", nw.Name, svc.Spec.Name))
				return
			}
		}
	}
	if err := h.Swarm.RemoveNetwork(r.Context(), id); err != nil {
		writeSwarmError(w, "network", id, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListSSHKeys lists stored SSH keys.
func (h *Handler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `select id::text, name, public_key, created_at from ssh_keys order by created_at desc`)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list ssh keys")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, publicKey string
		var createdAt time.Time
		if scanErr := rows.Scan(&id, &name, &publicKey, &createdAt); scanErr == nil {
			out = append(out, map[string]any{"id": id, "name": name, "publicKey": publicKey, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateSSHKey stores a new SSH key.
func (h *Handler) CreateSSHKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		PublicKey  string `json:"publicKey"`
		PrivateKey string `json:"privateKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	if req.Name == "" || req.PublicKey == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "name and publicKey are required")
		return
	}
	privateKey, sealErr := sealSensitive("ssh_key", req.PrivateKey)
	if sealErr != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt private key")
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `insert into ssh_keys(name, public_key, private_key) values ($1, $2, $3) returning id::text`, req.Name, req.PublicKey, privateKey).Scan(&id); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create ssh key")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// ListCertificates lists issued TLS certificates.
func (h *Handler) ListCertificates(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `select id::text, domain, created_at from certificates order by created_at desc`)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list certificates")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, domain string
		var createdAt time.Time
		if scanErr := rows.Scan(&id, &domain, &createdAt); scanErr == nil {
			out = append(out, map[string]any{"id": id, "domain": domain, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateCertificate issues a new TLS certificate.
func (h *Handler) CreateCertificate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain  string `json:"domain"`
		CertPEM string `json:"certPem"`
		KeyPEM  string `json:"keyPem"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	if req.Domain == "" || req.CertPEM == "" || req.KeyPEM == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "domain/cert/key are required")
		return
	}
	keyPEM, sealErr := sealSensitive("certificate_key", req.KeyPEM)
	if sealErr != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt certificate key")
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `insert into certificates(domain, cert_pem, key_pem) values ($1, $2, $3) on conflict (domain) do update set cert_pem = excluded.cert_pem, key_pem = excluded.key_pem returning id::text`, req.Domain, req.CertPEM, keyPEM).Scan(&id); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to save certificate")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}
