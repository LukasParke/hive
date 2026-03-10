package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	swarmtypes "github.com/docker/docker/api/types/swarm"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

type execSession struct {
	execID      string
	containerID string
	cancel      context.CancelFunc
	createdAt   time.Time
}

var (
	execSessions   = make(map[string]*execSession)
	execSessionsMu sync.Mutex
)

func (s *Server) apiCreateExec(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	var body struct {
		Command     string `json:"command"`
		ContainerID string `json:"container_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.Command = ""
	}

	containerID := body.ContainerID
	if containerID == "" {
		cid, findErr := s.findRunningContainer(r.Context(), app.Name)
		if findErr != nil {
			writeAPIError(w, http.StatusNotFound, "not_found", "no running container found: "+findErr.Error(), nil)
			return
		}
		containerID = cid
	}

	cmd := []string{"/bin/sh"}
	if body.Command != "" {
		cmd = []string{"/bin/sh", "-c", body.Command}
	}

	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}

	exec, err := s.sc.Docker().ContainerExecCreate(r.Context(), containerID, execCfg)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "exec create failed: "+err.Error(), nil)
		return
	}

	execSessionsMu.Lock()
	execSessions[exec.ID] = &execSession{
		execID:      exec.ID,
		containerID: containerID,
		createdAt:   time.Now(),
	}
	execSessionsMu.Unlock()

	go func() {
		time.Sleep(30 * time.Minute)
		execSessionsMu.Lock()
		delete(execSessions, exec.ID)
		execSessionsMu.Unlock()
	}()

	writeJSON(w, http.StatusOK, map[string]string{
		"exec_id":      exec.ID,
		"container_id": containerID,
	})
}

func (s *Server) wsExec(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	execID := chi.URLParam(r, "execId")

	execSessionsMu.Lock()
	session, ok := execSessions[execID]
	execSessionsMu.Unlock()
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "exec session not found", nil)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warnw("ws exec upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	session.cancel = cancel

	resp, attachErr := s.sc.Docker().ContainerExecAttach(ctx, execID, container.ExecAttachOptions{Tty: true})
	if attachErr != nil {
		msg, _ := json.Marshal(map[string]string{"error": "exec attach failed: " + attachErr.Error()})
		_ = conn.WriteMessage(websocket.TextMessage, msg)
		return
	}
	defer resp.Close()

	done := make(chan struct{})

	// Docker -> WebSocket
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Reader.Read(buf)
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				return
			}
		}
	}()

	// WebSocket -> Docker
	go func() {
		for {
			msgType, msg, readErr := conn.ReadMessage()
			if readErr != nil {
				cancel()
				return
			}
			if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
				if len(msg) > 0 {
					// Handle resize messages
					var resize struct {
						Type string `json:"type"`
						Cols int    `json:"cols"`
						Rows int    `json:"rows"`
					}
					if json.Unmarshal(msg, &resize) == nil && resize.Type == "resize" {
						_ = s.sc.Docker().ContainerExecResize(ctx, execID, container.ResizeOptions{
							Width:  uint(resize.Cols),
							Height: uint(resize.Rows),
						})
						continue
					}
					_, _ = resp.Conn.Write(msg)
				}
			}
		}
	}()

	<-done

	execSessionsMu.Lock()
	delete(execSessions, execID)
	execSessionsMu.Unlock()
}

func (s *Server) findRunningContainer(ctx context.Context, appName string) (string, error) {
	serviceName := "hive-app-" + appName

	tasks, err := s.sc.Docker().TaskList(ctx, swarmtypes.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("service", serviceName),
			filters.Arg("desired-state", "running"),
		),
	})
	if err != nil || len(tasks) == 0 {
		tasks, err = s.sc.Docker().TaskList(ctx, swarmtypes.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", appName),
				filters.Arg("desired-state", "running"),
			),
		})
		if err != nil {
			return "", err
		}
	}

	for _, t := range tasks {
		if t.Status.State == swarmtypes.TaskStateRunning && t.Status.ContainerStatus != nil {
			return t.Status.ContainerStatus.ContainerID, nil
		}
	}

	return "", &apiError{Status: http.StatusNotFound, Message: "no running container found for service"}
}

func (s *Server) apiListContainersForApp(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	serviceName := "hive-app-" + app.Name
	tasks, _ := s.sc.Docker().TaskList(r.Context(), swarmtypes.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("service", serviceName),
			filters.Arg("desired-state", "running"),
		),
	})
	if len(tasks) == 0 {
		tasks, _ = s.sc.Docker().TaskList(r.Context(), swarmtypes.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", app.Name),
				filters.Arg("desired-state", "running"),
			),
		})
	}

	type containerInfo struct {
		ContainerID string `json:"container_id"`
		NodeID      string `json:"node_id"`
		Slot        int    `json:"slot"`
		Image       string `json:"image"`
	}

	var containers []containerInfo
	for _, t := range tasks {
		if t.Status.ContainerStatus != nil {
			containers = append(containers, containerInfo{
				ContainerID: t.Status.ContainerStatus.ContainerID,
				NodeID:      t.NodeID,
				Slot:        t.Slot,
				Image:       t.Spec.ContainerSpec.Image,
			})
		}
	}
	if containers == nil {
		containers = []containerInfo{}
	}

	writeJSON(w, http.StatusOK, containers)
}
