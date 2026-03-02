package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
)

func CreateVolume(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")

	var body struct {
		Name          string `json:"name"`
		MountType     string `json:"mount_type"`
		RemoteHost    string `json:"remote_host"`
		RemotePath    string `json:"remote_path"`
		MountOptions  string `json:"mount_options"`
		Username      string `json:"username"`
		Password      string `json:"password"`
		StorageHostID string `json:"storage_host_id"`
		LocalPath     string `json:"local_path"`
		CephPool      string `json:"ceph_pool"`
		CephImage     string `json:"ceph_image"`
		CephFSName    string `json:"ceph_fs_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if body.MountType == "" {
		body.MountType = "volume"
	}

	s := storeFromRequest(r)

	// If a storage host is specified, auto-populate remote fields from its defaults
	if body.StorageHostID != "" {
		host, err := s.GetStorageHost(r.Context(), body.StorageHostID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "storage host not found"})
			return
		}
		if body.RemoteHost == "" {
			body.RemoteHost = host.Address
		}
		if body.RemotePath == "" && body.LocalPath == "" {
			body.RemotePath = host.DefaultExportPath + "/" + body.Name
			body.LocalPath = host.DefaultExportPath + "/" + body.Name
		}
		if body.MountType == "volume" {
			body.MountType = host.DefaultMountType
		}
		if body.MountOptions == "" {
			body.MountOptions = host.MountOptionsDefault
		}

		// For Hive-managed Ceph clusters, auto-populate pool/CephFS defaults
		if host.Type == "ceph" {
			clusters, _ := s.ListCephClusters(r.Context())
			for _, c := range clusters {
				if c.StorageHostID == host.ID {
					if body.CephPool == "" {
						body.CephPool = "hive-rbd"
					}
					if body.CephFSName == "" {
						body.CephFSName = "hive-fs"
					}
					break
				}
			}
		}
	}

	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker client: " + err.Error()})
		return
	}
	defer func() { _ = sc.Close() }()

	labels := map[string]string{
		"hive.project_id": projectID,
	}

	var driverOpts map[string]string
	driver := "local"

	switch body.MountType {
	case "nfs":
		if body.RemoteHost == "" || body.RemotePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "remote_host and remote_path are required for NFS"})
			return
		}
		_, err = sc.CreateNFSVolume(r.Context(), body.Name, body.RemoteHost, body.RemotePath, body.MountOptions, labels)
	case "cifs":
		if body.RemoteHost == "" || body.RemotePath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "remote_host and remote_path are required for CIFS"})
			return
		}
		_, err = sc.CreateCIFSVolume(r.Context(), body.Name, body.RemoteHost, body.RemotePath, body.Username, body.Password, body.MountOptions, labels)
	case "cephfs":
		if body.RemoteHost == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "monitor addresses (remote_host) required for CephFS"})
			return
		}
		_, err = sc.CreateCephFSVolume(r.Context(), body.Name, body.RemoteHost, body.CephFSName, body.RemotePath, body.MountOptions, labels)
	case "ceph-rbd":
		if body.RemoteHost == "" || body.CephPool == "" || body.CephImage == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "monitor addresses, ceph_pool, and ceph_image required for RBD"})
			return
		}
		_, err = sc.CreateCephRBDVolume(r.Context(), body.Name, body.RemoteHost, body.CephPool, body.CephImage, body.MountOptions, labels)
	case "local-bind":
		// No Docker volume to create for local bind mounts; handled at deploy time
	default:
		_, err = sc.CreateVolume(r.Context(), body.Name, driver, driverOpts, labels)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	driverOptsJSON, _ := json.Marshal(driverOpts)
	labelsJSON, _ := json.Marshal(labels)

	vol := &store.Volume{
		ProjectID:     projectID,
		Name:          body.Name,
		Driver:        driver,
		DriverOpts:    driverOptsJSON,
		Labels:        labelsJSON,
		MountType:     body.MountType,
		RemoteHost:    body.RemoteHost,
		RemotePath:    body.RemotePath,
		MountOptions:  body.MountOptions,
		Scope:         "local",
		Status:        "active",
		StorageHostID: body.StorageHostID,
		LocalPath:     body.LocalPath,
		CephPool:      body.CephPool,
		CephImage:     body.CephImage,
		CephFSName:    body.CephFSName,
	}
	if err := s.CreateVolume(r.Context(), vol); err != nil {
		if body.MountType != "local-bind" {
			_ = sc.RemoveVolume(r.Context(), body.Name, true)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, vol)
}

func ListVolumes(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	vols, err := s.ListVolumes(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if vols == nil {
		vols = []store.Volume{}
	}
	writeJSON(w, http.StatusOK, vols)
}

func GetVolume(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeId")
	s := storeFromRequest(r)
	vol, err := s.GetVolume(r.Context(), volumeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "volume not found"})
		return
	}
	writeJSON(w, http.StatusOK, vol)
}

func DeleteVolume(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeId")
	s := storeFromRequest(r)

	vol, err := s.GetVolume(r.Context(), volumeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "volume not found"})
		return
	}

	sc, sErr := swarm.NewClient(nil)
	if sErr == nil {
		_ = sc.RemoveVolume(r.Context(), vol.Name, false)
		_ = sc.Close()
	}

	if err := s.DeleteVolume(r.Context(), volumeID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": volumeID})
}

func AttachVolume(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeId")
	appID := chi.URLParam(r, "appId")

	var body struct {
		ContainerPath string `json:"container_path"`
		ReadOnly      bool   `json:"read_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.ContainerPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "container_path is required"})
		return
	}

	s := storeFromRequest(r)
	av := &store.AppVolume{
		AppID:         appID,
		VolumeID:      volumeID,
		ContainerPath: body.ContainerPath,
		ReadOnly:      body.ReadOnly,
	}
	if err := s.AttachVolume(r.Context(), av); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, av)
}

func DetachVolume(w http.ResponseWriter, r *http.Request) {
	volumeID := chi.URLParam(r, "volumeId")
	appID := chi.URLParam(r, "appId")

	s := storeFromRequest(r)
	if err := s.DetachVolume(r.Context(), appID, volumeID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"detached": volumeID})
}
