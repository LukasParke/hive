package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"connectrpc.com/connect"
	"github.com/moby/moby/api/types/system"
	dockerclient "github.com/moby/moby/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/luke/hive/agent/internal/hostmetrics"
	agentv1 "github.com/luke/hive/proto/gen/agent/v1"
)

// ---------------------------------------------------------------------------
// Test plumbing

// sliceConn implements connect.StreamingHandlerConn collecting every Send into
// a slice; suitable for server-streaming handlers.
type sliceConn struct {
	mu      sync.Mutex
	msgs    []any
	sendErr error
}

func (c *sliceConn) record(msg any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, msg)
}

func (c *sliceConn) Sent() []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]any(nil), c.msgs...)
}

func (c *sliceConn) Spec() connect.Spec           { return connect.Spec{} }
func (c *sliceConn) Peer() connect.Peer           { return connect.Peer{} }
func (c *sliceConn) RequestHeader() http.Header   { return http.Header{} }
func (c *sliceConn) ResponseHeader() http.Header  { return http.Header{} }
func (c *sliceConn) ResponseTrailer() http.Header { return http.Header{} }

func (c *sliceConn) Receive(any) error { return io.EOF }

func (c *sliceConn) Send(msg any) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.record(msg)
	return nil
}

// bidiConn implements connect.StreamingHandlerConn with channels so tests can
// script the client side of a bidirectional stream.
type bidiConn struct {
	recv    chan *agentv1.ExecInput
	sent    chan any
	sendErr error
}

func (c *bidiConn) Spec() connect.Spec           { return connect.Spec{} }
func (c *bidiConn) Peer() connect.Peer           { return connect.Peer{} }
func (c *bidiConn) RequestHeader() http.Header   { return http.Header{} }
func (c *bidiConn) ResponseHeader() http.Header  { return http.Header{} }
func (c *bidiConn) ResponseTrailer() http.Header { return http.Header{} }

func (c *bidiConn) Receive(msg any) error {
	m, ok := <-c.recv
	if !ok {
		return io.EOF
	}
	dst, ok := msg.(*agentv1.ExecInput)
	if !ok {
		return io.ErrUnexpectedEOF
	}
	dst.Body = m.Body
	return nil
}

func (c *bidiConn) Send(msg any) error {
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent <- msg
	return nil
}

// setConn writes the unexported conn field of a connect stream struct.
// Test-only glue; connect has no exported constructor for these types.
func setConn(dst any, c connect.StreamingHandlerConn) {
	v := reflect.ValueOf(dst).Elem().FieldByName("conn")
	//nolint:gosec // G103: test-only glue writing an unexported field
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(c))
}

func newBidiStream(c connect.StreamingHandlerConn) *connect.BidiStream[agentv1.ExecInput, agentv1.ExecOutput] {
	s := &connect.BidiStream[agentv1.ExecInput, agentv1.ExecOutput]{}
	setConn(s, c)
	return s
}

func newLogServerStream(c connect.StreamingHandlerConn) *connect.ServerStream[agentv1.LogChunk] {
	s := &connect.ServerStream[agentv1.LogChunk]{}
	setConn(s, c)
	return s
}

func newHostMetricsServerStream(c connect.StreamingHandlerConn) *connect.ServerStream[agentv1.HostMetricsResponse] {
	s := &connect.ServerStream[agentv1.HostMetricsResponse]{}
	setConn(s, c)
	return s
}

func newTestServer(mock *mockDocker, collector *hostmetrics.Collector) *Server {
	executor := hostmetrics.NewExecutor("", collector != nil && collector.HostMgmtEnabled())
	return New(mock, collector, executor, NewMetricsWithRegistry(prometheus.NewRegistry()))
}

func gaugeOf(g prometheus.Gauge) float64 {
	return testutil.ToFloat64(g)
}

// logFrame encodes one Docker multiplexed log frame.
func logFrame(streamType byte, payload string) []byte {
	buf := make([]byte, 8+len(payload))
	buf[0] = streamType
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(payload))) //nolint:gosec // G115: bounded test payload
	copy(buf[8:], payload)
	return buf
}

func concatFrames(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

// ---------------------------------------------------------------------------
// Health

func TestHealthFullPayload(t *testing.T) {
	mock := &mockDocker{
		infoResult: systemInfoFixture("27.0", "node-abc"),
	}
	collector := hostmetrics.NewCollector("", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(ctx)
	waitFor(t, func() bool { return collector.Metrics() != nil })

	srv := newTestServer(mock, collector)
	resp, err := srv.Health(context.Background(), connect.NewRequest(&agentv1.HealthRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.DockerVersion != "27.0" {
		t.Errorf("DockerVersion = %q", resp.Msg.DockerVersion)
	}
	if resp.Msg.NodeId != "node-abc" {
		t.Errorf("NodeId = %q", resp.Msg.NodeId)
	}
	if resp.Msg.MemoryTotal == 0 || resp.Msg.MemoryUsed == 0 {
		t.Errorf("memory fields not populated: %+v", resp.Msg)
	}
	if resp.Msg.DiskTotal == 0 || resp.Msg.DiskUsed == 0 {
		t.Errorf("disk fields not populated: total=%d used=%d", resp.Msg.DiskTotal, resp.Msg.DiskUsed)
	}
	if resp.Msg.CpuCount <= 0 {
		t.Error("CpuCount should be positive")
	}

	// Docker info failure must not fail the RPC nor zero other fields.
	mock.infoErr = context.DeadlineExceeded
	resp, err = srv.Health(context.Background(), connect.NewRequest(&agentv1.HealthRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.DockerVersion != "" {
		t.Errorf("expected empty docker version on info error, got %q", resp.Msg.DockerVersion)
	}
	if resp.Msg.MemoryTotal == 0 {
		t.Error("memory should still be populated after docker info failure")
	}
}

// ---------------------------------------------------------------------------
// GetContainerStats / GetHostMetrics / GetPackageStatus

func TestGetContainerStatsErrorMapping(t *testing.T) {
	mock := &mockDocker{statsErr: context.DeadlineExceeded}
	srv := newTestServer(mock, hostmetrics.NewCollector("", false))
	_, err := srv.GetContainerStats(context.Background(), connect.NewRequest(&agentv1.StatsRequest{
		ContainerIds: []string{"abc"},
	}))
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("code = %v, want Internal", connect.CodeOf(err))
	}
}

func TestGetHostMetricsBranches(t *testing.T) {
	collector := hostmetrics.NewCollector("", false)
	srv := newTestServer(&mockDocker{}, collector)

	_, err := srv.GetHostMetrics(context.Background(), connect.NewRequest(&agentv1.HostMetricsRequest{}))
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("empty cache: code = %v, want Unavailable", connect.CodeOf(err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(ctx)
	waitFor(t, func() bool { return collector.Metrics() != nil })

	resp, err := srv.GetHostMetrics(context.Background(), connect.NewRequest(&agentv1.HostMetricsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.CollectedAt == 0 {
		t.Error("expected populated metrics response")
	}
}

func TestGetPackageStatusPopulatesCache(t *testing.T) {
	collector := hostmetrics.NewCollector("", false)
	srv := newTestServer(&mockDocker{}, collector)

	resp, err := srv.GetPackageStatus(context.Background(), connect.NewRequest(&agentv1.PackageStatusRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Msg.PackageManager == "" {
		t.Error("expected package manager to be detected")
	}
	if collector.PackageStatus() == nil {
		t.Error("expected cache to be filled by the handler")
	}
}

// ---------------------------------------------------------------------------
// StreamContainerLogs

func TestStreamContainerLogsValidationAndErrors(t *testing.T) {
	t.Run("invalid container id", func(t *testing.T) {
		srv := newTestServer(&mockDocker{}, hostmetrics.NewCollector("", false))
		err := srv.StreamContainerLogs(context.Background(),
			connect.NewRequest(&agentv1.LogRequest{ContainerId: "bad/id"}),
			newLogServerStream(&sliceConn{}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("code = %v, want InvalidArgument", connect.CodeOf(err))
		}
	})

	t.Run("docker error maps to internal", func(t *testing.T) {
		mock := &mockDocker{streamLogsErr: context.DeadlineExceeded}
		srv := newTestServer(mock, hostmetrics.NewCollector("", false))
		err := srv.StreamContainerLogs(context.Background(),
			connect.NewRequest(&agentv1.LogRequest{ContainerId: "abc123"}),
			newLogServerStream(&sliceConn{}))
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Fatalf("code = %v, want Internal", connect.CodeOf(err))
		}
	})
}

func TestStreamContainerLogsHappyPath(t *testing.T) {
	frames := concatFrames(
		logFrame(1, "hello\n"),
		logFrame(2, "boo\n"),
		logFrame(2, ""),
		logFrame(1, "again"),
	)

	var gotID string
	var gotFollow, gotTimestamps bool
	var gotTail int32
	mock := &mockDocker{
		streamLogsFn: func(id string, follow bool, tail int32, timestamps bool) (io.ReadCloser, error) {
			gotID, gotFollow, gotTail, gotTimestamps = id, follow, tail, timestamps
			return io.NopCloser(bytes.NewReader(frames)), nil
		},
	}
	srv := newTestServer(mock, hostmetrics.NewCollector("", false))

	sc := &sliceConn{}
	req := connect.NewRequest(&agentv1.LogRequest{
		ContainerId: "abc123",
		Tail:        55,
		Follow:      true,
		Timestamps:  true,
	})
	if err := srv.StreamContainerLogs(context.Background(), req, newLogServerStream(sc)); err != nil {
		t.Fatal(err)
	}

	if gotID != "abc123" || !gotFollow || !gotTimestamps {
		t.Errorf("args id=%q follow=%v ts=%v", gotID, gotFollow, gotTimestamps)
	}
	if gotTail != 55 {
		t.Errorf("tail = %d, want passthrough of 55", gotTail)
	}

	var chunks []*agentv1.LogChunk
	for _, m := range sc.Sent() {
		chunks = append(chunks, m.(*agentv1.LogChunk))
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3 (zero-size frame skipped)", len(chunks))
	}
	if string(chunks[0].Data) != "hello\n" || chunks[0].IsStderr {
		t.Errorf("chunk0 = data=%q stderr=%v", chunks[0].Data, chunks[0].IsStderr)
	}
	if string(chunks[1].Data) != "boo\n" || !chunks[1].IsStderr {
		t.Errorf("chunk1 = data=%q stderr=%v", chunks[1].Data, chunks[1].IsStderr)
	}
	if string(chunks[2].Data) != "again" || chunks[2].IsStderr {
		t.Errorf("chunk2 = data=%q stderr=%v", chunks[2].Data, chunks[2].IsStderr)
	}
}

func TestStreamContainerLogsSkipsOversizedFrames(t *testing.T) {
	oversized := make([]byte, 8)
	binary.BigEndian.PutUint32(oversized[4:8], 64*1024+1) // too big -> skipped
	frames := concatFrames(oversized, logFrame(1, "after"))

	mock := &mockDocker{
		streamLogsFn: func(string, bool, int32, bool) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(frames)), nil
		},
	}
	srv := newTestServer(mock, hostmetrics.NewCollector("", false))
	sc := &sliceConn{}
	if err := srv.StreamContainerLogs(context.Background(),
		connect.NewRequest(&agentv1.LogRequest{ContainerId: "abc"}), newLogServerStream(sc)); err != nil {
		t.Fatal(err)
	}
	sent := sc.Sent()
	if len(sent) != 1 || string(sent[0].(*agentv1.LogChunk).Data) != "after" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestStreamContainerLogsTruncatedPayloadEndsCleanly(t *testing.T) {
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[4:8], 10)
	frames := concatFrames(header, []byte("abc")) // claims 10, delivers 3, then EOF

	mock := &mockDocker{
		streamLogsFn: func(string, bool, int32, bool) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(frames)), nil
		},
	}
	srv := newTestServer(mock, hostmetrics.NewCollector("", false))
	sc := &sliceConn{}
	if err := srv.StreamContainerLogs(context.Background(),
		connect.NewRequest(&agentv1.LogRequest{ContainerId: "abc"}), newLogServerStream(sc)); err != nil {
		t.Fatal(err)
	}
	if sent := sc.Sent(); len(sent) != 0 {
		t.Errorf("truncated payload should yield no chunks, got %d", len(sent))
	}
}

func TestStreamContainerLogsTailClamps(t *testing.T) {
	tests := []struct {
		name     string
		request  int32
		expected int32
	}{
		{"default when zero", 0, 200},
		{"default when negative", -5, 200},
		{"clamped to max", 500000, 10000},
		{"passthrough in range", 42, 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotTail int32
			mock := &mockDocker{
				streamLogsFn: func(id string, follow bool, tail int32, timestamps bool) (io.ReadCloser, error) {
					gotTail = tail
					return io.NopCloser(strings.NewReader("")), nil
				},
			}
			srv := newTestServer(mock, hostmetrics.NewCollector("", false))
			if err := srv.StreamContainerLogs(context.Background(),
				connect.NewRequest(&agentv1.LogRequest{ContainerId: "abc", Tail: tt.request}),
				newLogServerStream(&sliceConn{})); err != nil {
				t.Fatal(err)
			}
			if gotTail != tt.expected {
				t.Errorf("tail requested=%d passed through=%d, want %d", tt.request, gotTail, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExecStream

func execTestSetup(t *testing.T, mock *mockDocker) (*Server, *bidiConn) {
	t.Helper()
	srv := newTestServer(mock, hostmetrics.NewCollector("", false))
	bc := &bidiConn{
		recv: make(chan *agentv1.ExecInput, 8),
		sent: make(chan any, 64),
	}
	return srv, bc
}

func startExecMsg(id string, tty bool) *agentv1.ExecInput {
	return &agentv1.ExecInput{Body: &agentv1.ExecInput_Start{
		Start: &agentv1.ExecStart{ContainerId: id, Command: []string{"/bin/sh"}, Tty: tty},
	}}
}

func TestExecStreamBidirectionalHappyPath(t *testing.T) {
	serverSide, daemonSide := net.Pipe()
	defer func() { _ = serverSide.Close() }()

	mock := &mockDocker{
		execCreateFn: func(id string, cmd []string, tty bool) (string, error) {
			if id != "abc123" || !tty || len(cmd) != 1 || cmd[0] != "/bin/sh" {
				t.Errorf("exec create args: id=%q cmd=%v tty=%v", id, cmd, tty)
			}
			return "exec-77", nil
		},
		execAttachResp: dockerclient.HijackedResponse{
			Conn:   serverSide,
			Reader: bufio.NewReader(serverSide),
		},
		execInspect: dockerclient.ExecInspectResult{ExitCode: 7},
	}

	srv, bc := execTestSetup(t, mock)

	start := startExecMsg("abc123", true)
	start.GetStart().Rows = 30
	start.GetStart().Cols = 100
	bc.recv <- start
	stdinPayload := []byte("echo hello\n")
	bc.recv <- &agentv1.ExecInput{Body: &agentv1.ExecInput_Stdin{Stdin: stdinPayload}}
	bc.recv <- &agentv1.ExecInput{Body: &agentv1.ExecInput_Resize{
		Resize: &agentv1.ResizeTerminal{Rows: 40, Cols: 120},
	}}
	close(bc.recv)

	done := make(chan error, 1)
	go func() { done <- srv.ExecStream(context.Background(), newBidiStream(bc)) }()

	// Simulate the container writing output on the daemon side.
	if _, err := daemonSide.Write([]byte("hello from container\r\n")); err != nil {
		t.Fatal(err)
	}

	// Drain stdin written by the agent toward the daemon.
	readStdin := make([]byte, 0, len(stdinPayload))
	buf := make([]byte, 64)
	for len(readStdin) < len(stdinPayload) {
		n, err := daemonSide.Read(buf)
		if err != nil {
			t.Fatalf("reading stdin at %d/%d: %v", len(readStdin), len(stdinPayload), err)
		}
		readStdin = append(readStdin, buf[:n]...)
	}
	if !bytes.Equal(readStdin, stdinPayload) {
		t.Errorf("stdin = %q, want %q", readStdin, stdinPayload)
	}

	// Close the daemon side so the output goroutine sees EOF.
	_ = daemonSide.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExecStream: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ExecStream timed out")
	}

	var stdoutChunk *agentv1.ExecOutput
	exitCode := -1
	for {
		select {
		case m := <-bc.sent:
			eo := m.(*agentv1.ExecOutput)
			switch body := eo.Body.(type) {
			case *agentv1.ExecOutput_Stdout:
				if stdoutChunk == nil {
					stdoutChunk = eo
				}
			case *agentv1.ExecOutput_ExitCode:
				exitCode = int(body.ExitCode)
			}
		default:
			goto drained
		}
	}
drained:
	if stdoutChunk == nil {
		t.Fatal("no stdout chunk forwarded")
	}
	if !bytes.Contains(stdoutChunk.GetStdout(), []byte("hello from container")) {
		t.Errorf("stdout = %q", stdoutChunk.GetStdout())
	}
	if exitCode != 7 {
		t.Errorf("exit code = %d, want 7", exitCode)
	}
	if len(mock.resizeCalls) != 2 {
		t.Fatalf("resize calls = %+v, want initial + mid-stream", mock.resizeCalls)
	}
	if mock.resizeCalls[0] != (resizeCall{"exec-77", 30, 100}) || mock.resizeCalls[1] != (resizeCall{"exec-77", 40, 120}) {
		t.Errorf("resize calls = %+v", mock.resizeCalls)
	}
}

func systemInfoFixture(version, nodeID string) (s system.Info) {
	s.ServerVersion = version
	s.Swarm.NodeID = nodeID
	return s
}

func TestExecStreamErrorBranches(t *testing.T) {
	tests := []struct {
		name  string
		mock  *mockDocker
		input func(fc *bidiConn)
		want  connect.Code
	}{
		{
			name:  "first receive fails",
			mock:  &mockDocker{},
			input: func(fc *bidiConn) { close(fc.recv) },
			want:  connect.CodeInvalidArgument,
		},
		{
			name: "first message is not start",
			mock: &mockDocker{},
			input: func(fc *bidiConn) {
				fc.recv <- &agentv1.ExecInput{Body: &agentv1.ExecInput_Stdin{Stdin: []byte("x")}}
				close(fc.recv)
			},
			want: connect.CodeInvalidArgument,
		},
		{
			name: "invalid container id",
			mock: &mockDocker{},
			input: func(fc *bidiConn) {
				fc.recv <- &agentv1.ExecInput{Body: &agentv1.ExecInput_Start{
					Start: &agentv1.ExecStart{ContainerId: "bad/id"},
				}}
				close(fc.recv)
			},
			want: connect.CodeInvalidArgument,
		},
		{
			name: "tty shell not allowed",
			mock: &mockDocker{},
			input: func(fc *bidiConn) {
				fc.recv <- &agentv1.ExecInput{Body: &agentv1.ExecInput_Start{
					Start: &agentv1.ExecStart{ContainerId: "abc", Command: []string{"/bin/fish"}, Tty: true},
				}}
				close(fc.recv)
			},
			want: connect.CodeInvalidArgument,
		},
		{
			name: "exec create fails",
			mock: &mockDocker{
				execCreateFn: func(string, []string, bool) (string, error) {
					return "", context.DeadlineExceeded
				},
			},
			input: func(fc *bidiConn) {
				fc.recv <- startExecMsg("abc", true)
				close(fc.recv)
			},
			want: connect.CodeInternal,
		},
		{
			name: "exec attach fails",
			mock: &mockDocker{execAttachErr: context.DeadlineExceeded},
			input: func(fc *bidiConn) {
				fc.recv <- startExecMsg("abc", true)
				close(fc.recv)
			},
			want: connect.CodeInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, fc := execTestSetup(t, tc.mock)
			tc.input(fc)

			err := srv.ExecStream(context.Background(), newBidiStream(fc))
			if connect.CodeOf(err) != tc.want {
				t.Fatalf("code = %v (%v), want %v", connect.CodeOf(err), err, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StreamHostMetrics

func TestStreamHostMetricsSendsInitialThenTicks(t *testing.T) {
	collector := hostmetrics.NewCollector("", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(ctx)
	waitFor(t, func() bool { return collector.Metrics() != nil })

	srv := newTestServer(&mockDocker{}, collector)
	sc := &sliceConn{}

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer streamCancel()

	start := time.Now()
	err := srv.StreamHostMetrics(streamCtx,
		connect.NewRequest(&agentv1.HostMetricsStreamRequest{IntervalSeconds: 1}),
		newHostMetricsServerStream(sc))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("stream ended too early (%v): tick branch likely unexercised", elapsed)
	}
	if n := len(sc.Sent()); n < 2 {
		t.Errorf("sent %d snapshots, want initial + at least one tick", n)
	}
}

// ---------------------------------------------------------------------------
// HostExec

func TestHostExecEnabledRunsOperation(t *testing.T) {
	collector := hostmetrics.NewCollector("", true)
	srv := newTestServer(&mockDocker{}, collector)

	resp, err := srv.HostExec(context.Background(), connect.NewRequest(&agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_REBOOT_CANCEL,
	}))
	if err != nil {
		t.Fatalf("reboot-cancel should run without root: %v", err)
	}
	if resp.Msg.DurationMs < 0 {
		t.Errorf("DurationMs = %d", resp.Msg.DurationMs)
	}
}

func TestHostExecEnabledUnsupportedOpErrors(t *testing.T) {
	collector := hostmetrics.NewCollector("", true)
	srv := newTestServer(&mockDocker{}, collector)

	_, err := srv.HostExec(context.Background(), connect.NewRequest(&agentv1.HostOperationRequest{
		Operation: agentv1.HostOperation_HOST_OPERATION_UNSPECIFIED,
	}))
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("code = %v, want Internal", connect.CodeOf(err))
	}
}

// ---------------------------------------------------------------------------
// Metrics helpers

func TestUpdateNodeAndPackageMetrics(t *testing.T) {
	m := NewMetricsWithRegistry(prometheus.NewRegistry())

	m.UpdateNodeMetrics(nil) // no-op
	m.UpdatePackageMetrics(nil)

	resp := &agentv1.HostMetricsResponse{
		CpuCores:          []*agentv1.CpuCoreUsage{{Core: 0, Percent: 12.5}, {Core: 1, Percent: 87.5}},
		LoadAvg_1:         1,
		LoadAvg_5:         2,
		LoadAvg_15:        3,
		MemoryTotal:       100,
		MemoryUsed:        50,
		MemoryAvailable:   50,
		SwapTotal:         10,
		SwapUsed:          5,
		UptimeSeconds:     999,
		Filesystems:       []*agentv1.FilesystemInfo{{MountPoint: "/", Device: "sda1", TotalBytes: 1000, UsedBytes: 400, UsagePercent: 40}},
		DiskIo:            []*agentv1.DiskIOInfo{{Device: "sda", ReadBytes: 11, WriteBytes: 22}},
		NetworkInterfaces: []*agentv1.NetworkInterfaceInfo{{Name: "eth0", BytesRecv: 33, BytesSent: 44, ErrorsIn: 1, ErrorsOut: 2}},
	}
	m.UpdateNodeMetrics(resp)

	status := &agentv1.PackageStatusResponse{UpgradableCount: 7, SecurityCount: 2, RebootRequired: true}
	m.UpdatePackageMetrics(status)

	if got := gaugeOf(m.NodeMemoryTotal); got != 100 {
		t.Errorf("NodeMemoryTotal = %v", got)
	}
	if got := gaugeOf(m.NodeUptime); got != 999 {
		t.Errorf("NodeUptime = %v", got)
	}
	if got := gaugeOf(m.NodePkgsUpgradable); got != 7 {
		t.Errorf("NodePkgsUpgradable = %v", got)
	}
	if got := gaugeOf(m.NodeRebootRequired); got != 1 {
		t.Errorf("NodeRebootRequired = %v", got)
	}

	status.RebootRequired = false
	m.UpdatePackageMetrics(status)
	if got := gaugeOf(m.NodeRebootRequired); got != 0 {
		t.Errorf("NodeRebootRequired after clear = %v", got)
	}
}

// ---------------------------------------------------------------------------
// Remaining branch coverage

func TestStreamContainerLogsHeaderTruncatedAndSendError(t *testing.T) {
	t.Run("partial header ends stream", func(t *testing.T) {
		frames := []byte{1, 0, 0, 0} // 4 of 8 header bytes, then EOF
		mock := &mockDocker{streamLogsFn: func(string, bool, int32, bool) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(frames)), nil
		}}
		srv := newTestServer(mock, hostmetrics.NewCollector("", false))
		if err := srv.StreamContainerLogs(context.Background(),
			connect.NewRequest(&agentv1.LogRequest{ContainerId: "abc"}), newLogServerStream(&sliceConn{})); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("send failure ends stream", func(t *testing.T) {
		mock := &mockDocker{streamLogsFn: func(string, bool, int32, bool) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(logFrame(1, "x"))), nil
		}}
		srv := newTestServer(mock, hostmetrics.NewCollector("", false))
		sc := &sliceConn{sendErr: io.ErrClosedPipe}
		if err := srv.StreamContainerLogs(context.Background(),
			connect.NewRequest(&agentv1.LogRequest{ContainerId: "abc"}), newLogServerStream(sc)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExecStreamDefaultShellAndWriteFailure(t *testing.T) {
	t.Run("empty command defaults to /bin/sh", func(t *testing.T) {
		serverSide, daemonSide := net.Pipe()
		defer func() { _ = serverSide.Close() }()
		_ = daemonSide.Close()
		cmdCh := make(chan []string, 1)
		mock := &mockDocker{
			execCreateFn: func(id string, cmd []string, tty bool) (string, error) {
				cmdCh <- cmd
				return "exec-1", nil
			},
			execAttachResp: dockerclient.HijackedResponse{
				Conn:   serverSide,
				Reader: bufio.NewReader(serverSide),
			},
		}
		srv, bc := execTestSetup(t, mock)
		bc.recv <- startExecMsg("abc", false)
		close(bc.recv)
		go func() { _ = srv.ExecStream(context.Background(), newBidiStream(bc)) }()
		var gotCmd []string
		select {
		case gotCmd = <-cmdCh:
		case <-time.After(2 * time.Second):
			t.Fatal("ExecCreate never called")
		}
		if len(gotCmd) != 1 || gotCmd[0] != "/bin/sh" {
			t.Errorf("cmd = %v, want default /bin/sh", gotCmd)
		}
	})

	t.Run("stdin write failure cancels stream", func(t *testing.T) {
		serverSide, daemonSide := net.Pipe()
		_ = daemonSide.Close() // writes to the daemon side will fail immediately
		mock := &mockDocker{
			execAttachResp: dockerclient.HijackedResponse{
				Conn:   serverSide,
				Reader: bufio.NewReader(serverSide),
			},
		}
		srv, bc := execTestSetup(t, mock)
		bc.recv <- startExecMsg("abc", false)
		bc.recv <- &agentv1.ExecInput{Body: &agentv1.ExecInput_Stdin{Stdin: []byte("boom")}}
		close(bc.recv)
		done := make(chan error, 1)
		go func() { done <- srv.ExecStream(context.Background(), newBidiStream(bc)) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("ExecStream: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out")
		}
	})
}

func TestStreamHostMetricsDefaultsAndFailures(t *testing.T) {
	collector := hostmetrics.NewCollector("", false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(ctx)
	waitFor(t, func() bool { return collector.Metrics() != nil })
	srv := newTestServer(&mockDocker{}, collector)

	t.Run("default interval", func(t *testing.T) {
		streamCtx, streamCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer streamCancel()
		sc := &sliceConn{}
		if err := srv.StreamHostMetrics(streamCtx,
			connect.NewRequest(&agentv1.HostMetricsStreamRequest{}), newHostMetricsServerStream(sc)); err != nil {
			t.Fatal(err)
		}
		if len(sc.Sent()) < 1 {
			t.Error("expected the initial snapshot")
		}
	})

	t.Run("initial send failure", func(t *testing.T) {
		sc := &sliceConn{sendErr: io.ErrClosedPipe}
		start := time.Now()
		if err := srv.StreamHostMetrics(context.Background(),
			connect.NewRequest(&agentv1.HostMetricsStreamRequest{}), newHostMetricsServerStream(sc)); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > time.Second {
			t.Error("should return immediately after failed initial send")
		}
	})

	t.Run("nil cache during tick", func(t *testing.T) {
		emptyCollector := hostmetrics.NewCollector("", false)
		srv2 := newTestServer(&mockDocker{}, emptyCollector)
		streamCtx, streamCancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		defer streamCancel()
		sc := &sliceConn{}
		if err := srv2.StreamHostMetrics(streamCtx,
			connect.NewRequest(&agentv1.HostMetricsStreamRequest{IntervalSeconds: 1}),
			newHostMetricsServerStream(sc)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestNewMetricsDefaultRegistry(t *testing.T) {
	if m := NewMetrics(); m == nil || m.ExecStreamTotal == nil {
		t.Fatal("expected metrics registered on the default registry")
	}
}
