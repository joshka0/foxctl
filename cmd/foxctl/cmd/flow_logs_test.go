package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	flowmodel "github.com/joshka0/foxctl/internal/runtime/flow"
)

// ---------------------------------------------------------------------------
// Test: flow logs — basic invocation
// ---------------------------------------------------------------------------

func TestFlowLogs_BasicInvocation(t *testing.T) {
	origAutoStart := flowDaemonAutoStart
	flowDaemonAutoStart = false
	defer func() { flowDaemonAutoStart = origAutoStart }()

	t.Run("returns envelope with data.logs array", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}

		// Create flow + node + completed run with logs.
		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "logs-test-flow",
			Name:      "logs-test",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		n, err := store.AddNode(ctx, flowmodel.FlowNode{
			ID:     "logs-node-1",
			FlowID: f.ID,
			Kind:   flowmodel.NodeSkill,
			Label:  "src",
			Config: json.RawMessage(`{"skill":"test"}`),
		})
		if err != nil {
			t.Fatal(err)
		}

		now := time.Now().UTC()
		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID:        "logs-test-run",
			FlowID:    f.ID,
			State:     flowmodel.RunCompleted,
			StartedAt: now.Add(-time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		completed := now
		run.CompletedAt = &completed
		run, err = store.UpdateRun(ctx, run)
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID:    run.ID,
			NodeID:   n.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{"result":"ok","node":"` + n.ID + `"},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})
		if err != nil {
			t.Fatal(err)
		}

		store.Close()

		stdout, _ := executeFlowCommand(t, "flow", "logs", run.ID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/logs")

		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("expected data to be a map, got %T", env.Data)
		}
		logs, ok := data["logs"].([]any)
		if !ok {
			t.Fatalf("expected data.logs to be an array, got %T", data["logs"])
		}
		if len(logs) == 0 {
			t.Error("expected at least one log entry")
		}

		// Verify log entry fields.
		for _, entry := range logs {
			logEntry, ok := entry.(map[string]any)
			if !ok {
				t.Fatalf("expected log entry to be a map, got %T", entry)
			}
			if logEntry["seq"] == nil {
				t.Error("expected seq to be set")
			}
			if logEntry["node_id"] == nil || logEntry["node_id"] == "" {
				t.Error("expected node_id to be set")
			}
			if logEntry["envelope"] == nil {
				t.Error("expected envelope to be set")
			}
			if logEntry["created_at"] == nil || logEntry["created_at"] == "" {
				t.Error("expected created_at to be set")
			}
		}
	})

	t.Run("logs ordered by seq ascending", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}

		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "seq-test-flow",
			Name:      "seq-test",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		n1, _ := store.AddNode(ctx, flowmodel.FlowNode{
			ID: "seq-node-1", FlowID: f.ID, Kind: flowmodel.NodeSkill, Label: "src",
			Config: json.RawMessage(`{"skill":"test"}`),
		})
		n2, _ := store.AddNode(ctx, flowmodel.FlowNode{
			ID: "seq-node-2", FlowID: f.ID, Kind: flowmodel.NodeSkill, Label: "sink",
			Config: json.RawMessage(`{"skill":"test"}`),
		})

		now := time.Now().UTC()
		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID: "seq-test-run", FlowID: f.ID, State: flowmodel.RunRunning, StartedAt: now.Add(-time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}

		// Write multiple logs.
		_, _ = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID: run.ID, NodeID: n1.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})
		_, _ = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID: run.ID, NodeID: n2.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})

		// Complete the run.
		completed := now
		run.State = flowmodel.RunCompleted
		run.CompletedAt = &completed
		run, _ = store.UpdateRun(ctx, run)

		store.Close()

		stdout, _ := executeFlowCommand(t, "flow", "logs", run.ID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		data := env.Data.(map[string]any)
		logs := data["logs"].([]any)

		if len(logs) < 2 {
			t.Fatalf("expected at least 2 log entries, got %d", len(logs))
		}

		// Verify seq is ascending.
		var lastSeq float64
		for i, entry := range logs {
			seq := entry.(map[string]any)["seq"].(float64)
			if i > 0 && seq <= lastSeq {
				t.Errorf("seq not ascending: entry %d has seq %v, previous %v", i, seq, lastSeq)
			}
			lastSeq = seq
		}
	})

	t.Run("ENOTFOUND for nonexistent run", func(t *testing.T) {
		ws := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "logs", "nonexistent-run-id", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/logs")
		if code != "ENOTFOUND" {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("empty logs returns ok with empty array", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		// Create a real flow first (FK constraint requires it).
		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "empty-log-flow",
			Name:      "empty-log-flow",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID:        "empty-run-001",
			FlowID:    f.ID,
			State:     flowmodel.RunCompleted,
			StartedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		stdout, _ := executeFlowCommand(t, "flow", "logs", run.ID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/logs")

		data := env.Data.(map[string]any)
		logs, ok := data["logs"].([]any)
		if !ok {
			t.Fatalf("expected logs to be array, got %T", data["logs"])
		}
		if len(logs) != 0 {
			t.Errorf("expected empty logs, got %d entries", len(logs))
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow logs --node filter
// ---------------------------------------------------------------------------

func TestFlowLogs_NodeFilter(t *testing.T) {
	origAutoStart := flowDaemonAutoStart
	flowDaemonAutoStart = false
	defer func() { flowDaemonAutoStart = origAutoStart }()

	t.Run("filters by node ID", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}

		f, _ := store.CreateFlow(ctx, flowmodel.Flow{
			ID: "filter-flow", Name: "filter-test", Workspace: ws,
			State: flowmodel.FlowStopped, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
		n1, _ := store.AddNode(ctx, flowmodel.FlowNode{
			ID: "filter-node-1", FlowID: f.ID, Kind: flowmodel.NodeSkill, Label: "src",
			Config: json.RawMessage(`{"skill":"test"}`),
		})
		n2, _ := store.AddNode(ctx, flowmodel.FlowNode{
			ID: "filter-node-2", FlowID: f.ID, Kind: flowmodel.NodeSkill, Label: "sink",
			Config: json.RawMessage(`{"skill":"test"}`),
		})

		now := time.Now().UTC()
		run, _ := store.CreateRun(ctx, flowmodel.FlowRun{
			ID: "filter-run", FlowID: f.ID, State: flowmodel.RunRunning, StartedAt: now,
		})

		// Write logs from both nodes.
		_, _ = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID: run.ID, NodeID: n1.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{"node":"src"},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})
		_, _ = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID: run.ID, NodeID: n2.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{"node":"sink"},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})

		completed := now
		run.State = flowmodel.RunCompleted
		run.CompletedAt = &completed
		run, _ = store.UpdateRun(ctx, run)
		store.Close()

		// Filter by source node ID.
		stdout, _ := executeFlowCommand(t, "flow", "logs", run.ID, "--node", n1.ID, "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/logs")

		data := env.Data.(map[string]any)
		logs := data["logs"].([]any)
		for _, entry := range logs {
			logEntry := entry.(map[string]any)
			if logEntry["node_id"] != n1.ID {
				t.Errorf("expected node_id=%s, got %v", n1.ID, logEntry["node_id"])
			}
		}
	})

	t.Run("node filter on nonexistent node returns empty ok", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		// Create a real flow first.
		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "filter-empty-flow",
			Name:      "filter-empty-flow",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID:        "filter-empty-run",
			FlowID:    f.ID,
			State:     flowmodel.RunCompleted,
			StartedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		stdout, _ := executeFlowCommand(t, "flow", "logs", run.ID, "--node", "nonexistent-node", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		assertValidOKEnvelope(t, env, "flow/logs")

		data := env.Data.(map[string]any)
		logs := data["logs"].([]any)
		if len(logs) != 0 {
			t.Errorf("expected empty logs for nonexistent node filter, got %d", len(logs))
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow logs --run flag
// ---------------------------------------------------------------------------

func TestFlowLogs_RunFlag(t *testing.T) {
	origAutoStart := flowDaemonAutoStart
	flowDaemonAutoStart = false
	defer func() { flowDaemonAutoStart = origAutoStart }()

	t.Run("--run flag equivalent to positional", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}

		f, _ := store.CreateFlow(ctx, flowmodel.Flow{
			ID: "runflag-flow", Name: "run-flag-test", Workspace: ws,
			State: flowmodel.FlowStopped, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
		n, _ := store.AddNode(ctx, flowmodel.FlowNode{
			ID: "runflag-node", FlowID: f.ID, Kind: flowmodel.NodeSkill, Label: "src",
			Config: json.RawMessage(`{"skill":"test"}`),
		})

		now := time.Now().UTC()
		run, _ := store.CreateRun(ctx, flowmodel.FlowRun{
			ID: "runflag-run", FlowID: f.ID, State: flowmodel.RunCompleted, StartedAt: now,
		})
		completed := now
		run.CompletedAt = &completed
		run, _ = store.UpdateRun(ctx, run)

		_, _ = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID: run.ID, NodeID: n.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})
		store.Close()

		// Positional form.
		stdout, _ := executeFlowCommand(t, "flow", "logs", run.ID, "--workspace", ws)
		posEnv := parseEnvelope(t, stdout)

		// --run flag form.
		stdout, _ = executeFlowCommand(t, "flow", "logs", "--run", run.ID, "--workspace", ws)
		flagEnv := parseEnvelope(t, stdout)

		// Both should have same command and status.
		if posEnv.Command != flagEnv.Command {
			t.Errorf("command mismatch: %q vs %q", posEnv.Command, flagEnv.Command)
		}
		if posEnv.Status != flagEnv.Status {
			t.Errorf("status mismatch: %q vs %q", posEnv.Status, flagEnv.Status)
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow logs missing argument
// ---------------------------------------------------------------------------

func TestFlowLogs_MissingArgument(t *testing.T) {
	origAutoStart := flowDaemonAutoStart
	flowDaemonAutoStart = false
	defer func() { flowDaemonAutoStart = origAutoStart }()

	t.Run("missing run argument shows cobra usage error", func(t *testing.T) {
		ws := tempWorkspace(t)
		// No run-id argument.
		_, err := executeFlowCommand(t, "flow", "logs", "--workspace", ws)
		if err == nil {
			t.Error("expected error when run-id not provided")
		}
	})
}

// ---------------------------------------------------------------------------
// Test: flow logs --follow streaming
// ---------------------------------------------------------------------------

func TestFlowLogs_FollowStreaming(t *testing.T) {
	origAutoStart := flowDaemonAutoStart
	flowDaemonAutoStart = false
	defer func() { flowDaemonAutoStart = origAutoStart }()

	t.Run("completed run replays and exits", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}

		// Create flow.
		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "follow-completed-flow",
			Name:      "follow-completed",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		// Create node.
		n, err := store.AddNode(ctx, flowmodel.FlowNode{
			ID:     "follow-node-1",
			FlowID: f.ID,
			Kind:   flowmodel.NodeSkill,
			Label:  "src",
			Config: json.RawMessage(`{"skill":"test"}`),
		})
		if err != nil {
			t.Fatal(err)
		}

		// Create completed run.
		now := time.Now().UTC()
		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID:        "follow-completed-run",
			FlowID:    f.ID,
			State:     flowmodel.RunCompleted,
			StartedAt: now.Add(-time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		completed := now
		run.State = flowmodel.RunCompleted
		run.CompletedAt = &completed
		run, err = store.UpdateRun(ctx, run)
		if err != nil {
			t.Fatal(err)
		}

		// Write some log entries.
		_, err = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID:    run.ID,
			NodeID:   n.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{"result":"ok"},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})
		if err != nil {
			t.Fatal(err)
		}

		store.Close()

		// --follow on completed run should replay and exit.
		stdout, _ := executeFlowCommand(t, "flow", "logs", "--follow", run.ID, "--workspace", ws)

		// Parse NDJSON output.
		lines := splitNDJSON(t, stdout)
		if len(lines) < 1 {
			t.Fatal("expected at least one envelope line")
		}

		// Last line should be terminal with meta.final:true.
		lastEnv := parseEnvelope(t, []byte(lines[len(lines)-1]))
		if lastEnv.Meta.Final == nil || !*lastEnv.Meta.Final {
			t.Errorf("expected meta.final=true on last envelope, got %v", lastEnv.Meta.Final)
		}

		// Intermediate lines should have status:progress.
		for i, line := range lines[:len(lines)-1] {
			env := parseEnvelope(t, []byte(line))
			if env.Status != envelope.StatusProgress {
				t.Errorf("line %d: expected status 'progress', got %q", i, env.Status)
			}
			if env.Meta.TS == "" {
				t.Errorf("line %d: expected meta.ts to be set", i)
			}
		}

		// Terminal should be status:ok.
		if lastEnv.Status != envelope.StatusOK {
			t.Errorf("expected terminal status 'ok', got %q", lastEnv.Status)
		}
	})

	t.Run("empty completed run emits terminal envelope", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()

		// Create a real flow first.
		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "empty-follow-flow",
			Name:      "empty-follow-flow",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID:        "empty-follow-run",
			FlowID:    f.ID,
			State:     flowmodel.RunCompleted,
			StartedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		stdout, _ := executeFlowCommand(t, "flow", "logs", "--follow", run.ID, "--workspace", ws)
		lines := splitNDJSON(t, stdout)
		if len(lines) != 1 {
			t.Fatalf("expected exactly 1 line for empty completed run, got %d", len(lines))
		}

		env := parseEnvelope(t, []byte(lines[0]))
		if env.Meta.Final == nil || !*env.Meta.Final {
			t.Error("expected meta.final=true")
		}
		if env.Status != envelope.StatusOK {
			t.Errorf("expected status 'ok', got %q", env.Status)
		}
	})

	t.Run("ENOTFOUND for nonexistent run with --follow", func(t *testing.T) {
		ws := tempWorkspace(t)
		stdout, _ := executeFlowCommand(t, "flow", "logs", "--follow", "nonexistent", "--workspace", ws)
		env := parseEnvelope(t, stdout)
		code := assertValidErrorEnvelope(t, env, "flow/logs")
		if code != "ENOTFOUND" {
			t.Errorf("expected ENOTFOUND, got %q", code)
		}
	})

	t.Run("data includes node_id, seq, run_id", func(t *testing.T) {
		ws := tempWorkspace(t)
		ctx := context.Background()
		store, err := openFlowStore(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}

		// Create flow + node + completed run with logs.
		f, err := store.CreateFlow(ctx, flowmodel.Flow{
			ID:        "data-fields-flow",
			Name:      "data-fields",
			Workspace: ws,
			State:     flowmodel.FlowStopped,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}

		n, err := store.AddNode(ctx, flowmodel.FlowNode{
			ID:     "data-node-1",
			FlowID: f.ID,
			Kind:   flowmodel.NodeSkill,
			Label:  "src",
			Config: json.RawMessage(`{"skill":"test"}`),
		})
		if err != nil {
			t.Fatal(err)
		}

		now := time.Now().UTC()
		run, err := store.CreateRun(ctx, flowmodel.FlowRun{
			ID:        "data-fields-run",
			FlowID:    f.ID,
			State:     flowmodel.RunCompleted,
			StartedAt: now.Add(-time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		completed := now
		run.CompletedAt = &completed
		run, err = store.UpdateRun(ctx, run)
		if err != nil {
			t.Fatal(err)
		}

		_, err = store.WriteRunLog(ctx, flowmodel.RunLog{
			RunID:    run.ID,
			NodeID:   n.ID,
			Envelope: json.RawMessage(`{"version":1,"status":"ok","command":"mock","data":{"result":"ok"},"meta":{"ts":"` + now.Format(time.RFC3339) + `"},"error":{}}`),
		})
		if err != nil {
			t.Fatal(err)
		}

		store.Close()

		stdout, _ := executeFlowCommand(t, "flow", "logs", "--follow", run.ID, "--workspace", ws)
		lines := splitNDJSON(t, stdout)

		// First non-terminal line should have data fields.
		for _, line := range lines {
			env := parseEnvelope(t, []byte(line))
			if env.Meta.Final != nil && *env.Meta.Final {
				continue
			}
			data, ok := env.Data.(map[string]any)
			if !ok {
				continue
			}
			if data["node_id"] == nil {
				t.Error("expected node_id in data")
			}
			if data["seq"] == nil {
				t.Error("expected seq in data")
			}
			if data["run_id"] != run.ID {
				t.Errorf("expected run_id=%s, got %v", run.ID, data["run_id"])
			}
			break // Just check first progress envelope
		}
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// splitNDJSON splits stdout into individual JSON lines.
func splitNDJSON(t *testing.T, stdout []byte) []string {
	t.Helper()
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(stdout)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid JSON line: %q", line)
		}
		lines = append(lines, line)
	}
	return lines
}

// Ensure unused imports are satisfied.
var (
	_ = context.Background
	_ = fmt.Sprintf
	_ = time.Now
	_ = envelope.StatusOK
	_ = json.RawMessage{}
)
