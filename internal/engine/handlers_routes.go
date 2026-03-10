package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/proxy"
	"github.com/lholliger/hive/internal/store"
)

// Proxy routes
func (s *Server) apiCreateProxyRoute(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	var route store.ProxyRoute
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	route.ProjectID = projectID
	if route.Domain == "" || route.TargetService == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "domain and target_service are required", nil)
		return
	}
	if err := s.store.CreateProxyRoute(r.Context(), &route); handleErr(w, err) {
		return
	}
	if route.Enabled {
		s.applyProxyRouteLabels(r.Context(), &route)
	}
	s.auditLog(r, "create", "proxy_route", route.ID, "")
	writeJSON(w, http.StatusCreated, route)
}

func (s *Server) apiListProxyRoutes(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	routes, err := s.store.ListProxyRoutes(r.Context(), projectID)
	if handleErr(w, err) {
		return
	}
	if routes == nil {
		routes = []store.ProxyRoute{}
	}
	writeJSON(w, http.StatusOK, routes)
}

func (s *Server) apiGetProxyRoute(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "routeId")
	route, err := s.store.GetProxyRoute(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, route)
}

func (s *Server) apiUpdateProxyRoute(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "routeId")
	var route store.ProxyRoute
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	route.ID = id
	if err := s.store.UpdateProxyRoute(r.Context(), &route); handleErr(w, err) {
		return
	}
	updated, _ := s.store.GetProxyRoute(r.Context(), id)
	if updated != nil {
		if updated.Enabled {
			s.applyProxyRouteLabels(r.Context(), updated)
		} else {
			s.removeProxyRouteLabels(r.Context(), updated)
		}
	}
	s.auditLog(r, "update", "proxy_route", id, "")
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, route)
	}
}

func (s *Server) apiDeleteProxyRoute(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "routeId")
	route, _ := s.store.GetProxyRoute(r.Context(), id)
	if route != nil {
		s.removeProxyRouteLabels(r.Context(), route)
	}
	if err := s.store.DeleteProxyRoute(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "proxy_route", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Custom certificates
func (s *Server) apiCreateCustomCertificate(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	var cert store.CustomCertificate
	if err := json.NewDecoder(r.Body).Decode(&cert); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	cert.ProjectID = projectID
	if cert.Domain == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "domain is required", nil)
		return
	}
	if err := s.store.CreateCustomCertificate(r.Context(), &cert); handleErr(w, err) {
		return
	}
	if err := s.writeCertFiles(r.Context(), &cert); err != nil {
		s.log.Warnf("write cert files for %s: %v", cert.Domain, err)
	}
	s.auditLog(r, "create", "custom_certificate", cert.ID, "")
	writeJSON(w, http.StatusCreated, cert)
}

func (s *Server) apiListCustomCertificates(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	certs, err := s.store.ListCustomCertificates(r.Context(), projectID)
	if handleErr(w, err) {
		return
	}
	if certs == nil {
		certs = []store.CustomCertificate{}
	}
	writeJSON(w, http.StatusOK, certs)
}

func (s *Server) apiDeleteCustomCertificate(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "certId")
	if err := s.store.DeleteCustomCertificate(r.Context(), id); handleErr(w, err) {
		return
	}
	s.removeCertFiles(r.Context(), id)
	s.auditLog(r, "delete", "custom_certificate", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) applyProxyRouteLabels(ctx context.Context, route *store.ProxyRoute) {
	if s.sc == nil || route.TargetService == "" {
		return
	}
	svc, err := s.sc.GetService(ctx, route.TargetService)
	if err != nil || svc == nil {
		return
	}

	routerName := sanitizeLabel(route.Name)
	if routerName == "" {
		routerName = sanitizeLabel(route.ID)
	}

	labels := svc.Spec.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	labels["traefik.enable"] = "true"
	labels[fmt.Sprintf("traefik.http.routers.%s.entrypoints", routerName)] = "websecure"
	if route.TargetPort > 0 {
		labels[fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName)] = fmt.Sprintf("%d", route.TargetPort)
	}

	isWildcard := strings.HasPrefix(route.Domain, "*.")

	if isWildcard {
		baseDomain := route.Domain[2:]
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", routerName)] = fmt.Sprintf("HostRegexp(`{subdomain:.+}.%s`)", baseDomain)
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.domains[0].main", routerName)] = baseDomain
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.domains[0].sans[0]", routerName)] = route.Domain
	} else {
		labels[fmt.Sprintf("traefik.http.routers.%s.rule", routerName)] = fmt.Sprintf("Host(`%s`)", route.Domain)
	}

	sslMode := route.SSLMode
	if (sslMode == "" || sslMode == "letsencrypt") && s.store != nil {
		if mode, err := s.store.GetSetting(ctx, "ingress_mode"); err == nil && (mode == "cloudflare_tunnel" || mode == "both") {
			sslMode = "cloudflare"
		}
	}
	if sslMode == "" {
		sslMode = "letsencrypt"
	}

	switch sslMode {
	case "cloudflare":
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", routerName)] = "cloudflare"
		if isWildcard {
			baseDomain := route.Domain[2:]
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.domains[0].main", routerName)] = baseDomain
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.domains[0].sans[0]", routerName)] = route.Domain
		}
	case "custom":
		if route.CustomCertID != "" {
			labels[fmt.Sprintf("traefik.http.routers.%s.tls", routerName)] = "true"
		} else {
			labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", routerName)] = "letsencrypt"
		}
	default:
		labels[fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", routerName)] = "letsencrypt"
	}

	// Apply middleware config if present
	if len(route.MiddlewareConfig) > 0 {
		var mwConfig map[string]interface{}
		if err := json.Unmarshal(route.MiddlewareConfig, &mwConfig); err == nil && len(mwConfig) > 0 {
			mwName := routerName + "-mw"
			labels[fmt.Sprintf("traefik.http.routers.%s.middlewares", routerName)] = mwName
		}
	}

	svc.Spec.Labels = labels
	_ = s.sc.UpdateServiceSpec(ctx, svc.ID, svc.Meta.Version, svc.Spec)

	// Update Traefik dynamic config for file provider
	s.refreshTraefikDynamicConfig(ctx)
}

func (s *Server) removeProxyRouteLabels(ctx context.Context, route *store.ProxyRoute) {
	if s.sc == nil || route.TargetService == "" {
		return
	}
	svc, err := s.sc.GetService(ctx, route.TargetService)
	if err != nil || svc == nil {
		return
	}

	routerName := sanitizeLabel(route.Name)
	if routerName == "" {
		routerName = sanitizeLabel(route.ID)
	}

	labels := svc.Spec.Labels
	if labels == nil {
		return
	}

	prefix := fmt.Sprintf("traefik.http.routers.%s.", routerName)
	svcPrefix := fmt.Sprintf("traefik.http.services.%s.", routerName)
	for k := range labels {
		if strings.HasPrefix(k, prefix) || strings.HasPrefix(k, svcPrefix) {
			delete(labels, k)
		}
	}

	svc.Spec.Labels = labels
	_ = s.sc.UpdateServiceSpec(ctx, svc.ID, svc.Meta.Version, svc.Spec)
	s.refreshTraefikDynamicConfig(ctx)
}

func (s *Server) refreshTraefikDynamicConfig(ctx context.Context) {
	if s.store == nil {
		return
	}
	routes, err := s.store.ListAllProxyRoutes(ctx)
	if err != nil {
		s.log.Warnf("refresh traefik config: list routes: %v", err)
		return
	}
	defaultResolver := "letsencrypt"
	if mode, setErr := s.store.GetSetting(ctx, "ingress_mode"); setErr == nil && (mode == "cloudflare_tunnel" || mode == "both") {
		defaultResolver = "cloudflare"
	}
	cfg, err := proxy.GenerateDynamicConfig(routes, defaultResolver)
	if err != nil {
		s.log.Warnf("refresh traefik config: generate: %v", err)
		return
	}
	configDir := filepath.Join(s.cfg.DataDir, "traefik")
	if err := proxy.WriteDynamicConfig(configDir, cfg); err != nil {
		s.log.Warnf("refresh traefik config: write: %v", err)
	}
}

func sanitizeLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}
