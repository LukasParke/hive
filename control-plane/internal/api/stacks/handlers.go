package stacks

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/deploy"
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

func (h *Handler) CreateStack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID      string `json:"projectId"`
		Name           string `json:"name"`
		ComposeContent string `json:"composeContent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Name == "" || req.ComposeContent == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into stacks(project_id, name, compose_content) values ($1::uuid, $2, $3) returning id::text
	`, req.ProjectID, req.Name, req.ComposeContent).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.deployStackByID(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

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

func (h *Handler) UpdateStack(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "id")
	var req struct {
		ComposeContent string `json:"composeContent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ComposeContent == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	_, err := h.Pool.Exec(r.Context(), `update stacks set compose_content=$2 where id=$1::uuid`, stackID, req.ComposeContent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.deployStackByID(r.Context(), stackID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteStack(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "id")
	var stackName string
	err := h.Pool.QueryRow(r.Context(), `select name from stacks where id = $1::uuid`, stackID).Scan(&stackName)
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

func (h *Handler) DeployStack(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "id")
	if err := h.deployStackByID(r.Context(), stackID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (h *Handler) deployStackByID(ctx context.Context, stackID string) error {
	var stackName string
	var composeContent string
	if err := h.Pool.QueryRow(ctx, `select name, compose_content from stacks where id = $1::uuid`, stackID).Scan(&stackName, &composeContent); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "hive-stack-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "compose.yml")
	if err := os.WriteFile(path, []byte(composeContent), 0o600); err != nil {
		return err
	}
	return deploy.DeployStack(ctx, h.Swarm, stackName, path)
}

func (h *Handler) StartStack(w http.ResponseWriter, r *http.Request) {
	h.scaleStack(w, r, 1)
}

func (h *Handler) StopStack(w http.ResponseWriter, r *http.Request) {
	h.scaleStack(w, r, 0)
}

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

func ptrUint64(v uint64) *uint64 {
	return &v
}
