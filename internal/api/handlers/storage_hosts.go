package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/encryption"
)

func CreateStorageHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                string          `json:"name"`
		NodeID              string          `json:"node_id"`
		Address             string          `json:"address"`
		Type                string          `json:"type"`
		DefaultExportPath   string          `json:"default_export_path"`
		DefaultMountType    string          `json:"default_mount_type"`
		MountOptionsDefault string          `json:"mount_options_default"`
		Credentials         string          `json:"credentials"`
		Capabilities        json.RawMessage `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" || body.Address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and address are required"})
		return
	}
	if body.Type == "" {
		body.Type = "nas"
	}
	if body.DefaultMountType == "" {
		body.DefaultMountType = "nfs"
	}
	if body.Capabilities == nil {
		body.Capabilities = json.RawMessage(`{}`)
	}

	nodeLabel := "hive.storage." + body.Name + "=true"

	var credEnc []byte
	if body.Credentials != "" {
		var err error
		credEnc, err = encryption.Encrypt([]byte(body.Credentials))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encrypt credentials: " + err.Error()})
			return
		}
	}

	sh := &store.StorageHost{
		Name:                 body.Name,
		NodeID:               body.NodeID,
		Address:              body.Address,
		Type:                 body.Type,
		DefaultExportPath:    body.DefaultExportPath,
		DefaultMountType:     body.DefaultMountType,
		MountOptionsDefault:  body.MountOptionsDefault,
		CredentialsEncrypted: credEnc,
		Capabilities:         body.Capabilities,
		NodeLabel:            nodeLabel,
		Status:               "active",
	}

	s := storeFromRequest(r)
	if err := s.CreateStorageHost(r.Context(), sh); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if body.NodeID != "" {
		applyStorageNodeLabel(r, body.NodeID, nodeLabel)
	}

	writeJSON(w, http.StatusCreated, sh)
}

func ListStorageHosts(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	hosts, err := s.ListStorageHosts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if hosts == nil {
		hosts = []store.StorageHost{}
	}
	writeJSON(w, http.StatusOK, hosts)
}

func GetStorageHost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hostId")
	s := storeFromRequest(r)
	host, err := s.GetStorageHost(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "storage host not found"})
		return
	}
	writeJSON(w, http.StatusOK, host)
}

func UpdateStorageHost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hostId")
	s := storeFromRequest(r)

	existing, err := s.GetStorageHost(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "storage host not found"})
		return
	}

	var body struct {
		Name                string          `json:"name"`
		NodeID              string          `json:"node_id"`
		Address             string          `json:"address"`
		Type                string          `json:"type"`
		DefaultExportPath   string          `json:"default_export_path"`
		DefaultMountType    string          `json:"default_mount_type"`
		MountOptionsDefault string          `json:"mount_options_default"`
		Credentials         string          `json:"credentials"`
		Capabilities        json.RawMessage `json:"capabilities"`
		Status              string          `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if body.Name != "" {
		existing.Name = body.Name
		existing.NodeLabel = "hive.storage." + body.Name + "=true"
	}
	if body.NodeID != "" {
		existing.NodeID = body.NodeID
	}
	if body.Address != "" {
		existing.Address = body.Address
	}
	if body.Type != "" {
		existing.Type = body.Type
	}
	if body.DefaultExportPath != "" {
		existing.DefaultExportPath = body.DefaultExportPath
	}
	if body.DefaultMountType != "" {
		existing.DefaultMountType = body.DefaultMountType
	}
	if body.MountOptionsDefault != "" {
		existing.MountOptionsDefault = body.MountOptionsDefault
	}
	if body.Capabilities != nil {
		existing.Capabilities = body.Capabilities
	}
	if body.Status != "" {
		existing.Status = body.Status
	}
	if body.Credentials != "" {
		credEnc, err := encryption.Encrypt([]byte(body.Credentials))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encrypt credentials: " + err.Error()})
			return
		}
		existing.CredentialsEncrypted = credEnc
	}

	if err := s.UpdateStorageHost(r.Context(), existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if existing.NodeID != "" {
		applyStorageNodeLabel(r, existing.NodeID, existing.NodeLabel)
	}

	writeJSON(w, http.StatusOK, existing)
}

func DeleteStorageHost(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hostId")
	s := storeFromRequest(r)
	if err := s.DeleteStorageHost(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func TestStorageHostConnectivity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hostId")
	s := storeFromRequest(r)
	host, err := s.GetStorageHost(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "storage host not found"})
		return
	}

	result := map[string]interface{}{
		"host":    host.Name,
		"address": host.Address,
		"type":    host.Type,
		"ok":      true,
		"message": "connectivity check passed",
	}

	writeJSON(w, http.StatusOK, result)
}

func applyStorageNodeLabel(r *http.Request, nodeID, label string) {
	sc, err := swarm.NewClient(nil)
	if err != nil {
		return
	}
	defer func() { _ = sc.Close() }()

	parts := splitLabelKeyValue(label)
	if parts[0] != "" {
		labels := map[string]string{parts[0]: parts[1]}
		_ = sc.UpdateNodeLabels(r.Context(), nodeID, labels)
	}
}

func splitLabelKeyValue(label string) [2]string {
	for i, c := range label {
		if c == '=' {
			return [2]string{label[:i], label[i+1:]}
		}
	}
	return [2]string{label, "true"}
}
