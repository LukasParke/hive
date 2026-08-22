// Package tunnels serves the frozen /tunnels API surface: create, list,
// get (with live connector status), ingress replacement and deletion of
// Cloudflare tunnels.
package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/cloudflare"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/secrets"
	tunnels "github.com/luke/hive/control-plane/internal/tunnels"
)

// Manager is the tunnel orchestration seam the handlers depend on;
// *tunnels.Manager satisfies it and tests supply a fake.
type Manager interface {
	Create(ctx context.Context, p tunnels.CreateParams) (*tunnels.View, error)
	List(ctx context.Context) ([]*tunnels.View, error)
	Get(ctx context.Context, id string) (*tunnels.View, error)
	UpdateIngress(ctx context.Context, id string, rules []tunnels.IngressRule) (*tunnels.View, error)
	Delete(ctx context.Context, id string) error
}

var _ Manager = (*tunnels.Manager)(nil)

// Handler serves tunnel management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
	Mgr  Manager

	// authorizeOverride replaces the owner/admin RBAC gate. Nil in
	// production; tests set it to exercise handlers without a database.
	authorizeOverride func(w http.ResponseWriter, r *http.Request) bool
}

// NewHandler returns a tunnel Handler wired to the pool, swarm client and
// a Cloudflare client factory that builds a real REST v4 client per
// request token.
func NewHandler(pool *pgxpool.Pool, swarmAPI tunnels.SwarmAPI, factory func(apiToken string) cloudflare.API) *Handler {
	return &Handler{
		Pool: pool,
		Mgr:  tunnels.NewManager(pool, swarmAPI, secrets.Runtime(), factory),
	}
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.authorizeOverride != nil {
		return h.authorizeOverride(w, r)
	}
	_, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	return ok
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

// connectorPayload shapes the frozen TunnelConnectorStatus schema.
type connectorPayload struct {
	DesiredReplicas  uint64 `json:"desiredReplicas"`
	RunningReplicas  int    `json:"runningReplicas"`
	CloudflareStatus string `json:"cloudflareStatus,omitempty"`
}

// tunnelPayload shapes the frozen Tunnel schema. The stored API token and
// credentials are never echoed.
type tunnelPayload struct {
	ID                 string                `json:"id"`
	Name               string                `json:"name"`
	CloudflareTunnelID string                `json:"cloudflareTunnelId"`
	Status             string                `json:"status"`
	Ingress            []tunnels.IngressRule `json:"ingress"`
	Connector          *connectorPayload     `json:"connector,omitempty"`
	ErrorMessage       string                `json:"errorMessage,omitempty"`
	CreatedAt          time.Time             `json:"createdAt"`
	UpdatedAt          time.Time             `json:"updatedAt"`
}

func toPayload(view *tunnels.View) tunnelPayload {
	p := tunnelPayload{
		ID:                 view.Row.ID,
		Name:               view.Row.Name,
		CloudflareTunnelID: view.Row.CfTunnelID,
		Status:             view.Row.Status,
		Ingress:            view.Row.Ingress,
		CreatedAt:          view.Row.CreatedAt,
		UpdatedAt:          view.Row.UpdatedAt,
	}
	if p.Ingress == nil {
		p.Ingress = []tunnels.IngressRule{}
	}
	if view.Row.ErrorMessage != "" {
		p.ErrorMessage = view.Row.ErrorMessage
	}
	p.Connector = &connectorPayload{
		DesiredReplicas:  view.Connector.DesiredReplicas,
		RunningReplicas:  view.Connector.RunningReplicas,
		CloudflareStatus: view.Connector.CloudflareStatus,
	}
	return p
}

// writeMgrError maps manager failures onto the frozen response statuses.
func writeMgrError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tunnels.ErrInvalidInput):
		common.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, tunnels.ErrNotFound):
		common.WriteError(w, http.StatusNotFound, "not_found", "tunnel not found")
	case errors.Is(err, tunnels.ErrConflict):
		common.WriteError(w, http.StatusConflict, "conflict", "tunnel name already in use")
	default:
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "tunnel operation failed")
	}
}

// ListTunnels lists every managed tunnel with live connector health.
func (h *Handler) ListTunnels(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	views, err := h.Mgr.List(r.Context())
	if err != nil {
		writeMgrError(w, err)
		return
	}
	items := make([]tunnelPayload, 0, len(views))
	for _, v := range views {
		items = append(items, toPayload(v))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// CreateTunnel provisions and deploys a new tunnel.
func (h *Handler) CreateTunnel(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	var req struct {
		Name      string                `json:"name"`
		AccountID string                `json:"accountId"`
		ZoneID    string                `json:"zoneId"`
		APIToken  string                `json:"apiToken"`
		Ingress   []tunnels.IngressRule `json:"ingress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	if req.Name == "" || req.AccountID == "" || req.APIToken == "" || len(req.Ingress) == 0 {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "name, accountId, apiToken and at least one ingress rule are required")
		return
	}
	view, err := h.Mgr.Create(r.Context(), tunnels.CreateParams{
		Name:      req.Name,
		AccountID: req.AccountID,
		ZoneID:    req.ZoneID,
		APIToken:  req.APIToken,
		Ingress:   req.Ingress,
	})
	if err != nil {
		writeMgrError(w, err)
		return
	}
	h.audit(r, "create", "tunnel", view.Row.ID, map[string]any{"name": view.Row.Name, "cloudflareTunnelId": view.Row.CfTunnelID})
	common.WriteJSON(w, http.StatusCreated, toPayload(view))
}

// GetTunnel returns one tunnel with live connector health.
func (h *Handler) GetTunnel(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	view, err := h.Mgr.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeMgrError(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, toPayload(view))
}

// UpdateTunnelIngress replaces the ingress rule list and redeploys the
// connector.
func (h *Handler) UpdateTunnelIngress(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	var req struct {
		Ingress []tunnels.IngressRule `json:"ingress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	view, err := h.Mgr.UpdateIngress(r.Context(), chi.URLParam(r, "id"), req.Ingress)
	if err != nil {
		writeMgrError(w, err)
		return
	}
	h.audit(r, "update", "tunnel", view.Row.ID, map[string]any{"ingressRules": len(view.Row.Ingress)})
	common.WriteJSON(w, http.StatusOK, toPayload(view))
}

// DeleteTunnel tears the tunnel down everywhere and removes its row.
func (h *Handler) DeleteTunnel(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Mgr.Delete(r.Context(), id); err != nil {
		writeMgrError(w, err)
		return
	}
	h.audit(r, "delete", "tunnel", id, nil)
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
