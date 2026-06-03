package volumebackups

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

func (h *Handler) ListVolumeBackups(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	orgUUID, err := common.ToUUID(orgID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid organization id")
		return
	}
	items, err := h.Q.ListVolumeBackups(r.Context(), orgUUID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list volume backups")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetVolumeBackup(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.Q.GetVolumeBackup(r.Context(), dbgen.GetVolumeBackupParams{ID: id, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "volume backup not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) CreateVolumeBackup(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	var req struct {
		VolumeName    string `json:"volumeName"`
		DestinationID string `json:"destinationId"`
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
	destUUID, err := common.ToUUID(req.DestinationID)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid destination id")
		return
	}
	id, err := h.Q.CreateVolumeBackup(r.Context(), dbgen.CreateVolumeBackupParams{
		OrganizationID: orgUUID,
		VolumeName:     req.VolumeName,
		DestinationID:  destUUID,
	})
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create volume backup")
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

func (h *Handler) DeleteVolumeBackup(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Q.DeleteVolumeBackup(r.Context(), dbgen.DeleteVolumeBackupParams{ID: id, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete volume backup")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
