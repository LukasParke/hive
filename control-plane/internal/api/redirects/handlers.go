package redirects

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

// Handler serves redirect management endpoints.
type Handler struct {
	Pool *pgxpool.Pool
	Q    *dbgen.Queries
}

// NewHandler returns a redirect Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool)}
}

// ListRedirects lists redirects for the organization.
func (h *Handler) ListRedirects(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	items, err := h.Q.ListRedirects(r.Context(), orgUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list redirects")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetRedirect returns a single redirect by ID.
func (h *Handler) GetRedirect(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.Q.GetRedirect(r.Context(), dbgen.GetRedirectParams{ID: id, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "redirect not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

// CreateRedirect creates a new redirect.
func (h *Handler) CreateRedirect(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	var req struct {
		DomainID   string `json:"domainId"`
		Path       string `json:"path"`
		Target     string `json:"target"`
		StatusCode int32  `json:"statusCode"`
		Permanent  bool   `json:"permanent"`
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
	domainUUID, err := common.ToUUID(req.DomainID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid domain id")
		return
	}
	id, err := h.Q.CreateRedirect(r.Context(), dbgen.CreateRedirectParams{
		OrganizationID: orgUUID,
		DomainID:       domainUUID,
		Path:           req.Path,
		Target:         req.Target,
		StatusCode:     req.StatusCode,
		Permanent:      req.Permanent,
	})
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create redirect")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// UpdateRedirect updates an existing redirect.
func (h *Handler) UpdateRedirect(w http.ResponseWriter, r *http.Request) {
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
		DomainID   string `json:"domainId"`
		Path       string `json:"path"`
		Target     string `json:"target"`
		StatusCode int32  `json:"statusCode"`
		Permanent  bool   `json:"permanent"`
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
	domainUUID, err := common.ToUUID(req.DomainID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid domain id")
		return
	}
	if err := h.Q.UpdateRedirect(r.Context(), dbgen.UpdateRedirectParams{
		ID:             id,
		DomainID:       domainUUID,
		Path:           req.Path,
		Target:         req.Target,
		StatusCode:     req.StatusCode,
		Permanent:      req.Permanent,
		OrganizationID: orgUUID,
	}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update redirect")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
}

// DeleteRedirect removes a redirect.
func (h *Handler) DeleteRedirect(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Q.DeleteRedirect(r.Context(), dbgen.DeleteRedirectParams{ID: id, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete redirect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
