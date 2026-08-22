package domains

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/proxy"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// Handler serves domain management endpoints.
type Handler struct {
	Pool  *pgxpool.Pool
	Swarm proxy.ServiceStore
}

// NewHandler returns a domain Handler backed by the given pool and swarm
// client. The Swarm field is the proxy.ServiceStore seam; tests inject
// fakes.
func NewHandler(pool *pgxpool.Pool, swarm proxy.ServiceStore) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

// ListDomains lists domains for the organization.
func (h *Handler) ListDomains(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select d.id::text, d.application_id::text, d.hostname, d.tls_enabled,
		       d.route_type, d.path_prefix, d.strip_prefix, d.priority, d.created_at
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where p.organization_id = $1::uuid
		order by d.created_at desc
	`, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type item struct {
		ID            string    `json:"id"`
		ApplicationID string    `json:"applicationId"`
		Hostname      string    `json:"hostname"`
		RouteType     string    `json:"routeType"`
		PathPrefix    string    `json:"pathPrefix"`
		StripPrefix   bool      `json:"stripPrefix"`
		Priority      *int      `json:"priority"`
		TLSEnabled    bool      `json:"tlsEnabled"`
		CreatedAt     time.Time `json:"createdAt"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.ApplicationID, &it.Hostname, &it.TLSEnabled,
			&it.RouteType, &it.PathPrefix, &it.StripPrefix, &it.Priority, &it.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// CreateDomain registers a new domain.
func (h *Handler) CreateDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	var req struct {
		ApplicationID string `json:"applicationId"`
		Hostname      string `json:"hostname"`
		TLSEnabled    bool   `json:"tlsEnabled"`
		RouteType     string `json:"routeType"`
		PathPrefix    string `json:"pathPrefix"`
		StripPrefix   bool   `json:"stripPrefix"`
		Priority      int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ApplicationID == "" || req.Hostname == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	req.Hostname = normalizeHostname(req.Hostname)
	if !validHostname(req.Hostname) {
		http.Error(w, `{"message":"invalid hostname"}`, http.StatusBadRequest)
		return
	}
	route, err := normalizeRoute(req.Hostname, req.RouteType, req.PathPrefix, req.StripPrefix, req.Priority)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `
		insert into domains(application_id, hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority)
		select a.id, $2, $3, $4, $5, $6, $7
		from applications a
		join projects p on p.id = a.project_id
		where a.id = $1::uuid and p.organization_id = $8::uuid
		returning id::text
	`, req.ApplicationID, req.Hostname, req.TLSEnabled,
		route.RouteType, route.PathPrefix, route.StripPrefix, nullablePriority(route.Priority), orgID).Scan(&id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.applyDomainsForApp(r.Context(), req.ApplicationID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// GetDomain returns a single domain by ID.
func (h *Handler) GetDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin, rbac.RoleMember)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	row := h.Pool.QueryRow(r.Context(), `
		select d.id::text, d.application_id::text, d.hostname, d.tls_enabled,
		       d.route_type, d.path_prefix, d.strip_prefix, d.priority, d.created_at
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID)
	var domainID, appID, host string
	var tls bool
	var routeType, pathPrefix string
	var stripPrefix bool
	var priority *int
	var createdAt time.Time
	if err := row.Scan(&domainID, &appID, &host, &tls,
		&routeType, &pathPrefix, &stripPrefix, &priority, &createdAt); err != nil {
		http.Error(w, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	out := map[string]any{
		"id":            domainID,
		"applicationId": appID,
		"hostname":      host,
		"tlsEnabled":    tls,
		"routeType":     routeType,
		"pathPrefix":    pathPrefix,
		"stripPrefix":   stripPrefix,
		"priority":      priority,
		"createdAt":     createdAt,
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// UpdateDomain updates an existing domain.
func (h *Handler) UpdateDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Hostname    string  `json:"hostname"`
		TLSEnabled  *bool   `json:"tlsEnabled"`
		RouteType   *string `json:"routeType"`
		PathPrefix  *string `json:"pathPrefix"`
		StripPrefix *bool   `json:"stripPrefix"`
		Priority    *int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	var oldAppID, oldHost, oldRouteType, oldPathPrefix string
	var oldStripPrefix bool
	var oldPriority *int
	_ = h.Pool.QueryRow(r.Context(), `
		select d.application_id::text, d.hostname, d.route_type, d.path_prefix, d.strip_prefix, d.priority
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&oldAppID, &oldHost, &oldRouteType, &oldPathPrefix, &oldStripPrefix, &oldPriority)

	if strings.TrimSpace(req.Hostname) != "" {
		req.Hostname = normalizeHostname(req.Hostname)
	}
	if req.Hostname != "" && !validHostname(req.Hostname) {
		http.Error(w, `{"message":"invalid hostname"}`, http.StatusBadRequest)
		return
	}

	// Validate the effective routing combination (new values merged over the
	// stored row) so e.g. switching to wildcard without a *. hostname fails
	// up front instead of producing a broken Traefik rule.
	effectiveHost := oldHost
	if req.Hostname != "" {
		effectiveHost = req.Hostname
	}
	effectiveType := oldRouteType
	if req.RouteType != nil {
		effectiveType = *req.RouteType
	}
	effectivePrefix := oldPathPrefix
	if req.PathPrefix != nil {
		effectivePrefix = *req.PathPrefix
	}
	effectiveStrip := oldStripPrefix
	if req.StripPrefix != nil {
		effectiveStrip = *req.StripPrefix
	}
	effectivePriority := 0
	if oldPriority != nil {
		effectivePriority = *oldPriority
	}
	if req.Priority != nil {
		effectivePriority = *req.Priority
	}
	route, err := normalizeRoute(effectiveHost, effectiveType, effectivePrefix, effectiveStrip, effectivePriority)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	tlsValue := false
	hasTLS := false
	if req.TLSEnabled != nil {
		tlsValue = *req.TLSEnabled
		hasTLS = true
	}
	cmd, err := h.Pool.Exec(r.Context(), `
		update domains d
		set hostname = coalesce(nullif($3,''), d.hostname),
			tls_enabled = case when $4 then $5 else d.tls_enabled end,
			route_type = $6,
			path_prefix = $7,
			strip_prefix = $8,
			priority = $9
		from applications a
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and a.id = d.application_id and p.organization_id = $2::uuid
	`, id, orgID, req.Hostname, hasTLS, tlsValue,
		route.RouteType, route.PathPrefix, route.StripPrefix, nullablePriority(route.Priority))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	if oldAppID != "" && oldHost != "" && req.Hostname != "" && req.Hostname != oldHost {
		_ = h.removeDomainRoute(r.Context(), oldAppID, oldHost)
	}
	if oldAppID != "" {
		_ = h.applyDomainsForApp(r.Context(), oldAppID)
	}
	h.GetDomain(w, r)
}

// DeleteDomain removes a domain.
func (h *Handler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	orgID, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	if !ok {
		return
	}
	id := chi.URLParam(r, "id")
	var appID, host string
	_ = h.Pool.QueryRow(r.Context(), `
		select d.application_id::text, d.hostname
		from domains d
		join applications a on a.id = d.application_id
		join projects p on p.id = a.project_id
		where d.id = $1::uuid and p.organization_id = $2::uuid
	`, id, orgID).Scan(&appID, &host)
	cmd, err := h.Pool.Exec(r.Context(), `
		delete from domains d
		using applications a, projects p
		where d.id = $1::uuid and a.id = d.application_id and p.id = a.project_id and p.organization_id = $2::uuid
	`, id, orgID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cmd.RowsAffected() == 0 {
		http.Error(w, `{"message":"domain not found"}`, http.StatusNotFound)
		return
	}
	if appID != "" && host != "" {
		_ = h.removeDomainRoute(r.Context(), appID, host)
		_ = h.applyDomainsForApp(r.Context(), appID)
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) applyDomainsForApp(ctx context.Context, appID string) error {
	// Serialize with deploys and other domain writers: everyone mutates the
	// same ServiceSpec via read-modify-write.
	return deploy.WithAppLock(ctx, h.Pool, appID, func(ctx context.Context) error {
		return h.applyDomainsForAppLocked(ctx, appID)
	})
}

func (h *Handler) applyDomainsForAppLocked(ctx context.Context, appID string) error {
	services, err := h.Swarm.ListServices(ctx)
	if err != nil {
		return err
	}
	serviceID := ""
	containerPort := 3000
	for _, svc := range services {
		if svc.Spec.Labels["hive.app.id"] == appID {
			serviceID = svc.ID
			if p := svc.Spec.Labels["hive.app.port"]; p != "" {
				if parsed, parseErr := strconv.Atoi(p); parseErr == nil {
					containerPort = parsed
				}
			}
			break
		}
	}
	if serviceID == "" {
		return nil
	}
	rows, err := h.Pool.Query(ctx, `
		select hostname, tls_enabled, route_type, path_prefix, strip_prefix, priority
		from domains where application_id = $1::uuid`, appID)
	if err != nil {
		return err
	}
	defer rows.Close()
	manager := proxy.NewDomainManager(h.Swarm)
	for rows.Next() {
		route, scanErr := scanRoute(rows)
		if scanErr != nil {
			return scanErr
		}
		if err := manager.ApplyDomain(ctx, serviceID, proxy.RouterNameFromHost(route.Host), route, containerPort); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) removeDomainRoute(ctx context.Context, appID, host string) error {
	return deploy.WithAppLock(ctx, h.Pool, appID, func(ctx context.Context) error {
		services, err := h.Swarm.ListServices(ctx)
		if err != nil {
			return err
		}
		for _, svc := range services {
			if svc.Spec.Labels["hive.app.id"] != appID {
				continue
			}
			return proxy.NewDomainManager(h.Swarm).RemoveDomain(ctx, svc.ID, proxy.RouterNameFromHost(host))
		}
		return nil
	})
}

func normalizeHostname(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	return strings.Trim(host, ".")
}

// scanRoute scans one domains row (hostname, tls_enabled, route_type,
// path_prefix, strip_prefix, priority — in that order) into a proxy.Route.
func scanRoute(row pgx.Row) (proxy.Route, error) {
	var r proxy.Route
	var priority *int
	if err := row.Scan(&r.Host, &r.TLSEnabled, &r.RouteType, &r.PathPrefix, &r.StripPrefix, &priority); err != nil {
		return r, err
	}
	if priority != nil {
		r.Priority = *priority
	}
	return r, nil
}

// nullablePriority maps the auto-priority sentinel 0 to SQL NULL.
func nullablePriority(priority int) any {
	if priority > 0 {
		return priority
	}
	return nil
}

// normalizeRoute validates the routing fields of a domain request against
// its (normalized) hostname and returns the proxy.Route to persist.
func normalizeRoute(host, routeType, pathPrefix string, stripPrefix bool, priority int) (proxy.Route, error) {
	switch routeType {
	case "", proxy.RouteTypeHost:
		routeType = proxy.RouteTypeHost
	case proxy.RouteTypeWildcard:
		if !strings.HasPrefix(host, "*.") {
			return proxy.Route{}, fmt.Errorf("wildcard routes require a hostname starting with '*.'")
		}
	case proxy.RouteTypePath:
		pathPrefix = strings.TrimSpace(pathPrefix)
		if pathPrefix == "" {
			return proxy.Route{}, fmt.Errorf("path routes require a path prefix")
		}
		if !strings.HasPrefix(pathPrefix, "/") {
			pathPrefix = "/" + pathPrefix
		}
	default:
		return proxy.Route{}, fmt.Errorf("invalid route type %q", routeType)
	}
	if priority < 0 {
		return proxy.Route{}, fmt.Errorf("priority must be a positive integer")
	}
	r := proxy.Route{
		Host:        host,
		RouteType:   routeType,
		PathPrefix:  pathPrefix,
		StripPrefix: stripPrefix,
		Priority:    priority,
	}
	if routeType != proxy.RouteTypePath {
		r.StripPrefix = false
	}
	return r, nil
}

func validHostname(host string) bool {
	if host == "localhost" {
		return true
	}
	// A single leading "*." marks a wildcard domain; no other '*' is allowed.
	wildcard := strings.HasPrefix(host, "*.")
	if wildcard {
		host = host[len("*."):]
	}
	if strings.Contains(host, "*") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return false
		}
	}
	return len(host) <= 253
}
