package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/config"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
)

func TestService_StartsAndStops(t *testing.T) {
	// Note: not parallel because we modify XDG_RUNTIME_DIR

	// Create temp directories
	tmp := t.TempDir()
	skipIfUnixSocketsUnavailable(t, tmp)
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

	socketPath := SocketPath()
	waitForSocket(t, socketPath, errCh)

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
	skipIfUnixSocketsUnavailable(t, tmp)
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

	socketPath := SocketPath()
	waitForSocket(t, socketPath, errCh)

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

func TestServiceResolveLLMConfig_DefaultsToLMStudio(t *testing.T) {
	t.Setenv("LMSTUDIO_MODEL", "")
	svc := &Service{cfg: config.Config{}}

	provider, apiKey, model := svc.resolveLLMConfig()
	if provider != "lmstudio" {
		t.Fatalf("provider=%q want lmstudio", provider)
	}
	if apiKey != "lm-studio" {
		t.Fatalf("apiKey=%q want lm-studio", apiKey)
	}
	wantModel := llmproviders.DefaultModelForProvider("lmstudio")
	if model != wantModel {
		t.Fatalf("model=%q want %q", model, wantModel)
	}
}

func TestServiceResolveLLMConfig_RespectsConfiguredProvider(t *testing.T) {
	t.Setenv("LMSTUDIO_MODEL", "")
	svc := &Service{
		cfg: config.Config{
			LLM: config.LLMSettings{
				Provider:         "openrouter",
				OpenRouterAPIKey: "or-key",
				OpenRouterModel:  "openrouter/custom",
			},
		},
	}

	provider, apiKey, model := svc.resolveLLMConfig()
	if provider != "openrouter" {
		t.Fatalf("provider=%q want openrouter", provider)
	}
	if apiKey != "or-key" {
		t.Fatalf("apiKey=%q want or-key", apiKey)
	}
	if model != "openrouter/custom" {
		t.Fatalf("model=%q want openrouter/custom", model)
	}
}

func waitForSocket(t *testing.T, socketPath string, errCh <-chan error) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("service run: %v", err)
			}
			t.Fatalf("service stopped before socket created")
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("socket not created: %v", err)
	}
}

func skipIfUnixSocketsUnavailable(t *testing.T, dir string) {
	t.Helper()
	socketPath := filepath.Join(dir, "agentctl-test.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EINVAL) || os.IsPermission(err) {
			t.Skipf("unix sockets not permitted: %v", err)
		}
		t.Fatalf("probe unix socket: %v", err)
	}
	_ = listener.Close()
	_ = os.Remove(socketPath)
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

func TestSocketPath_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	expected := filepath.Join(tmp, "custom.sock")
	t.Setenv(EnvDaemonSocketPath, expected)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "ignored"))

	path := SocketPath()
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}

func TestService_UsesSocketEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	skipIfUnixSocketsUnavailable(t, tmp)

	socketPath := filepath.Join(tmp, "daemon.sock")
	t.Setenv(EnvDaemonSocketPath, socketPath)
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(tmp, "ignored"))

	cfg := config.Config{
		Paths: config.Paths{
			Cache: filepath.Join(tmp, "cache"),
			CAS:   filepath.Join(tmp, "cas"),
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc, err := NewService(cfg, ServiceOptions{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if svc.socketPath != socketPath {
		t.Fatalf("expected service socketPath %s, got %s", socketPath, svc.socketPath)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.Run(ctx)
	}()

	waitForSocket(t, socketPath, errCh)

	client := NewClient()
	if client.socketPath != socketPath {
		t.Fatalf("expected client socketPath %s, got %s", socketPath, client.socketPath)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := svc.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("expected socket to be removed, err=%v", err)
	}
}
