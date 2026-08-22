// Package nodeops implements the admin node operations from openapi.yaml:
// PUT /nodes/{id}/labels, POST /nodes/{id}/drain|promote|demote and
// DELETE /nodes/{id}.
package nodeops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	swarm "github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/api/common"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// SwarmAPI is the subset of the swarm client the node handlers need.
// *swarmclient.Client satisfies it; tests supply a fake.
type SwarmAPI interface {
	GetNode(ctx context.Context, nodeID string) (swarm.Node, error)
	UpdateNode(ctx context.Context, nodeID string, version uint64, spec swarm.NodeSpec) error
	RemoveNode(ctx context.Context, nodeID string, force bool) error
	ListNodes(ctx context.Context) ([]swarm.Node, error)
}

// Handler serves node operation endpoints.
type Handler struct {
	Pool  *pgxpool.Pool
	Swarm SwarmAPI

	// authorizeOverride replaces the org-admin RBAC gate. Nil in production;
	// tests set it to exercise handlers without a database.
	authorizeOverride func(w http.ResponseWriter, r *http.Request) bool
}

// NewHandler returns a node ops Handler backed by the given pool and swarm API.
func NewHandler(pool *pgxpool.Pool, swarm SwarmAPI) *Handler {
	return &Handler{Pool: pool, Swarm: swarm}
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.authorizeOverride != nil {
		return h.authorizeOverride(w, r)
	}
	_, ok := common.RequireOrgAccess(w, r, h.Pool, rbac.RoleOwner, rbac.RoleAdmin)
	return ok
}

// UpdateNodeLabels merges the provided labels into the node's current label
// set (existing keys are overwritten, others preserved) via UpdateNode.
func (h *Handler) UpdateNodeLabels(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	nodeID := chi.URLParam(r, "id")
	var req struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Labels == nil {
		common.WriteError(w, http.StatusBadRequest, "bad_request", "labels object is required")
		return
	}
	node, err := h.Swarm.GetNode(r.Context(), nodeID)
	if err != nil {
		writeNodeError(w, nodeID, err)
		return
	}
	spec := node.Spec
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}
	for k, v := range req.Labels {
		spec.Labels[k] = v
	}
	if err := h.Swarm.UpdateNode(r.Context(), nodeID, node.Version.Index, spec); err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", fmt.Sprintf("failed to update node %s", nodeID))
		return
	}
	updated, err := h.Swarm.GetNode(r.Context(), nodeID)
	if err != nil {
		writeNodeError(w, nodeID, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, nodeResponse(updated))
}

// DrainNode sets the node's availability to drain.
func (h *Handler) DrainNode(w http.ResponseWriter, r *http.Request) {
	h.setAvailability(w, r, swarm.NodeAvailabilityDrain)
}

// PromoteNode promotes a worker to manager.
func (h *Handler) PromoteNode(w http.ResponseWriter, r *http.Request) {
	h.setRole(w, r, swarm.NodeRoleManager)
}

// DemoteNode demotes a manager to worker.
func (h *Handler) DemoteNode(w http.ResponseWriter, r *http.Request) {
	h.setRole(w, r, swarm.NodeRoleWorker)
}

func (h *Handler) setAvailability(w http.ResponseWriter, r *http.Request, availability swarm.NodeAvailability) {
	if !h.authorize(w, r) {
		return
	}
	nodeID := chi.URLParam(r, "id")
	node, err := h.Swarm.GetNode(r.Context(), nodeID)
	if err != nil {
		writeNodeError(w, nodeID, err)
		return
	}
	spec := node.Spec
	spec.Availability = availability
	if err := h.Swarm.UpdateNode(r.Context(), nodeID, node.Version.Index, spec); err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", fmt.Sprintf("failed to update node %s", nodeID))
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) setRole(w http.ResponseWriter, r *http.Request, role swarm.NodeRole) {
	if !h.authorize(w, r) {
		return
	}
	nodeID := chi.URLParam(r, "id")
	node, err := h.Swarm.GetNode(r.Context(), nodeID)
	if err != nil {
		writeNodeError(w, nodeID, err)
		return
	}
	spec := node.Spec
	spec.Role = role
	if err := h.Swarm.UpdateNode(r.Context(), nodeID, node.Version.Index, spec); err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", fmt.Sprintf("failed to update node %s", nodeID))
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// RemoveNode removes the node from the swarm. The force query parameter is
// forwarded to the daemon (removes unreachable nodes). Removing the last
// manager is refused regardless of force.
func (h *Handler) RemoveNode(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}
	nodeID := chi.URLParam(r, "id")
	force := strings.EqualFold(r.URL.Query().Get("force"), "true")

	nodes, err := h.Swarm.ListNodes(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "runtime_error", "failed to list cluster nodes")
		return
	}
	var target *swarm.Node
	managers := 0
	for i := range nodes {
		if nodes[i].Spec.Role == swarm.NodeRoleManager {
			managers++
		}
		if nodes[i].ID == nodeID {
			target = &nodes[i]
		}
	}
	if target != nil && target.Spec.Role == swarm.NodeRoleManager && managers <= 1 {
		common.WriteError(w, http.StatusConflict, "conflict", "cannot remove the last manager node")
		return
	}
	if err := h.Swarm.RemoveNode(r.Context(), nodeID, force); err != nil {
		writeNodeError(w, nodeID, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// nodeResponse maps a swarm node to the openapi Node schema.
func nodeResponse(node swarm.Node) map[string]any {
	out := map[string]any{
		"id":       node.ID,
		"hostname": node.Description.Hostname,
		"status":   string(node.Status.State),
		"role":     string(node.Spec.Role),
	}
	if node.Spec.Availability != "" {
		out["availability"] = string(node.Spec.Availability)
	}
	if len(node.Spec.Labels) > 0 {
		out["labels"] = node.Spec.Labels
	}
	return out
}

func writeNodeError(w http.ResponseWriter, nodeID string, err error) {
	if cerrdefs.IsNotFound(err) {
		common.WriteError(w, http.StatusNotFound, "not_found", fmt.Sprintf("node %s not found", nodeID))
		return
	}
	common.WriteError(w, http.StatusBadGateway, "runtime_error", fmt.Sprintf("swarm request for node %s failed", nodeID))
}
