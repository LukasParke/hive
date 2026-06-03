package domains

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/proxy"
	"github.com/luke/hive/control-plane/internal/rbac"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type Handler struct {
	Pool  *pgxpool.Pool
	Swarm *swarmclient.Client
}

func NewHandler(pool *pgxpool.Pool, swarm *swarmclient.Client) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

func (h *Handler) ListDomains(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select d.id::text, d.application_id::text, d.hostname, d.tls_enabled, d.created_at
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where p.organization_id = $1::uuid
		order by d.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type item struct {
		ID            string    `json:"id"`
		ApplicationID string    `json:"applicationId"`
		Hostname      string    `json:"hostname"`
		TLSEnabled    bool      `json:"tlsEnabled"`
		CreatedAt     time.Time `json:"createdAt"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.ApplicationID, &it.Hostname, &it.TLSEnabled, &it.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ApplicationID string `json:"applicationId"`
		Hostname      string `json:"hostname"`
		TLSEnabled    bool   `json:"tlsEnabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ApplicationID == "" || req.Hostname == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into domains(application_id, hostname, tls_enabled)
		values ($1::uuid, $2, $3)
		returning id::text
	`, req.ApplicationID, req.Hostname, req.TLSEnabled).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.applyDomainsForApp(r.Context(), req.ApplicationID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) GetDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var out map[string]any
	row := h.Pool.QueryRow(r.Context(), `
		select d.id::text, d.application_id::text, d.hostname, d.tls_enabled, d.created_at
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID)
	var domainID, appID, host string
	var tls bool
	var createdAt time.Time
	if err := row.Scan(&domainID, &appID, &host, &tls, &createdAt); err != nil {
		http.Error(w, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	out = map[string]any{"id": domainID, "applicationId": appID, "hostname": host, "tlsEnabled": tls, "createdAt": createdAt}
	common.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) UpdateDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Hostname   string `json:"hostname"`
		TLSEnabled *bool  `json:"tlsEnabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	tlsValue := false
	hasTLS := false
	if req.TLSEnabled != nil {
		tlsValue = *req.TLSEnabled
		hasTLS = true
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update domains d
		set hostname = coalesce(nullif($3,''), d.hostname),
			tls_enabled = case when $4 then $5 else d.tls_enabled end
		from applications a
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and a.id = d.application_id and p.organization_id = $2::uuid
	`, id, orgID, req.Hostname, hasTLS, tlsValue)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	var appID string
	if err := h.Pool.QueryRow(r.Context(), `select application_id::text from domains where id = $1::uuid`, id).Scan(&appID); err == nil {
		_ = h.applyDomainsForApp(r.Context(), appID)
	}
	h.GetDomain(w, r)
}

func (h *Handler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var appID string
	_ = h.Pool.QueryRow(r.Context(), `
		select d.application_id::text
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&appID)
	cmd, err := h.Pool.Exec(r.Context(), `
		delete from domains d
		using applications a, projects p
		where d.id = $1::uuid and a.id = d.application_id and p.id = a.project_id and p.organization_id = $2::uuid
	`, id, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	if appID != "" {
		_ = h.applyDomainsForApp(r.Context(), appID)
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) applyDomainsForApp(ctx context.Context, appID string) error {
	services, err := h.Swarm.ListServices(ctx)
	if err != nil {
		return err
	}
	serviceID := ""
	containerPort := 3000
	for _, svc := range services {
		if svc.Spec.Labels["dokploy.app.id"] == appID {
			serviceID = svc.ID
			if p := svc.Spec.Labels["dokploy.app.port"]; p != "" {
				if parsed, parseErr := strconv.Atoi(p); parseErr == nil {
					containerPort = parsed
				}
			}
			break
		}
	}
	if serviceID == "" {
		return nil
	}
	rows, err := h.Pool.Query(ctx, `select hostname from domains where application_id = $1::uuid`, appID)
	if err != nil {
		return err
	}
	defer rows.Close()
	manager := proxy.NewDomainManager(h.Swarm)
	for rows.Next() {
		var host string
		if err := rows.Scan(&host); err != nil {
			return err
		}
		if err := manager.ApplyDomain(ctx, serviceID, proxy.RouterNameFromHost(host), host, containerPort); err != nil {
			return err
		}
	}
	return nil
}
