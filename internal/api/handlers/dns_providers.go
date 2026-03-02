package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/dns"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func CreateDNSProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string            `json:"name"`
		Type      string            `json:"type"`
		Config    map[string]string  `json:"config"`
		IsDefault bool              `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" || body.Type == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and type are required"})
		return
	}

	configJSON, err := json.Marshal(body.Config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid config"})
		return
	}
	configEnc, err := encryption.Encrypt(configJSON)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encrypt config: " + err.Error()})
		return
	}

	s := storeFromRequest(r)
	orgID := orgIDFromRequest(r)

	provider := &store.DNSProvider{
		OrgID:           orgID,
		Name:            body.Name,
		Type:            body.Type,
		ConfigEncrypted: configEnc,
		IsDefault:       body.IsDefault,
	}
	if err := s.CreateDNSProvider(r.Context(), provider); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, provider)
}

func ListDNSProviders(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	orgID := orgIDFromRequest(r)
	providers, err := s.ListDNSProviders(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if providers == nil {
		providers = []store.DNSProvider{}
	}
	writeJSON(w, http.StatusOK, providers)
}

func GetDNSProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerId")
	s := storeFromRequest(r)
	provider, err := s.GetDNSProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	orgID := orgIDFromRequest(r)
	if provider.OrgID != orgID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	writeJSON(w, http.StatusOK, provider)
}

func DeleteDNSProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerId")
	s := storeFromRequest(r)
	provider, err := s.GetDNSProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	orgID := orgIDFromRequest(r)
	if provider.OrgID != orgID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	if err := s.DeleteDNSProvider(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func TestDNSProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "providerId")
	s := storeFromRequest(r)
	provider, err := s.GetDNSProvider(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	orgID := orgIDFromRequest(r)
	if provider.OrgID != orgID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}

	plain, err := encryption.Decrypt(provider.ConfigEncrypted)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "decrypt config: " + err.Error()})
		return
	}
	var config map[string]string
	if err := json.Unmarshal(plain, &config); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid stored config"})
		return
	}

	p, err := dns.NewProvider(provider.Type, config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_, err = p.ListRecords(r.Context(), "")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "failed", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
