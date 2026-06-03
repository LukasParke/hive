package security

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/proxy"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type Handler struct {
	Pool  *pgxpool.Pool
	Q     *dbgen.Queries
	Swarm *swarmclient.Client
}

func NewHandler(pool *pgxpool.Pool, swarm *swarmclient.Client) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool), Swarm: swarm}
}

func (h *Handler) ListSecurityRules(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	items, err := h.Q.ListSecurityRules(r.Context(), orgUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list security rules")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetSecurityRule(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	id, err := common.ToUUID(chi.URLParam(r, "id"))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	item, err := h.Q.GetSecurityRule(r.Context(), dbgen.GetSecurityRuleParams{ID: id, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "security rule not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateSecurityRule(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	var req struct {
		ApplicationID string          `json:"applicationId"`
		Name          string          `json:"name"`
		Type          string          `json:"type"`
		Config        json.RawMessage `json:"config"`
		Priority      int32           `json:"priority"`
		Enabled       bool            `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	appUUID := pgtype.UUID{}
	if req.ApplicationID != "" {
		appUUID, err = common.ToUUID(req.ApplicationID)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
			return
		}
	}
	config := []byte("{}")
	if len(req.Config) > 0 {
		config = req.Config
	}
	id, err := h.Q.CreateSecurityRule(r.Context(), dbgen.CreateSecurityRuleParams{
		OrganizationID: orgUUID,
		ApplicationID:  appUUID,
		Name:           req.Name,
		Type:           req.Type,
		Config:         config,
		Priority:       req.Priority,
		Enabled:        req.Enabled,
	})
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create security rule")
		return
	}
	if req.ApplicationID != "" && h.Swarm != nil {
		_ = proxy.ApplySecurityRulesForApplication(r.Context(), h.Pool, h.Swarm, req.ApplicationID)
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) UpdateSecurityRule(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	id, err := common.ToUUID(chi.URLParam(r, "id"))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var req struct {
		ApplicationID string          `json:"applicationId"`
		Name          string          `json:"name"`
		Type          string          `json:"type"`
		Config        json.RawMessage `json:"config"`
		Priority      int32           `json:"priority"`
		Enabled       bool            `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	appUUID := pgtype.UUID{}
	if req.ApplicationID != "" {
		appUUID, err = common.ToUUID(req.ApplicationID)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
			return
		}
	}
	config := []byte("{}")
	if len(req.Config) > 0 {
		config = req.Config
	}
	if err := h.Q.UpdateSecurityRule(r.Context(), dbgen.UpdateSecurityRuleParams{
		ID:             id,
		OrganizationID: orgUUID,
		ApplicationID:  appUUID,
		Name:           req.Name,
		Type:           req.Type,
		Config:         config,
		Priority:       req.Priority,
		Enabled:        req.Enabled,
	}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update security rule")
		return
	}
	if req.ApplicationID != "" && h.Swarm != nil {
		_ = proxy.ApplySecurityRulesForApplication(r.Context(), h.Pool, h.Swarm, req.ApplicationID)
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
}

func (h *Handler) DeleteSecurityRule(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	id, err := common.ToUUID(chi.URLParam(r, "id"))
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	// Find application_id before deleting so we can re-apply labels.
	var appID string
	_ = h.Pool.QueryRow(r.Context(), `select application_id::text from security_rules where id = $1::uuid and organization_id = $2::uuid`, id, orgUUID).Scan(&appID)
	if err := h.Q.DeleteSecurityRule(r.Context(), dbgen.DeleteSecurityRuleParams{ID: id, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete security rule")
		return
	}
	if appID != "" && h.Swarm != nil {
		_ = proxy.ApplySecurityRulesForApplication(r.Context(), h.Pool, h.Swarm, appID)
	}
	w.WriteHeader(http.StatusNoContent)
}
