package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	terminalruntime "github.com/joshka0/foxctl/internal/runtime/terminal"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
)

// testLogger creates a logger that discards output for tests that don't need
// to inspect logs.
func testLogger() zerolog.Logger {
	return zerolog.New(io.Discard).With().Timestamp().Logger()
}

// findFreePort finds an available TCP port on localhost for testing.
func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func findFreeDevPort(t *testing.T) int {
	t.Helper()
	for range 64 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := ln.Addr().(*net.TCPAddr).Port
		ssh, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+1))
		if err == nil {
			_ = ssh.Close()
			_ = ln.Close()
			return port
		}
		_ = ln.Close()
	}
	t.Fatal("failed to find free gateway dev port pair")
	return 0
}

func waitForHealthyGateway(t *testing.T, port int) *http.Response {
	t.Helper()

	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			return resp
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	require.NoError(t, lastErr)
	return nil
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	assert.Equal(t, DefaultPort, opts.Port)
	assert.Equal(t, DefaultStateDir, opts.StateDir)
	assert.Equal(t, HostnamePrefix, opts.Hostname)
	assert.False(t, opts.Dev)
}

func TestResolveStateDir(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, result string)
	}{
		{
			name:  "expands tilde",
			input: "~/test-gateway",
			check: func(t *testing.T, result string) {
				home, _ := os.UserHomeDir()
				assert.Equal(t, filepath.Join(home, "test-gateway"), result)
			},
		},
		{
			name:  "absolute path unchanged",
			input: "/var/lib/gateway",
			check: func(t *testing.T, result string) {
				assert.Equal(t, "/var/lib/gateway", result)
			},
		},
		{
			name:  "relative path resolved to absolute",
			input: "relative/path",
			check: func(t *testing.T, result string) {
				assert.True(t, filepath.IsAbs(result))
				assert.True(t, strings.HasSuffix(result, "relative/path"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveStateDir(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestResolveAuthKey(t *testing.T) {
	tests := []struct {
		name   string
		optKey string
		envKey string
		want   string
		envSet bool
	}{
		{
			name:   "option key takes priority",
			optKey: "opt-key-123",
			envKey: "env-key-456",
			want:   "opt-key-123",
			envSet: true,
		},
		{
			name:   "env key used when no option key",
			optKey: "",
			envKey: "env-key-456",
			want:   "env-key-456",
			envSet: true,
		},
		{
			name:   "empty when no key available",
			optKey: "",
			envKey: "",
			want:   "",
			envSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envSet {
				t.Setenv("TS_AUTHKEY", tt.envKey)
			} else {
				t.Setenv("TS_AUTHKEY", "")
			}
			got := ResolveAuthKey(tt.optKey)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewServer(t *testing.T) {
	opts := Options{
		Dev:      true,
		Port:     9999,
		Hostname: "test-gateway",
	}
	srv := NewServer(opts, testLogger())

	assert.Equal(t, 9999, srv.opts.Port)
	assert.Equal(t, "test-gateway", srv.opts.Hostname)
	assert.True(t, srv.opts.Dev)
	assert.Equal(t, "starting", srv.health.Tsnet)
	assert.Equal(t, "starting", srv.health.Store)
}

func TestNewServer_DefaultPort(t *testing.T) {
	opts := Options{Port: 0}
	srv := NewServer(opts, testLogger())
	assert.Equal(t, DefaultPort, srv.opts.Port)
}

func TestHealth(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())

	// Initial health
	h := srv.Health()
	assert.Equal(t, "starting", h.Tsnet)

	// Set health
	srv.setHealth(HealthStatus{
		Tsnet: "ok",
		Store: "ok",
		Tmux:  "ok",
	})
	h = srv.Health()
	assert.Equal(t, "ok", h.Tsnet)
	assert.Equal(t, "ok", h.Store)
	assert.Equal(t, "ok", h.Tmux)
}

func TestHandleHealthz_OK(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	srv.setHealth(HealthStatus{
		Tsnet: "ok",
		Store: "ok",
		Tmux:  "ok",
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var health HealthStatus
	err := json.NewDecoder(w.Body).Decode(&health)
	require.NoError(t, err)
	assert.Equal(t, "ok", health.Tsnet)
	assert.Equal(t, "ok", health.Store)
	assert.Equal(t, "ok", health.Tmux)

	// Verify content type
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
}

func TestHandleHealthz_Starting(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	srv.setHealth(HealthStatus{
		Tsnet: "starting",
		Store: "ok",
		Tmux:  "ok",
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// "starting" is not degraded, should return 200
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleHealthz_Degraded(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	srv.setHealth(HealthStatus{
		Tsnet: "error",
		Store: "ok",
		Tmux:  "ok",
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var health HealthStatus
	err := json.NewDecoder(w.Body).Decode(&health)
	require.NoError(t, err)
	assert.Equal(t, "error", health.Tsnet)
}

func TestHandleHealthz_Disabled(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	srv.setHealth(HealthStatus{
		Tsnet: "disabled",
		Store: "ok",
		Tmux:  "ok",
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// "disabled" is not degraded (it's expected in dev mode)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleHealthz_MethodNotAllowed(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestStartDevMode(t *testing.T) {
	port := findFreeDevPort(t)

	opts := Options{
		Dev:  true,
		Port: port,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Verify healthz works once the listener is actually accepting connections
	resp := waitForHealthyGateway(t, port)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var (
		health HealthStatus
		err    error
	)
	err = json.NewDecoder(resp.Body).Decode(&health)
	require.NoError(t, err)
	assert.Equal(t, "disabled", health.Tsnet)
	assert.Equal(t, "ok", health.Store)
	assert.Equal(t, "ok", health.Tmux)

	// Shutdown
	cancel()
	err = <-errCh
	assert.NoError(t, err)
}

func TestStartDevMode_GracefulShutdown(t *testing.T) {
	port := findFreeDevPort(t)

	opts := Options{
		Dev:  true,
		Port: port,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Verify it's running once the listener is actually accepting connections
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	resp := waitForHealthyGateway(t, port)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Trigger shutdown
	cancel()

	// Wait for shutdown to complete
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("shutdown timed out")
	}

	// Verify server is stopped
	_, err := http.Get(url)
	assert.Error(t, err, "server should be stopped after shutdown")
}

func TestStartDevMode_SSHPortConflict(t *testing.T) {
	var (
		port    int
		sshPort int
		ln      net.Listener
		err     error
	)
	for range 32 {
		port = findFreePort(t)
		sshPort = port + 1
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", sshPort))
		if err == nil {
			break
		}
	}
	require.NoError(t, err)
	defer ln.Close()

	opts := Options{
		Dev:  true,
		Port: port,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = srv.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ssh server")
}

func TestStartTwiceReturnsError(t *testing.T) {
	port := findFreeDevPort(t)

	opts := Options{
		Dev:  true,
		Port: port,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Wait for first start to become healthy
	resp := waitForHealthyGateway(t, port)
	resp.Body.Close()

	// Second start should fail
	err := srv.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")

	// Cleanup
	cancel()
	<-errCh
}

func TestAuthKeyError(t *testing.T) {
	err := &AuthKeyError{StateDir: "/tmp/gateway"}

	assert.Equal(t, "no Tailscale auth key provided and no existing state", err.Error())

	env := err.Envelope()
	assert.NotNil(t, env)

	data, ok := env["data"].(map[string]string)
	require.True(t, ok)
	assert.Contains(t, data["hint"], "TS_AUTHKEY")
	assert.Contains(t, data["hint"], "--ts-authkey")
	assert.Equal(t, "/tmp/gateway", data["state"])
}

func TestAuthKeyError_Wrapped(t *testing.T) {
	err := &AuthKeyError{StateDir: "/tmp/gateway"}
	wrapped := fmt.Errorf("outer: %w", err)

	var target *AuthKeyError
	assert.True(t, errors.As(wrapped, &target))
	assert.Equal(t, err.StateDir, target.StateDir)
}

func TestAuthKeyError_NoStateNoKey(t *testing.T) {
	// Create temp dir for state (empty, no identity)
	tmpDir := t.TempDir()

	// No auth key, no existing state
	opts := Options{
		StateDir: tmpDir,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := srv.Start(ctx)
	require.Error(t, err)

	var authErr *AuthKeyError
	assert.ErrorAs(t, err, &authErr)
}

func TestStateDirResolution(t *testing.T) {
	port := findFreeDevPort(t)
	tmpDir := filepath.Join(t.TempDir(), "gateway-state")

	opts := Options{
		Dev:      true,
		Port:     port,
		StateDir: tmpDir,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	resp := waitForHealthyGateway(t, port)
	resp.Body.Close()

	// Verify the state dir option was stored correctly
	assert.Equal(t, tmpDir, srv.opts.StateDir)

	cancel()
	<-errCh
}

func TestRun_DevMode(t *testing.T) {
	port := findFreeDevPort(t)

	opts := Options{
		Dev:  true,
		Port: port,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, opts, testLogger())
	}()

	// Verify it's running once the listener is actually accepting connections
	resp := waitForHealthyGateway(t, port)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Cancel context to trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not complete within timeout")
	}
}

func TestRun_AuthKeyError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TS_AUTHKEY", "")

	opts := Options{
		StateDir: tmpDir,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := Run(ctx, opts, testLogger())
	require.Error(t, err)

	var authErr *AuthKeyError
	assert.ErrorAs(t, err, &authErr)
}

func TestShutdown_SetsStoppedHealth(t *testing.T) {
	port := findFreeDevPort(t)

	opts := Options{
		Dev:  true,
		Port: port,
	}
	srv := NewServer(opts, testLogger())

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	resp := waitForHealthyGateway(t, port)
	resp.Body.Close()

	// Verify running
	h := srv.Health()
	assert.Equal(t, "ok", h.Store)

	// Shutdown
	cancel()
	<-errCh

	// After shutdown, health should be "stopped"
	h = srv.Health()
	assert.Equal(t, "stopped", h.Tsnet)
	assert.Equal(t, "stopped", h.Store)
	assert.Equal(t, "stopped", h.Tmux)
}

// --- Room HTTP API tests ---

func TestHandleRooms_RegisterRoom(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	handler := srv.Handler()

	body := `{"room_id":"test-room","tmux_session":"foxctl-sandbox-test-room","max_connections":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "registered", resp["status"])
	assert.Equal(t, "test-room", resp["room_id"])
	assert.Equal(t, "foxctl-sandbox-test-room", resp["tmux_session"])

	// Verify room is actually registered in the hub and ssh rooms
	assert.True(t, srv.termHub.HasRoom("test-room"), "web terminal hub should have room")
	assert.True(t, srv.sshRooms.HasRoom("test-room"), "ssh rooms should have room")
}

func TestHandleRooms_RegisterRoom_MissingRoomID(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	handler := srv.Handler()

	body := `{"tmux_session":"session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRooms_RegisterRoom_MissingTmuxSession(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	handler := srv.Handler()

	body := `{"room_id":"test-room"}`
	req := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRooms_MethodNotAllowed(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/rooms", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHandleRoomByID_Unregister(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	// Pre-register a room so we have something to unregister.
	srv.rooms.Register(agentpane.ResolveTerminalRoomConfig("del-room", "foxctl-sandbox-del-room", 0))
	require.True(t, srv.termHub.HasRoom("del-room"))

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/del-room", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "unregistered", resp["status"])
	assert.Equal(t, "del-room", resp["room_id"])

	// Room should be gone from both hubs.
	assert.False(t, srv.termHub.HasRoom("del-room"), "web terminal hub should not have room")
	assert.False(t, srv.sshRooms.HasRoom("del-room"), "ssh rooms should not have room")
}

func TestHandleRoomByID_Unregister_MethodNotAllowed(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/rooms/some-room", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestRegisterTerminalRoom_DefaultTmuxSession(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())

	srv.rooms.Register(agentpane.ResolveTerminalRoomConfig("alpha-room", "", 2))

	assert.True(t, srv.termHub.HasRoom("alpha-room"))

	sshConfig, ok := srv.sshRooms.TerminalRoomConfig("alpha-room")
	require.True(t, ok)
	assert.Equal(t, terminalruntime.DefaultRoomTmuxSession("alpha-room"), sshConfig.TmuxSession)
}

func TestHandleRoomByID_Unregister_MissingID(t *testing.T) {
	srv := NewServer(DefaultOptions(), testLogger())
	handler := srv.Handler()

	// Trailing slash with no room ID
	req := httptest.NewRequest(http.MethodDelete, "/api/rooms/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
