package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
)

func ListDNSRecords(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	s := storeFromRequest(r)
	provider, err := s.GetDNSProvider(r.Context(), providerID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	orgID := orgIDFromRequest(r)
	if provider.OrgID != orgID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	records, err := s.ListDNSRecords(r.Context(), providerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if records == nil {
		records = []store.DNSRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

func DeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "providerId")
	recordID := chi.URLParam(r, "recordId")
	s := storeFromRequest(r)
	provider, err := s.GetDNSProvider(r.Context(), providerID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	orgID := orgIDFromRequest(r)
	if provider.OrgID != orgID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dns provider not found"})
		return
	}
	if err := s.DeleteDNSRecord(r.Context(), recordID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": recordID})
}
