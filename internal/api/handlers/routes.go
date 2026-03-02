package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/proxy"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func CreateProxyRoute(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	var body struct {
		Name             string                 `json:"name"`
		Domain           string                 `json:"domain"`
		TargetService    string                 `json:"target_service"`
		TargetPort       int                    `json:"target_port"`
		SSLMode          string                 `json:"ssl_mode"`
		CustomCertID     string                 `json:"custom_cert_id"`
		MiddlewareConfig map[string]interface{} `json:"middleware_config"`
		Enabled          *bool                  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" || body.Domain == "" || body.TargetService == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, domain, and target_service required"})
		return
	}
	if body.TargetPort == 0 {
		body.TargetPort = 80
	}
	if body.SSLMode == "" {
		body.SSLMode = "letsencrypt"
	}

	mwJSON, _ := json.Marshal(body.MiddlewareConfig)
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	s := storeFromRequest(r)
	route := &store.ProxyRoute{
		ProjectID:        projectID,
		Name:             body.Name,
		Domain:           body.Domain,
		TargetService:    body.TargetService,
		TargetPort:       body.TargetPort,
		SSLMode:          body.SSLMode,
		CustomCertID:     body.CustomCertID,
		MiddlewareConfig: mwJSON,
		Enabled:          enabled,
	}
	if err := s.CreateProxyRoute(r.Context(), route); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	regenerateTraefikConfig(r, s)
	writeJSON(w, http.StatusCreated, route)
}

func ListProxyRoutes(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	routes, err := s.ListProxyRoutes(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if routes == nil {
		routes = []store.ProxyRoute{}
	}
	writeJSON(w, http.StatusOK, routes)
}

func UpdateProxyRoute(w http.ResponseWriter, r *http.Request) {
	routeID := chi.URLParam(r, "routeId")
	s := storeFromRequest(r)

	existing, err := s.GetProxyRoute(r.Context(), routeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "route not found"})
		return
	}

	var body struct {
		Name             *string                `json:"name"`
		Domain           *string                `json:"domain"`
		TargetService    *string                `json:"target_service"`
		TargetPort       *int                   `json:"target_port"`
		SSLMode          *string                `json:"ssl_mode"`
		CustomCertID     *string                `json:"custom_cert_id"`
		MiddlewareConfig map[string]interface{} `json:"middleware_config"`
		Enabled          *bool                  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.Domain != nil {
		existing.Domain = *body.Domain
	}
	if body.TargetService != nil {
		existing.TargetService = *body.TargetService
	}
	if body.TargetPort != nil {
		existing.TargetPort = *body.TargetPort
	}
	if body.SSLMode != nil {
		existing.SSLMode = *body.SSLMode
	}
	if body.CustomCertID != nil {
		existing.CustomCertID = *body.CustomCertID
	}
	if body.MiddlewareConfig != nil {
		mwJSON, _ := json.Marshal(body.MiddlewareConfig)
		existing.MiddlewareConfig = mwJSON
	}
	if body.Enabled != nil {
		existing.Enabled = *body.Enabled
	}

	if err := s.UpdateProxyRoute(r.Context(), existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	regenerateTraefikConfig(r, s)
	writeJSON(w, http.StatusOK, existing)
}

func DeleteProxyRoute(w http.ResponseWriter, r *http.Request) {
	routeID := chi.URLParam(r, "routeId")
	s := storeFromRequest(r)
	if err := s.DeleteProxyRoute(r.Context(), routeID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	regenerateTraefikConfig(r, s)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": routeID})
}

func CreateCustomCertificate(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	var body struct {
		Domain     string `json:"domain"`
		CertPEM    string `json:"cert_pem"`
		KeyPEM     string `json:"key_pem"`
		IsWildcard bool   `json:"is_wildcard"`
		Provider   string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Domain == "" || body.CertPEM == "" || body.KeyPEM == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain, cert_pem, and key_pem required"})
		return
	}
	if body.Provider == "" {
		body.Provider = "manual"
	}

	keyEncrypted, err := encryption.Encrypt([]byte(body.KeyPEM))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed"})
		return
	}

	s := storeFromRequest(r)
	cert := &store.CustomCertificate{
		ProjectID:       projectID,
		Domain:          body.Domain,
		CertPEM:         body.CertPEM,
		KeyPEMEncrypted: keyEncrypted,
		IsWildcard:      body.IsWildcard,
		Provider:        body.Provider,
	}
	if err := s.CreateCustomCertificate(r.Context(), cert); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := proxy.WriteCertificateFiles("/data/traefik", cert); err != nil {
		log.Printf("WARN: failed to write certificate files: %v", err)
	}
	writeJSON(w, http.StatusCreated, cert)
}

func ListCustomCertificates(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	certs, err := s.ListCustomCertificates(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if certs == nil {
		certs = []store.CustomCertificate{}
	}
	writeJSON(w, http.StatusOK, certs)
}

func DeleteCustomCertificate(w http.ResponseWriter, r *http.Request) {
	certID := chi.URLParam(r, "certId")
	s := storeFromRequest(r)
	if err := s.DeleteCustomCertificate(r.Context(), certID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	proxy.RemoveCertificateFiles("/data/traefik", certID)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": certID})
}

func regenerateTraefikConfig(r *http.Request, s *store.Store) {
	routes, err := s.ListAllProxyRoutes(r.Context())
	if err != nil {
		log.Printf("WARN: failed to list proxy routes for traefik config: %v", err)
		return
	}
	cfg, err := proxy.GenerateDynamicConfig(routes, s)
	if err != nil {
		log.Printf("WARN: failed to generate traefik config: %v", err)
		return
	}
	if err := proxy.WriteDynamicConfig("/data/traefik", cfg); err != nil {
		log.Printf("WARN: failed to write traefik config: %v", err)
	}
}
