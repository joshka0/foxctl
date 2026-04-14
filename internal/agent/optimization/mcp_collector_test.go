package optimization_test

import (
	"context"
	"testing"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func TestMCPPatternCollector_RecordToolCall(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Record a successful tool call
	err := collector.RecordToolCall(ctx, "coder", "fix auth bug", "grep", true, 150)
	if err != nil {
		t.Fatalf("record tool call: %v", err)
	}

	// Verify pattern was recorded using List (GetTopPatterns requires count >= 3)
	patterns, err := patternStore.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}

	if patterns[0].ToolSequence[0] != "grep" {
		t.Errorf("tool: got %q, want %q", patterns[0].ToolSequence[0], "grep")
	}
}

func TestMCPPatternCollector_RecordToolCallFailure(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Record a failed tool call
	err := collector.RecordToolCall(ctx, "coder", "fix bug", "edit", false, 500)
	if err != nil {
		t.Fatalf("record tool call: %v", err)
	}

	// Verify pattern was recorded using List (GetTopPatterns requires count >= 3)
	patterns, err := patternStore.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}

	// Should have failure recorded
	if patterns[0].Outcome != "failure" {
		t.Errorf("outcome: got %q, want %q", patterns[0].Outcome, "failure")
	}
}

func TestMCPPatternCollector_GetHints(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Record successful patterns for authentication tasks
	for i := 0; i < 5; i++ {
		err := collector.RecordToolCall(ctx, "coder", "fix authentication", "grep", true, 100)
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// Get hints for similar task
	hints, err := collector.GetHints(ctx, "coder", "authentication bug fix")
	if err != nil {
		t.Fatalf("get hints: %v", err)
	}

	if len(hints) == 0 {
		t.Error("expected hints for authentication task")
	}
}

func TestMCPPatternCollector_GetHintsEmpty(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Get hints with no patterns recorded
	hints, err := collector.GetHints(ctx, "coder", "random task")
	if err != nil {
		t.Fatalf("get hints: %v", err)
	}

	// Should return empty, not error
	if hints == nil {
		t.Error("hints should be empty slice, not nil")
	}
}

func TestMCPPatternCollector_FormatHintsForPrompt(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Record patterns with high success rate
	for i := 0; i < 10; i++ {
		err := collector.RecordToolCall(ctx, "coder", "write tests", "read", true, 100)
		if err != nil {
			t.Fatalf("record read: %v", err)
		}
		err = collector.RecordToolCall(ctx, "coder", "write tests", "write", true, 200)
		if err != nil {
			t.Fatalf("record write: %v", err)
		}
	}

	hints, err := collector.GetHints(ctx, "coder", "write tests")
	if err != nil {
		t.Fatalf("get hints: %v", err)
	}

	formatted := collector.FormatHintsForPrompt(hints)
	if formatted == "" {
		t.Error("expected formatted hints, got empty string")
	}

	// Should contain tool recommendations
	if len(formatted) < 10 {
		t.Errorf("formatted hints too short: %q", formatted)
	}
}

func TestMCPPatternCollector_MultipleTasks(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Record patterns for different tasks
	tasks := []struct {
		context string
		tool    string
	}{
		{"fix database connection", "grep"},
		{"fix database connection", "edit"},
		{"add user feature", "read"},
		{"add user feature", "write"},
		{"refactor code", "grep"},
	}

	for _, task := range tasks {
		if err := collector.RecordToolCall(ctx, "coder", task.context, task.tool, true, 100); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Get hints for database task (use "database" to match via LIKE)
	hints, err := collector.GetHints(ctx, "coder", "database")
	if err != nil {
		t.Fatalf("get hints: %v", err)
	}

	// Should find database-related patterns (via FindSimilar LIKE matching)
	if len(hints) == 0 {
		t.Error("expected hints for database task")
	}
}

func TestMCPPatternCollector_DifferentRoles(t *testing.T) {
	ctx := context.Background()
	patternStore := openTestPatternStore(t)
	defer patternStore.Close() //nolint:errcheck

	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)

	// Record for coder role
	if err := collector.RecordToolCall(ctx, "coder", "write code", "edit", true, 100); err != nil {
		t.Fatalf("record coder: %v", err)
	}

	// Record for planner role
	if err := collector.RecordToolCall(ctx, "planner", "create plan", "read", true, 100); err != nil {
		t.Fatalf("record planner: %v", err)
	}

	// Get hints for coder should not include planner patterns
	coderHints, err := collector.GetHints(ctx, "coder", "write code")
	if err != nil {
		t.Fatalf("get coder hints: %v", err)
	}

	plannerHints, err := collector.GetHints(ctx, "planner", "create plan")
	if err != nil {
		t.Fatalf("get planner hints: %v", err)
	}

	// Verify hints are role-specific (using ToolName field)
	for _, hint := range coderHints {
		if hint.ToolName == "read" && hint.Reason == "create plan" {
			t.Error("coder hints should not include planner patterns")
		}
	}

	for _, hint := range plannerHints {
		if hint.ToolName == "edit" && hint.Reason == "write code" {
			t.Error("planner hints should not include coder patterns")
		}
	}
}

func openTestTrajStore(t *testing.T) trajectory.Store {
	t.Helper()
	store, err := trajectory.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open trajectory store: %v", err)
	}
	return store
}
