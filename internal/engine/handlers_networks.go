package engine

import (
	"encoding/json"
	"net/http"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/go-chi/chi/v5"
)

func (s *Server) apiListNetworks(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	networks, err := s.sc.Docker().NetworkList(r.Context(), network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("driver", "overlay")),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	type netInfo struct {
		ID         string            `json:"id"`
		Name       string            `json:"name"`
		Driver     string            `json:"driver"`
		Scope      string            `json:"scope"`
		Internal   bool              `json:"internal"`
		Attachable bool              `json:"attachable"`
		Encrypted  bool              `json:"encrypted"`
		Labels     map[string]string `json:"labels"`
		CreatedAt  string            `json:"created_at"`
		Containers int               `json:"containers"`
	}

	var result []netInfo
	for _, n := range networks {
		encrypted := false
		if n.Options != nil {
			encrypted = n.Options["encrypted"] == "true"
		}
		result = append(result, netInfo{
			ID:         n.ID,
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Internal:   n.Internal,
			Attachable: n.Attachable,
			Encrypted:  encrypted,
			Labels:     n.Labels,
			CreatedAt:  n.Created.Format("2006-01-02T15:04:05Z"),
			Containers: len(n.Containers),
		})
	}
	if result == nil {
		result = []netInfo{}
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiCreateNetwork(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	var body struct {
		Name       string            `json:"name"`
		Encrypted  bool              `json:"encrypted"`
		Attachable bool              `json:"attachable"`
		Internal   bool              `json:"internal"`
		Labels     map[string]string `json:"labels"`
		Subnet     string            `json:"subnet"`
		Gateway    string            `json:"gateway"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body", nil)
		return
	}
	if body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name required", nil)
		return
	}

	opts := network.CreateOptions{
		Driver:     "overlay",
		Attachable: body.Attachable,
		Internal:   body.Internal,
		Labels:     body.Labels,
		Options:    map[string]string{},
	}
	if body.Encrypted {
		opts.Options["encrypted"] = "true"
	}
	if body.Subnet != "" || body.Gateway != "" {
		ipam := network.IPAMConfig{}
		if body.Subnet != "" {
			ipam.Subnet = body.Subnet
		}
		if body.Gateway != "" {
			ipam.Gateway = body.Gateway
		}
		opts.IPAM = &network.IPAM{
			Config: []network.IPAMConfig{ipam},
		}
	}

	resp, err := s.sc.Docker().NetworkCreate(r.Context(), body.Name, opts)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": resp.ID, "name": body.Name})
}

func (s *Server) apiInspectNetwork(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	networkID := chi.URLParam(r, "networkId")
	net, err := s.sc.Docker().NetworkInspect(r.Context(), networkID, network.InspectOptions{})
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, net)
}

func (s *Server) apiRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	networkID := chi.URLParam(r, "networkId")
	if err := s.sc.Docker().NetworkRemove(r.Context(), networkID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": networkID})
}

func (s *Server) apiConnectServiceToNetwork(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	networkID := chi.URLParam(r, "networkId")
	var body struct {
		ContainerID string `json:"container_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ContainerID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "container_id required", nil)
		return
	}

	err = s.sc.Docker().NetworkConnect(r.Context(), networkID, body.ContainerID, nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

func (s *Server) apiDisconnectServiceFromNetwork(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	networkID := chi.URLParam(r, "networkId")
	var body struct {
		ContainerID string `json:"container_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ContainerID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "container_id required", nil)
		return
	}

	err = s.sc.Docker().NetworkDisconnect(r.Context(), networkID, body.ContainerID, false)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}
