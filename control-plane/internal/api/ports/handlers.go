package ports

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

func (h *Handler) ListPortPolicies(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	items, err := h.Q.ListPortPolicies(r.Context(), orgUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list port policies")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetPortPolicy(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.Q.GetPortPolicy(r.Context(), dbgen.GetPortPolicyParams{ID: id, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "port policy not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CreatePortPolicy(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	var req struct {
		ApplicationID string `json:"applicationId"`
		PublishedPort int32  `json:"publishedPort"`
		TargetPort    int32  `json:"targetPort"`
		Protocol      string `json:"protocol"`
		Mode          string `json:"mode"`
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
	id, err := h.Q.CreatePortPolicy(r.Context(), dbgen.CreatePortPolicyParams{
		OrganizationID: orgUUID,
		ApplicationID:  appUUID,
		PublishedPort:  req.PublishedPort,
		TargetPort:     req.TargetPort,
		Protocol:       req.Protocol,
		Mode:           req.Mode,
	})
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create port policy")
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) UpdatePortPolicy(w http.ResponseWriter, r *http.Request) {
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
		PublishedPort int32  `json:"publishedPort"`
		TargetPort    int32  `json:"targetPort"`
		Protocol      string `json:"protocol"`
		Mode          string `json:"mode"`
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
	if err := h.Q.UpdatePortPolicy(r.Context(), dbgen.UpdatePortPolicyParams{
		ID:             id,
		ApplicationID:  appUUID,
		PublishedPort:  req.PublishedPort,
		TargetPort:     req.TargetPort,
		Protocol:       req.Protocol,
		Mode:           req.Mode,
		OrganizationID: orgUUID,
	}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to update port policy")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"id": chi.URLParam(r, "id")})
}

func (h *Handler) DeletePortPolicy(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Q.DeletePortPolicy(r.Context(), dbgen.DeletePortPolicyParams{ID: id, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete port policy")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
