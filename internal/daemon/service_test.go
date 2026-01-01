package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestService_StartsAndStops(t *testing.T) {
	// Note: not parallel because we modify XDG_RUNTIME_DIR

	// Create temp directories
	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
	}

	// Override socket path for test (t.Setenv is safe for parallel tests)
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := NewService(cfg, ServiceOptions{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// Start in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	// Wait for socket to be created
	socketPath := SocketPath()
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify socket exists
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket not created: %v", err)
	}

	// Test connection
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	conn.Close()

	// Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := svc.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestService_StatusRequest(t *testing.T) {
	// Note: not parallel because we modify XDG_RUNTIME_DIR

	tmp := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
	}

	t.Setenv("XDG_RUNTIME_DIR", tmp)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := NewService(cfg, ServiceOptions{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	// Wait for socket
	socketPath := SocketPath()
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Connect and send status request
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	req := Request{Method: "status", ID: "test-1"}
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	// Read response
	decoder := json.NewDecoder(conn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s: %s", resp.Error.Code, resp.Error.Message)
	}

	if resp.ID != "test-1" {
		t.Fatalf("expected ID test-1, got %s", resp.ID)
	}

	// Verify status result
	resultBytes, _ := json.Marshal(resp.Result)
	var status StatusResult
	if err := json.Unmarshal(resultBytes, &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}

	if status.PID == 0 {
		t.Fatal("expected non-zero PID")
	}
	if status.RequestCount != 1 {
		t.Fatalf("expected request count 1, got %d", status.RequestCount)
	}

	// Cleanup
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = svc.Shutdown(shutdownCtx)
}

func TestClient_IsRunning_NoDaemon(t *testing.T) {
	// Note: not parallel because we modify XDG_RUNTIME_DIR

	// Use a non-existent socket
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	client := NewClient()
	if client.IsRunning() {
		t.Fatal("expected IsRunning to return false when no daemon")
	}
}

func TestClient_FallsBackWhenDaemonUnavailable(t *testing.T) {
	// Note: not parallel because we modify XDG_RUNTIME_DIR

	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	client := NewClient()

	// Attempt to run - should fail gracefully
	_, err := client.Run("test-skill", []byte(`{}`), "/tmp", false)
	if err == nil {
		t.Fatal("expected error when daemon not running")
	}
}

func TestSocketPath(t *testing.T) {
	// Note: not parallel because we modify XDG_RUNTIME_DIR
	// Test with XDG_RUNTIME_DIR set
	tmp := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tmp)

	path := SocketPath()
	expected := filepath.Join(tmp, "agentctl.sock")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}
