package engine

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/ceph"
	"github.com/lholliger/hive/internal/store"
)

// Ceph clusters
func (s *Server) apiCreateCephCluster(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var c store.CephCluster
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if c.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}
	if err := s.store.CreateCephCluster(r.Context(), &c); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "ceph_cluster", c.ID, "")
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) apiListCephClusters(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	clusters, err := s.store.ListCephClusters(r.Context())
	if handleErr(w, err) {
		return
	}
	if clusters == nil {
		clusters = []store.CephCluster{}
	}
	writeJSON(w, http.StatusOK, clusters)
}

func (s *Server) apiGetCephCluster(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	c, err := s.store.GetCephCluster(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) apiDeleteCephCluster(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteCephCluster(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "ceph_cluster", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Ceph OSDs
func (s *Server) apiCreateCephOSD(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	clusterID := chi.URLParam(r, "clusterId")
	var o store.CephOSD
	if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	o.ClusterID = clusterID
	if o.NodeID == "" || o.DevicePath == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "node_id and device_path are required", nil)
		return
	}
	if err := s.store.CreateCephOSD(r.Context(), &o); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "ceph_osd", o.ID, "")
	writeJSON(w, http.StatusCreated, o)
}

func (s *Server) apiListCephOSDs(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	clusterID := chi.URLParam(r, "clusterId")
	osds, err := s.store.ListCephOSDs(r.Context(), clusterID)
	if handleErr(w, err) {
		return
	}
	if osds == nil {
		osds = []store.CephOSD{}
	}
	writeJSON(w, http.StatusOK, osds)
}

func (s *Server) apiDeleteCephOSD(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteCephOSD(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "ceph_osd", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Ceph pools
func (s *Server) apiCreateCephPool(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	clusterID := chi.URLParam(r, "clusterId")
	var p store.CephPool
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	p.ClusterID = clusterID
	if p.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}
	if err := s.store.CreateCephPool(r.Context(), &p); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "ceph_pool", p.ID, "")
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) apiListCephPools(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	clusterID := chi.URLParam(r, "clusterId")
	pools, err := s.store.ListCephPools(r.Context(), clusterID)
	if handleErr(w, err) {
		return
	}
	if pools == nil {
		pools = []store.CephPool{}
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) apiDeleteCephPool(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteCephPool(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "ceph_pool", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiCephClusterHealth(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	clusterID := chi.URLParam(r, "clusterId")
	cluster, err := s.store.GetCephCluster(r.Context(), clusterID)
	if handleErr(w, err) {
		return
	}

	report := ceph.HealthCache.Get(cluster.FSID)
	if report == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "unknown", "cluster_id": clusterID})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) apiAllDisks(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}

	nodes, err := s.sc.ListNodes(r.Context())
	if handleErr(w, err) {
		return
	}

	type nodeDisks struct {
		NodeID   string          `json:"node_id"`
		Hostname string          `json:"hostname"`
		Disks    json.RawMessage `json:"disks"`
	}

	var results []nodeDisks
	for _, node := range nodes {
		hostname := node.Description.Hostname
		subject := "hive.ceph.cmd." + node.ID
		payload, _ := json.Marshal(map[string]string{"command": "device_ls"})
		msg, err := s.nc.Request(subject, payload, 10*time.Second)
		if err != nil {
			results = append(results, nodeDisks{
				NodeID:   node.ID,
				Hostname: hostname,
				Disks:    json.RawMessage("[]"),
			})
			continue
		}
		results = append(results, nodeDisks{
			NodeID:   node.ID,
			Hostname: hostname,
			Disks:    json.RawMessage(msg.Data),
		})
	}

	writeJSON(w, http.StatusOK, results)
}
