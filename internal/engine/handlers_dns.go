package engine

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiCreateDNSProvider(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name      string            `json:"name"`
		Type      string            `json:"type"`
		Config    map[string]string `json:"config"`
		IsDefault bool              `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name == "" || req.Type == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and type are required", nil)
		return
	}

	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid config", nil)
		return
	}

	p := &store.DNSProvider{
		OrgID:           user.OrgID,
		Name:            req.Name,
		Type:            req.Type,
		ConfigEncrypted: configBytes,
		IsDefault:       req.IsDefault,
	}
	if err := s.store.CreateDNSProvider(r.Context(), p); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "dns_provider", p.ID, "")
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) apiListDNSProviders(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	providers, err := s.store.ListDNSProviders(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if providers == nil {
		providers = []store.DNSProvider{}
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) apiGetDNSProvider(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "providerId")
	p, err := s.requireDNSProviderAccess(r.Context(), id, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) apiDeleteDNSProvider(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "providerId")
	if _, err := s.requireDNSProviderAccess(r.Context(), id, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}
	if err := s.store.DeleteDNSProvider(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "dns_provider", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiUpdateDNSProvider(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "providerId")
	existing, err := s.requireDNSProviderAccess(r.Context(), id, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	var req struct {
		Name      string            `json:"name"`
		Type      string            `json:"type"`
		Config    map[string]string `json:"config"`
		IsDefault bool              `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Name == "" || req.Type == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and type are required", nil)
		return
	}
	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid config", nil)
		return
	}
	existing.Name = req.Name
	existing.Type = req.Type
	existing.ConfigEncrypted = configBytes
	existing.IsDefault = req.IsDefault
	if err := s.store.UpdateDNSProvider(r.Context(), existing); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "dns_provider", id, "")
	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) apiTestDNSProvider(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "providerId")
	p, err := s.requireDNSProviderAccess(r.Context(), id, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}

	provider, _, err := s.instantiateDNSProvider(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider": p.Type,
			"status":   "error",
			"error":    "Invalid configuration: " + err.Error(),
		})
		return
	}

	records, err := provider.ListRecords(r.Context(), "")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"provider": p.Type,
			"status":   "error",
			"error":    "API call failed: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider":     p.Type,
		"status":       "ok",
		"message":      "Connected successfully",
		"record_count": len(records),
	})
}

func (s *Server) apiCreateDNSRecord(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	providerID := chi.URLParam(r, "providerId")
	if providerID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "provider_id is required", nil)
		return
	}
	var rec store.DNSRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if rec.Domain == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "domain is required", nil)
		return
	}
	rec.ProviderID = providerID
	if rec.RecordType == "" {
		rec.RecordType = "A"
	}
	if rec.Value == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "value is required", nil)
		return
	}
	p, err := s.requireDNSProviderAccess(r.Context(), providerID, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	provider, _, err := s.instantiateDNSProvider(r.Context(), p)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to instantiate DNS provider: "+err.Error(), nil)
		return
	}
	externalID, err := provider.CreateRecord(r.Context(), rec.Domain, rec.RecordType, rec.Value, rec.Proxied)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to create provider DNS record: "+err.Error(), nil)
		return
	}
	rec.ExternalID = externalID
	rec.Managed = true
	if err := s.store.CreateDNSRecord(r.Context(), &rec); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "dns_record", rec.ID, "")
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) apiListDNSRecords(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	providerID := chi.URLParam(r, "providerId")
	if _, err := s.requireDNSProviderAccess(r.Context(), providerID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}
	records, err := s.store.ListDNSRecords(r.Context(), providerID)
	if handleErr(w, err) {
		return
	}
	if records == nil {
		records = []store.DNSRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) apiDeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	providerID := chi.URLParam(r, "providerId")
	recordID := chi.URLParam(r, "recordId")
	if _, err := s.requireDNSProviderAccess(r.Context(), providerID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}

	rec, _, err := s.requireDNSRecordAccess(r.Context(), recordID, user.OrgID)
	if errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if handleErr(w, err) {
		return
	}
	if rec.ProviderID != providerID {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "record does not belong to provider", nil)
		return
	}

	if rec.Managed && rec.ExternalID != "" {
		provider, pErr := s.dnsProviderFromRecord(r.Context(), rec)
		if pErr != nil {
			writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to instantiate DNS provider: "+pErr.Error(), nil)
			return
		}
		if delErr := provider.DeleteRecord(r.Context(), rec.ExternalID); delErr != nil {
			writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to delete provider record: "+delErr.Error(), nil)
			return
		}
	}

	if err := s.store.DeleteDNSRecord(r.Context(), recordID); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "dns_record", recordID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
