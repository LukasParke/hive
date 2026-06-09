package infrastructure

import (
	"encoding/json"
	"net/http"
	"time"

	dockerswarm "github.com/moby/moby/api/types/swarm"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type Handler struct {
	Pool  *pgxpool.Pool
	Swarm *swarmclient.Client
}

func NewHandler(pool *pgxpool.Pool, swarm *swarmclient.Client) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

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

func (h *Handler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	id, err := h.Swarm.CreateSecret(r.Context(), dockerswarm.SecretSpec{
		Annotations: dockerswarm.Annotations{Name: req.Name},
		Data:        []byte(req.Data),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

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

func (h *Handler) CreateConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	id, err := h.Swarm.CreateConfig(r.Context(), dockerswarm.ConfigSpec{
		Annotations: dockerswarm.Annotations{Name: req.Name},
		Data:        []byte(req.Data),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

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

func (h *Handler) CreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
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
	var id string
	if err := h.Pool.QueryRow(r.Context(), `insert into ssh_keys(name, public_key, private_key) values ($1, $2, $3) returning id::text`, req.Name, req.PublicKey, req.PrivateKey).Scan(&id); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create ssh key")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

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
	var id string
	if err := h.Pool.QueryRow(r.Context(), `insert into certificates(domain, cert_pem, key_pem) values ($1, $2, $3) on conflict (domain) do update set cert_pem = excluded.cert_pem, key_pem = excluded.key_pem returning id::text`, req.Domain, req.CertPEM, req.KeyPEM).Scan(&id); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to save certificate")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}
