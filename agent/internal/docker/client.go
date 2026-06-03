package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
	dockerclient "github.com/docker/docker/client"
)

var containerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

var allowedShells = map[string]bool{
	"/bin/bash": true,
	"/bin/sh":   true,
	"/bin/zsh":  true,
	"/bin/ash":  true,
}

// DockerOperations defines the interface for Docker interactions (enables mock testing).
type DockerOperations interface {
	Info(ctx context.Context) (system.Info, error)
	ContainerStats(ctx context.Context, ids []string) ([]*ContainerStat, error)
	StreamLogs(ctx context.Context, id string, follow bool, tail int32, timestamps bool) (io.ReadCloser, error)
	ExecCreate(ctx context.Context, id string, cmd []string, tty bool) (string, error)
	ExecAttach(ctx context.Context, execID string, tty bool) (types.HijackedResponse, error)
	ExecResize(ctx context.Context, execID string, rows, cols uint) error
	ExecInspect(ctx context.Context, execID string) (container.ExecInspect, error)
	ListContainers(ctx context.Context) ([]container.Summary, error)
	Close() error
}

// ContainerStat holds computed stats for a single container.
type ContainerStat struct {
	ContainerID string
	Name        string
	CPUPercent  float64
	MemoryUsage uint64
	MemoryLimit uint64
	NetworkRx   uint64
	NetworkTx   uint64
	BlockRead   uint64
	BlockWrite  uint64
}

// Client implements DockerOperations using the Docker SDK.
type Client struct {
	raw *dockerclient.Client
}

// NewClient creates a new Docker client.
func NewClient(host string) (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(host),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Client{raw: cli}, nil
}

func (c *Client) Close() error {
	return c.raw.Close()
}

// ValidateContainerID checks that an ID matches the expected pattern.
func ValidateContainerID(id string) error {
	if id == "" {
		return fmt.Errorf("container ID is empty")
	}
	if !containerIDPattern.MatchString(id) {
		return fmt.Errorf("invalid container ID: %q", id)
	}
	return nil
}

// ValidateShell checks that a shell command is in the allowlist.
func ValidateShell(shell string) error {
	if !allowedShells[shell] {
		return fmt.Errorf("shell not allowed: %q (allowed: %s)", shell, strings.Join(allowedShellsList(), ", "))
	}
	return nil
}

func allowedShellsList() []string {
	out := make([]string, 0, len(allowedShells))
	for s := range allowedShells {
		out = append(out, s)
	}
	return out
}

func (c *Client) Info(ctx context.Context) (system.Info, error) {
	return c.raw.Info(ctx)
}

func (c *Client) ListContainers(ctx context.Context) ([]container.Summary, error) {
	return c.raw.ContainerList(ctx, container.ListOptions{})
}

// ContainerStats retrieves stats for the given container IDs.
// If ids is empty, stats for all running containers are returned.
func (c *Client) ContainerStats(ctx context.Context, ids []string) ([]*ContainerStat, error) {
	if len(ids) == 0 {
		containers, err := c.ListContainers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list containers: %w", err)
		}
		for _, ct := range containers {
			ids = append(ids, ct.ID)
		}
	}

	stats := make([]*ContainerStat, 0, len(ids))
	for _, id := range ids {
		if err := ValidateContainerID(id); err != nil {
			return nil, err
		}
		s, err := c.containerStatsOneShot(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("stats for %s: %w", id, err)
		}
		stats = append(stats, s)
	}
	return stats, nil
}

func (c *Client) containerStatsOneShot(ctx context.Context, id string) (*ContainerStat, error) {
	resp, err := c.raw.ContainerStatsOneShot(ctx, id)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var v struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		CPU  struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
			Online uint64 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPU struct {
			Usage struct {
				Total uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			System uint64 `json:"system_cpu_usage"`
		} `json:"precpu_stats"`
		Memory struct {
			Usage uint64 `json:"usage"`
			Limit uint64 `json:"limit"`
		} `json:"memory_stats"`
		Networks map[string]struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		} `json:"networks"`
		BlkIO struct {
			Service []struct {
				Op    string `json:"op"`
				Value uint64 `json:"value"`
			} `json:"io_service_bytes_recursive"`
		} `json:"blkio_stats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("decode stats: %w", err)
	}

	cpuPercent := calculateCPUPercent(
		v.PreCPU.Usage.Total, v.CPU.Usage.Total,
		v.PreCPU.System, v.CPU.System,
		v.CPU.Online,
	)

	var netRx, netTx uint64
	for _, iface := range v.Networks {
		netRx += iface.RxBytes
		netTx += iface.TxBytes
	}

	var blockRead, blockWrite uint64
	for _, entry := range v.BlkIO.Service {
		switch strings.ToLower(entry.Op) {
		case "read":
			blockRead += entry.Value
		case "write":
			blockWrite += entry.Value
		}
	}

	name := strings.TrimPrefix(v.Name, "/")

	return &ContainerStat{
		ContainerID: id,
		Name:        name,
		CPUPercent:  cpuPercent,
		MemoryUsage: v.Memory.Usage,
		MemoryLimit: v.Memory.Limit,
		NetworkRx:   netRx,
		NetworkTx:   netTx,
		BlockRead:   blockRead,
		BlockWrite:  blockWrite,
	}, nil
}

// calculateCPUPercent computes CPU usage percentage using the delta formula.
func calculateCPUPercent(preCPU, curCPU, preSystem, curSystem, onlineCPUs uint64) float64 {
	cpuDelta := float64(curCPU - preCPU)
	systemDelta := float64(curSystem - preSystem)
	if systemDelta <= 0 || cpuDelta < 0 {
		return 0
	}
	return (cpuDelta / systemDelta) * float64(onlineCPUs) * 100.0
}

func (c *Client) StreamLogs(ctx context.Context, id string, follow bool, tail int32, timestamps bool) (io.ReadCloser, error) {
	if err := ValidateContainerID(id); err != nil {
		return nil, err
	}
	tailStr := "200"
	if tail > 0 {
		if tail > 10000 {
			tail = 10000
		}
		tailStr = fmt.Sprintf("%d", tail)
	}
	return c.raw.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tailStr,
		Timestamps: timestamps,
	})
}

func (c *Client) ExecCreate(ctx context.Context, id string, cmd []string, tty bool) (string, error) {
	if err := ValidateContainerID(id); err != nil {
		return "", err
	}
	resp, err := c.raw.ContainerExecCreate(ctx, id, container.ExecOptions{
		Cmd:          cmd,
		Tty:          tty,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("exec create: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) ExecAttach(ctx context.Context, execID string, tty bool) (types.HijackedResponse, error) {
	return c.raw.ContainerExecAttach(ctx, execID, container.ExecAttachOptions{
		Tty: tty,
	})
}

func (c *Client) ExecResize(ctx context.Context, execID string, rows, cols uint) error {
	return c.raw.ContainerExecResize(ctx, execID, container.ResizeOptions{
		Height: rows,
		Width:  cols,
	})
}

func (c *Client) ExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	resp, err := c.raw.ContainerExecInspect(ctx, execID)
	if err != nil {
		return container.ExecInspect{}, fmt.Errorf("exec inspect: %w", err)
	}
	return resp, nil
}
