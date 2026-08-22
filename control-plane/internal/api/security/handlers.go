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

// applySecurityRules is a seam so tests can observe rule application without
// a swarm cluster; production points at the real proxy applier.
var applySecurityRules = proxy.ApplySecurityRulesForApplication

// Handler serves security rule management endpoints.
type Handler struct {
	Pool  *pgxpool.Pool
	Q     *dbgen.Queries
	Swarm *swarmclient.Client
}

// NewHandler returns a security rules Handler.
func NewHandler(pool *pgxpool.Pool, swarm *swarmclient.Client) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool), Swarm: swarm}
}

// ListSecurityRules returns all security rules for the organization.
func (h *Handler) ListSecurityRules(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	items, err := h.Q.ListSecurityRules(r.Context(), orgUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list security rules")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetSecurityRule returns a single security rule.
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
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	item, err := h.Q.GetSecurityRule(r.Context(), dbgen.GetSecurityRuleParams{ID: id, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "security rule not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

// CreateSecurityRule validates and stores a new security rule.
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
	if err := proxy.ValidateSecurityRuleType(req.Type); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	var err error
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
		_ = applySecurityRules(r.Context(), h.Pool, h.Swarm, req.ApplicationID)
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// UpdateSecurityRule replaces a security rule configuration.
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
	if err := proxy.ValidateSecurityRuleType(req.Type); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
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
		_ = applySecurityRules(r.Context(), h.Pool, h.Swarm, req.ApplicationID)
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
}

// DeleteSecurityRule removes a security rule.
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
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	// Find application_id before deleting so we can re-apply labels.
	var appID string
	_ = h.Pool.QueryRow(r.Context(), `select application_id::text from security_rules where id = $1::uuid and organization_id = $2::uuid`, id, orgUUID).Scan(&appID)
	if err := h.Q.DeleteSecurityRule(r.Context(), dbgen.DeleteSecurityRuleParams{ID: id, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete security rule")
		return
	}
	if appID != "" && h.Swarm != nil {
		_ = applySecurityRules(r.Context(), h.Pool, h.Swarm, appID)
	}
	w.WriteHeader(http.StatusNoContent)
}
