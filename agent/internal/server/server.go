package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"runtime"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/sync/errgroup"

	"github.com/luke/hive/agent/internal/docker"
	"github.com/luke/hive/agent/internal/hostmetrics"
	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
	"github.com/luke/hive/proto/gen/agent/v1/agentv1connect"
)

// Version is the agent's build version, reported in health responses.
var Version = "dev"

// Server implements the AgentServiceHandler interface.
type Server struct {
	agentv1connect.UnimplementedAgentServiceHandler

	docker    docker.Operations
	collector *hostmetrics.Collector
	executor  *hostmetrics.Executor
	metrics   *Metrics
}

// New creates a new agent server.
func New(dockerOps docker.Operations, collector *hostmetrics.Collector, executor *hostmetrics.Executor, m *Metrics) *Server {
	return &Server{
		docker:    dockerOps,
		collector: collector,
		executor:  executor,
		metrics:   m,
	}
}

// Health returns agent health information.
func (s *Server) Health(ctx context.Context, req *connect.Request[agentv1.HealthRequest]) (*connect.Response[agentv1.HealthResponse], error) {
	s.metrics.HealthCheckTotal.Inc()

	hostname, _ := os.Hostname()

	resp := &agentv1.HealthResponse{
		AgentVersion: Version,
		Hostname:     hostname,
		CpuCount:     int32(min(runtime.NumCPU(), math.MaxInt32)), //nolint:gosec // clamped to MaxInt32; CPU count cannot overflow
	}

	// Docker info
	if info, err := s.docker.Info(ctx); err == nil {
		resp.DockerVersion = info.ServerVersion
		resp.NodeId = info.Swarm.NodeID
	}

	// Memory from /proc/meminfo
	if m := s.collector.Metrics(); m != nil {
		resp.MemoryTotal = m.MemoryTotal
		resp.MemoryUsed = m.MemoryUsed
	}

	// Disk
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		resp.DiskTotal = stat.Blocks * uint64(stat.Bsize)                  //nolint:gosec // Bsize is a non-negative filesystem block size
		resp.DiskUsed = resp.DiskTotal - (stat.Bfree * uint64(stat.Bsize)) //nolint:gosec // Bsize is a non-negative filesystem block size
	}

	return connect.NewResponse(resp), nil
}

// GetContainerStats returns stats for the requested containers.
func (s *Server) GetContainerStats(ctx context.Context, req *connect.Request[agentv1.StatsRequest]) (*connect.Response[agentv1.StatsResponse], error) {
	start := time.Now()
	defer func() {
		s.metrics.StatsRequestDuration.Observe(time.Since(start).Seconds())
	}()
	s.metrics.StatsRequestTotal.Inc()

	stats, err := s.docker.ContainerStats(ctx, req.Msg.ContainerIds)
	if err != nil {
		s.metrics.DockerAPIErrors.WithLabelValues("container_stats").Inc()
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	items := make([]*agentv1.ContainerStats, len(stats))
	for i, st := range stats {
		items[i] = &agentv1.ContainerStats{
			ContainerId: st.ContainerID,
			Name:        st.Name,
			CpuPercent:  st.CPUPercent,
			MemoryUsage: st.MemoryUsage,
			MemoryLimit: st.MemoryLimit,
			NetworkRx:   st.NetworkRx,
			NetworkTx:   st.NetworkTx,
			BlockRead:   st.BlockRead,
			BlockWrite:  st.BlockWrite,
		}
	}

	return connect.NewResponse(&agentv1.StatsResponse{Items: items}), nil
}

// StreamContainerLogs streams container logs to the client.
func (s *Server) StreamContainerLogs(ctx context.Context, req *connect.Request[agentv1.LogRequest], stream *connect.ServerStream[agentv1.LogChunk]) error {
	s.metrics.LogStreamTotal.Inc()
	s.metrics.LogStreamActive.Inc()
	defer s.metrics.LogStreamActive.Dec()

	id := req.Msg.ContainerId
	if err := docker.ValidateContainerID(id); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	tail := req.Msg.Tail
	if tail <= 0 {
		tail = 200
	}
	if tail > 10000 {
		tail = 10000
	}

	reader, err := s.docker.StreamLogs(ctx, id, req.Msg.Follow, tail, req.Msg.Timestamps)
	if err != nil {
		s.metrics.DockerAPIErrors.WithLabelValues("container_logs").Inc()
		return connect.NewError(connect.CodeInternal, err)
	}
	defer func() { _ = reader.Close() }()

	// Docker multiplexed log format: 8-byte header + payload
	// Header: [stream_type(1)][0][0][0][size(4 big-endian)]
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(reader, header)
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return nil // stream ended
		}

		streamType := header[0]
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 || size > 64*1024 {
			continue
		}

		payload := make([]byte, size)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			return nil
		}

		chunk := &agentv1.LogChunk{
			Data:     payload,
			IsStderr: streamType == 2,
		}

		if err := stream.Send(chunk); err != nil {
			return nil
		}
	}
}

// ExecStream handles bidirectional terminal exec streams.
func (s *Server) ExecStream(ctx context.Context, stream *connect.BidiStream[agentv1.ExecInput, agentv1.ExecOutput]) error {
	s.metrics.ExecStreamTotal.Inc()
	s.metrics.ExecStreamActive.Inc()
	defer s.metrics.ExecStreamActive.Dec()

	// First message must be ExecStart
	msg, err := stream.Receive()
	if err != nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("expected ExecStart as first message"))
	}
	startMsg := msg.GetStart()
	if startMsg == nil {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("first message must be ExecStart"))
	}

	if err := docker.ValidateContainerID(startMsg.ContainerId); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}

	cmd := startMsg.Command
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}
	if startMsg.Tty {
		if err := docker.ValidateShell(cmd[0]); err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	execID, err := s.docker.ExecCreate(ctx, startMsg.ContainerId, cmd, startMsg.Tty)
	if err != nil {
		s.metrics.DockerAPIErrors.WithLabelValues("exec_create").Inc()
		return connect.NewError(connect.CodeInternal, err)
	}

	hijack, err := s.docker.ExecAttach(ctx, execID, startMsg.Tty)
	if err != nil {
		s.metrics.DockerAPIErrors.WithLabelValues("exec_attach").Inc()
		return connect.NewError(connect.CodeInternal, err)
	}
	defer hijack.Close()

	// Initial resize if TTY
	if startMsg.Tty && startMsg.Rows > 0 && startMsg.Cols > 0 {
		_ = s.docker.ExecResize(ctx, execID, uint(startMsg.Rows), uint(startMsg.Cols)) //nolint:gosec // Rows and Cols are validated positive above
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	// Output goroutine: read from hijacked connection, send to stream
	g.Go(func() error {
		buf := make([]byte, 32*1024)
		for {
			select {
			case <-gctx.Done():
				return nil
			default:
			}
			n, readErr := hijack.Reader.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				sendErr := stream.Send(&agentv1.ExecOutput{
					Body: &agentv1.ExecOutput_Stdout{Stdout: data},
				})
				if sendErr != nil {
					cancel()
					return nil
				}
			}
			if readErr != nil {
				return nil
			}
		}
	})

	// Input goroutine: read from stream, write to hijacked connection
	g.Go(func() error {
		for {
			select {
			case <-gctx.Done():
				return nil
			default:
			}
			input, recvErr := stream.Receive()
			if recvErr != nil {
				cancel()
				return nil
			}

			switch body := input.Body.(type) {
			case *agentv1.ExecInput_Stdin:
				if _, err := hijack.Conn.Write(body.Stdin); err != nil {
					cancel()
					return nil
				}
			case *agentv1.ExecInput_Resize:
				if body.Resize.Rows > 0 && body.Resize.Cols > 0 {
					_ = s.docker.ExecResize(ctx, execID, uint(body.Resize.Rows), uint(body.Resize.Cols)) //nolint:gosec // Rows and Cols are validated positive above
				}
			}
		}
	})

	_ = g.Wait()

	// Get exit code
	inspect, err := s.docker.ExecInspect(context.Background(), execID)
	if err == nil {
		_ = stream.Send(&agentv1.ExecOutput{
			Body: &agentv1.ExecOutput_ExitCode{ExitCode: int32(inspect.ExitCode)}, //nolint:gosec // Docker exit codes are uint8-range values
		})
	}

	return nil
}

// GetHostMetrics returns cached host metrics.
func (s *Server) GetHostMetrics(ctx context.Context, req *connect.Request[agentv1.HostMetricsRequest]) (*connect.Response[agentv1.HostMetricsResponse], error) {
	m := s.collector.Metrics()
	if m == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("metrics not yet collected"))
	}
	return connect.NewResponse(m), nil
}

// StreamHostMetrics streams host metrics at the requested interval.
func (s *Server) StreamHostMetrics(ctx context.Context, req *connect.Request[agentv1.HostMetricsStreamRequest], stream *connect.ServerStream[agentv1.HostMetricsResponse]) error {
	interval := req.Msg.IntervalSeconds
	if interval <= 0 {
		interval = 15
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	// Send initial metrics immediately
	if m := s.collector.Metrics(); m != nil {
		if err := stream.Send(m); err != nil {
			return nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m := s.collector.Metrics()
			if m == nil {
				continue
			}
			if err := stream.Send(m); err != nil {
				return nil
			}
		}
	}
}

// GetPackageStatus returns cached package status.
func (s *Server) GetPackageStatus(ctx context.Context, req *connect.Request[agentv1.PackageStatusRequest]) (*connect.Response[agentv1.PackageStatusResponse], error) {
	status := s.collector.PackageStatus()
	if status == nil {
		// Force initial collection
		s.collector.RefreshPackageStatus(ctx)
		status = s.collector.PackageStatus()
	}
	if status == nil {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("package status not available"))
	}
	return connect.NewResponse(status), nil
}

// HostExec executes a host operation.
func (s *Server) HostExec(ctx context.Context, req *connect.Request[agentv1.HostOperationRequest]) (*connect.Response[agentv1.HostOperationResponse], error) {
	if !s.collector.HostMgmtEnabled() {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("host management is not enabled"))
	}

	s.metrics.HostExecTotal.WithLabelValues(req.Msg.Operation.String(), "started").Inc()

	resp, err := s.executor.Execute(ctx, req.Msg)
	if err != nil {
		s.metrics.HostExecTotal.WithLabelValues(req.Msg.Operation.String(), "error").Inc()
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	result := "success"
	if resp.ExitCode != 0 {
		result = "failed"
	}
	s.metrics.HostExecTotal.WithLabelValues(req.Msg.Operation.String(), result).Inc()

	log.Printf("host exec: op=%s exit=%d duration=%dms", req.Msg.Operation, resp.ExitCode, resp.DurationMs)

	return connect.NewResponse(resp), nil
}
