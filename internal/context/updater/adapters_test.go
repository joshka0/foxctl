package updater

import (
	"context"
	"testing"
	"time"
)

func TestShortTermMemory_Deduplication(t *testing.T) {
	mem := NewShortTermMemory(10, 30*time.Minute)

	// First injection should not be duplicate
	if mem.WasRecentlyInjected("ctx-1", "test content", "session-1") {
		t.Error("first injection should not be marked as duplicate")
	}

	// Record the injection
	mem.Record("ctx-1", "test content", "session-1", []string{"topic1"})

	// Same content should now be duplicate
	if !mem.WasRecentlyInjected("ctx-1", "test content", "session-1") {
		t.Error("second injection of same content should be marked as duplicate")
	}

	// Different content should not be duplicate
	if mem.WasRecentlyInjected("ctx-2", "different content", "session-1") {
		t.Error("different content should not be marked as duplicate")
	}

	// Same content in different session should NOT be duplicate (session-scoped)
	if mem.WasRecentlyInjected("ctx-1", "test content", "session-2") {
		t.Error("same content in different session should NOT be duplicate (session-scoped dedup)")
	}
}

func TestShortTermMemory_TTL(t *testing.T) {
	// Note: NewShortTermMemory enforces a minimum TTL of 1 minute.
	// Testing real TTL expiration would require unacceptably long test durations.
	// This test verifies the TTL is set correctly (clamped to minimum).
	mem := NewShortTermMemory(10, 10*time.Millisecond)

	// Record an injection
	mem.Record("ctx-1", "test content", "session-1", []string{"topic1"})

	// Should be duplicate immediately (TTL not expired)
	if !mem.WasRecentlyInjected("ctx-1", "test content", "session-1") {
		t.Error("should be duplicate immediately after recording")
	}

	// Since TTL is clamped to 30 minutes (when < 1 minute), we can't test
	// actual expiration without waiting 30+ minutes. Instead, we verify
	// that the record exists and the TTL clamping worked correctly by
	// confirming the record is still valid after a short wait.
	time.Sleep(50 * time.Millisecond)

	// Should still be duplicate (TTL is 30 minutes, not 10ms)
	if !mem.WasRecentlyInjected("ctx-1", "test content", "session-1") {
		t.Error("should still be duplicate (TTL clamped to 30 minutes)")
	}
}

func TestShortTermMemory_Capacity(t *testing.T) {
	// Create memory with capacity of 3
	mem := NewShortTermMemory(3, 30*time.Minute)

	// Record 3 items
	mem.Record("ctx-1", "content-1", "session-1", []string{"topic"})
	mem.Record("ctx-2", "content-2", "session-1", []string{"topic"})
	mem.Record("ctx-3", "content-3", "session-1", []string{"topic"})

	// All 3 should be tracked
	if !mem.WasRecentlyInjected("ctx-1", "content-1", "session-1") {
		t.Error("ctx-1 should be tracked")
	}
	if !mem.WasRecentlyInjected("ctx-2", "content-2", "session-1") {
		t.Error("ctx-2 should be tracked")
	}
	if !mem.WasRecentlyInjected("ctx-3", "content-3", "session-1") {
		t.Error("ctx-3 should be tracked")
	}

	// Add a 4th item, should evict the oldest (ctx-1)
	mem.Record("ctx-4", "content-4", "session-1", []string{"topic"})

	// ctx-1 should no longer be tracked (evicted)
	if mem.WasRecentlyInjected("ctx-1", "content-1", "session-1") {
		t.Error("ctx-1 should have been evicted")
	}

	// ctx-4 should be tracked
	if !mem.WasRecentlyInjected("ctx-4", "content-4", "session-1") {
		t.Error("ctx-4 should be tracked")
	}
}

func TestCombinedFinder_NilSources(t *testing.T) {
	// Test that nil sources are handled gracefully
	finder := NewCombinedFinder(nil, nil, nil)

	analysis := &AnalysisResult{
		Topics:        []string{"test"},
		SearchQueries: []string{"test query"},
	}

	candidates, err := finder.FindContext(context.Background(), analysis, "session-1", "test-workspace")
	if err != nil {
		t.Fatalf("FindContext with nil sources should not error: %v", err)
	}

	// Should return empty but not error
	if candidates == nil {
		candidates = []ContextCandidate{}
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates with nil sources, got %d", len(candidates))
	}
}

func TestCombinedFinder_NilAnalysis(t *testing.T) {
	finder := NewCombinedFinder(nil, nil, nil)

	candidates, err := finder.FindContext(context.Background(), nil, "session-1", "test-workspace")
	if err != nil {
		t.Fatalf("FindContext with nil analysis should not error: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates with nil analysis, got %d", len(candidates))
	}
}

func TestCombinedFinder_EmptySearchQueries(t *testing.T) {
	finder := NewCombinedFinder(nil, nil, nil)

	analysis := &AnalysisResult{
		Topics:        []string{"test"},
		SearchQueries: []string{}, // Empty search queries
	}

	candidates, err := finder.FindContext(context.Background(), analysis, "session-1", "test-workspace")
	if err != nil {
		t.Fatalf("FindContext with empty queries should not error: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates with empty queries, got %d", len(candidates))
	}
}

// MockMemorySearcher for testing CombinedFinder.
type MockMemorySearcher struct {
	results []MemoryResult
}

func (m *MockMemorySearcher) SearchByQuery(ctx context.Context, workspace, query string, limit int) ([]MemoryResult, error) {
	return m.results, nil
}

// MockSessionSearcher for testing CombinedFinder.
type MockSessionSearcher struct {
	results []SessionResult
}

func (m *MockSessionSearcher) SearchSessions(ctx context.Context, query string, limit int) ([]SessionResult, error) {
	return m.results, nil
}

func TestCombinedFinder_WithMocks(t *testing.T) {
	mockMemory := &MockMemorySearcher{
		results: []MemoryResult{
			{ID: "mem-1", Type: "gotcha", Summary: "Watch out for race conditions", Score: 0.85},
		},
	}

	mockSessions := &MockSessionSearcher{
		results: []SessionResult{
			{SessionID: "sess-old", Content: "Previous learning about concurrency", Type: "learning", Score: 0.7},
		},
	}

	finder := NewCombinedFinder(mockMemory, mockSessions, nil)

	analysis := &AnalysisResult{
		Topics:        []string{"concurrency", "goroutines"},
		Intent:        "fixing race condition",
		SearchQueries: []string{"race conditions"},
	}

	candidates, err := finder.FindContext(context.Background(), analysis, "current-session", "test-workspace")
	if err != nil {
		t.Fatalf("FindContext failed: %v", err)
	}

	// Should find results from both memory and sessions
	if len(candidates) == 0 {
		t.Error("expected some candidates, got none")
	}

	// Check that memory result is present (with boosted score for gotcha)
	foundMemory := false
	for _, c := range candidates {
		if c.Type == "memory" && c.ID == "mem-1" {
			foundMemory = true
			// Gotchas get boosted by 0.15
			expectedScore := float32(0.85 + 0.15)
			if c.Score != expectedScore {
				t.Errorf("expected boosted score %f, got %f", expectedScore, c.Score)
			}
		}
	}
	if !foundMemory {
		t.Error("memory result not found in candidates")
	}
}

func TestFormatForHook(t *testing.T) {
	messages := []ContextMessage{
		{
			Content: "First context piece",
			Reason:  "Relevant to current task",
		},
		{
			Content: "Second context piece",
			Reason:  "",
		},
	}

	formatted := FormatForHook(messages)

	if formatted == "" {
		t.Error("FormatForHook should not return empty string for non-empty messages")
	}

	// Should contain the content
	if !contains(formatted, "First context piece") {
		t.Error("formatted output should contain first message content")
	}

	// Should contain reason for first message
	if !contains(formatted, "Surfaced because: Relevant to current task") {
		t.Error("formatted output should contain reason for first message")
	}
}

func TestFormatForHook_Empty(t *testing.T) {
	formatted := FormatForHook(nil)
	if formatted != "" {
		t.Errorf("FormatForHook with nil should return empty string, got %q", formatted)
	}

	formatted = FormatForHook([]ContextMessage{})
	if formatted != "" {
		t.Errorf("FormatForHook with empty slice should return empty string, got %q", formatted)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
