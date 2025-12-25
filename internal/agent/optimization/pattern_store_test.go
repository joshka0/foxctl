package optimization_test

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
)

func TestPatternStore_OpenAndClose(t *testing.T) {
	store := openTestPatternStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestPatternStore_RecordAndGet(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	pattern := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "fix bug in authentication",
		ToolSequence: []string{"grep", "read", "edit"},
		Outcome:      "success",
	}

	if err := store.Record(ctx, pattern); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Use List instead of GetTopPatterns (GetTopPatterns requires count >= 3)
	patterns, err := store.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern, got %d", len(patterns))
	}

	got := patterns[0]
	if got.AgentRole != "coder" {
		t.Errorf("agent_role: got %q, want %q", got.AgentRole, "coder")
	}
	if got.Context != "fix bug in authentication" {
		t.Errorf("context: got %q, want %q", got.Context, "fix bug in authentication")
	}
	if len(got.ToolSequence) != 3 {
		t.Errorf("tool_sequence: got %d tools, want 3", len(got.ToolSequence))
	}
	if got.Outcome != "success" {
		t.Errorf("outcome: got %q, want %q", got.Outcome, "success")
	}
}

func TestPatternStore_RecordIncrementsCount(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	pattern := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "write tests",
		ToolSequence: []string{"read", "write"},
		Outcome:      "success",
		Count:        1,
	}

	// Record same pattern multiple times
	for i := 0; i < 3; i++ {
		if err := store.Record(ctx, pattern); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	patterns, err := store.GetTopPatterns(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("get top patterns: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern (deduplicated), got %d", len(patterns))
	}

	if patterns[0].Count != 3 {
		t.Errorf("count: got %d, want 3", patterns[0].Count)
	}
}

func TestPatternStore_FindSimilar(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	// Record patterns with different contexts and unique tool sequences
	patterns := []optimization.Pattern{
		{AgentRole: "coder", Context: "fix authentication bug", ToolSequence: []string{"grep", "edit", "test"}, Outcome: "success"},
		{AgentRole: "coder", Context: "add user login feature", ToolSequence: []string{"read", "write", "test"}, Outcome: "success"},
		{AgentRole: "coder", Context: "fix database connection", ToolSequence: []string{"grep", "read", "write"}, Outcome: "failure"},
	}

	for _, p := range patterns {
		if err := store.Record(ctx, p); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Find similar to "authentication" - FindSimilar uses LIKE matching
	similar, err := store.FindSimilar(ctx, "coder", "authentication", 0.1)
	if err != nil {
		t.Fatalf("find similar: %v", err)
	}

	if len(similar) == 0 {
		t.Error("expected to find similar patterns")
	}

	// Check that we found the authentication pattern
	found := false
	for _, p := range similar {
		if p.Context == "fix authentication bug" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find 'fix authentication bug' in similar patterns")
	}
}

func TestPatternStore_GetTopPatternsByRole(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	// Record patterns for different roles using List (GetTopPatterns requires count >= 3)
	coderPattern := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "write code",
		ToolSequence: []string{"edit"},
		Outcome:      "success",
	}
	plannerPattern := optimization.Pattern{
		AgentRole:    "planner",
		Context:      "create plan",
		ToolSequence: []string{"read"},
		Outcome:      "success",
	}

	if err := store.Record(ctx, coderPattern); err != nil {
		t.Fatalf("record coder: %v", err)
	}
	if err := store.Record(ctx, plannerPattern); err != nil {
		t.Fatalf("record planner: %v", err)
	}

	// Get only coder patterns using List (GetTopPatterns requires count >= 3)
	coderPatterns, err := store.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list coder patterns: %v", err)
	}

	if len(coderPatterns) != 1 {
		t.Fatalf("expected 1 coder pattern, got %d", len(coderPatterns))
	}
	if coderPatterns[0].AgentRole != "coder" {
		t.Errorf("expected coder role, got %q", coderPatterns[0].AgentRole)
	}
}

func TestPatternStore_SuccessRate(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	// Record success and failure using same tool sequence
	successPattern := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "task A",
		ToolSequence: []string{"edit"},
		Outcome:      "success",
	}
	if err := store.Record(ctx, successPattern); err != nil {
		t.Fatalf("record success: %v", err)
	}

	// Record failure for same tool sequence (will update same pattern)
	failurePattern := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "task A",
		ToolSequence: []string{"edit"},
		Outcome:      "failure",
	}
	if err := store.Record(ctx, failurePattern); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	// Use List instead of GetTopPatterns (which requires count >= 3)
	patterns, err := store.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("expected 1 pattern (aggregated), got %d", len(patterns))
	}

	// Success rate should be 0.5 (1 success, 1 failure)
	rate := patterns[0].SuccessRate()
	if rate < 0.4 || rate > 0.6 {
		t.Errorf("success rate: got %.2f, want ~0.5", rate)
	}
}

func TestPatternStore_Limit(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	// Record many patterns with UNIQUE tool sequences
	for i := 0; i < 20; i++ {
		pattern := optimization.Pattern{
			AgentRole:    "coder",
			Context:      "task " + string(rune('A'+i)),
			ToolSequence: []string{"tool_" + string(rune('A'+i))}, // Unique tool sequence
			Outcome:      "success",
		}
		if err := store.Record(ctx, pattern); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// Get with limit using List (GetTopPatterns requires count >= 3)
	patterns, err := store.List(ctx, "coder", 5)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) != 5 {
		t.Errorf("expected 5 patterns, got %d", len(patterns))
	}
}

func openTestPatternStore(t *testing.T) optimization.PatternStore {
	t.Helper()
	store, err := optimization.OpenPatternStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open pattern store: %v", err)
	}
	return store
}

// Test that patterns are ordered by success rate/count
func TestPatternStore_Ordering(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	// Record low count pattern once
	lowCount := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "low count task",
		ToolSequence: []string{"read"},
		Outcome:      "success",
	}
	if err := store.Record(ctx, lowCount); err != nil {
		t.Fatalf("record low: %v", err)
	}

	// Record high count pattern multiple times to boost it
	highCount := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "high count task",
		ToolSequence: []string{"edit"},
		Outcome:      "success",
	}
	for i := 0; i < 10; i++ {
		if err := store.Record(ctx, highCount); err != nil {
			t.Fatalf("record high %d: %v", i, err)
		}
	}

	// Use List instead of GetTopPatterns
	patterns, err := store.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) < 2 {
		t.Fatalf("expected at least 2 patterns, got %d", len(patterns))
	}

	// High count (10 successes) should be ordered before low count (1 success)
	// List orders by success rate DESC, then count DESC
	if patterns[0].Context != "high count task" {
		t.Errorf("expected 'high count task' first, got %q", patterns[0].Context)
	}
}

// Test timestamp updates
func TestPatternStore_LastSeenUpdated(t *testing.T) {
	ctx := context.Background()
	store := openTestPatternStore(t)
	defer store.Close() //nolint:errcheck

	pattern := optimization.Pattern{
		AgentRole:    "coder",
		Context:      "timestamp test",
		ToolSequence: []string{"read"},
		Outcome:      "success",
	}

	if err := store.Record(ctx, pattern); err != nil {
		t.Fatalf("record initial: %v", err)
	}

	// Get initial timestamp using List (GetTopPatterns requires count >= 3)
	patterns, err := store.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns: %v", err)
	}

	if len(patterns) == 0 {
		t.Fatal("expected at least one pattern")
	}
	initialTime := patterns[0].LastSeen

	// Wait a bit and record again
	time.Sleep(10 * time.Millisecond)
	if err := store.Record(ctx, pattern); err != nil {
		t.Fatalf("record again: %v", err)
	}

	// Get updated timestamp
	patterns, err = store.List(ctx, "coder", 10)
	if err != nil {
		t.Fatalf("list patterns again: %v", err)
	}

	if !patterns[0].LastSeen.After(initialTime) {
		t.Error("expected LastSeen to be updated")
	}
}
