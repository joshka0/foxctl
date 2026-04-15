package goruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentdomain "github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/v2/core/spawn"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
	runtimeworkers "github.com/joshka0/foxctl/internal/v2/runtime/workers"
)

func TestChildSpawner_SpawnChild_EmitsRunningAndCompleted(t *testing.T) {
	t.Parallel()

	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 16})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Run(ctx) }()

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher: state,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			return CommandSpec{
				Path: "/bin/sh",
				Args: []string{"-c", "exit 0"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	resp, err := spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-1",
		RunID:         "run-1",
		AgentID:       "agent:child-1",
		ActorID:       "actor:child-1",
		Role:          "worker",
		ParentAgentID: "agent:parent",
		Metadata:      map[string]any{"workspace_id": "ws-1"},
	})
	if err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}
	if resp.Status != "spawned" {
		t.Fatalf("status=%q want spawned", resp.Status)
	}

	record := waitForWorkerStatus(t, state, "subprocess:agent:child-1", coreworker.StatusCompleted)
	if record.AgentID != "agent:child-1" {
		t.Fatalf("agent_id=%q want agent:child-1", record.AgentID)
	}
	if record.ParentAgentID != "agent:parent" {
		t.Fatalf("parent_agent_id=%q want agent:parent", record.ParentAgentID)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("state.Run() error = %v", err)
	}
}

func TestChildSpawner_SpawnChild_EmitsFailedOnCommandError(t *testing.T) {
	t.Parallel()

	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 16})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Run(ctx) }()

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher: state,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			return CommandSpec{
				Path: "/bin/sh",
				Args: []string{"-c", "exit 7"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	if _, err := spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-2",
		RunID:         "run-2",
		AgentID:       "agent:child-2",
		Role:          "worker",
		ParentAgentID: "agent:parent",
	}); err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}

	record := waitForWorkerStatus(t, state, "subprocess:agent:child-2", coreworker.StatusFailed)
	if record.ExitCode != 7 {
		t.Fatalf("exit_code=%d want 7", record.ExitCode)
	}
	if record.StopReason == "" {
		t.Fatal("expected stop_reason")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("state.Run() error = %v", err)
	}
}

func TestChildSpawner_SpawnChild_EmitsRecentLogsInRawState(t *testing.T) {
	t.Parallel()

	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 32})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Run(ctx) }()

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher: state,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			return CommandSpec{
				Path: "/bin/sh",
				Args: []string{"-c", "echo hello-stdout; sleep 1; echo hello-stderr 1>&2; sleep 1; exit 0"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	if _, err := spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-logs-1",
		RunID:         "run-logs-1",
		AgentID:       "agent:logs-1",
		Role:          "worker",
		ParentAgentID: "agent:parent",
	}); err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}

	recentLogs := waitForRecentLogs(t, state, "subprocess:agent:logs-1", 1)
	if !recentLogsContainAnyText(recentLogs, "hello-stdout", "hello-stderr") {
		t.Fatalf("recent_logs=%v want at least one expected log entry", recentLogs)
	}
	_ = waitForWorkerStatus(t, state, "subprocess:agent:logs-1", coreworker.StatusCompleted)

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("state.Run() error = %v", err)
	}
}

func TestChildSpawner_SpawnChild_CanBeCancelledViaSignaler(t *testing.T) {
	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 32})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Run(ctx) }()

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher: state,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			return CommandSpec{
				Path: "/bin/sh",
				Args: []string{"-c", "sleep 5"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	if _, err := spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-cancel-1",
		RunID:         "run-cancel-1",
		AgentID:       "agent:cancel-1",
		Role:          "worker",
		ParentAgentID: "agent:parent",
	}); err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}

	_ = waitForWorkerStatus(t, state, "subprocess:agent:cancel-1", coreworker.StatusRunning)
	signaler := NewSignaler(SignalerConfig{Publisher: state})
	resp, err := signaler.SignalWorker(context.Background(), coreworker.SignalRequest{
		AgentID:   "agent:cancel-1",
		RequestID: "signal-cancel-1",
		Signal:    "terminate",
		Reason:    "test cancellation",
	})
	if err != nil {
		t.Fatalf("SignalWorker() error = %v", err)
	}
	if resp.Status != coreworker.StatusStopping {
		t.Fatalf("signal status=%q want %q", resp.Status, coreworker.StatusStopping)
	}

	record := waitForWorkerStatus(t, state, "subprocess:agent:cancel-1", coreworker.StatusCancelled)
	if record.StopReason != "test cancellation" {
		t.Fatalf("stop_reason=%q want test cancellation", record.StopReason)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("state.Run() error = %v", err)
	}
}

func TestChildSpawner_SpawnChild_EmitsHeartbeatWhileRunning(t *testing.T) {
	t.Parallel()

	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 64})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Run(ctx) }()

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher:         state,
		HeartbeatInterval: 10 * time.Millisecond,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			return CommandSpec{
				Path: "/bin/sh",
				Args: []string{"-c", "sleep 0.2"},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	if _, err := spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-heartbeat-1",
		RunID:         "run-heartbeat-1",
		AgentID:       "agent:heartbeat-1",
		Role:          "worker",
		ParentAgentID: "agent:parent",
		Metadata:      map[string]any{"workspace_id": "ws-heartbeat"},
	}); err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}

	record := waitForHeartbeat(t, state, "subprocess:agent:heartbeat-1")
	if record.WorkspaceID != "ws-heartbeat" {
		t.Fatalf("workspace_id=%q want ws-heartbeat", record.WorkspaceID)
	}
	if record.HeartbeatAt.IsZero() {
		t.Fatal("expected heartbeat_at to be populated")
	}
	if record.UpdatedAt.Before(record.HeartbeatAt) {
		// okay; UpdatedAt should track the latest event, including heartbeats.
	} else if !record.UpdatedAt.Equal(record.HeartbeatAt) {
		t.Fatalf("updated_at=%s want latest heartbeat timestamp %s", record.UpdatedAt, record.HeartbeatAt)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("state.Run() error = %v", err)
	}
}

func TestChildSpawner_SpawnChild_FailsWithoutParent(t *testing.T) {
	t.Parallel()

	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 4})
	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher: state,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			return CommandSpec{Path: "/bin/sh", Args: []string{"-c", "exit 0"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}

	if _, err := spawner.SpawnChild(context.Background(), spawn.Request{AgentID: "agent:x", Role: "worker"}); err == nil {
		t.Fatal("expected missing parent_agent_id error")
	}
}

func TestManagedAgentSpawner_CreatesAgentRecordAndLaunchesProcess(t *testing.T) {
	t.Parallel()

	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	state := runtimeworkers.NewStateComponent(runtimeworkers.Config{Buffer: 16})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- state.Run(ctx) }()

	spawner, err := NewManagedAgentSpawner(ManagedAgentSpawnerConfig{
		StorageRoot:   storageRoot,
		WorkspaceRoot: workspaceRoot,
		Publisher:     state,
		BuildCommand: func(record agentdomain.Agent, req spawn.Request) (CommandSpec, error) {
			if record.ID != "agent:managed-1" {
				t.Fatalf("record.ID=%q want agent:managed-1", record.ID)
			}
			return CommandSpec{
				Path: "/bin/sh",
				Args: []string{"-c", "exit 0"},
				Dir:  workspaceRoot,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewManagedAgentSpawner() error = %v", err)
	}

	_, err = spawner.SpawnChild(context.Background(), spawn.Request{
		RequestID:     "req-managed-1",
		RunID:         "run-managed-1",
		AgentID:       "agent:managed-1",
		Role:          "coder",
		Prompt:        "Review the runtime tree",
		ParentAgentID: "agent:parent",
	})
	if err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}

	record := waitForWorkerStatus(t, state, "subprocess:agent:managed-1", coreworker.StatusCompleted)
	if record.AgentID != "agent:managed-1" {
		t.Fatalf("worker agent_id=%q want agent:managed-1", record.AgentID)
	}

	store, err := agents.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("agents.Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	agentRecord, err := store.Get(context.Background(), "agent:managed-1")
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}
	if agentRecord.ExecutionLayer != agentdomain.ExecutionLayerClassic {
		t.Fatalf("execution_layer=%q want %q", agentRecord.ExecutionLayer, agentdomain.ExecutionLayerClassic)
	}
	if agentRecord.Namespace != "agent:parent/child-agent:managed-1" {
		t.Fatalf("namespace=%q", agentRecord.Namespace)
	}
	if agentRecord.WorkspaceRoot != workspaceRoot {
		t.Fatalf("workspace_root=%q want %q", agentRecord.WorkspaceRoot, workspaceRoot)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("state.Run() error = %v", err)
	}
}

func TestDefaultAgentRunCommandBuilder_UsesFoxctlRun(t *testing.T) {
	t.Parallel()

	builder := defaultAgentRunCommandBuilder("/tmp/foxctl-bin", "/tmp/workspace")
	spec, err := builder(agentdomain.Agent{
		ID:            "agent:run-1",
		WorkspaceRoot: "/tmp/workspace",
	}, spawn.Request{})
	if err != nil {
		t.Fatalf("builder() error = %v", err)
	}
	if spec.Path != "/tmp/foxctl-bin" {
		t.Fatalf("path=%q want /tmp/foxctl-bin", spec.Path)
	}
	if len(spec.Args) != 3 || spec.Args[0] != "agent" || spec.Args[1] != "run" || spec.Args[2] != "agent:run-1" {
		t.Fatalf("args=%v", spec.Args)
	}
	if spec.Dir != filepath.Clean("/tmp/workspace") {
		t.Fatalf("dir=%q want /tmp/workspace", spec.Dir)
	}
}

func TestFilteredFoxctlEnv_RemovesJidoVars(t *testing.T) {
	t.Parallel()

	env := []string{
		"PATH=/usr/bin",
		"FOXCTL_JIDO_SOCKET=/tmp/jido.sock",
		"FOXCTL_JIDO_RPC_PATH=/rpc",
		"FOXCTL_V2_ASK_DISPATCHER=jido",
		"HOME=/tmp/home",
	}
	filtered := filteredFoxctlEnv(env)
	got := map[string]bool{}
	for _, kv := range filtered {
		got[kv] = true
	}
	if got["FOXCTL_JIDO_SOCKET=/tmp/jido.sock"] || got["FOXCTL_JIDO_RPC_PATH=/rpc"] || got["FOXCTL_V2_ASK_DISPATCHER=jido"] {
		t.Fatalf("filtered env still contains jido vars: %v", filtered)
	}
	if !got["PATH=/usr/bin"] || !got["HOME=/tmp/home"] {
		t.Fatalf("filtered env dropped non-jido vars: %v", filtered)
	}
}

func TestEnsureAgentRecord_ReusesExistingRecord(t *testing.T) {
	t.Parallel()

	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	now := time.Date(2026, time.April, 6, 18, 0, 0, 0, time.UTC)
	store, err := agents.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("agents.Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Create(context.Background(), agentdomain.Agent{
		ID:              "agent:existing-1",
		Namespace:       "existing-ns",
		WorkspaceRoot:   workspaceRoot,
		WorkspaceSource: "local",
		Role:            "reviewer",
		Prompt:          "existing prompt",
		ShareBB:         "scoped",
		State:           agentdomain.StateRunning,
		CreatedAt:       now,
		HeartbeatAt:     now,
		ExecMode:        agentdomain.ModeReactive,
		ExecutionLayer:  agentdomain.ExecutionLayerClassic,
		MaxIterations:   10,
		MaxAutoTurns:    1,
		ThinkInterval:   60,
		MemoryScope:     agentdomain.MemoryScopeAgent,
		MemoryRetention: agentdomain.MemoryRetentionDurable,
	}); err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}

	record, err := ensureAgentRecord(context.Background(), storageRoot, workspaceRoot, func() time.Time { return now }, spawn.Request{
		AgentID:       "agent:existing-1",
		Role:          "coder",
		ParentAgentID: "agent:parent",
	})
	if err != nil {
		t.Fatalf("ensureAgentRecord() error = %v", err)
	}
	if record.Namespace != "existing-ns" {
		t.Fatalf("namespace=%q want existing-ns", record.Namespace)
	}
	if record.Role != "reviewer" {
		t.Fatalf("role=%q want reviewer", record.Role)
	}
}

func waitForWorkerStatus(t *testing.T, state *runtimeworkers.StateComponent, workerID string, want coreworker.Status) coreworker.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := state.Snapshot()
		record, ok := snapshot.Workers[workerID]
		if ok && record.Status == want {
			return record
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker %q did not reach status %q", workerID, want)
	return coreworker.Record{}
}

func waitForRecentLogs(t *testing.T, state *runtimeworkers.StateComponent, workerID string, minCount int) []any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := state.Snapshot()
		record, ok := snapshot.Workers[workerID]
		if ok && len(record.RawState) > 0 {
			var raw map[string]any
			if err := json.Unmarshal(record.RawState, &raw); err == nil {
				foxctlState, _ := raw["foxctl"].(map[string]any)
				recentLogs, _ := foxctlState["recent_logs"].([]any)
				if len(recentLogs) >= minCount {
					return recentLogs
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker %q did not accumulate %d recent log entries", workerID, minCount)
	return nil
}

func waitForHeartbeat(t *testing.T, state *runtimeworkers.StateComponent, workerID string) coreworker.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := state.Snapshot()
		record, ok := snapshot.Workers[workerID]
		if ok && !record.HeartbeatAt.IsZero() {
			return record
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("worker %q did not emit heartbeat", workerID)
	return coreworker.Record{}
}

func recentLogsContainAnyText(recentLogs []any, wantTexts ...string) bool {
	for _, entry := range recentLogs {
		record, _ := entry.(map[string]any)
		text := strings.TrimSpace(fmt.Sprint(record["text"]))
		for _, want := range wantTexts {
			if text == want {
				return true
			}
		}
	}
	return false
}
