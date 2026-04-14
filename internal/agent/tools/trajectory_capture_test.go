package tools

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func TestToolTrajectoryCapture_CodeSymbolSearch_EmitsToolCallAndResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	cfg := Config{
		WorkspaceRoot:         t.TempDir(),
		WorkspaceID:           "ws-1",
		TaskID:                "task-123",
		EpicID:                "epic-1",
		AgentRole:             "coder",
		TraceID:               "trace-1",
		TrajectoryStorageRoot: root,
		ActorID:               "actor:agent:dspy:test",
		MaxSearchResults:      10,
	}

	reg, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	// Invoke via the exported tool registry to ensure wrapWithTelemetry is exercised.
	tool, err := reg.Get("code.symbol_search")
	if err != nil {
		t.Fatalf("get tool: %v", err)
	}

	// Execute may fail (no memory store); we only care that trajectory events were captured.
	_, _ = tool.Call(ctx, map[string]any{ //nolint:errcheck
		"workspace_id": "ws-1",
		"question":     "How does login work?",
		"max_results":  5,
	})

	store, err := trajectory.Open(ctx, root)
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	events, err := store.GetEventsByTraceID(ctx, "ws-1", "trace-1")
	if err != nil {
		t.Fatalf("GetEventsByTraceID: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (tool_call + tool_result), got %d", len(events))
	}

	foundCall := false
	foundResult := false
	for _, e := range events {
		if e.Command != "code.symbol_search" {
			continue
		}
		subkind, _ := e.DataInline["subkind"].(string)
		if subkind != string(trajectory.EventKindGraphSearch) {
			continue
		}
		switch e.Kind {
		case trajectory.EventKindToolCall:
			foundCall = true
		case trajectory.EventKindToolResult:
			foundResult = true
		}
	}

	if !foundCall {
		t.Fatalf("expected tool_call event for code.symbol_search")
	}
	if !foundResult {
		t.Fatalf("expected tool_result event for code.symbol_search")
	}
}
