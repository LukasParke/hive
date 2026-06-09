package applications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	dockerswarm "github.com/moby/moby/api/types/swarm"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
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

func (h *Handler) ListApplications(w http.ResponseWriter, r *http.Request) {
	claims, ok := apimiddleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Organization-Id"))
	if orgID == "" {
		http.Error(w, `{"message":"missing X-Organization-Id"}`, http.StatusBadRequest)
		return
	}
	if err := rbac.Require(h.Pool, orgID, claims.UserID, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); err != nil {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select a.id, a.project_id, a.name, a.source_type, a.image, a.created_at
		from applications a
		join projects p on p.id = a.project_id
		where p.organization_id = $1::uuid
		order by a.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type item struct {
		ID        string    `json:"id"`
		ProjectID string    `json:"projectId"`
		Name      string    `json:"name"`
		Source    string    `json:"sourceType"`
		Image     string    `json:"image"`
		CreatedAt time.Time `json:"createdAt"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.ProjectID, &it.Name, &it.Source, &it.Image, &it.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateApplication(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID     string   `json:"projectId"`
		Name          string   `json:"name"`
		Source        string   `json:"sourceType"`
		Image         string   `json:"image"`
		RepositoryURL string   `json:"repositoryUrl"`
		GitRef        string   `json:"gitRef"`
		ContainerPort int      `json:"containerPort"`
		WatchPaths    []string `json:"watchPaths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.ProjectID == "" || req.Source == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	claims, ok := apimiddleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Organization-Id"))
	if orgID == "" {
		http.Error(w, `{"message":"missing X-Organization-Id"}`, http.StatusBadRequest)
		return
	}
	if err := rbac.Require(h.Pool, orgID, claims.UserID, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember); err != nil {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		return
	}

	if req.ContainerPort == 0 {
		req.ContainerPort = 3000
	}
	if req.GitRef == "" {
		req.GitRef = "main"
	}
	if req.WatchPaths == nil {
		req.WatchPaths = []string{}
	}
	var projectOrgID string
	if err := h.Pool.QueryRow(r.Context(), `select organization_id::text from projects where id = $1::uuid`, req.ProjectID).Scan(&projectOrgID); err != nil {
		http.Error(w, `{"message":"project not found"}`, http.StatusBadRequest)
		return
	}
	if projectOrgID == "" || projectOrgID != orgID {
		http.Error(w, `{"message":"project not in active organization"}`, http.StatusForbidden)
		return
	}

	var id string
	var createdAt time.Time
	if err := h.Pool.QueryRow(r.Context(), `
		insert into applications(project_id, name, source_type, image, service_spec, repository_url, git_ref, container_port, watch_paths)
		values ($1::uuid, $2, $3::source_type, $4, '{}'::jsonb, $5, $6, $7, $8::text[])
		returning id, created_at
	`, req.ProjectID, req.Name, req.Source, req.Image, req.RepositoryURL, req.GitRef, req.ContainerPort, req.WatchPaths).Scan(&id, &createdAt); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]any{
		"id": id, "projectId": req.ProjectID, "name": req.Name, "sourceType": req.Source, "image": req.Image, "repositoryUrl": req.RepositoryURL, "gitRef": req.GitRef, "containerPort": req.ContainerPort, "createdAt": createdAt,
	})
}

func (h *Handler) GetApplication(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var appID, projectID, name, sourceType, image, repositoryURL, gitRef string
	var containerPort int
	var createdAt time.Time
	err := h.Pool.QueryRow(r.Context(), `
		select a.id::text, a.project_id::text, a.name, a.source_type::text, coalesce(a.image,''), coalesce(a.repository_url,''), coalesce(a.git_ref,''), coalesce(a.container_port,0), a.created_at
		from applications a
		join projects p on p.id = a.project_id
		where a.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&appID, &projectID, &name, &sourceType, &image, &repositoryURL, &gitRef, &containerPort, &createdAt)
	if err != nil {
		http.Error(w, `{"message":"application not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": appID, "projectId": projectID, "name": name, "sourceType": sourceType, "image": image, "repositoryUrl": repositoryURL, "gitRef": gitRef, "containerPort": containerPort, "createdAt": createdAt,
	})
}

func (h *Handler) UpdateApplication(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Name          string `json:"name"`
		Image         string `json:"image"`
		RepositoryURL string `json:"repositoryUrl"`
		GitRef        string `json:"gitRef"`
		ContainerPort int    `json:"containerPort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update applications a
		set name = coalesce(nullif($3, ''), a.name),
			image = coalesce($4, a.image),
			repository_url = coalesce($5, a.repository_url),
			git_ref = coalesce($6, a.git_ref),
			container_port = case when $7 > 0 then $7 else a.container_port end
		from projects p
		where a.id = $1::uuid and p.id = a.project_id and p.organization_id = $2::uuid
	`, id, orgID, req.Name, req.Image, req.RepositoryURL, req.GitRef, req.ContainerPort)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"application not found"}`, http.StatusNotFound)
		return
	}
	h.GetApplication(w, r)
}

func (h *Handler) DeleteApplication(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")

	// Best-effort cleanup of Docker secrets for env vars before cascade delete.
	secretRows, err := h.Pool.Query(r.Context(), `
		select ev.docker_secret_id from app_env_vars ev
		join applications a on a.id = ev.application_id
		join projects p on p.id = a.project_id
		where ev.application_id = $1::uuid and p.organization_id = $2::uuid and ev.is_secret = true and ev.docker_secret_id != ''
	`, id, orgID)
	if err == nil {
		defer secretRows.Close()
		for secretRows.Next() {
			var dockerSecretID string
			if err := secretRows.Scan(&dockerSecretID); err == nil {
				_ = h.Swarm.RemoveSecret(r.Context(), dockerSecretID)
			}
		}
	}

	cmd, err := h.Pool.Exec(r.Context(), `
		delete from applications a
		using projects p
		where a.id = $1::uuid and p.id = a.project_id and p.organization_id = $2::uuid
	`, id, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"application not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── App Env Vars ──

func (h *Handler) ListAppEnvVars(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	rows, err := h.Pool.Query(r.Context(), `
		select ev.id, ev.key, ev.value, ev.is_secret, ev.created_at, ev.updated_at
		from app_env_vars ev
		join applications a on a.id = ev.application_id
		join projects p on p.id = a.project_id
		where ev.application_id = $1::uuid and p.organization_id = $2::uuid
		order by ev.key
	`, appID, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type envVarItem struct {
		ID        string    `json:"id"`
		Key       string    `json:"key"`
		Value     *string   `json:"value"`
		IsSecret  bool      `json:"isSecret"`
		CreatedAt time.Time `json:"createdAt"`
		UpdatedAt time.Time `json:"updatedAt"`
	}
	var out []envVarItem
	for rows.Next() {
		var it envVarItem
		if err := rows.Scan(&it.ID, &it.Key, &it.Value, &it.IsSecret, &it.CreatedAt, &it.UpdatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

var envKeyRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (h *Handler) CreateAppEnvVar(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	var req struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		IsSecret bool   `json:"isSecret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	if !envKeyRegexp.MatchString(req.Key) {
		http.Error(w, `{"message":"invalid key: must match [A-Za-z_][A-Za-z0-9_]*"}`, http.StatusBadRequest)
		return
	}
	if req.Value == "" {
		http.Error(w, `{"message":"value is required"}`, http.StatusBadRequest)
		return
	}

	// Verify app belongs to org.
	var exists bool
	if err := h.Pool.QueryRow(r.Context(), `
		select exists(
			select 1 from applications a join projects p on p.id = a.project_id
			where a.id = $1::uuid and p.organization_id = $2::uuid
		)
	`, appID, orgID).Scan(&exists); err != nil || !exists {
		http.Error(w, `{"message":"application not found"}`, http.StatusNotFound)
		return
	}

	if req.IsSecret {
		truncID := appID
		if len(truncID) > 12 {
			truncID = truncID[:12]
		}
		secretName := fmt.Sprintf("hive.%s.%s.v1", truncID, req.Key)
		if len(secretName) > 64 {
			http.Error(w, `{"message":"secret name too long (key too long)"}`, http.StatusBadRequest)
			return
		}
		dockerSecretID, err := h.Swarm.CreateSecret(r.Context(), dockerswarm.SecretSpec{
			Annotations: dockerswarm.Annotations{Name: secretName},
			Data:        []byte(req.Value),
		})
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"message":"failed to create docker secret: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		var id string
		if err := h.Pool.QueryRow(r.Context(), `
			insert into app_env_vars(application_id, key, value, is_secret, secret_version, docker_secret_id)
			values ($1::uuid, $2, NULL, true, 1, $3)
			returning id::text
		`, appID, req.Key, dockerSecretID).Scan(&id); err != nil {
			// Best-effort cleanup of docker secret on DB failure.
			_ = h.Swarm.RemoveSecret(r.Context(), dockerSecretID)
			http.Error(w, fmt.Sprintf(`{"message":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
	} else {
		var id string
		if err := h.Pool.QueryRow(r.Context(), `
			insert into app_env_vars(application_id, key, value, is_secret)
			values ($1::uuid, $2, $3, false)
			returning id::text
		`, appID, req.Key, req.Value).Scan(&id); err != nil {
			http.Error(w, fmt.Sprintf(`{"message":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
	}
}

func (h *Handler) UpdateAppEnvVar(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	varID := chi.URLParam(r, "varId")
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Value == "" {
		http.Error(w, `{"message":"value is required"}`, http.StatusBadRequest)
		return
	}

	// Fetch existing row with org scoping.
	var isSecret bool
	var secretVersion int
	var dockerSecretID string
	var key string
	err := h.Pool.QueryRow(r.Context(), `
		select ev.key, ev.is_secret, ev.secret_version, ev.docker_secret_id
		from app_env_vars ev
		join applications a on a.id = ev.application_id
		join projects p on p.id = a.project_id
		where ev.id = $1::uuid and ev.application_id = $2::uuid and p.organization_id = $3::uuid
	`, varID, appID, orgID).Scan(&key, &isSecret, &secretVersion, &dockerSecretID)
	if err != nil {
		http.Error(w, `{"message":"env var not found"}`, http.StatusNotFound)
		return
	}

	if isSecret {
		newVersion := secretVersion + 1
		truncID := appID
		if len(truncID) > 12 {
			truncID = truncID[:12]
		}
		newSecretName := fmt.Sprintf("hive.%s.%s.v%d", truncID, key, newVersion)
		newDockerSecretID, err := h.Swarm.CreateSecret(r.Context(), dockerswarm.SecretSpec{
			Annotations: dockerswarm.Annotations{Name: newSecretName},
			Data:        []byte(req.Value),
		})
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"message":"failed to create docker secret: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_, err = h.Pool.Exec(r.Context(), `
			update app_env_vars set secret_version = $2, docker_secret_id = $3, updated_at = now()
			where id = $1::uuid
		`, varID, newVersion, newDockerSecretID)
		if err != nil {
			_ = h.Swarm.RemoveSecret(r.Context(), newDockerSecretID)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Best-effort removal of old secret.
		if dockerSecretID != "" {
			_ = h.Swarm.RemoveSecret(r.Context(), dockerSecretID)
		}
	} else {
		_, err = h.Pool.Exec(r.Context(), `
			update app_env_vars set value = $2, updated_at = now() where id = $1::uuid
		`, varID, req.Value)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *Handler) DeleteAppEnvVar(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	varID := chi.URLParam(r, "varId")

	// Fetch row for cleanup info.
	var isSecret bool
	var dockerSecretID string
	err := h.Pool.QueryRow(r.Context(), `
		select ev.is_secret, ev.docker_secret_id
		from app_env_vars ev
		join applications a on a.id = ev.application_id
		join projects p on p.id = a.project_id
		where ev.id = $1::uuid and ev.application_id = $2::uuid and p.organization_id = $3::uuid
	`, varID, appID, orgID).Scan(&isSecret, &dockerSecretID)
	if err != nil {
		http.Error(w, `{"message":"env var not found"}`, http.StatusNotFound)
		return
	}

	_, err = h.Pool.Exec(r.Context(), `delete from app_env_vars where id = $1::uuid`, varID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Best-effort Docker secret cleanup.
	if isSecret && dockerSecretID != "" {
		_ = h.Swarm.RemoveSecret(r.Context(), dockerSecretID)
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) StartApplication(w http.ResponseWriter, r *http.Request) {
	h.scaleApp(w, r, 1)
}

func (h *Handler) StopApplication(w http.ResponseWriter, r *http.Request) {
	h.scaleApp(w, r, 0)
}

func (h *Handler) RestartApplication(w http.ResponseWriter, r *http.Request) {
	h.scaleApp(w, r, 1)
}

func (h *Handler) scaleApp(w http.ResponseWriter, r *http.Request, replicas uint64) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "id")
	var serviceName string
	if err := h.Pool.QueryRow(r.Context(), `
		select a.service_name
		from applications a
		join projects p on p.id = a.project_id
		where a.id = $1::uuid and p.organization_id = $2::uuid
	`, appID, orgID).Scan(&serviceName); err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", "application not found")
		return
	}
	if err := h.scaleServiceByName(r.Context(), serviceName, replicas); err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "replicas": replicas})
}

func (h *Handler) scaleServiceByName(ctx context.Context, serviceName string, replicas uint64) error {
	services, err := h.Swarm.ListServices(ctx)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if svc.Spec.Name != serviceName {
			continue
		}
		spec := svc.Spec
		spec.Mode = dockerswarm.ServiceMode{Replicated: &dockerswarm.ReplicatedService{Replicas: ptrUint64(replicas)}}
		return h.Swarm.UpdateService(ctx, svc.ID, svc.Version.Index, spec)
	}
	return fmt.Errorf("service %s not found", serviceName)
}

func ptrUint64(v uint64) *uint64 {
	return &v
}
