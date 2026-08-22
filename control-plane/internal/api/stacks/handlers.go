package stacks

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/rbac"
	dockerswarm "github.com/moby/moby/api/types/swarm"
)

// Handler serves stack management endpoints. Swarm is the deploy.SwarmStack
// seam; *swarmclient.Client satisfies it and tests inject fakes.
type Handler struct {
	Pool  *pgxpool.Pool
	Swarm deploy.SwarmStack
}

// NewHandler returns a stack Handler backed by the given pool.
func NewHandler(pool *pgxpool.Pool, swarm deploy.SwarmStack) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

// ListStacks lists deployed stacks.
func (h *Handler) ListStacks(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select s.id::text, s.project_id::text, s.name, s.created_at
		from stacks s
		join projects p on p.id = s.project_id
		where p.organization_id = $1::uuid
		order by s.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, projectID, name string
		var createdAt time.Time
		if err := rows.Scan(&id, &projectID, &name, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{"id": id, "projectId": projectID, "name": name, "createdAt": createdAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateStack deploys a new stack from a compose file.
func (h *Handler) CreateStack(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	var req struct {
		ProjectID      string `json:"projectId"`
		Name           string `json:"name"`
		ComposeContent string `json:"composeContent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	req.Name = normalizeStackName(req.Name)
	req.ComposeContent = strings.TrimSpace(req.ComposeContent)
	if req.ProjectID == "" || req.Name == "" || req.ComposeContent == "" {
		http.Error(w, `{"message":"projectId, name, and composeContent are required"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into stacks(project_id, name, compose_content)
		select p.id, $2, $3
		from projects p
		where p.id = $1::uuid and p.organization_id = $4::uuid
		returning id::text
	`, req.ProjectID, req.Name, req.ComposeContent, orgID).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.deployStackByID(r.Context(), id, orgID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GetStack returns a single stack by name.
func (h *Handler) GetStack(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	stackID := chi.URLParam(r, "id")
	var id, projectID, name, composeContent string
	var createdAt time.Time
	err := h.Pool.QueryRow(r.Context(), `
		select s.id::text, s.project_id::text, s.name, s.compose_content, s.created_at
		from stacks s
		join projects p on p.id = s.project_id
		where s.id = $1::uuid and p.organization_id = $2::uuid
	`, stackID, orgID).Scan(&id, &projectID, &name, &composeContent, &createdAt)
	if err != nil {
		http.Error(w, `{"message":"stack not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "projectId": projectID, "name": name, "composeContent": composeContent, "createdAt": createdAt,
	})
}

// UpdateStack redeploys a stack with a new compose file.
func (h *Handler) UpdateStack(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	stackID := chi.URLParam(r, "id")
	var req struct {
		ComposeContent string `json:"composeContent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.ComposeContent) == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update stacks s
		set compose_content = $3
		from projects p
		where s.id = $1::uuid and p.id = s.project_id and p.organization_id = $2::uuid
	`, stackID, orgID, req.ComposeContent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"stack not found"}`, http.StatusNotFound)
		return
	}
	if err := h.deployStackByID(r.Context(), stackID, orgID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeleteStack removes a stack and its services.
func (h *Handler) DeleteStack(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	stackID := chi.URLParam(r, "id")
	var stackName string
	err := h.Pool.QueryRow(r.Context(), `
		select s.name
		from stacks s
		join projects p on p.id = s.project_id
		where s.id = $1::uuid and p.organization_id = $2::uuid
	`, stackID, orgID).Scan(&stackName)
	if err != nil {
		http.Error(w, `{"message":"stack not found"}`, http.StatusNotFound)
		return
	}
	services, err := h.Swarm.ListServices(r.Context())
	if err == nil {
		for _, svc := range services {
			if svc.Spec.Labels["com.docker.stack.namespace"] == stackName {
				_ = h.Swarm.RemoveService(r.Context(), svc.ID)
			}
		}
	}
	_, _ = h.Pool.Exec(r.Context(), `delete from stacks where id = $1::uuid`, stackID)
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DeployStack redeploys the stack's compose file as-is.
func (h *Handler) DeployStack(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	stackID := chi.URLParam(r, "id")
	if err := h.deployStackByID(r.Context(), stackID, orgID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) deployStackByID(ctx context.Context, stackID, orgID string) error {
	var stackName string
	var composeContent string
	if err := h.Pool.QueryRow(ctx, `
		select s.name, s.compose_content
		from stacks s
		join projects p on p.id = s.project_id
		where s.id = $1::uuid and p.organization_id = $2::uuid
	`, stackID, orgID).Scan(&stackName, &composeContent); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "hive-stack-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte(composeContent), 0o600); err != nil {
		return err
	}
	return deploy.Stack(ctx, h.Swarm, stackName, path)
}

// StartStack scales all stack services up.
func (h *Handler) StartStack(w http.ResponseWriter, r *http.Request) {
	h.scaleStack(w, r, 1)
}

// StopStack scales all stack services to zero.
func (h *Handler) StopStack(w http.ResponseWriter, r *http.Request) {
	h.scaleStack(w, r, 0)
}

// RestartStack forces a rolling restart of stack services.
func (h *Handler) RestartStack(w http.ResponseWriter, r *http.Request) {
	h.scaleStack(w, r, 1)
}

func (h *Handler) scaleStack(w http.ResponseWriter, r *http.Request, replicas uint64) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	stackID := chi.URLParam(r, "id")
	var stackName string
	if err := h.Pool.QueryRow(r.Context(), `
		select s.name
		from stacks s
		join projects p on p.id = s.project_id
		where s.id = $1::uuid and p.organization_id = $2::uuid
	`, stackID, orgID).Scan(&stackName); err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "stack not found")
		return
	}
	services, err := h.Swarm.ListServices(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to list services")
		return
	}
	for _, svc := range services {
		if svc.Spec.Labels["com.docker.stack.namespace"] != stackName {
			continue
		}
		spec := svc.Spec
		spec.Mode = dockerswarm.ServiceMode{Replicated: &dockerswarm.ReplicatedService{Replicas: ptrUint64(replicas)}}
		if err := h.Swarm.UpdateService(r.Context(), svc.ID, svc.Version.Index, spec); err != nil {
			common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to update stack services")
			return
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "replicas": replicas})
}

func normalizeStackName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "stack"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	if out == "" {
		return "stack"
	}
	return out
}

func ptrUint64(v uint64) *uint64 {
	return &v
}
