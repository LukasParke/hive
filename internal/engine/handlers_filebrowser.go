package engine

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/go-chi/chi/v5"
)

type fileEntry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	Mode    string `json:"mode"`
	ModTime string `json:"mod_time"`
	Owner   string `json:"owner"`
}

func (s *Server) apiListFiles(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	var body struct {
		ContainerID string `json:"container_id"`
		Path        string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.Path = "/"
	}
	if body.Path == "" {
		body.Path = "/"
	}

	containerID := body.ContainerID
	if containerID == "" {
		cid, findErr := s.findRunningContainer(r.Context(), app.Name)
		if findErr != nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "no running container", nil)
			return
		}
		containerID = cid
	}

	cmd := fmt.Sprintf("ls -la --time-style=long-iso %s 2>/dev/null || ls -la %s", body.Path, body.Path)
	exec, err := s.sc.Docker().ContainerExecCreate(r.Context(), containerID, container.ExecOptions{
		Cmd:          []string{"/bin/sh", "-c", cmd},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	resp, err := s.sc.Docker().ContainerExecAttach(r.Context(), exec.ID, container.ExecAttachOptions{})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	defer resp.Close()

	output, _ := io.ReadAll(resp.Reader)
	entries := parseLsOutput(string(output))

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    body.Path,
		"entries": entries,
	})
}

func parseLsOutput(output string) []fileEntry {
	var entries []fileEntry
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		name := strings.Join(fields[7:], " ")
		if name == "." || name == ".." {
			continue
		}
		// Strip symlink targets
		if idx := strings.Index(name, " -> "); idx >= 0 {
			name = name[:idx]
		}
		entries = append(entries, fileEntry{
			Name:    name,
			IsDir:   fields[0][0] == 'd',
			Mode:    fields[0],
			Owner:   fields[2],
			ModTime: fields[5] + " " + fields[6],
		})
	}
	return entries
}

func (s *Server) apiDownloadFile(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	filePath := r.URL.Query().Get("path")
	containerID := r.URL.Query().Get("container")
	if filePath == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "path required", nil)
		return
	}

	if containerID == "" {
		cid, findErr := s.findRunningContainer(r.Context(), app.Name)
		if findErr != nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "no running container", nil)
			return
		}
		containerID = cid
	}

	reader, _, err := s.sc.Docker().CopyFromContainer(r.Context(), containerID, filePath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	defer reader.Close()

	tr := tar.NewReader(reader)
	header, err := tr.Next()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to read archive", nil)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(header.Name)))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, tr)
}

func (s *Server) apiUploadFile(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid form", nil)
		return
	}

	targetPath := r.FormValue("path")
	containerID := r.FormValue("container")
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "file required", nil)
		return
	}
	defer file.Close()

	if containerID == "" {
		cid, findErr := s.findRunningContainer(r.Context(), app.Name)
		if findErr != nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "no running container", nil)
			return
		}
		containerID = cid
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content, _ := io.ReadAll(file)
	_ = tw.WriteHeader(&tar.Header{
		Name: header.Filename,
		Size: int64(len(content)),
		Mode: 0644,
	})
	_, _ = tw.Write(content)
	_ = tw.Close()

	if targetPath == "" {
		targetPath = "/"
	}

	err = s.sc.Docker().CopyToContainer(r.Context(), containerID, targetPath, &buf, container.CopyToContainerOptions{})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded"})
}

func (s *Server) apiViewFile(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	filePath := r.URL.Query().Get("path")
	containerID := r.URL.Query().Get("container")
	if filePath == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "path required", nil)
		return
	}

	if containerID == "" {
		cid, findErr := s.findRunningContainer(r.Context(), app.Name)
		if findErr != nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "no running container", nil)
			return
		}
		containerID = cid
	}

	reader, _, err := s.sc.Docker().CopyFromContainer(r.Context(), containerID, filePath)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	defer reader.Close()

	tr := tar.NewReader(reader)
	_, err = tr.Next()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to read", nil)
		return
	}

	content, _ := io.ReadAll(io.LimitReader(tr, 1<<20))
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    filePath,
		"content": string(content),
		"size":    len(content),
	})
}
