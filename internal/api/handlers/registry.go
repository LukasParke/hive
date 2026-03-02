package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/swarm"
)

func RegistryStatus(w http.ResponseWriter, r *http.Request) {
	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer func() { _ = sc.Close() }()

	exists, _ := sc.ServiceExists(r.Context(), "hive-registry")
	status := map[string]interface{}{
		"running": exists,
	}

	if exists {
		resp, err := http.Get("http://127.0.0.1:5000/v2/_catalog")
		if err == nil {
			defer resp.Body.Close()
			var catalog struct {
				Repositories []string `json:"repositories"`
			}
			json.NewDecoder(resp.Body).Decode(&catalog)
			status["image_count"] = len(catalog.Repositories)
		}
	}

	writeJSON(w, http.StatusOK, status)
}

func RegistryImages(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get("http://127.0.0.1:5000/v2/_catalog")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registry unavailable"})
		return
	}
	defer resp.Body.Close()

	var catalog struct {
		Repositories []string `json:"repositories"`
	}
	json.NewDecoder(resp.Body).Decode(&catalog)

	type ImageInfo struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}

	var images []ImageInfo
	for _, repo := range catalog.Repositories {
		tagsResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:5000/v2/%s/tags/list", repo))
		if err != nil {
			continue
		}
		var tagList struct {
			Tags []string `json:"tags"`
		}
		json.NewDecoder(tagsResp.Body).Decode(&tagList)
		tagsResp.Body.Close()
		images = append(images, ImageInfo{Name: repo, Tags: tagList.Tags})
	}

	if images == nil {
		images = []ImageInfo{}
	}
	writeJSON(w, http.StatusOK, images)
}

func RegistryDeleteImage(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	tag := chi.URLParam(r, "tag")

	digestResp, err := http.Head(fmt.Sprintf("http://127.0.0.1:5000/v2/%s/manifests/%s", name, tag))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not get manifest"})
		return
	}
	digestResp.Body.Close()

	digest := digestResp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "digest not found"})
		return
	}

	req, _ := http.NewRequest("DELETE", fmt.Sprintf("http://127.0.0.1:5000/v2/%s/manifests/%s", name, digest), nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	delResp.Body.Close()

	writeJSON(w, http.StatusOK, map[string]string{"deleted": name + ":" + tag})
}
