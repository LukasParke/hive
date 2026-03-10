package engine

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/storage"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiListVolumes(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	vols, err := s.store.ListVolumes(r.Context(), chi.URLParam(r, "projectId"))
	if handleErr(w, err) {
		return
	}
	if vols == nil {
		vols = []store.Volume{}
	}
	writeJSON(w, http.StatusOK, vols)
}

func (s *Server) apiCreateVolume(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Name          string            `json:"name"`
		MountType     string            `json:"mount_type"`
		RemoteHost    string            `json:"remote_host"`
		RemotePath    string            `json:"remote_path"`
		MountOptions  string            `json:"mount_options"`
		Username      string            `json:"username"`
		Password      string            `json:"password"`
		StorageHostID string            `json:"storage_host_id"`
		LocalPath     string            `json:"local_path"`
		Driver        string            `json:"driver"`
		DriverOpts    map[string]string `json:"driver_opts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	vol := &store.Volume{
		ProjectID:     chi.URLParam(r, "projectId"),
		Name:          req.Name,
		MountType:     req.MountType,
		RemoteHost:    req.RemoteHost,
		RemotePath:    req.RemotePath,
		MountOptions:  req.MountOptions,
		StorageHostID: req.StorageHostID,
		LocalPath:     req.LocalPath,
		Driver:        req.Driver,
		Scope:         "project",
		Status:        "pending",
	}
	if len(req.DriverOpts) > 0 {
		raw, _ := json.Marshal(req.DriverOpts)
		vol.DriverOpts = raw
	}

	if req.StorageHostID != "" {
		host, err := s.store.GetStorageHost(r.Context(), req.StorageHostID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid storage host", nil)
			return
		}
		if vol.RemoteHost == "" {
			vol.RemoteHost = host.Address
		}
		if vol.RemotePath == "" && host.DefaultExportPath != "" {
			vol.RemotePath = host.DefaultExportPath + "/" + req.Name
		}
		if vol.MountType == "" {
			vol.MountType = host.DefaultMountType
		}
	}

	if err := s.store.CreateVolume(r.Context(), vol); handleErr(w, err) {
		return
	}

	if dockerErr := s.ensureDockerVolume(r.Context(), vol, req.Username, req.Password); dockerErr != nil {
		s.log.Warnf("volume %s: Docker volume creation failed: %v", vol.Name, dockerErr)
		_ = s.store.UpdateVolumeStatus(r.Context(), vol.ID, "error")
		vol.Status = "error"
	} else {
		_ = s.store.UpdateVolumeStatus(r.Context(), vol.ID, "active")
		vol.Status = "active"
	}

	s.auditLog(r, "create", "volume", vol.ID, "")
	writeJSON(w, http.StatusCreated, vol)
}

func (s *Server) ensureDockerVolume(ctx context.Context, vol *store.Volume, username, password string) error {
	if s.sc == nil {
		return nil
	}

	labels := map[string]string{
		"hive.managed":    "true",
		"hive.volume_id":  vol.ID,
		"hive.project_id": vol.ProjectID,
	}

	switch vol.MountType {
	case "nfs":
		host := vol.RemoteHost
		path := vol.RemotePath
		if host == "" || path == "" {
			if vol.StorageHostID != "" {
				sh, err := s.store.GetStorageHost(ctx, vol.StorageHostID)
				if err == nil {
					if host == "" {
						host = sh.Address
					}
					if path == "" {
						path = sh.DefaultExportPath + "/" + vol.Name
					}
				}
			}
		}
		if host == "" || path == "" {
			return nil
		}
		_, err := s.sc.CreateNFSVolume(ctx, vol.Name, host, path, vol.MountOptions, labels)
		return err

	case "cifs":
		host := vol.RemoteHost
		share := vol.RemotePath
		if host == "" || share == "" {
			return nil
		}
		if username == "" && vol.StorageHostID != "" {
			if sh, err := s.store.GetStorageHost(ctx, vol.StorageHostID); err == nil {
				creds, _ := storage.DecryptCredentials(sh)
				if creds != "" {
					username = creds
				}
			}
		}
		_, err := s.sc.CreateCIFSVolume(ctx, vol.Name, host, share, username, password, vol.MountOptions, labels)
		return err

	case "cephfs":
		if vol.StorageHostID == "" {
			return nil
		}
		sh, err := s.store.GetStorageHost(ctx, vol.StorageHostID)
		if err != nil {
			return err
		}
		monitors := storage.CephMonitorAddresses(sh)
		_, err = s.sc.CreateCephFSVolume(ctx, vol.Name, joinAddrs(monitors), vol.CephFSName, vol.RemotePath, vol.MountOptions, labels)
		return err

	case "ceph-rbd":
		if vol.StorageHostID == "" {
			return nil
		}
		sh, err := s.store.GetStorageHost(ctx, vol.StorageHostID)
		if err != nil {
			return err
		}
		monitors := storage.CephMonitorAddresses(sh)
		_, err = s.sc.CreateCephRBDVolume(ctx, vol.Name, joinAddrs(monitors), vol.CephPool, vol.CephImage, vol.MountOptions, labels)
		return err

	default:
		return nil
	}
}

func joinAddrs(addrs []string) string {
	result := ""
	for i, a := range addrs {
		if i > 0 {
			result += ","
		}
		result += a
	}
	return result
}

func (s *Server) apiGetVolume(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	vol, err := s.store.GetVolume(r.Context(), chi.URLParam(r, "volumeId"))
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, vol)
}

func (s *Server) apiDeleteVolume(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "volumeId")
	if err := s.store.DeleteVolume(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "volume", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiAttachVolume(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		ContainerPath string `json:"container_path"`
		ReadOnly      bool   `json:"read_only"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	av := &store.AppVolume{
		AppID:         chi.URLParam(r, "appId"),
		VolumeID:      chi.URLParam(r, "volumeId"),
		ContainerPath: req.ContainerPath,
		ReadOnly:      req.ReadOnly,
	}
	if err := s.store.AttachVolume(r.Context(), av); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "attached"})
}

func (s *Server) apiDetachVolume(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	if err := s.store.DetachVolume(r.Context(), chi.URLParam(r, "appId"), chi.URLParam(r, "volumeId")); handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
}

func (s *Server) apiListAppVolumes(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	vols, err := s.store.ListAppVolumes(r.Context(), chi.URLParam(r, "appId"))
	if handleErr(w, err) {
		return
	}
	if vols == nil {
		vols = []store.AppVolume{}
	}
	writeJSON(w, http.StatusOK, vols)
}
