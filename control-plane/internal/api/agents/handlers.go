package agents

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/ca"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
	"github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
)

type Handler struct {
	Pool           *pgxpool.Pool
	Swarm          *swarmclient.Client
	AgentDialer    *agentclient.Dialer
	Authority      *ca.Authority
	BootstrapToken string
}

func NewHandler(pool *pgxpool.Pool, swarm *swarmclient.Client, agentDialer *agentclient.Dialer, authority *ca.Authority, bootstrapToken string) *Handler {
	return &Handler{
		Pool:           pool,
		Swarm:          swarm,
		AgentDialer:    agentDialer,
		Authority:      authority,
		BootstrapToken: bootstrapToken,
	}
}

func (h *Handler) ListServers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `select id::text, name, host, ssh_port, description, created_at from servers order by created_at desc`)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list servers")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name, host, description string
		var sshPort int
		var createdAt time.Time
		if scanErr := rows.Scan(&id, &name, &host, &sshPort, &description, &createdAt); scanErr == nil {
			out = append(out, map[string]any{"id": id, "name": name, "host": host, "sshPort": sshPort, "description": description, "createdAt": createdAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Host        string `json:"host"`
		SSHPort     int    `json:"sshPort"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "invalid payload")
		return
	}
	if req.Name == "" || req.Host == "" {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "name and host are required")
		return
	}
	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	var id string
	if err := h.Pool.QueryRow(r.Context(), `insert into servers(name, host, ssh_port, description) values ($1, $2, $3, $4) returning id::text`, req.Name, req.Host, req.SSHPort, req.Description).Scan(&id); err != nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "failed to create server")
		return
	}
	_, _ = h.Pool.Exec(r.Context(), `insert into request_events(category, message, payload) values ('server', 'server created', jsonb_build_object('serverId',$1::text))`, id)
	common.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (h *Handler) ClusterInfo(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.Swarm.ListNodes(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list cluster nodes")
		return
	}
	services, _ := h.Swarm.ListServices(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]any{"nodeCount": len(nodes), "serviceCount": len(services), "nodes": nodes})
}

func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	items, err := h.Swarm.ListNodes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type item struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		Status   string `json:"status"`
	}
	out := make([]item, 0, len(items))
	for _, node := range items {
		out = append(out, item{ID: node.ID, Hostname: node.Description.Hostname, Status: string(node.Status.State)})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	items, err := h.Swarm.ListServices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := make([]item, 0, len(items))
	for _, svc := range items {
		out = append(out, item{ID: svc.ID, Name: svc.Spec.Name})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) GetNodeMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	client, err := h.resolveAgentClient(r.Context(), nodeID)
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_unavailable", err.Error())
		return
	}

	resp, err := client.GetHostMetrics(r.Context(), connect.NewRequest(&agentv1.HostMetricsRequest{}))
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_error", err.Error())
		return
	}

	common.WriteJSON(w, http.StatusOK, resp.Msg)
}

func (h *Handler) GetNodePackages(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	client, err := h.resolveAgentClient(r.Context(), nodeID)
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_unavailable", err.Error())
		return
	}

	resp, err := client.GetPackageStatus(r.Context(), connect.NewRequest(&agentv1.PackageStatusRequest{}))
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_error", err.Error())
		return
	}

	common.WriteJSON(w, http.StatusOK, resp.Msg)
}

func (h *Handler) TriggerPackageCheck(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")
	client, err := h.resolveAgentClient(r.Context(), nodeID)
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_unavailable", err.Error())
		return
	}

	resp, err := client.HostExec(r.Context(), connect.NewRequest(&agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK,
	}))
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_error", err.Error())
		return
	}

	common.WriteJSON(w, http.StatusOK, resp.Msg)
}

func (h *Handler) TriggerNodeMaintenance(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "id")

	var req struct {
		Operations     []string `json:"operations"`
		RebootIfNeeded bool     `json:"reboot_if_needed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "invalid_payload", "invalid JSON body")
		return
	}

	client, err := h.resolveAgentClient(r.Context(), nodeID)
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_unavailable", err.Error())
		return
	}

	var results []map[string]any
	for _, op := range req.Operations {
		var operation agentv1.HostOperation
		switch op {
		case "security_updates":
			operation = agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY
		case "all_updates":
			operation = agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL
		case "update_check":
			operation = agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK
		default:
			continue
		}

		resp, err := client.HostExec(r.Context(), connect.NewRequest(&agentv1.HostOperationRequest{
			Operation: operation,
		}))
		if err != nil {
			results = append(results, map[string]any{
				"operation": op,
				"error":     err.Error(),
			})
			continue
		}
		results = append(results, map[string]any{
			"operation":   op,
			"exit_code":   resp.Msg.ExitCode,
			"stdout":      resp.Msg.Stdout,
			"stderr":      resp.Msg.Stderr,
			"duration_ms": resp.Msg.DurationMs,
		})
	}

	if req.RebootIfNeeded {
		pkgResp, err := client.GetPackageStatus(r.Context(), connect.NewRequest(&agentv1.PackageStatusRequest{}))
		if err == nil && pkgResp.Msg.RebootRequired {
			rebootResp, err := client.HostExec(r.Context(), connect.NewRequest(&agentv1.HostOperationRequest{
				Operation: agentv1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE,
				Params:    map[string]string{"minutes": "1"},
			}))
			if err == nil {
				results = append(results, map[string]any{
					"operation":   "reboot",
					"exit_code":   rebootResp.Msg.ExitCode,
					"stdout":      rebootResp.Msg.Stdout,
					"duration_ms": rebootResp.Msg.DurationMs,
				})
			}
		}
	}

	common.WriteJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (h *Handler) GetClusterResources(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.Swarm.ListNodes(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "swarm_error", err.Error())
		return
	}

	type nodeResources struct {
		NodeID     string  `json:"node_id"`
		Hostname   string  `json:"hostname"`
		Status     string  `json:"status"`
		CPUCores   int32   `json:"cpu_cores"`
		CPUPercent float64 `json:"cpu_percent"`
		MemTotal   uint64  `json:"memory_total"`
		MemUsed    uint64  `json:"memory_used"`
		DiskTotal  uint64  `json:"disk_total"`
		DiskUsed   uint64  `json:"disk_used"`
	}

	var items []nodeResources
	var totalCPU int32
	var totalMem, totalDisk, usedMem, usedDisk uint64

	for _, node := range nodes {
		nr := nodeResources{
			NodeID:   node.ID,
			Hostname: node.Description.Hostname,
			Status:   string(node.Status.State),
		}

		if node.Status.State == "ready" && h.AgentDialer != nil {
			addr := node.Status.Addr + ":9090"
			client := h.AgentDialer.ClientPlaintext(node.ID, addr)
			if resp, err := client.GetHostMetrics(r.Context(), connect.NewRequest(&agentv1.HostMetricsRequest{})); err == nil {
				m := resp.Msg
				nr.CPUCores = int32(len(m.CpuCores))
				nr.CPUPercent = m.CpuTotalPercent
				nr.MemTotal = m.MemoryTotal
				nr.MemUsed = m.MemoryUsed
				for _, fs := range m.Filesystems {
					if fs.MountPoint == "/" {
						nr.DiskTotal = fs.TotalBytes
						nr.DiskUsed = fs.UsedBytes
						break
					}
				}
			}
		}

		totalCPU += nr.CPUCores
		totalMem += nr.MemTotal
		usedMem += nr.MemUsed
		totalDisk += nr.DiskTotal
		usedDisk += nr.DiskUsed
		items = append(items, nr)
	}

	common.WriteJSON(w, http.StatusOK, map[string]any{
		"nodes": items,
		"cluster": map[string]any{
			"total_cpu_cores":    totalCPU,
			"total_memory_bytes": totalMem,
			"used_memory_bytes":  usedMem,
			"total_disk_bytes":   totalDisk,
			"used_disk_bytes":    usedDisk,
			"node_count":         len(nodes),
		},
	})
}

func (h *Handler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID         string `json:"nodeId"`
		BootstrapToken string `json:"bootstrapToken"`
		CSR            string `json:"csr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	if h.BootstrapToken != "" && req.BootstrapToken != h.BootstrapToken {
		http.Error(w, `{"message":"invalid bootstrap token"}`, http.StatusUnauthorized)
		return
	}
	csrBlock, _ := pem.Decode([]byte(req.CSR))
	if csrBlock == nil {
		http.Error(w, `{"message":"invalid csr"}`, http.StatusBadRequest)
		return
	}
	parsed, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	certPEM, err := h.Authority.SignAgentCSR(parsed, req.NodeID, 72*time.Hour)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{
		"cert":   string(certPEM),
		"caCert": string(h.Authority.CertPEM()),
	})
}

func (h *Handler) resolveAgentClient(ctx context.Context, nodeID string) (agentv1connect.AgentServiceClient, error) {
	if h.AgentDialer == nil {
		return nil, fmt.Errorf("agent dialer not configured")
	}
	node, err := h.Swarm.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	addr := node.Status.Addr + ":9090"
	return h.AgentDialer.ClientPlaintext(nodeID, addr), nil
}

func (h *Handler) WsTerminal(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "containerID")
	shell := r.URL.Query().Get("shell")
	if shell == "" {
		shell = "/bin/sh"
	}

	// Find which node runs this container
	nodeID, err := h.resolveContainerNode(r.Context(), containerID)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	client, err := h.resolveAgentClient(r.Context(), nodeID)
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_unavailable", err.Error())
		return
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	rows := 24
	cols := 80
	if v := r.URL.Query().Get("rows"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rows = n
		}
	}
	if v := r.URL.Query().Get("cols"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cols = n
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stream := client.ExecStream(ctx)

	// Send ExecStart
	if err := stream.Send(&agentv1.ExecInput{
		Body: &agentv1.ExecInput_Start{Start: &agentv1.ExecStart{
			ContainerId: containerID,
			Command:     []string{shell},
			Tty:         true,
			Rows:        int32(rows),
			Cols:        int32(cols),
		}},
	}); err != nil {
		_ = conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}

	// Bridge: agent output -> websocket
	go func() {
		defer cancel()
		for {
			msg, err := stream.Receive()
			if err != nil {
				return
			}
			switch body := msg.Body.(type) {
			case *agentv1.ExecOutput_Stdout:
				_ = conn.WriteMessage(websocket.BinaryMessage, body.Stdout)
			case *agentv1.ExecOutput_Stderr:
				_ = conn.WriteMessage(websocket.BinaryMessage, body.Stderr)
			case *agentv1.ExecOutput_ExitCode:
				_ = conn.WriteJSON(map[string]int32{"exitCode": body.ExitCode})
				return
			}
		}
	}()

	// Bridge: websocket -> agent input
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			cancel()
			_ = stream.CloseRequest()
			return
		}

		// Try to parse as JSON resize message
		if msgType == websocket.TextMessage {
			var resize struct {
				Type string `json:"type"`
				Rows int32  `json:"rows"`
				Cols int32  `json:"cols"`
			}
			if json.Unmarshal(data, &resize) == nil && resize.Type == "resize" {
				_ = stream.Send(&agentv1.ExecInput{
					Body: &agentv1.ExecInput_Resize{Resize: &agentv1.ResizeTerminal{
						Rows: resize.Rows,
						Cols: resize.Cols,
					}},
				})
				continue
			}
		}

		// Raw stdin
		_ = stream.Send(&agentv1.ExecInput{
			Body: &agentv1.ExecInput_Stdin{Stdin: data},
		})
	}
}

func (h *Handler) WsLogs(w http.ResponseWriter, r *http.Request) {
	containerID := chi.URLParam(r, "containerID")

	nodeID, err := h.resolveContainerNode(r.Context(), containerID)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}

	client, err := h.resolveAgentClient(r.Context(), nodeID)
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "agent_unavailable", err.Error())
		return
	}

	follow := r.URL.Query().Get("follow") == "true"
	tail := int32(200)
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = int32(n)
		}
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	stream, err := client.StreamContainerLogs(ctx, connect.NewRequest(&agentv1.LogRequest{
		ContainerId: containerID,
		Follow:      follow,
		Tail:        tail,
		Timestamps:  true,
	}))
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}

	// Detect client disconnect
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	// Forward log chunks
	for stream.Receive() {
		chunk := stream.Msg()
		if err := conn.WriteMessage(websocket.TextMessage, chunk.Data); err != nil {
			return
		}
	}
}

func (h *Handler) resolveContainerNode(ctx context.Context, containerID string) (string, error) {
	tasks, err := h.Swarm.ListAllTasks(ctx)
	if err != nil {
		return "", fmt.Errorf("list tasks: %w", err)
	}
	for _, task := range tasks {
		if task.Status.ContainerStatus != nil && strings.HasPrefix(task.Status.ContainerStatus.ContainerID, containerID) {
			return task.NodeID, nil
		}
	}
	return "", fmt.Errorf("container %s not found on any node", containerID)
}

func ptrUint64(v uint64) *uint64 {
	return &v
}
