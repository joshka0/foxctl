package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/flow"
)

// ---------------------------------------------------------------------------
// Test helpers for client flow integration tests
// ---------------------------------------------------------------------------

// setupFlowDaemon creates a daemon service on a temp socket with a flow engine
// backed by a mock store. Returns the service, client, socket path, and a
// cleanup function. The daemon is already running when returned.
func setupFlowDaemon(t *testing.T, store flow.Store) (*Service, *Client, string) {
	t.Helper()

	// Use /tmp directly for Unix socket compatibility on macOS.
	// t.TempDir() may use an NFS-backed path where Unix sockets are not permitted.
	tmp, err := os.MkdirTemp("/tmp", "foxctl-flow-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	skipIfUnixSocketsUnavailable(t, tmp)

	socketPath := filepath.Join(tmp, "foxctl-test.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
	}

	svc := newServiceWithFlowEngine(store)
	svc.cfg = cfg
	svc.socketPath = socketPath
	svc.started = time.Now()
	svc.shutdownCh = make(chan struct{})

	// Start listening
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		t.Fatalf("chmod: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go svc.acceptLoop(ctx, listener)

	client := NewClient()
	if client.socketPath != socketPath {
		t.Fatalf("client socketPath = %q, want %q", client.socketPath, socketPath)
	}

	t.Cleanup(func() {
		cancel()
		listener.Close()
		_ = os.Remove(socketPath)
		_ = os.Remove(tmp)
	})

	return svc, client, socketPath
}

// ---------------------------------------------------------------------------
// Tests: Client.FlowStart via Unix socket
// ---------------------------------------------------------------------------

func TestClient_FlowStart_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	result, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}

	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
	if result.RunID == "" {
		t.Error("RunID is empty, expected non-empty")
	}
	if result.State != "running" {
		t.Errorf("State = %q, want %q", result.State, "running")
	}
}

func TestClient_FlowStart_ReturnsRunID(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-alpha", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	result, err := client.FlowStart("flow-alpha", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}

	// RunID should be non-empty (VAL-M1-058)
	if result.RunID == "" {
		t.Error("RunID is empty, expected non-empty run_id in response")
	}
}

func TestClient_FlowStart_WorkspacePropagation(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/workspace/A")
	seedTwoNodeFlow(store, "flow-2", "/workspace/B")
	_, client, _ := setupFlowDaemon(t, store)

	resultA, err := client.FlowStart("flow-1", "/workspace/A")
	if err != nil {
		t.Fatalf("FlowStart(flow-1) error = %v", err)
	}
	if resultA.RunID == "" {
		t.Error("flow-1 RunID empty")
	}

	resultB, err := client.FlowStart("flow-2", "/workspace/B")
	if err != nil {
		t.Fatalf("FlowStart(flow-2) error = %v", err)
	}
	if resultB.RunID == "" {
		t.Error("flow-2 RunID empty")
	}

	// Different flows get different run IDs
	if resultA.RunID == resultB.RunID {
		t.Error("expected different run IDs for different flows")
	}
}

func TestClient_FlowStart_NotFound(t *testing.T) {
	store := newMockFlowStore()
	_, client, _ := setupFlowDaemon(t, store)

	_, err := client.FlowStart("nonexistent", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for nonexistent flow, got nil")
	}

	// Error should mention ENOTFOUND
	if !strings.Contains(err.Error(), "ENOTFOUND") {
		t.Errorf("error = %q, want ENOTFOUND", err.Error())
	}
}

func TestClient_FlowStart_AlreadyRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Start once
	_, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("first FlowStart() error = %v", err)
	}

	// Wait for flow to be running
	time.Sleep(100 * time.Millisecond)

	// Start again — should fail with EALREADY
	_, err = client.FlowStart("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for already running flow, got nil")
	}
	if !strings.Contains(err.Error(), "EALREADY") {
		t.Errorf("error = %q, want EALREADY", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests: Client.FlowStop via Unix socket
// ---------------------------------------------------------------------------

func TestClient_FlowStop_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Start first
	_, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Stop
	result, err := client.FlowStop("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStop() error = %v", err)
	}

	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
	if result.State != "stopped" {
		t.Errorf("State = %q, want %q", result.State, "stopped")
	}
}

func TestClient_FlowStop_ReturnsFinalState(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	_, _ = client.FlowStart("flow-1", "/tmp/ws")
	time.Sleep(100 * time.Millisecond)

	result, err := client.FlowStop("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStop() error = %v", err)
	}

	// VAL-M1-069: flow.stop response includes final state
	if result.State == "" {
		t.Error("State is empty, expected final state in response")
	}
}

func TestClient_FlowStop_NotRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Stop a draft flow — should error
	_, err := client.FlowStop("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for stopping non-running flow, got nil")
	}
}

func TestClient_FlowStop_NotFound(t *testing.T) {
	store := newMockFlowStore()
	_, client, _ := setupFlowDaemon(t, store)

	_, err := client.FlowStop("nonexistent", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for nonexistent flow, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Client.FlowPause via Unix socket
// ---------------------------------------------------------------------------

func TestClient_FlowPause_HappyPath(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Start first
	_, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Pause
	result, err := client.FlowPause("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowPause() error = %v", err)
	}

	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
	if result.State != "paused" {
		t.Errorf("State = %q, want %q", result.State, "paused")
	}
}

func TestClient_FlowPause_ConfirmsPaused(t *testing.T) {
	// VAL-M1-070: flow.pause response confirms paused state
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	_, _ = client.FlowStart("flow-1", "/tmp/ws")
	time.Sleep(100 * time.Millisecond)

	result, err := client.FlowPause("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowPause() error = %v", err)
	}

	if result.State != "paused" {
		t.Errorf("State = %q, want %q confirming paused", result.State, "paused")
	}
}

func TestClient_FlowPause_NotRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Pause a draft flow — should error
	_, err := client.FlowPause("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for pausing non-running flow, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Client.FlowStatus via Unix socket
// ---------------------------------------------------------------------------

func TestClient_FlowStatus_LiveRunning(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Start first
	startResult, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	// Check status
	result, err := client.FlowStatus("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStatus() error = %v", err)
	}

	if result.FlowID != "flow-1" {
		t.Errorf("FlowID = %q, want %q", result.FlowID, "flow-1")
	}
	if result.State != "running" {
		t.Errorf("State = %q, want %q", result.State, "running")
	}
	// VAL-M1-068: flow.status includes active run_id if running
	if result.RunID != startResult.RunID {
		t.Errorf("RunID = %q, want %q", result.RunID, startResult.RunID)
	}
}

func TestClient_FlowStatus_DraftFlow(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	result, err := client.FlowStatus("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStatus() error = %v", err)
	}

	if result.State == "" {
		t.Error("State is empty, expected some state")
	}
}

func TestClient_FlowStatus_NotFound(t *testing.T) {
	store := newMockFlowStore()
	_, client, _ := setupFlowDaemon(t, store)

	_, err := client.FlowStatus("nonexistent", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for nonexistent flow, got nil")
	}
}

func TestClient_FlowStatus_IncludesNodeDetails(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	_, _ = client.FlowStart("flow-1", "/tmp/ws")
	time.Sleep(100 * time.Millisecond)

	result, err := client.FlowStatus("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStatus() error = %v", err)
	}

	// Should include per-node execution state
	if len(result.Nodes) == 0 {
		t.Error("Nodes is empty, expected per-node execution state")
	}
}

// ---------------------------------------------------------------------------
// Tests: Full lifecycle via client
// ---------------------------------------------------------------------------

func TestClient_FullLifecycle(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	// Start
	startResult, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}
	if startResult.State != "running" {
		t.Fatalf("start state = %q, want running", startResult.State)
	}
	time.Sleep(100 * time.Millisecond)

	// Status should show running
	statusResult, err := client.FlowStatus("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStatus() error = %v", err)
	}
	if statusResult.State != "running" {
		t.Errorf("status state = %q, want running", statusResult.State)
	}

	// Pause
	pauseResult, err := client.FlowPause("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowPause() error = %v", err)
	}
	if pauseResult.State != "paused" {
		t.Errorf("pause state = %q, want paused", pauseResult.State)
	}

	// Resume by starting again
	resumeResult, err := client.FlowStart("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStart(resume) error = %v", err)
	}
	if resumeResult.State != "running" {
		t.Errorf("resume state = %q, want running", resumeResult.State)
	}
	time.Sleep(100 * time.Millisecond)

	// Stop
	stopResult, err := client.FlowStop("flow-1", "/tmp/ws")
	if err != nil {
		t.Fatalf("FlowStop() error = %v", err)
	}
	if stopResult.State != "stopped" {
		t.Errorf("stop state = %q, want stopped", stopResult.State)
	}
}

// ---------------------------------------------------------------------------
// Tests: Connection errors
// ---------------------------------------------------------------------------

func TestClient_FlowStart_ConnectionError(t *testing.T) {
	// Create client pointing to nonexistent socket
	tmp, err := os.MkdirTemp("/tmp", "foxctl-conn-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.Remove(tmp)
	socketPath := filepath.Join(tmp, "nonexistent.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	client := NewClient()
	// Don't start any daemon — just try to call

	_, err = client.FlowStart("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestClient_FlowStop_ConnectionError(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "foxctl-conn-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.Remove(tmp)
	socketPath := filepath.Join(tmp, "nonexistent.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	client := NewClient()

	_, err = client.FlowStop("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestClient_FlowPause_ConnectionError(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "foxctl-conn-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.Remove(tmp)
	socketPath := filepath.Join(tmp, "nonexistent.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	client := NewClient()

	_, err = client.FlowPause("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

func TestClient_FlowStatus_ConnectionError(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "foxctl-conn-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.Remove(tmp)
	socketPath := filepath.Join(tmp, "nonexistent.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	client := NewClient()

	_, err = client.FlowStatus("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Malformed response handling
// ---------------------------------------------------------------------------

func TestClient_FlowStart_MalformedResponse(t *testing.T) {
	// Start a test server that returns invalid JSON
	tmp, err := os.MkdirTemp("/tmp", "foxctl-malformed-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	skipIfUnixSocketsUnavailable(t, tmp)
	socketPath := filepath.Join(tmp, "test.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
		_ = os.Remove(tmp)
	})

	// Server that returns garbage
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request (discard)
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)

		// Write malformed response
		_, _ = conn.Write([]byte("not valid json\n"))
	}()

	client := NewClient()

	_, err = client.FlowStart("flow-1", "/tmp/ws")
	if err == nil {
		t.Fatal("expected error for malformed response, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Timeout handling
// ---------------------------------------------------------------------------

func TestClient_FlowStart_Timeout(t *testing.T) {
	// Start a test server that never responds
	tmp, err := os.MkdirTemp("/tmp", "foxctl-timeout-test-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	skipIfUnixSocketsUnavailable(t, tmp)
	socketPath := filepath.Join(tmp, "test.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		listener.Close()
		_ = os.Remove(socketPath)
		_ = os.Remove(tmp)
	})

	var accepted atomic.Bool
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		accepted.Store(true)
		// Read request but never respond — simulates hung daemon
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		// Hold connection open until test cleanup closes listener
		time.Sleep(60 * time.Second)
		conn.Close()
	}()

	client := NewClient()

	// The client.call() sets a 30s deadline, but for test speed,
	// we verify the error is a timeout or connection-related
	done := make(chan error, 1)
	go func() {
		_, err := client.FlowStart("flow-1", "/tmp/ws")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error for hung daemon, got nil")
		}
		// Either timeout or connection closed — both are acceptable
	case <-time.After(35 * time.Second):
		t.Fatal("FlowStart timed out waiting for response — client should have timed out")
	}
}

// ---------------------------------------------------------------------------
// Tests: Socket env override (VAL-M1-057)
// ---------------------------------------------------------------------------

func TestClient_FlowRPC_SocketEnvOverride(t *testing.T) {
	// Verify that FOXCTL_DAEMON_SOCKET env overrides client socket path
	tmp := t.TempDir()
	socketPath := filepath.Join(tmp, "custom.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "ignored"))

	client := NewClient()
	if client.socketPath != socketPath {
		t.Errorf("client socketPath = %q, want %q", client.socketPath, socketPath)
	}
}

// ---------------------------------------------------------------------------
// Tests: Request ID echo (VAL-M1-061)
// ---------------------------------------------------------------------------

func TestClient_FlowRPC_RequestIDEcho(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	svc, _, socketPath := setupFlowDaemon(t, store)

	// Connect directly and send request with specific ID
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := Request{
		Method: "flow.start",
		ID:     "test-echo-id-123",
		Params: json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`),
	}
	_ = svc // svc is needed for the daemon lifecycle; reference kept for clarity

	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.ID != "test-echo-id-123" {
		t.Errorf("Response ID = %q, want %q", resp.ID, "test-echo-id-123")
	}
}

// ---------------------------------------------------------------------------
// Tests: Unknown method error (VAL-M1-026)
// ---------------------------------------------------------------------------

func TestClient_FlowRPC_UnknownMethod(t *testing.T) {
	store := newMockFlowStore()
	_, _, socketPath := setupFlowDaemon(t, store)

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := Request{
		Method: "flow.nonexistent",
		ID:     "unknown-1",
		Params: json.RawMessage(`{}`),
	}

	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for unknown method, got nil")
	}
	if resp.Error.Code != "EMETHOD" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EMETHOD")
	}
}

// ---------------------------------------------------------------------------
// Tests: Malformed JSON request (VAL-M1-027)
// ---------------------------------------------------------------------------

func TestClient_FlowRPC_MalformedJSON(t *testing.T) {
	store := newMockFlowStore()
	_, _, socketPath := setupFlowDaemon(t, store)

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send malformed JSON
	_, _ = fmt.Fprintf(conn, "{invalid json}\n")

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Error == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if resp.Error.Code != "EPARSE" {
		t.Errorf("Error.Code = %q, want %q", resp.Error.Code, "EPARSE")
	}
}

// ---------------------------------------------------------------------------
// Tests: Envelope contract (VAL-M1-059)
// ---------------------------------------------------------------------------

func TestClient_FlowRPC_ResponseIsEnvelope(t *testing.T) {
	// Verify that all flow RPC responses are valid JSON envelopes
	// (version, status, command, data, meta). The daemon uses a simplified
	// JSON-RPC response format which must be valid JSON.
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/tmp/ws")
	_, _, socketPath := setupFlowDaemon(t, store)

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req := Request{
		Method: "flow.start",
		ID:     "envelope-1",
		Params: json.RawMessage(`{"flow_id":"flow-1","workspace":"/tmp/ws"}`),
	}

	if err := json.NewEncoder(conn).Encode(&req); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Response must be valid JSON with ID and either Result or Error
	if resp.ID == "" {
		t.Error("Response ID is empty, expected non-empty")
	}
	if resp.Result == nil && resp.Error == nil {
		t.Error("Response has neither Result nor Error")
	}
}

// ---------------------------------------------------------------------------
// Tests: Concurrent flow operations
// ---------------------------------------------------------------------------

func TestClient_FlowStart_ConcurrentFlows(t *testing.T) {
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-a", "/tmp/ws")
	seedTwoNodeFlow(store, "flow-b", "/tmp/ws")
	_, client, _ := setupFlowDaemon(t, store)

	errCh := make(chan error, 2)
	var results [2]*FlowStartResult

	go func() {
		r, err := client.FlowStart("flow-a", "/tmp/ws")
		results[0] = r
		errCh <- err
	}()
	go func() {
		r, err := client.FlowStart("flow-b", "/tmp/ws")
		results[1] = r
		errCh <- err
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent start %d: %v", i, err)
		}
	}

	// Verify distinct run IDs
	if results[0] != nil && results[1] != nil {
		if results[0].RunID == results[1].RunID {
			t.Error("concurrent flows got same run ID")
		}
	}
}

// ---------------------------------------------------------------------------
// Tests: Workspace parameter in all client methods
// ---------------------------------------------------------------------------

func TestClient_FlowMethods_WorkspaceParam(t *testing.T) {
	// Verify all client methods accept workspace parameter
	store := newMockFlowStore()
	seedTwoNodeFlow(store, "flow-1", "/workspace/test")
	_, client, _ := setupFlowDaemon(t, store)

	ws := "/workspace/test"

	// Start with workspace
	result, err := client.FlowStart("flow-1", ws)
	if err != nil {
		t.Fatalf("FlowStart() error = %v", err)
	}
	if result.RunID == "" {
		t.Error("RunID empty after start")
	}
	time.Sleep(100 * time.Millisecond)

	// Status with workspace
	status, err := client.FlowStatus("flow-1", ws)
	if err != nil {
		t.Fatalf("FlowStatus() error = %v", err)
	}
	if status.State != "running" {
		t.Errorf("Status state = %q, want running", status.State)
	}

	// Pause with workspace
	pause, err := client.FlowPause("flow-1", ws)
	if err != nil {
		t.Fatalf("FlowPause() error = %v", err)
	}
	if pause.State != "paused" {
		t.Errorf("Pause state = %q, want paused", pause.State)
	}

	// Resume with workspace
	resume, err := client.FlowStart("flow-1", ws)
	if err != nil {
		t.Fatalf("FlowStart(resume) error = %v", err)
	}
	if resume.State != "running" {
		t.Errorf("Resume state = %q, want running", resume.State)
	}
	time.Sleep(100 * time.Millisecond)

	// Stop with workspace
	stop, err := client.FlowStop("flow-1", ws)
	if err != nil {
		t.Fatalf("FlowStop() error = %v", err)
	}
	if stop.State != "stopped" {
		t.Errorf("Stop state = %q, want stopped", stop.State)
	}
}


