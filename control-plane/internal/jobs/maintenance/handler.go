package maintenance

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"connectrpc.com/connect"

	"github.com/luke/hive/control-plane/internal/agentclient"
	"github.com/luke/hive/control-plane/internal/swarm"
	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

// NodeMaintenanceJob represents a maintenance job for a single node.
type NodeMaintenanceJob struct {
	NodeID         string   `json:"node_id"`
	Operations     []string `json:"operations"` // "security_updates", "all_updates", "update_check"
	DrainFirst     bool     `json:"drain_first"`
	UndrainAfter   bool     `json:"undrain_after"`
	RebootIfNeeded bool     `json:"reboot_if_needed"`
}

// Result holds the outcome of a maintenance run.
type Result struct {
	NodeID     string       `json:"node_id"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	Steps      []StepResult `json:"steps"`
	Success    bool         `json:"success"`
	Error      string       `json:"error,omitempty"`
}

// StepResult holds the result of a single maintenance step.
type StepResult struct {
	Operation  string `json:"operation"`
	ExitCode   int32  `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// Handler executes node maintenance jobs.
type Handler struct {
	dialer *agentclient.Dialer
	swarm  *swarm.Client
}

// NewHandler creates a new maintenance job handler.
func NewHandler(dialer *agentclient.Dialer, sw *swarm.Client) *Handler {
	return &Handler{
		dialer: dialer,
		swarm:  sw,
	}
}

// agentAddrPort is appended to the resolved agent overlay IP; overridable in
// tests so a fake agent can listen on an ephemeral port.
var agentAddrPort = ":9090"

// Execute runs a maintenance job on a single node.
func (h *Handler) Execute(ctx context.Context, job NodeMaintenanceJob) (*Result, error) {
	result := &Result{
		NodeID:    job.NodeID,
		StartedAt: time.Now(),
	}

	defer func() {
		result.FinishedAt = time.Now()
	}()

	// Resolve agent address
	addr, err := h.resolveAgentAddr(ctx, job.NodeID)
	if err != nil {
		result.Error = fmt.Sprintf("resolve agent address: %v", err)
		return result, err
	}
	client := h.dialer.Client(job.NodeID, addr)

	// Pre-flight: check node health
	_, err = client.Health(ctx, connect.NewRequest(&agentv1.HealthRequest{}))
	if err != nil {
		result.Error = fmt.Sprintf("node health check failed: %v", err)
		return result, err
	}

	// Execute each operation
	for _, op := range job.Operations {
		var operation agentv1.HostOperation
		switch op {
		case "security_updates":
			operation = agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_SECURITY
		case "all_updates":
			operation = agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPGRADE_ALL
		case "update_check":
			operation = agentv1.HostOperation_HOST_OPERATION_PACKAGE_UPDATE_CHECK
		default:
			result.Steps = append(result.Steps, StepResult{
				Operation: op,
				Error:     "unknown operation",
			})
			continue
		}

		log.Printf("maintenance: node %s, executing %s", job.NodeID, op)

		resp, err := client.HostExec(ctx, connect.NewRequest(&agentv1.HostOperationRequest{
			Operation: operation,
		}))
		if err != nil {
			step := StepResult{
				Operation: op,
				Error:     err.Error(),
			}
			result.Steps = append(result.Steps, step)
			result.Error = fmt.Sprintf("operation %s failed: %v", op, err)
			return result, err
		}

		result.Steps = append(result.Steps, StepResult{
			Operation:  op,
			ExitCode:   resp.Msg.ExitCode,
			Stdout:     resp.Msg.Stdout,
			Stderr:     resp.Msg.Stderr,
			DurationMs: resp.Msg.DurationMs,
		})
	}

	// Reboot if needed
	if job.RebootIfNeeded {
		pkgResp, err := client.GetPackageStatus(ctx, connect.NewRequest(&agentv1.PackageStatusRequest{}))
		if err == nil && pkgResp.Msg.RebootRequired {
			log.Printf("maintenance: node %s, scheduling reboot", job.NodeID)

			rebootResp, err := client.HostExec(ctx, connect.NewRequest(&agentv1.HostOperationRequest{
				Operation: agentv1.HostOperation_HOST_OPERATION_REBOOT_SCHEDULE,
				Params:    map[string]string{"minutes": "1"},
			}))
			if err != nil {
				result.Steps = append(result.Steps, StepResult{
					Operation: "reboot",
					Error:     err.Error(),
				})
			} else {
				result.Steps = append(result.Steps, StepResult{
					Operation:  "reboot",
					ExitCode:   rebootResp.Msg.ExitCode,
					Stdout:     rebootResp.Msg.Stdout,
					DurationMs: rebootResp.Msg.DurationMs,
				})
			}
		}
	}

	// Post-flight health check
	_, err = client.Health(ctx, connect.NewRequest(&agentv1.HealthRequest{}))
	if err != nil {
		result.Error = fmt.Sprintf("post-maintenance health check failed: %v", err)
		return result, nil // still return result, not error
	}

	result.Success = true
	return result, nil
}

// ExecuteRolling runs maintenance on all nodes sequentially.
// Workers first, then managers, sorted by fewest tasks.
func (h *Handler) resolveAgentAddr(ctx context.Context, nodeID string) (string, error) {
	ip, err := h.swarm.ServiceTaskIPOnNetwork(ctx, "hive.service", "agent", nodeID, "hive_internal")
	if err != nil {
		return "", fmt.Errorf("resolve agent address: %w", err)
	}
	return ip + agentAddrPort, nil
}

// ExecuteRolling performs rolling maintenance across cluster nodes.
func (h *Handler) ExecuteRolling(ctx context.Context, job NodeMaintenanceJob) ([]Result, error) {
	nodes, err := h.swarm.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	// Separate workers and managers, workers go first
	var workers, managers []string
	for _, n := range nodes {
		if n.Spec.Role == "manager" {
			managers = append(managers, n.ID)
		} else {
			workers = append(workers, n.ID)
		}
	}

	ordered := slices.Concat(workers, managers)
	var results []Result

	for _, nodeID := range ordered {
		nodeJob := job
		nodeJob.NodeID = nodeID

		log.Printf("maintenance: starting rolling maintenance on node %s", nodeID)
		result, err := h.Execute(ctx, nodeJob)
		if result != nil {
			results = append(results, *result)
		}
		if err != nil {
			log.Printf("maintenance: node %s failed, pausing rolling maintenance: %v", nodeID, err)
			break
		}
	}

	return results, nil
}
