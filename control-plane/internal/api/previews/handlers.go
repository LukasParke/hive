package previews

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/riverqueue/river"
)

// Handler serves preview deployment endpoints.
type Handler struct {
	Pool        *pgxpool.Pool
	Q           *dbgen.Queries
	RiverClient *river.Client[pgx.Tx]
}

// NewHandler returns a preview Handler wired to the given dependencies.
func NewHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool), RiverClient: riverClient}
}

// ListPreviewDeployments lists preview deployments for an application.
func (h *Handler) ListPreviewDeployments(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	appIDStr := chi.URLParam(r, "id")
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	appUUID, err := common.ToUUID(appIDStr)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
		return
	}
	if _, err := h.Q.GetApplication(r.Context(), dbgen.GetApplicationParams{ID: appUUID, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "application not found")
		return
	}
	items, err := h.Q.ListPreviewDeployments(r.Context(), dbgen.ListPreviewDeploymentsParams{ApplicationID: appUUID, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list preview deployments")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GetPreviewDeployment returns a single preview deployment by ID.
func (h *Handler) GetPreviewDeployment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	appIDStr := chi.URLParam(r, "id")
	previewIDStr := chi.URLParam(r, "previewId")
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	appUUID, err := common.ToUUID(appIDStr)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
		return
	}
	previewUUID, err := common.ToUUID(previewIDStr)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid preview id")
		return
	}
	if _, err := h.Q.GetApplication(r.Context(), dbgen.GetApplicationParams{ID: appUUID, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "application not found")
		return
	}
	item, err := h.Q.GetPreviewDeployment(r.Context(), dbgen.GetPreviewDeploymentParams{ID: previewUUID, ApplicationID: appUUID, OrganizationID: orgUUID})
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "preview deployment not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, item)
}

// CreatePreviewDeployment schedules a new preview deployment.
func (h *Handler) CreatePreviewDeployment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	appIDStr := chi.URLParam(r, "id")
	var req struct {
		PrNumber  int32  `json:"prNumber"`
		Branch    string `json:"branch"`
		CommitSha string `json:"commitSha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	appUUID, err := common.ToUUID(appIDStr)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
		return
	}
	if _, err := h.Q.GetApplication(r.Context(), dbgen.GetApplicationParams{ID: appUUID, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "application not found")
		return
	}
	previewID, err := h.Q.CreatePreviewDeployment(r.Context(), dbgen.CreatePreviewDeploymentParams{
		OrganizationID: orgUUID,
		ApplicationID:  appUUID,
		PrNumber:       req.PrNumber,
		Branch:         req.Branch,
		CommitSha:      pgtype.Text{String: req.CommitSha, Valid: req.CommitSha != ""},
		Status:         "building",
		Url:            pgtype.Text{String: "", Valid: false},
	})
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create preview deployment")
		return
	}
	if h.RiverClient != nil {
		_, _ = h.RiverClient.Insert(r.Context(), riverjobs.PreviewDeployJobArgs{
			PreviewID:     previewID,
			ApplicationID: appIDStr,
			Branch:        req.Branch,
			CommitSha:     req.CommitSha,
		}, nil)
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"id": previewID})
}

// DeletePreviewDeployment removes a preview deployment and its service.
func (h *Handler) DeletePreviewDeployment(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool)
	if !ok {
		return
	}
	appIDStr := chi.URLParam(r, "id")
	previewIDStr := chi.URLParam(r, "previewId")
	// RequireOrgAccess guarantees orgID is a validated organization UUID.
	orgUUID, _ := common.ToUUID(orgID)
	appUUID, err := common.ToUUID(appIDStr)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid application id")
		return
	}
	previewUUID, err := common.ToUUID(previewIDStr)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid preview id")
		return
	}
	if _, err := h.Q.GetApplication(r.Context(), dbgen.GetApplicationParams{ID: appUUID, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "application not found")
		return
	}
	if err := h.Q.DeletePreviewDeployment(r.Context(), dbgen.DeletePreviewDeploymentParams{ID: previewUUID, ApplicationID: appUUID, OrganizationID: orgUUID}); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to delete preview deployment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
