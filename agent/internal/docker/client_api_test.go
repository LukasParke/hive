package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
	dockerclient "github.com/moby/moby/client"
)

// stubAPIClient implements apiClient with scripted results.
type stubAPIClient struct {
	closeErr       error
	infoResult     system.Info
	infoErr        error
	listResult     []container.Summary
	listErr        error
	statsBody      []byte
	statsErr       error
	logsResult     io.ReadCloser
	logsOpts       dockerclient.ContainerLogsOptions
	logsID         string
	logsErr        error
	execCreateID   string
	execCreateErr  error
	execCreateOpts dockerclient.ExecCreateOptions
	execAttachResp dockerclient.HijackedResponse
	execAttachErr  error
	execResizeOpts dockerclient.ExecResizeOptions
	execResizeErr  error
	execInspect    dockerclient.ExecInspectResult
	execInspectErr error
}

func (s *stubAPIClient) Close() error { return s.closeErr }

func (s *stubAPIClient) Info(ctx context.Context, opts dockerclient.InfoOptions) (dockerclient.SystemInfoResult, error) {
	return dockerclient.SystemInfoResult{Info: s.infoResult}, s.infoErr
}

func (s *stubAPIClient) ContainerList(ctx context.Context, opts dockerclient.ContainerListOptions) (dockerclient.ContainerListResult, error) {
	return dockerclient.ContainerListResult{Items: s.listResult}, s.listErr
}

func (s *stubAPIClient) ContainerStats(ctx context.Context, containerID string, opts dockerclient.ContainerStatsOptions) (dockerclient.ContainerStatsResult, error) {
	if s.statsErr != nil {
		return dockerclient.ContainerStatsResult{}, s.statsErr
	}
	return dockerclient.ContainerStatsResult{Body: io.NopCloser(bytes.NewReader(s.statsBody))}, nil
}

func (s *stubAPIClient) ContainerLogs(ctx context.Context, containerID string, opts dockerclient.ContainerLogsOptions) (dockerclient.ContainerLogsResult, error) {
	s.logsID = containerID
	s.logsOpts = opts
	if s.logsErr != nil {
		return nil, s.logsErr
	}
	if s.logsResult == nil {
		s.logsResult = io.NopCloser(strings.NewReader(""))
	}
	return s.logsResult, nil
}

func (s *stubAPIClient) ExecCreate(ctx context.Context, containerID string, opts dockerclient.ExecCreateOptions) (dockerclient.ExecCreateResult, error) {
	s.execCreateOpts = opts
	if s.execCreateErr != nil {
		return dockerclient.ExecCreateResult{}, s.execCreateErr
	}
	return dockerclient.ExecCreateResult{ID: s.execCreateID}, nil
}

func (s *stubAPIClient) ExecAttach(ctx context.Context, execID string, opts dockerclient.ExecAttachOptions) (dockerclient.ExecAttachResult, error) {
	if s.execAttachErr != nil {
		return dockerclient.ExecAttachResult{}, s.execAttachErr
	}
	return dockerclient.ExecAttachResult{HijackedResponse: s.execAttachResp}, nil
}

func (s *stubAPIClient) ExecResize(ctx context.Context, execID string, opts dockerclient.ExecResizeOptions) (dockerclient.ExecResizeResult, error) {
	s.execResizeOpts = opts
	return dockerclient.ExecResizeResult{}, s.execResizeErr
}

func (s *stubAPIClient) ExecInspect(ctx context.Context, execID string, opts dockerclient.ExecInspectOptions) (dockerclient.ExecInspectResult, error) {
	return s.execInspect, s.execInspectErr
}

// logFrame encodes one Docker multiplexed stream frame:
// [stream_type, 0, 0, 0, size(4 big-endian)] followed by the payload.
func logFrame(streamType byte, payload string) []byte {
	buf := make([]byte, 8+len(payload))
	buf[0] = streamType
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(payload))) //nolint:gosec // G115: test payload length is bounded
	copy(buf[8:], payload)
	return buf
}

func TestClientInfo(t *testing.T) {
	stub := &stubAPIClient{
		infoResult: system.Info{ServerVersion: "27.0", ID: "node-1"},
	}
	c := &Client{raw: stub}
	got, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerVersion != "27.0" || got.ID != "node-1" {
		t.Errorf("unexpected info: %+v", got)
	}

	stub.infoErr = errors.New("boom")
	if _, err := c.Info(context.Background()); err == nil {
		t.Error("expected error propagation")
	}
}

func TestClientListContainers(t *testing.T) {
	stub := &stubAPIClient{
		listResult: []container.Summary{{ID: "abc123"}},
	}
	c := &Client{raw: stub}
	got, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "abc123" {
		t.Errorf("unexpected list: %+v", got)
	}

	stub.listErr = errors.New("boom")
	if _, err := c.ListContainers(context.Background()); err == nil {
		t.Error("expected error propagation")
	}
}

func TestClientClose(t *testing.T) {
	stub := &stubAPIClient{closeErr: errors.New("already closed")}
	c := &Client{raw: stub}
	if err := c.Close(); err == nil || err.Error() != "already closed" {
		t.Errorf("expected close error propagation, got %v", err)
	}
}

func TestNewClient(t *testing.T) {
	c, err := NewClient("unix:///nonexistent/docker.sock")
	if err != nil {
		t.Fatalf("NewClient should not dial eagerly: %v", err)
	}
	defer func() { _ = c.Close() }()

	if _, err := NewClient("::bad-host::"); err == nil {
		t.Error("expected error for invalid host")
	}
}

func statsFixture(id, name string, cpu, preCPU, sys, preSys, online uint64) []byte {
	payload := map[string]any{
		"id":   id,
		"name": name,
		"cpu_stats": map[string]any{
			"cpu_usage":        map[string]any{"total_usage": cpu},
			"system_cpu_usage": sys,
			"online_cpus":      online,
		},
		"precpu_stats": map[string]any{
			"cpu_usage":        map[string]any{"total_usage": preCPU},
			"system_cpu_usage": preSys,
		},
		"memory_stats": map[string]any{"usage": 2048, "limit": 4096},
		"networks": map[string]any{
			"eth0": map[string]any{"rx_bytes": 100, "tx_bytes": 200},
			"eth1": map[string]any{"rx_bytes": 10, "tx_bytes": 20},
		},
		"blkio_stats": map[string]any{
			"io_service_bytes_recursive": []map[string]any{
				{"op": "Read", "value": 500},
				{"op": "write", "value": 700},
				{"op": "sync", "value": 9999}, // ignored op
			},
		},
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func TestContainerStatsOneShotDecode(t *testing.T) {
	stub := &stubAPIClient{}
	c := &Client{raw: stub}
	stub.statsBody = statsFixture("abc123", "/web-1", 300, 100, 1000, 0, 2)
	stats, err := c.ContainerStats(context.Background(), []string{"abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d", len(stats))
	}
	st := stats[0]
	if st.ContainerID != "abc123" {
		t.Errorf("expected id abc123, got %q", st.ContainerID)
	}
	if st.Name != "web-1" {
		t.Errorf("expected leading slash trimmed, got %q", st.Name)
	}
	// cpuDelta=200, systemDelta=1000, onlineCPUs=2 -> (200/1000)*2*100 = 40
	if st.CPUPercent != 40.0 {
		t.Errorf("expected CPU 40%%, got %f", st.CPUPercent)
	}
	if st.MemoryUsage != 2048 || st.MemoryLimit != 4096 {
		t.Errorf("unexpected memory: usage=%d limit=%d", st.MemoryUsage, st.MemoryLimit)
	}
	if st.NetworkRx != 110 || st.NetworkTx != 220 {
		t.Errorf("expected summed network counters, got rx=%d tx=%d", st.NetworkRx, st.NetworkTx)
	}
	if st.BlockRead != 500 || st.BlockWrite != 700 {
		t.Errorf("expected blkio read/write aggregation, got read=%d write=%d", st.BlockRead, st.BlockWrite)
	}
}

func TestContainerStatsAllContainers(t *testing.T) {
	stub := &stubAPIClient{
		listResult: []container.Summary{{ID: "one"}, {ID: "two"}},
	}
	c := &Client{raw: stub}
	stub.statsBody = statsFixture("x", "x", 0, 0, 0, 0, 1)

	stats, err := c.ContainerStats(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].ContainerID != "one" || stats[1].ContainerID != "two" {
		t.Errorf("expected stats for both listed containers, got %+v", stats)
	}
}

func TestContainerStatsErrors(t *testing.T) {
	c := &Client{raw: &stubAPIClient{listErr: errors.New("daemon down")}}
	if _, err := c.ContainerStats(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "list containers") {
		t.Errorf("expected wrapped list error, got %v", err)
	}

	stub := &stubAPIClient{listErr: errors.New("unused")}
	stub.statsErr = errors.New("no such container")
	c = &Client{raw: stub}
	if _, err := c.ContainerStats(context.Background(), []string{"valid"}); err == nil || !strings.Contains(err.Error(), "stats for valid") {
		t.Errorf("expected wrapped stats error, got %v", err)
	}

	// Invalid ID is rejected before any API call.
	c = &Client{raw: &stubAPIClient{}}
	if _, err := c.ContainerStats(context.Background(), []string{"bad/id"}); err == nil {
		t.Error("expected invalid ID error")
	}

	// Malformed stats payload.
	stub = &stubAPIClient{statsBody: []byte("{not json")}
	if _, err := c.ContainerStats(context.Background(), []string{"valid"}); err == nil || !strings.Contains(err.Error(), "decode stats") {
		t.Errorf("expected decode error, got %v", err)
	}
}

func TestCalculateCPUPercentTable(t *testing.T) {
	tests := []struct {
		name           string
		preCPU, curCPU uint64
		preSys, curSys uint64
		online         uint64
		want           float64
	}{
		{"zero deltas", 0, 0, 0, 0, 4, 0},
		{"zero system delta", 100, 200, 5, 5, 4, 0},
		{"single core", 0, 250, 0, 1000, 1, 25},
		{"multi core", 0, 300, 0, 1000, 2, 60},
		{"large counters", 1 << 40, 1<<40 + 500, 1 << 50, 1<<50 + 2000, 2, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCPUPercent(tt.preCPU, tt.curCPU, tt.preSys, tt.curSys, tt.online)
			if got != tt.want {
				t.Errorf("calculateCPUPercent(%d,%d,%d,%d,%d) = %f, want %f",
					tt.preCPU, tt.curCPU, tt.preSys, tt.curSys, tt.online, got, tt.want)
			}
		})
	}
}

func TestStreamLogs(t *testing.T) {
	stream := io.NopCloser(strings.NewReader(string(logFrame(1, "hi"))))

	tests := []struct {
		name       string
		id         string
		follow     bool
		tail       int32
		timestamps bool
		wantTail   string
		wantErr    bool
	}{
		{"default tail", "abc123", false, 0, false, "200", false},
		{"explicit tail", "abc123", true, 50, true, "50", false},
		{"tail clamped to max", "abc123", false, 99999, false, "10000", false},
		{"negative tail falls back", "abc123", false, -7, false, "200", false},
		{"invalid id rejected", "bad/", false, 0, false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubAPIClient{logsResult: stream}
			c := &Client{raw: stub}

			rc, err := c.StreamLogs(context.Background(), tt.id, tt.follow, tt.tail, tt.timestamps)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rc.Close() }()
			opts := stub.logsOpts
			if opts.Tail != tt.wantTail {
				t.Errorf("tail = %q, want %q", opts.Tail, tt.wantTail)
			}
			if !opts.ShowStdout || !opts.ShowStderr {
				t.Error("both stdout and stderr must be requested")
			}
			if opts.Follow != tt.follow {
				t.Errorf("follow = %v, want %v", opts.Follow, tt.follow)
			}
			if opts.Timestamps != tt.timestamps {
				t.Errorf("timestamps = %v, want %v", opts.Timestamps, tt.timestamps)
			}
			if stub.logsID != tt.id {
				t.Errorf("container id = %q, want %q", stub.logsID, tt.id)
			}
		})
	}

	// Error propagation from the daemon call.
	c := &Client{raw: &stubAPIClient{logsErr: errors.New("boom")}}
	if _, err := c.StreamLogs(context.Background(), "abc123", false, 0, false); err == nil {
		t.Error("expected logs error propagation")
	}
}

func TestExecCreate(t *testing.T) {
	stub := &stubAPIClient{execCreateID: "exec-1"}
	c := &Client{raw: stub}

	id, err := c.ExecCreate(context.Background(), "abc123", []string{"/bin/sh", "-c", "ls"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if id != "exec-1" {
		t.Errorf("id = %q, want exec-1", id)
	}
	opts := stub.execCreateOpts
	if !opts.TTY || !opts.AttachStdin || !opts.AttachStdout || !opts.AttachStderr {
		t.Errorf("expected full attach options, got %+v", opts)
	}
	if len(opts.Cmd) != 3 {
		t.Errorf("cmd not passed through: %v", opts.Cmd)
	}

	if _, err := c.ExecCreate(context.Background(), "bad id", nil, false); err == nil {
		t.Error("expected invalid ID error")
	}

	stub.execCreateErr = errors.New("container not running")
	if _, err := c.ExecCreate(context.Background(), "abc123", nil, false); err == nil || !strings.Contains(err.Error(), "exec create") {
		t.Errorf("expected wrapped create error, got %v", err)
	}
}

func TestExecAttachAndResizeAndInspect(t *testing.T) {
	stub := &stubAPIClient{}
	c := &Client{raw: stub}

	hijack, err := c.ExecAttach(context.Background(), "exec-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if hijack.Conn != nil {
		t.Error("expected scripted empty hijack")
	}

	stub.execAttachErr = errors.New("attach refused")
	if _, err := c.ExecAttach(context.Background(), "exec-1", true); err == nil {
		t.Error("expected attach error propagation")
	}

	stub.execAttachErr = nil
	stub.execResizeErr = errors.New("resize failed")
	if err := c.ExecResize(context.Background(), "exec-1", 40, 120); err == nil {
		t.Error("expected resize error propagation")
	}
	if stub.execResizeOpts.Height != 40 || stub.execResizeOpts.Width != 120 {
		t.Errorf("resize dims not passed through: %+v", stub.execResizeOpts)
	}

	stub.execResizeErr = nil
	if err := c.ExecResize(context.Background(), "exec-1", 24, 80); err != nil {
		t.Fatal(err)
	}

	stub.execInspect = dockerclient.ExecInspectResult{ExitCode: 3, Running: false}
	res, err := c.ExecInspect(context.Background(), "exec-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 {
		t.Errorf("exit code = %d, want 3", res.ExitCode)
	}

	stub.execInspectErr = errors.New("gone")
	if _, err := c.ExecInspect(context.Background(), "exec-1"); err == nil || !strings.Contains(err.Error(), "exec inspect") {
		t.Errorf("expected wrapped inspect error, got %v", err)
	}
}

var _ apiClient = (*stubAPIClient)(nil)
