package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/agent"
	hiveceph "github.com/lholliger/hive/internal/ceph"
	"github.com/lholliger/hive/internal/store"
)

func CreateCephCluster(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name            string                    `json:"name"`
			BootstrapNodeID string                    `json:"bootstrap_node_id"`
			MonNodes        []hiveceph.NodeSelection  `json:"mon_nodes"`
			OSDSelections   []hiveceph.OSDSelection   `json:"osd_selections"`
			ReplicationSize int                       `json:"replication_size"`
			CreateCephFS    bool                      `json:"create_cephfs"`
			CephFSName      string                    `json:"cephfs_name"`
			PublicNetwork   string                    `json:"public_network"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name == "" || body.BootstrapNodeID == "" || len(body.MonNodes) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name, bootstrap_node_id, and mon_nodes are required"})
			return
		}
		if len(body.OSDSelections) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one OSD selection is required"})
			return
		}

		s := storeFromRequest(r)
		orch := hiveceph.NewOrchestrator(nc, s, nil)

		cluster, err := orch.Deploy(r.Context(), hiveceph.DeployRequest{
			Name:            body.Name,
			BootstrapNodeID: body.BootstrapNodeID,
			MonNodes:        body.MonNodes,
			OSDSelections:   body.OSDSelections,
			ReplicationSize: body.ReplicationSize,
			CreateCephFS:    body.CreateCephFS,
			CephFSName:      body.CephFSName,
			PublicNetwork:   body.PublicNetwork,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusAccepted, cluster)
	}
}

func ListCephClusters(w http.ResponseWriter, r *http.Request) {
	s := storeFromRequest(r)
	clusters, err := s.ListCephClusters(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if clusters == nil {
		clusters = []store.CephCluster{}
	}

	type clusterWithHealth struct {
		store.CephCluster
		Health *agent.CephHealthReport `json:"health,omitempty"`
	}

	result := make([]clusterWithHealth, len(clusters))
	for i, c := range clusters {
		result[i] = clusterWithHealth{CephCluster: c}
		if c.FSID != "" {
			result[i].Health = hiveceph.HealthCache.Get(c.FSID)
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func GetCephCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clusterId")
	s := storeFromRequest(r)
	cluster, err := s.GetCephCluster(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}

	health := hiveceph.HealthCache.Get(cluster.FSID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cluster": cluster,
		"health":  health,
	})
}

func DeleteCephCluster(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "clusterId")
		s := storeFromRequest(r)
		orch := hiveceph.NewOrchestrator(nc, s, nil)

		if err := orch.DestroyCluster(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
	}
}

func GetCephClusterHealth(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clusterId")
	s := storeFromRequest(r)
	cluster, err := s.GetCephCluster(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "cluster not found"})
		return
	}

	health := hiveceph.HealthCache.Get(cluster.FSID)
	if health == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no health data available"})
		return
	}

	writeJSON(w, http.StatusOK, health)
}

func ListCephOSDs(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterId")
	s := storeFromRequest(r)
	osds, err := s.ListCephOSDs(r.Context(), clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if osds == nil {
		osds = []store.CephOSD{}
	}
	writeJSON(w, http.StatusOK, osds)
}

func AddCephOSD(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clusterID := chi.URLParam(r, "clusterId")
		var body struct {
			NodeID     string `json:"node_id"`
			Hostname   string `json:"hostname"`
			DevicePath string `json:"device_path"`
			DeviceSize int64  `json:"device_size"`
			DeviceType string `json:"device_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.NodeID == "" || body.Hostname == "" || body.DevicePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "node_id, hostname, and device_path are required"})
			return
		}

		s := storeFromRequest(r)
		orch := hiveceph.NewOrchestrator(nc, s, nil)
		osd, err := orch.AddOSD(r.Context(), clusterID, hiveceph.OSDSelection{
			NodeID:     body.NodeID,
			Hostname:   body.Hostname,
			DevicePath: body.DevicePath,
			DeviceSize: body.DeviceSize,
			DeviceType: body.DeviceType,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, osd)
	}
}

func RemoveCephOSD(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clusterID := chi.URLParam(r, "clusterId")
		osdID := chi.URLParam(r, "osdId")

		s := storeFromRequest(r)
		orch := hiveceph.NewOrchestrator(nc, s, nil)

		if err := orch.RemoveOSD(r.Context(), clusterID, osdID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"deleted": osdID})
	}
}

func ListCephPools(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterId")
	s := storeFromRequest(r)
	pools, err := s.ListCephPools(r.Context(), clusterID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if pools == nil {
		pools = []store.CephPool{}
	}
	writeJSON(w, http.StatusOK, pools)
}

func CreateCephPool(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clusterID := chi.URLParam(r, "clusterId")
		var body struct {
			Name        string `json:"name"`
			PGNum       int    `json:"pg_num"`
			Size        int    `json:"size"`
			Type        string `json:"type"`
			Application string `json:"application"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.PGNum == 0 {
			body.PGNum = 32
		}
		if body.Size == 0 {
			body.Size = 3
		}
		if body.Type == "" {
			body.Type = "replicated"
		}
		if body.Application == "" {
			body.Application = "rbd"
		}

		s := storeFromRequest(r)
		orch := hiveceph.NewOrchestrator(nc, s, nil)

		pool := &store.CephPool{
			Name:        body.Name,
			PGNum:       body.PGNum,
			Size:        body.Size,
			Type:        body.Type,
			Application: body.Application,
		}
		if err := orch.CreatePool(r.Context(), clusterID, pool); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, pool)
	}
}

func DiscoverDisks(w http.ResponseWriter, r *http.Request) {
	nodeIDParam := r.URL.Query().Get("node_id")

	type nodeDisks struct {
		NodeID       string              `json:"node_id"`
		BlockDevices []agent.BlockDevice `json:"block_devices"`
	}

	reports := MetricsCache.GetAll()
	var result []nodeDisks

	for _, report := range reports {
		if nodeIDParam != "" && report.NodeID != nodeIDParam {
			continue
		}
		var available []agent.BlockDevice
		for _, bd := range report.BlockDevices {
			if bd.Available {
				available = append(available, bd)
			}
		}
		result = append(result, nodeDisks{
			NodeID:       report.NodeID,
			BlockDevices: available,
		})
	}

	if result == nil {
		result = []nodeDisks{}
	}
	writeJSON(w, http.StatusOK, result)
}

func DiscoverAllDisks(w http.ResponseWriter, r *http.Request) {
	_ = r.URL.Query().Get("node_id")

	reports := MetricsCache.GetAll()
	type nodeDisks struct {
		NodeID       string              `json:"node_id"`
		Hostname     string              `json:"hostname"`
		BlockDevices []agent.BlockDevice `json:"block_devices"`
	}

	var result []nodeDisks
	for _, report := range reports {
		result = append(result, nodeDisks{
			NodeID:       report.NodeID,
			Hostname:     report.Hostname,
			BlockDevices: report.BlockDevices,
		})
	}
	if result == nil {
		result = []nodeDisks{}
	}
	writeJSON(w, http.StatusOK, result)
}

