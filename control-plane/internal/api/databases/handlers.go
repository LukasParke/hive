package databases

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	dockerswarm "github.com/moby/moby/api/types/swarm"
)

type Handler struct {
	Pool  *pgxpool.Pool
	Swarm *swarmclient.Client
}

func NewHandler(pool *pgxpool.Pool, swarm *swarmclient.Client) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

func (h *Handler) ListDatabaseServices(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select ds.id::text, ds.project_id::text, ds.engine, ds.name, ds.version, ds.service_name, ds.database_name, ds.port, ds.created_at
		from database_services ds
		join projects p on p.id = ds.project_id
		where p.organization_id = $1::uuid
		order by ds.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, projectID, engine, name, version, serviceName, databaseName string
		var port int
		var createdAt time.Time
		if err := rows.Scan(&id, &projectID, &engine, &name, &version, &serviceName, &databaseName, &port, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, map[string]any{
			"id": id, "projectId": projectID, "engine": engine, "name": name, "version": version, "serviceName": serviceName, "databaseName": databaseName, "port": port, "createdAt": createdAt,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateDatabaseService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID          string `json:"projectId"`
		Engine             string `json:"engine"`
		Name               string `json:"name"`
		Version            string `json:"version"`
		Username           string `json:"username"`
		PasswordSecretName string `json:"passwordSecretName"`
		DatabaseName       string `json:"databaseName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProjectID == "" || req.Engine == "" || req.Name == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	var projectOrgID string
	if err := h.Pool.QueryRow(r.Context(), `select organization_id::text from projects where id = $1::uuid`, req.ProjectID).Scan(&projectOrgID); err != nil {
		http.Error(w, `{"message":"project not found"}`, http.StatusBadRequest)
		return
	}
	if projectOrgID != orgID {
		http.Error(w, `{"message":"project does not belong to active organization"}`, http.StatusForbidden)
		return
	}
	req.Engine = strings.ToLower(strings.TrimSpace(req.Engine))
	req.Name = normalizeServiceName(req.Name)
	req.Version = strings.TrimSpace(req.Version)
	req.Username = strings.TrimSpace(req.Username)
	req.PasswordSecretName = strings.TrimSpace(req.PasswordSecretName)
	req.DatabaseName = strings.TrimSpace(req.DatabaseName)
	image, port, supported := dbEngineImage(req.Engine, req.Version)
	if !supported {
		http.Error(w, `{"message":"unsupported database engine"}`, http.StatusBadRequest)
		return
	}
	serviceName := fmt.Sprintf("db-%s", req.Name)
	if req.PasswordSecretName != "" && !h.secretExists(r.Context(), req.PasswordSecretName) {
		http.Error(w, `{"message":"password secret not found"}`, http.StatusBadRequest)
		return
	}
	env := dbServiceEnv(req.Engine, req.Username, req.PasswordSecretName, req.DatabaseName)
	var secretRefs []*dockerswarm.SecretReference
	if req.PasswordSecretName != "" {
		secretRefs = append(secretRefs, &dockerswarm.SecretReference{
			File: &dockerswarm.SecretReferenceFileTarget{
				Name: req.PasswordSecretName,
				UID:  "0",
				GID:  "0",
				Mode: 0o400,
			},
			SecretName: req.PasswordSecretName,
		})
	}
	spec := dockerswarm.ServiceSpec{
		Annotations: dockerswarm.Annotations{
			Name: serviceName,
			Labels: map[string]string{
				"hive.db.engine":  req.Engine,
				"hive.db.name":    req.Name,
				"hive.org.id":     orgID,
				"hive.project.id": req.ProjectID,
			},
		},
		TaskTemplate: dockerswarm.TaskSpec{
			ContainerSpec: &dockerswarm.ContainerSpec{
				Image:   image,
				Env:     env,
				Secrets: secretRefs,
			},
			RestartPolicy: &dockerswarm.RestartPolicy{Condition: dockerswarm.RestartPolicyConditionAny},
			Networks: []dockerswarm.NetworkAttachmentConfig{
				{Target: "hive_internal"},
			},
		},
		UpdateConfig: &dockerswarm.UpdateConfig{Order: "start-first", FailureAction: "rollback"},
		Mode: dockerswarm.ServiceMode{
			Replicated: &dockerswarm.ReplicatedService{Replicas: ptrUint64(1)},
		},
	}
	serviceID, err := h.Swarm.CreateService(r.Context(), spec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var id string
	err = h.Pool.QueryRow(r.Context(), `
		insert into database_services(project_id, engine, name, version, service_name, username, password_secret_name, database_name, port)
		values ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9)
		returning id::text
	`, req.ProjectID, req.Engine, req.Name, req.Version, serviceName, req.Username, req.PasswordSecretName, req.DatabaseName, port).Scan(&id)
	if err != nil {
		_ = h.Swarm.RemoveService(r.Context(), serviceID)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) GetDatabaseService(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var outID, projectID, engine, name, version, serviceName, databaseName string
	var port int
	var createdAt time.Time
	err := h.Pool.QueryRow(r.Context(), `
		select ds.id::text, ds.project_id::text, ds.engine, ds.name, ds.version, ds.service_name, ds.database_name, ds.port, ds.created_at
		from database_services ds
		join projects p on p.id = ds.project_id
		where ds.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&outID, &projectID, &engine, &name, &version, &serviceName, &databaseName, &port, &createdAt)
	if err != nil {
		http.Error(w, `{"message":"database service not found"}`, http.StatusNotFound)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id": outID, "projectId": projectID, "engine": engine, "name": name, "version": version, "serviceName": serviceName, "databaseName": databaseName, "port": port, "createdAt": createdAt,
	})
}

func (h *Handler) DeleteDatabaseService(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var serviceName string
	err := h.Pool.QueryRow(r.Context(), `
		select ds.service_name
		from database_services ds
		join projects p on p.id = ds.project_id
		where ds.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&serviceName)
	if err != nil {
		http.Error(w, `{"message":"database service not found"}`, http.StatusNotFound)
		return
	}
	services, err := h.Swarm.ListServices(r.Context())
	if err == nil {
		for _, svc := range services {
			if svc.Spec.Name == serviceName {
				_ = h.Swarm.RemoveService(r.Context(), svc.ID)
				break
			}
		}
	}
	_, err = h.Pool.Exec(r.Context(), `delete from database_services where id = $1::uuid`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func dbEngineImage(engine, version string) (string, int, bool) {
	if version == "" {
		version = "latest"
	}
	switch strings.ToLower(engine) {
	case "postgres":
		return "postgres:" + version, 5432, true
	case "mysql":
		return "mysql:" + version, 3306, true
	case "mariadb":
		return "mariadb:" + version, 3306, true
	case "mongo":
		return "mongo:" + version, 27017, true
	case "redis":
		return "redis:" + version, 6379, true
	default:
		return "", 0, false
	}
}

func dbServiceEnv(engine, username, passwordSecretName, database string) []string {
	if username == "" {
		username = "hive"
	}
	password := passwordSecretName
	passwordFile := ""
	if passwordSecretName == "" {
		password = "hive-password"
	} else {
		passwordFile = "/run/secrets/" + passwordSecretName
	}
	if database == "" {
		database = "hive"
	}
	switch engine {
	case "postgres":
		out := []string{"POSTGRES_USER=" + username, "POSTGRES_DB=" + database}
		if passwordFile != "" {
			return append(out, "POSTGRES_PASSWORD_FILE="+passwordFile)
		}
		return append(out, "POSTGRES_PASSWORD="+password)
	case "mysql", "mariadb":
		out := []string{"MYSQL_USER=" + username, "MYSQL_DATABASE=" + database}
		if passwordFile != "" {
			return append(out, "MYSQL_PASSWORD_FILE="+passwordFile, "MYSQL_ROOT_PASSWORD_FILE="+passwordFile)
		}
		return append(out, "MYSQL_PASSWORD="+password, "MYSQL_ROOT_PASSWORD="+password)
	case "mongo":
		out := []string{"MONGO_INITDB_ROOT_USERNAME=" + username, "MONGO_INITDB_DATABASE=" + database}
		if passwordFile != "" {
			return append(out, "MONGO_INITDB_ROOT_PASSWORD_FILE="+passwordFile)
		}
		return append(out, "MONGO_INITDB_ROOT_PASSWORD="+password)
	case "redis":
		if passwordFile != "" {
			return []string{"REDIS_PASSWORD_FILE=" + passwordFile}
		}
		return []string{"REDIS_PASSWORD=" + password}
	default:
		return []string{}
	}
}

func (h *Handler) secretExists(ctx context.Context, name string) bool {
	secrets, err := h.Swarm.ListSecrets(ctx)
	if err != nil {
		return false
	}
	for _, secret := range secrets {
		if secret.Spec.Name == name {
			return true
		}
	}
	return false
}

func normalizeServiceName(name string) string {
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
		return "database"
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	if out == "" {
		return "database"
	}
	return out
}

func ptrUint64(v uint64) *uint64 {
	return &v
}
