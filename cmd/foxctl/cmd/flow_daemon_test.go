package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/daemon"
	flowmodel "github.com/joshka0/foxctl/internal/runtime/flow"
)

// ---------------------------------------------------------------------------
// Mock daemon client for testing
// ---------------------------------------------------------------------------

// mockFlowDaemonClient is a mock flowDaemonClient for testing.
type mockFlowDaemonClient struct {
	running             bool
	autoStartErr        error
	flowStartFn         func(flowID, workspace string) (*daemon.FlowStartResult, error)
	flowStopFn          func(flowID, workspace string) (*daemon.FlowStopResult, error)
	flowPauseFn         func(flowID, workspace string) (*daemon.FlowPauseResult, error)
	flowStatusFn        func(flowID, workspace string) (*daemon.FlowStatusResult, error)
	flowOutputFn        func(flowID, runID, nodeID string, data json.RawMessage, workspace string) (*daemon.FlowOutputResult, error)
	ensureRunningCalled bool
}

func (m *mockFlowDaemonClient) IsRunning() bool {
	return m.running
}

func (m *mockFlowDaemonClient) EnsureRunning() error {
	m.ensureRunningCalled = true
	if m.autoStartErr != nil {
		return m.autoStartErr
	}
	m.running = true
	return nil
}

func (m *mockFlowDaemonClient) FlowStart(flowID, workspace string) (*daemon.FlowStartResult, error) {
	if m.flowStartFn != nil {
		return m.flowStartFn(flowID, workspace)
	}
	return &daemon.FlowStartResult{
		FlowID: flowID,
		RunID:  "run-mock-001",
		State:  "running",
	}, nil
}

func (m *mockFlowDaemonClient) FlowStop(flowID, workspace string) (*daemon.FlowStopResult, error) {
	if m.flowStopFn != nil {
		return m.flowStopFn(flowID, workspace)
	}
	return &daemon.FlowStopResult{
		FlowID: flowID,
		State:  "stopped",
	}, nil
}

func (m *mockFlowDaemonClient) FlowPause(flowID, workspace string) (*daemon.FlowPauseResult, error) {
	if m.flowPauseFn != nil {
		return m.flowPauseFn(flowID, workspace)
	}
	return &daemon.FlowPauseResult{
		FlowID: flowID,
		State:  "paused",
	}, nil
}

func (m *mockFlowDaemonClient) FlowStatus(flowID, workspace string) (*daemon.FlowStatusResult, error) {
	if m.flowStatusFn != nil {
		return m.flowStatusFn(flowID, workspace)
	}
	return &daemon.FlowStatusResult{
		FlowID: flowID,
		State:  "running",
		RunID:  "run-mock-001",
	}, nil
}

func (m *mockFlowDaemonClient) FlowOutput(flowID, runID, nodeID string, data json.RawMessage, workspace string) (*daemon.FlowOutputResult, error) {
	if m.flowOutputFn != nil {
		return m.flowOutputFn(flowID, runID, nodeID, data, workspace)
	}
	return &daemon.FlowOutputResult{
		FlowID: flowID,
		NodeID: nodeID,
		RunID:  "run-mock-001",
		OK:     true,
	}, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupFlowDaemonTest sets up the test environment with mock daemon client.
// Returns a cleanup function that must be deferred.
func setupFlowDaemonTest(t *testing.T) (*mockFlowDaemonClient, func()) {
	t.Helper()

	// Save original values
	origClient := newFlowDaemonClient
	origAutoStart := flowDaemonAutoStart
	origExecutors := flowEngineRegistry.testExecutors

	mock := &mockFlowDaemonClient{
		flowStartFn: func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			return &daemon.FlowStartResult{
				FlowID: flowID,
				RunID:  "run-mock-001",
				State:  "running",
			}, nil
		},
	}

	newFlowDaemonClient = func() flowDaemonClient {
		return mock
	}

	// Also install mock executors for in-process fallback tests.
	flowEngineRegistry.mu.Lock()
	flowEngineRegistry.testExecutors = map[flowmodel.NodeKind]flowmodel.NodeExecutor{
		flowmodel.NodeSkill:     &mockCLIExecutor{},
		flowmodel.NodeTransform: &mockCLIExecutor{},
	}
	flowEngineRegistry.mu.Unlock()

	cleanup := func() {
		newFlowDaemonClient = origClient
		flowDaemonAutoStart = origAutoStart
		flowEngineRegistry.mu.Lock()
		flowEngineRegistry.testExecutors = origExecutors
		for id := range flowEngineRegistry.engines {
			removeEngine(id)
		}
		flowEngineRegistry.mu.Unlock()
	}

	return mock, cleanup
}

// ---------------------------------------------------------------------------
// Tests: daemon routing — start
// ---------------------------------------------------------------------------

func TestFlowStart_DaemonRouting(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("routes through daemon when available", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		// Create a flow first.
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "daemon-start", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)

		// Add a node so it's a valid flow.
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Start via daemon.
		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		data := env.Data.(map[string]any)
		if data["run_id"] != "run-mock-001" {
			t.Errorf("expected run_id from daemon 'run-mock-001', got %v", data["run_id"])
		}
		if data["state"] != "running" {
			t.Errorf("expected state 'running', got %v", data["state"])
		}
	})

	t.Run("resolves name and routes through daemon", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		// Create a flow with a name.
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "named-flow", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)

		// Add a node.
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Start by name (not ID).
		stdout, _ = executeFlowCommand(t, "flow", "start", "named-flow", "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		data := env.Data.(map[string]any)
		if data["id"] != flowID {
			t.Errorf("expected flow ID %q, got %v", flowID, data["id"])
		}
	})

	t.Run("auto-starts daemon when not running", func(t *testing.T) {
		mock.running = false // Daemon not running initially.
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "auto-start-flow", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		if !mock.ensureRunningCalled {
			t.Error("expected EnsureRunning to be called")
		}
	})

	t.Run("falls back to in-process when auto-start fails", func(t *testing.T) {
		mock.running = false
		mock.autoStartErr = fmt.Errorf("auto-start failed: permission denied")
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "fallback-flow", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Should succeed via in-process fallback because auto-start failed.
		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		data := env.Data.(map[string]any)
		if data["state"] != "running" {
			t.Errorf("expected state 'running' via fallback, got %v", data["state"])
		}

		// Clean up in-process engine.
		removeEngine(flowID)
	})

	t.Run("falls back to in-process when auto-started daemon returns ENOTFOUND", func(t *testing.T) {
		mock.running = false // Start not running — will auto-start.
		// After auto-start, mock.running = true (set by EnsureRunning).
		// Then FlowStart returns ENOTFOUND — should fall back.
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			return nil, fmt.Errorf("ENOTFOUND: flow not found")
		}
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "autostart-notfound", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Should succeed via in-process fallback because daemon returned ENOTFOUND
		// for an auto-started daemon (workspace not configured).
		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		data := env.Data.(map[string]any)
		if data["state"] != "running" {
			t.Errorf("expected state 'running' via fallback, got %v", data["state"])
		}

		// Clean up in-process engine.
		removeEngine(flowID)
		// Reset mock.
		mock.flowStartFn = nil
	})

	t.Run("falls back when daemon connection lost", func(t *testing.T) {
		mock.running = true
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			return nil, fmt.Errorf("connect to daemon: connection refused")
		}
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "conn-lost", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Should succeed via in-process fallback.
		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		data := env.Data.(map[string]any)
		if data["state"] != "running" {
			t.Errorf("expected state 'running' via fallback, got %v", data["state"])
		}

		removeEngine(flowID)
		// Reset mock.
		mock.flowStartFn = nil
	})

	t.Run("returns daemon error envelope for flow errors (non-ENOTFOUND)", func(t *testing.T) {
		mock.running = true
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			return nil, fmt.Errorf("EALREADY: flow already running")
		}

		ws := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "already-r", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/start")
		if code != string(protocol.ErrorCodeEARG) {
			t.Errorf("expected EARG for already running, got %q", code)
		}

		// Reset mock.
		mock.flowStartFn = nil
	})

	t.Run("ENOTFOUND from daemon falls back to in-process", func(t *testing.T) {
		mock.running = true
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			return nil, fmt.Errorf("ENOTFOUND: flow not found")
		}

		ws := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "enotfound-fallback", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Should fall back to in-process since daemon returns ENOTFOUND.
		stdout, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/start")

		data := env.Data.(map[string]any)
		if data["state"] != "running" {
			t.Errorf("expected state 'running' via fallback, got %v", data["state"])
		}

		removeEngine(flowID)
		// Reset mock.
		mock.flowStartFn = nil
	})

	t.Run("workspace flag propagated to daemon", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		var capturedWorkspace string
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			capturedWorkspace = workspace
			return &daemon.FlowStartResult{
				FlowID: flowID,
				RunID:  "run-ws-test",
				State:  "running",
			}, nil
		}

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "ws-test", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		_, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)

		if capturedWorkspace != ws && capturedWorkspace != flowResolveWorkspace(ws) {
			t.Errorf("expected workspace to be propagated, got %q", capturedWorkspace)
		}

		// Reset mock.
		mock.flowStartFn = nil
	})
}

// ---------------------------------------------------------------------------
// Tests: daemon routing — stop
// ---------------------------------------------------------------------------

func TestFlowStop_DaemonRouting(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("routes through daemon when available", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "stop-daemon", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "stop", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/stop")

		data := env.Data.(map[string]any)
		if data["stopped"] != true {
			t.Errorf("expected stopped=true, got %v", data["stopped"])
		}
	})

	t.Run("resolves name and routes stop", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		_, _ = executeFlowCommand(t, "flow", "create", "--name", "stop-named", "--workspace", ws)

		stdout, _ := executeFlowCommand(t, "flow", "stop", "stop-named", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/stop")
	})

	t.Run("falls back to in-process when daemon unavailable", func(t *testing.T) {
		mock.running = false
		mock.autoStartErr = fmt.Errorf("auto-start failed")
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "stop-fallback", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)
		_, _ = executeFlowCommand(t, "flow", "start", flowID, "--workspace", ws)

		stdout, _ = executeFlowCommand(t, "flow", "stop", flowID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/stop")

		data := env.Data.(map[string]any)
		if data["stopped"] != true {
			t.Errorf("expected stopped=true via fallback, got %v", data["stopped"])
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: daemon routing — pause
// ---------------------------------------------------------------------------

func TestFlowPause_DaemonRouting(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("routes through daemon when available", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "pause-daemon", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "pause", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/pause")

		data := env.Data.(map[string]any)
		if data["paused"] != true {
			t.Errorf("expected paused=true, got %v", data["paused"])
		}
	})

	t.Run("resolves name and routes pause", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		_, _ = executeFlowCommand(t, "flow", "create", "--name", "pause-named", "--workspace", ws)

		stdout, _ := executeFlowCommand(t, "flow", "pause", "pause-named", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/pause")
	})
}

// ---------------------------------------------------------------------------
// Tests: daemon routing — status
// ---------------------------------------------------------------------------

func TestFlowStatus_DaemonRouting(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("routes through daemon when available", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "status-daemon", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flowID := env.Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "status", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/status")

		data := env.Data.(map[string]any)
		if data["flow_state"] != "running" {
			t.Errorf("expected flow_state 'running' from daemon, got %v", data["flow_state"])
		}
	})

	t.Run("resolves name and routes status", func(t *testing.T) {
		mock.running = true
		ws := tempWorkspace(t)

		_, _ = executeFlowCommand(t, "flow", "create", "--name", "status-named", "--workspace", ws)

		stdout, _ := executeFlowCommand(t, "flow", "status", "status-named", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/status")
	})

	t.Run("falls back to in-process when daemon unavailable", func(t *testing.T) {
		mock.running = false
		mock.autoStartErr = fmt.Errorf("auto-start failed")
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "status-fallback", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{}`, "--workspace", ws)

		stdout, _ = executeFlowCommand(t, "flow", "status", flowID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/status")

		data := env.Data.(map[string]any)
		if data["flow_state"] != "draft" {
			t.Errorf("expected flow_state 'draft' from fallback, got %v", data["flow_state"])
		}
	})
}

// ---------------------------------------------------------------------------
// Tests: stderr messages
// ---------------------------------------------------------------------------

func TestFlowDaemonStderrMessages(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("auto-start prints progress to stderr", func(t *testing.T) {
		mock.running = false // Start not running.
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "stderr-test", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Capture stderr.
		var stderr bytes.Buffer
		root := rootCmd
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&stderr)
		root.SetArgs([]string{"flow", "start", flowID, "--workspace", ws})
		_ = root.Execute()

		stderrStr := stderr.String()
		if !strings.Contains(stderrStr, "daemon not running") {
			t.Errorf("expected 'daemon not running' in stderr, got: %q", stderrStr)
		}
		if !strings.Contains(stderrStr, "daemon started") {
			t.Errorf("expected 'daemon started' in stderr, got: %q", stderrStr)
		}
	})

	t.Run("fallback prints message to stderr", func(t *testing.T) {
		mock.running = false
		mock.autoStartErr = fmt.Errorf("auto-start failed: no permission")
		flowDaemonAutoStart = true
		ws := tempWorkspace(t)

		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "fallback-stderr", "--workspace", ws)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)
		_, _ = executeFlowCommand(t, "flow", "add-node", flowID, "--label", "src", "--kind", "skill",
			"--config", `{"skill":"test"}`, "--workspace", ws)

		// Capture stderr.
		var stderr bytes.Buffer
		root := rootCmd
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&stderr)
		root.SetArgs([]string{"flow", "start", flowID, "--workspace", ws})
		_ = root.Execute()

		stderrStr := stderr.String()
		if !strings.Contains(stderrStr, "auto-start failed") {
			t.Errorf("expected 'auto-start failed' in stderr, got: %q", stderrStr)
		}
		if !strings.Contains(stderrStr, "in-process execution") {
			t.Errorf("expected 'in-process execution' in stderr, got: %q", stderrStr)
		}

		// Clean up.
		removeEngine(flowID)
	})
}

// ---------------------------------------------------------------------------
// Tests: isULID helper
// ---------------------------------------------------------------------------

func TestIsULID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"01HGWYNB5R0000000000000001", true},
		{"01HGWYNB5RABCDEF0000000000", true},
		{"", false},
		{"short", false},
		{"01HGWYNB5R000000000000000", false},   // 25 chars
		{"01HGWYNB5R00000000000000012", false}, // 27 chars
		{"01HGWYNB5R00000000000000ab", false},  // lowercase
		{"my-flow-name", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isULID(tt.input)
			if got != tt.want {
				t.Errorf("isULID(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: CRUD commands still work without daemon (backward compatibility)
// ---------------------------------------------------------------------------

func TestFlowCRUD_NoDaemon(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	// Daemon is NOT running and auto-start will fail.
	mock.running = false
	mock.autoStartErr = fmt.Errorf("auto-start failed")
	flowDaemonAutoStart = true

	ws := tempWorkspace(t)

	t.Run("create works without daemon", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "crud-no-daemon", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/create")
	})

	t.Run("list works without daemon", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "list", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/list")
	})

	t.Run("show works without daemon", func(t *testing.T) {
		stdout, _ := executeFlowCommand(t, "flow", "list", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		flows := env.Data.(map[string]any)["flows"].([]any)
		if len(flows) == 0 {
			t.Fatal("expected at least one flow")
		}
		flowID := flows[0].(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "show", flowID, "--workspace", ws)
		env = parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/show")
	})

	t.Run("delete works without daemon", func(t *testing.T) {
		delWs := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "del-no-daemon", "--workspace", delWs)
		flowID := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "delete", flowID, "--workspace", delWs)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/delete")
	})
}

// ---------------------------------------------------------------------------
// Tests: daemon connection error has helpful message
// ---------------------------------------------------------------------------

func TestFlowDaemonConnectionError(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("daemon RPC error for non-ENOTFOUND produces actionable error envelope", func(t *testing.T) {
		mock.running = true
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			return nil, fmt.Errorf("EFLOW: flow engine not initialized")
		}

		ws := tempWorkspace(t)
		// Use a ULID-like ID that the daemon would recognize.
		stdout, _ := executeFlowCommand(t, "flow", "start", "01HGWYNB5R0000000000000001", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/start")
		if code != string(protocol.ErrorCodeESkillDown) {
			t.Errorf("expected ESKILLDOWN for EFLOW, got %q", code)
		}
		if !strings.Contains(env.Error.Message, "not initialized") {
			t.Errorf("expected 'not initialized' in error message, got %q", env.Error.Message)
		}

		// Reset mock.
		mock.flowStartFn = nil
	})
}

// ---------------------------------------------------------------------------
// Tests: multi-workspace support via daemon
// ---------------------------------------------------------------------------

func TestFlowDaemonMultiWorkspace(t *testing.T) {
	mock, cleanup := setupFlowDaemonTest(t)
	defer cleanup()

	t.Run("different workspaces routed independently", func(t *testing.T) {
		mock.running = true
		wsA := tempWorkspace(t)
		wsB := tempWorkspace(t)

		var capturedWS []string
		mock.flowStartFn = func(flowID, workspace string) (*daemon.FlowStartResult, error) {
			capturedWS = append(capturedWS, workspace)
			return &daemon.FlowStartResult{
				FlowID: flowID,
				RunID:  "run-ws-" + flowID[:8],
				State:  "running",
			}, nil
		}

		// Create flows in both workspaces.
		stdout, _ := executeFlowCommand(t, "flow", "create", "--name", "ws-a-flow", "--workspace", wsA)
		flowA := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		stdout, _ = executeFlowCommand(t, "flow", "create", "--name", "ws-b-flow", "--workspace", wsB)
		flowB := parseEnvelope(t, stdout).Data.(map[string]any)["id"].(string)

		// Start both.
		_, _ = executeFlowCommand(t, "flow", "start", flowA, "--workspace", wsA)
		_, _ = executeFlowCommand(t, "flow", "start", flowB, "--workspace", wsB)

		if len(capturedWS) != 2 {
			t.Fatalf("expected 2 daemon calls, got %d", len(capturedWS))
		}

		// Verify workspaces are different.
		if capturedWS[0] == capturedWS[1] {
			t.Errorf("expected different workspaces, got same: %q", capturedWS[0])
		}

		// Reset mock.
		mock.flowStartFn = nil
	})
}

// Ensure required imports are used.
var (
	_ = json.Marshal
	_ = strings.Contains
	_ = bytes.Buffer{}
	_ = envelope.Version
	_ = protocol.ErrorCodeEARG
	_ = daemon.FlowStartResult{}
	_ = flowmodel.FlowDraft
	_ = fmt.Sprintf
)
