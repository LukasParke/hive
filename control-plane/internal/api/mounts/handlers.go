package mounts

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
)

type Handler struct {
	Pool *pgxpool.Pool
	Q    *dbgen.Queries
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool)}
}

func (h *Handler) ListMounts(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	items, err := h.Q.ListMounts(r.Context(), orgUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list mounts")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetMount(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.Q.GetMount(r.Context(), dbgen.GetMountParams{ID: id, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "mount not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateMount(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	var req struct {
		ApplicationID string `json:"applicationId"`
		Type          string `json:"type"`
		Source        string `json:"source"`
		Target        string `json:"target"`
		ReadOnly      bool   `json:"readOnly"`
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
	appUUID, err := common.ToUUID(req.ApplicationID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
		return
	}
	id, err := h.Q.CreateMount(r.Context(), dbgen.CreateMountParams{
		OrganizationID: orgUUID,
		ApplicationID:  appUUID,
		Type:           req.Type,
		Source:         req.Source,
		Target:         req.Target,
		ReadOnly:       req.ReadOnly,
	})
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create mount")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) UpdateMount(w http.ResponseWriter, r *http.Request) {
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
		ApplicationID string `json:"applicationId"`
		Type          string `json:"type"`
		Source        string `json:"source"`
		Target        string `json:"target"`
		ReadOnly      bool   `json:"readOnly"`
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
	appUUID, err := common.ToUUID(req.ApplicationID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
		return
	}
	if err := h.Q.UpdateMount(r.Context(), dbgen.UpdateMountParams{
		ID:             id,
		ApplicationID:  appUUID,
		Type:           req.Type,
		Source:         req.Source,
		Target:         req.Target,
		ReadOnly:       req.ReadOnly,
		OrganizationID: orgUUID,
	}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update mount")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
}

func (h *Handler) DeleteMount(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Q.DeleteMount(r.Context(), dbgen.DeleteMountParams{ID: id, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete mount")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
