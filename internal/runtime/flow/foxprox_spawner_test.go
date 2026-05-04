package flow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/vtscreen"
	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
)

// ---------------------------------------------------------------------------
// Mock FoxproxClient
// ---------------------------------------------------------------------------

// mockFoxproxClient implements the FoxproxClient interface for testing.
type mockFoxproxClient struct {
	mu sync.Mutex

	// CreateSession behavior
	createSessionResp *httpjson.SessionResponse
	createSessionErr  error
	createSessionReqs []httpjson.CreateSessionRequest

	// DeleteSession behavior
	deleteSessionErr  error
	deleteSessionIDs  []string

	// SessionReadiness behavior
	readinessResults map[string][]*httpjson.ReadinessResponse // sessionID -> sequence of responses
	readinessErr     error
	readinessDefaultIdle bool // default true; set false for timeout tests

	// SessionScreen behavior
	screenResults map[string][]*vtscreen.Snapshot // sessionID -> sequence of responses
	screenErr     error

	// TerminalSubmit behavior
	submitResp *client.TerminalSubmitResponse
	submitErr  error
	submitReqs []client.TerminalSubmitRequest
}

func (m *mockFoxproxClient) CreateSession(ctx context.Context, req httpjson.CreateSessionRequest) (httpjson.SessionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createSessionReqs = append(m.createSessionReqs, req)
	if m.createSessionErr != nil {
		return httpjson.SessionResponse{}, m.createSessionErr
	}
	if m.createSessionResp != nil {
		return *m.createSessionResp, nil
	}
	return httpjson.SessionResponse{ID: "test-session-123", Status: "running"}, nil
}

func (m *mockFoxproxClient) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteSessionIDs = append(m.deleteSessionIDs, id)
	return m.deleteSessionErr
}

func (m *mockFoxproxClient) SessionReadiness(ctx context.Context, id string, opts client.SessionReadinessOptions) (httpjson.ReadinessResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.readinessErr != nil {
		return httpjson.ReadinessResponse{}, m.readinessErr
	}
	if results, ok := m.readinessResults[id]; ok && len(results) > 0 {
		result := results[0]
		m.readinessResults[id] = results[1:]
		return *result, nil
	}
	// Default based on readinessDefaultIdle (defaults to true)
	idle := m.readinessDefaultIdle
	return httpjson.ReadinessResponse{SessionID: id, Idle: idle}, nil
}

func (m *mockFoxproxClient) SessionScreen(ctx context.Context, id string) (vtscreen.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.screenErr != nil {
		return vtscreen.Snapshot{}, m.screenErr
	}
	if results, ok := m.screenResults[id]; ok && len(results) > 0 {
		result := results[0]
		m.screenResults[id] = results[1:]
		return *result, nil
	}
	return vtscreen.Snapshot{Rows: 24, Cols: 80, Lines: []string{}}, nil
}

func (m *mockFoxproxClient) TerminalSubmit(ctx context.Context, req client.TerminalSubmitRequest) (client.TerminalSubmitResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.submitReqs = append(m.submitReqs, req)
	if m.submitErr != nil {
		return client.TerminalSubmitResponse{}, m.submitErr
	}
	if m.submitResp != nil {
		return *m.submitResp, nil
	}
	return client.TerminalSubmitResponse{SessionID: req.SessionID, Written: len(req.Text)}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFoxproxSpawner_Spawn_CreatesPTYSessions(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-1",
			Status: "running",
			Cmd:    []string{"droid"},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	result, err := spawner.Spawn(context.Background(), "researcher", "Research the auth module", AgentSpawnOptions{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.AgentID != "fp-session-1" {
		t.Errorf("expected agent_id fp-session-1, got %q", result.AgentID)
	}
	if result.SessionID != "fp-session-1" {
		t.Errorf("expected session_id fp-session-1, got %q", result.SessionID)
	}

	// Verify CreateSession was called with correct parameters
	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "droid" {
		t.Errorf("expected cmd [droid], got %v", req.Cmd)
	}
	if req.Cwd != "/tmp/workspace" {
		t.Errorf("expected cwd /tmp/workspace, got %q", req.Cwd)
	}
}

func TestFoxproxSpawner_Spawn_CustomCLICmd(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-2",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "claude",
	})

	_, err := spawner.Spawn(context.Background(), "coder", "Write code", AgentSpawnOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "claude" {
		t.Errorf("expected cmd [claude], got %v", req.Cmd)
	}
}

func TestFoxproxSpawner_Spawn_CreateSessionError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionErr: fmt.Errorf("foxprox daemon not running"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "foxprox daemon not running") {
		t.Errorf("expected error containing 'foxprox daemon not running', got: %v", err)
	}
}

func TestFoxproxSpawner_Spawn_CancelledContext(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionErr: context.Canceled,
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := spawner.Spawn(ctx, "researcher", "test", AgentSpawnOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFoxproxSpawner_Spawn_ReadinessProfile(t *testing.T) {
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-3",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "test", AgentSpawnOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify readiness profile was set on the session
	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if req.Readiness == nil {
		t.Fatal("expected readiness profile to be set")
	}
	if req.Readiness.DebounceMS == 0 {
		t.Error("expected debounce_ms > 0 in readiness profile")
	}
}

func TestFoxproxSpawner_Ask_SendsPromptAndCapturesOutput(t *testing.T) {
	fpClient := &mockFoxproxClient{
		submitResp: &client.TerminalSubmitResponse{
			SessionID: "fp-session-1",
			Written:   42,
		},
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: false},
				{SessionID: "fp-session-1", Idle: true},
			},
		},
		screenResults: map[string][]*vtscreen.Snapshot{
			"fp-session-1": {
				{
					Rows:  24,
					Cols:  80,
					Lines: []string{"Agent response: found 3 files", "$ "},
					Cursor: vtscreen.Cursor{Row: 1, Col: 2, Visible: true},
				},
			},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		PollInterval: 10 * time.Millisecond, // Fast polling for tests
	})

	result, err := spawner.Ask(context.Background(), "fp-session-1", "What files match auth?", 5000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Reply == "" {
		t.Error("expected non-empty reply")
	}
	if result.Status != "replied" {
		t.Errorf("expected status replied, got %q", result.Status)
	}

	// Verify submit was called
	if len(fpClient.submitReqs) != 1 {
		t.Fatalf("expected 1 submit call, got %d", len(fpClient.submitReqs))
	}
	if fpClient.submitReqs[0].SessionID != "fp-session-1" {
		t.Errorf("expected session_id fp-session-1, got %q", fpClient.submitReqs[0].SessionID)
	}
	if fpClient.submitReqs[0].Text != "What files match auth?" {
		t.Errorf("expected text 'What files match auth?', got %q", fpClient.submitReqs[0].Text)
	}
}

func TestFoxproxSpawner_Ask_SubmitError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		submitErr: fmt.Errorf("session not found"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Ask(context.Background(), "fp-session-1", "test", 5000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("expected error containing 'session not found', got: %v", err)
	}
}

func TestFoxproxSpawner_Ask_ReadinessPollingError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		submitResp: &client.TerminalSubmitResponse{
			SessionID: "fp-session-1",
			Written:   5,
		},
		readinessErr: fmt.Errorf("readiness check failed"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		PollInterval: 10 * time.Millisecond,
	})

	_, err := spawner.Ask(context.Background(), "fp-session-1", "test", 1000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFoxproxSpawner_Ask_Timeout(t *testing.T) {
	fpClient := &mockFoxproxClient{
		submitResp: &client.TerminalSubmitResponse{
			SessionID: "fp-session-1",
			Written:   5,
		},
		readinessDefaultIdle: false, // Never becomes idle → timeout
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		PollInterval: 10 * time.Millisecond,
	})

	_, err := spawner.Ask(context.Background(), "fp-session-1", "test", 50) // 50ms timeout
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestFoxproxSpawner_Ask_ContextCancellation(t *testing.T) {
	fpClient := &mockFoxproxClient{
		submitResp: &client.TerminalSubmitResponse{
			SessionID: "fp-session-1",
			Written:   5,
		},
		readinessDefaultIdle: false, // Never idle → context cancellation kicks in
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		PollInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := spawner.Ask(ctx, "fp-session-1", "test", 30000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("expected context cancelled error, got: %v", err)
	}
}

func TestFoxproxSpawner_Ask_ScreenCaptureError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		submitResp: &client.TerminalSubmitResponse{
			SessionID: "fp-session-1",
			Written:   5,
		},
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: true},
			},
		},
		screenErr: fmt.Errorf("screen capture failed"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
		PollInterval: 10 * time.Millisecond,
	})

	_, err := spawner.Ask(context.Background(), "fp-session-1", "test", 5000)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFoxproxSpawner_Info_Running(t *testing.T) {
	fpClient := &mockFoxproxClient{
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: false, OutputRateBPS: 1500.0},
			},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Status != "running" {
		t.Errorf("expected status running, got %q", info.Status)
	}
}

func TestFoxproxSpawner_Info_Exited(t *testing.T) {
	fpClient := &mockFoxproxClient{
		readinessErr: fmt.Errorf("session not found"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.Status != "exited" {
		t.Errorf("expected status exited, got %q", info.Status)
	}
}

func TestFoxproxSpawner_Info_IdleSession(t *testing.T) {
	fpClient := &mockFoxproxClient{
		readinessResults: map[string][]*httpjson.ReadinessResponse{
			"fp-session-1": {
				{SessionID: "fp-session-1", Idle: true},
			},
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	info, err := spawner.Info(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Idle means the session is still alive, just idle
	if info.Status != "running" {
		t.Errorf("expected status running (idle), got %q", info.Status)
	}
}

func TestFoxproxSpawner_Kill_DeletesSession(t *testing.T) {
	fpClient := &mockFoxproxClient{}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	err := spawner.Kill(context.Background(), "fp-session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.deleteSessionIDs) != 1 {
		t.Fatalf("expected 1 delete call, got %d", len(fpClient.deleteSessionIDs))
	}
	if fpClient.deleteSessionIDs[0] != "fp-session-1" {
		t.Errorf("expected delete session fp-session-1, got %q", fpClient.deleteSessionIDs[0])
	}
}

func TestFoxproxSpawner_Kill_DeleteError(t *testing.T) {
	fpClient := &mockFoxproxClient{
		deleteSessionErr: fmt.Errorf("session already gone"),
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	err := spawner.Kill(context.Background(), "fp-session-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "session already gone") {
		t.Errorf("expected error containing 'session already gone', got: %v", err)
	}
}

func TestFoxproxSpawner_ImplementsAgentSpawnerInterface(t *testing.T) {
	// Compile-time interface compliance check
	var _ AgentSpawner = &foxproxAgentSpawner{}
}

func TestFoxproxSpawner_Spawn_CLICmdOverrideFromOpts(t *testing.T) {
	// Spawner default is "droid", but opts.CLICmd overrides to "claude"
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-override",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "coder", "Write code", AgentSpawnOptions{
		CLICmd: "claude",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "claude" {
		t.Errorf("expected cmd [claude] from opts override, got %v", req.Cmd)
	}
}

func TestFoxproxSpawner_Spawn_CLICmdDefaultWhenNoOverride(t *testing.T) {
	// Spawner default is "droid", and opts.CLICmd is empty → uses default
	fpClient := &mockFoxproxClient{
		createSessionResp: &httpjson.SessionResponse{
			ID:     "fp-session-default",
			Status: "running",
		},
	}

	spawner := NewFoxproxAgentSpawner(fpClient, FoxproxSpawnerConfig{
		CLICmd: "droid",
	})

	_, err := spawner.Spawn(context.Background(), "researcher", "Research", AgentSpawnOptions{
		CLICmd: "", // empty → use default
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(fpClient.createSessionReqs) != 1 {
		t.Fatalf("expected 1 CreateSession call, got %d", len(fpClient.createSessionReqs))
	}
	req := fpClient.createSessionReqs[0]
	if len(req.Cmd) != 1 || req.Cmd[0] != "droid" {
		t.Errorf("expected cmd [droid] from default, got %v", req.Cmd)
	}
}

func TestFoxproxSpawner_ScreenToString(t *testing.T) {
	tests := []struct {
		name     string
		snap     vtscreen.Snapshot
		expected string
	}{
		{
			name:     "empty screen",
			snap:     vtscreen.Snapshot{Rows: 24, Cols: 80, Lines: []string{}},
			expected: "",
		},
		{
			name:     "single line",
			snap:     vtscreen.Snapshot{Rows: 24, Cols: 80, Lines: []string{"Hello, world!"}},
			expected: "Hello, world!",
		},
		{
			name:     "multiple lines with trailing spaces",
			snap:     vtscreen.Snapshot{Rows: 24, Cols: 80, Lines: []string{"Line 1   ", "Line 2\t\t", "Line 3"}},
			expected: "Line 1\nLine 2\nLine 3",
		},
		{
			name:     "lines with empty strings after trim",
			snap:     vtscreen.Snapshot{Rows: 24, Cols: 80, Lines: []string{"Hello", "   ", "World", ""}},
			expected: "Hello\nWorld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := screenToString(tt.snap)
			if got != tt.expected {
				t.Errorf("screenToString() = %q, want %q", got, tt.expected)
			}
		})
	}
}
