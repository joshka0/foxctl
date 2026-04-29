package companion

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// --- ExtractionPolicy interface tests ---

func TestToolResultBypassPolicy_ToolResultPasses(t *testing.T) {
	policy := ToolResultBypassPolicy{}
	event := ConversationEvent{EventType: EventTypeToolResult, Content: "any content"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("tool_result events should always pass extraction gate")
	}
	if categories != nil {
		t.Fatal("tool_result bypass should return nil categories")
	}
}

func TestToolResultBypassPolicy_UserMessageBlocked(t *testing.T) {
	policy := ToolResultBypassPolicy{}
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "I prefer Go"}
	ok, _ := policy.ShouldExtract(event)
	if ok {
		t.Fatal("user_message events should not pass tool_result bypass")
	}
}

func TestPatternExtractionPolicy_PreferenceDetected(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "I prefer short responses"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("preference should be detected")
	}
	if !containsString(categories, ExtractionCategoryPreference) {
		t.Fatalf("categories=%v should contain %q", categories, ExtractionCategoryPreference)
	}
}

func TestPatternExtractionPolicy_DecisionDetected(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "Let's go with option B"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("decision should be detected")
	}
	if !containsString(categories, ExtractionCategoryDecision) {
		t.Fatalf("categories=%v should contain %q", categories, ExtractionCategoryDecision)
	}
}

func TestPatternExtractionPolicy_QuestionDetected(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "Should we use Go?"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("question should be detected")
	}
	if !containsString(categories, ExtractionCategoryQuestion) {
		t.Fatalf("categories=%v should contain %q", categories, ExtractionCategoryQuestion)
	}
}

func TestPatternExtractionPolicy_DefinitionDetected(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeAssistantMessage, Content: "A handler means a function that processes requests"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("definition should be detected")
	}
	if !containsString(categories, ExtractionCategoryDefinition) {
		t.Fatalf("categories=%v should contain %q", categories, ExtractionCategoryDefinition)
	}
}

func TestPatternExtractionPolicy_GoalChangeDetected(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "The goal is to ship the context engine"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("goal change should be detected")
	}
	if !containsString(categories, ExtractionCategoryGoalChange) {
		t.Fatalf("categories=%v should contain %q", categories, ExtractionCategoryGoalChange)
	}
}

func TestPatternExtractionPolicy_RetractionDetected(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "Actually no, forget that approach"}
	ok, categories := policy.ShouldExtract(event)
	if !ok {
		t.Fatal("retraction should be detected")
	}
	if !containsString(categories, ExtractionCategoryRetraction) {
		t.Fatalf("categories=%v should contain %q", categories, ExtractionCategoryRetraction)
	}
}

func TestPatternExtractionPolicy_NoMatch(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "The weather is nice today"}
	ok, _ := policy.ShouldExtract(event)
	if ok {
		t.Fatal("generic content should not match extraction patterns")
	}
}

func TestPatternExtractionPolicy_QuestionSuffix(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	// Questions ending with "?" are detected by extractOpenQuestions in the extraction
	// phase, not by the policy gate. The policy gate only checks for indicator patterns.
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: "What time is it?"}
	ok, _ := policy.ShouldExtract(event)
	// The "?" suffix alone is NOT a pattern indicator — only explicit phrases like "what about"
	// are checked. This is correct: the extraction logic handles "?" in extractOpenQuestions.
	if ok {
		t.Fatal("question mark alone should not trigger pattern extraction")
	}
}

func TestPatternExtractionPolicy_IgnoresToolEvents(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeToolResult, Content: "I prefer short responses"}
	ok, _ := policy.ShouldExtract(event)
	if ok {
		t.Fatal("pattern policy should only check user/assistant messages, not tool events")
	}
}

func TestPatternExtractionPolicy_EmptyContent(t *testing.T) {
	policy := NewDefaultPatternExtractionPolicy()
	event := ConversationEvent{EventType: EventTypeUserMessage, Content: ""}
	ok, _ := policy.ShouldExtract(event)
	if ok {
		t.Fatal("empty content should not match")
	}
}

// --- CompositeExtractionPolicy tests ---

func TestCompositeExtractionPolicy_AnyTrue(t *testing.T) {
	composite := NewCompositeExtractionPolicy(
		ToolResultBypassPolicy{},
		NewDefaultPatternExtractionPolicy(),
	)

	// tool_result passes via bypass even without content
	toolEvent := ConversationEvent{EventType: EventTypeToolResult, Content: "tool output"}
	ok, _ := composite.ShouldExtract(toolEvent)
	if !ok {
		t.Fatal("composite should pass tool_result via bypass")
	}

	// user message with preference passes via pattern
	userEvent := ConversationEvent{EventType: EventTypeUserMessage, Content: "I prefer short responses"}
	ok, categories := composite.ShouldExtract(userEvent)
	if !ok {
		t.Fatal("composite should pass user message with preference via pattern")
	}
	if !containsString(categories, ExtractionCategoryPreference) {
		t.Fatalf("categories=%v should contain preference", categories)
	}

	// user message without any signal is rejected
	plainEvent := ConversationEvent{EventType: EventTypeUserMessage, Content: "The weather is nice"}
	ok, _ = composite.ShouldExtract(plainEvent)
	if ok {
		t.Fatal("composite should reject plain user message")
	}
}

func TestCompositeExtractionPolicy_DeduplicatesCategories(t *testing.T) {
	// Create a composite with two policies that both return "preference"
	p1 := &stubPolicy{ok: true, categories: []string{ExtractionCategoryPreference}}
	p2 := &stubPolicy{ok: true, categories: []string{ExtractionCategoryPreference, ExtractionCategoryDecision}}
	composite := NewCompositeExtractionPolicy(p1, p2)

	ok, categories := composite.ShouldExtract(ConversationEvent{})
	if !ok {
		t.Fatal("expected true")
	}
	preferenceCount := 0
	for _, c := range categories {
		if c == ExtractionCategoryPreference {
			preferenceCount++
		}
	}
	if preferenceCount != 1 {
		t.Fatalf("preference should appear exactly once, got %d in %v", preferenceCount, categories)
	}
}

type stubPolicy struct {
	ok         bool
	categories []string
}

func (s *stubPolicy) ShouldExtract(_ ConversationEvent) (bool, []string) {
	return s.ok, s.categories
}

func (s *stubPolicy) ExtractEntries(_ string, _ []string) []ExtractedEntry {
	return nil
}

// --- TypedSignalExtractor tests ---

func TestTypedSignalExtractor_RetractionSignal(t *testing.T) {
	extractor := NewDefaultTypedSignalExtractor()
	tests := []struct {
		content string
		want    EpisodeBoundarySignal
	}{
		{"actually no, that's wrong", SignalAssumptionInvalidated},
		{"that was wrong", SignalAssumptionInvalidated},
		{"I take that back", SignalAssumptionInvalidated},
		{"never mind, let's reset", SignalAssumptionInvalidated},
		{"assumption invalidated", SignalAssumptionInvalidated},
		{"I'm sorry, I was incorrect", SignalAssumptionInvalidated},
	}
	for _, tt := range tests {
		got := extractor.DetectBoundarySignal(tt.content)
		if got != tt.want {
			t.Errorf("DetectBoundarySignal(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestTypedSignalExtractor_UserRedirectSignal(t *testing.T) {
	extractor := NewDefaultTypedSignalExtractor()
	tests := []struct {
		content string
		want    EpisodeBoundarySignal
	}{
		{"let's move on to the next topic", SignalUserRedirect},
		{"forget that, let's try something else", SignalUserRedirect},
		{"let's switch to Go", SignalUserRedirect},
		{"let's pivot to a new approach", SignalUserRedirect},
		{"moving on to deployment", SignalUserRedirect},
	}
	for _, tt := range tests {
		got := extractor.DetectBoundarySignal(tt.content)
		if got != tt.want {
			t.Errorf("DetectBoundarySignal(%q) = %q, want %q", tt.content, got, tt.want)
		}
	}
}

func TestTypedSignalExtractor_NoSignal(t *testing.T) {
	extractor := NewDefaultTypedSignalExtractor()
	tests := []struct {
		content string
	}{
		{"I prefer short responses"},
		{"Let's go with option B"},
		{"The weather is nice"},
		{""},
	}
	for _, tt := range tests {
		got := extractor.DetectBoundarySignal(tt.content)
		if got != SignalNone {
			t.Errorf("DetectBoundarySignal(%q) = %q, want %q", tt.content, got, SignalNone)
		}
	}
}

// --- BuildHybridContextLayers uses ExtractionPolicy ---

func TestBuildHybridContextLayers_UsesExtractionPolicy(t *testing.T) {
	// Test that the pipeline correctly uses an ExtractionPolicy to gate Tier 1.
	// A policy that always rejects should produce zero hard state entries.
	ctx := backgroundContext(t)
	db := openTestDB(t)
	mem := newTestMemory(t, db)
	convID := "conv-policy-test"

	// Insert turns and events
	if err := mem.AppendTurn(ctx, ConversationTurn{
		ID: "turn-1", ConversationID: convID, Role: "user",
		Content: "I prefer terse responses.", CreatedAt: testTime(1),
	}); err != nil {
		t.Fatalf("append turn: %v", err)
	}
	if err := mem.InsertEvent(ctx, &ConversationEvent{
		ConversationID: convID, EventType: EventTypeUserMessage,
		Content: "I prefer terse responses.", TurnID: "turn-1",
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := mem.EnsureHybridMode(ctx, convID); err != nil {
		t.Fatalf("ensure hybrid mode: %v", err)
	}

	// Use default policy (should extract preferences)
	if err := mem.BuildHybridContextLayers(ctx, convID); err != nil {
		t.Fatalf("build hybrid context layers: %v", err)
	}

	// Verify hard state entries were created
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = $1
	`, convID).Scan(&count); err != nil {
		t.Fatalf("count hard state: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one hard state entry from preference extraction")
	}
}

func TestBuildHybridContextLayers_RejectingPolicyProducesNoTier1(t *testing.T) {
	// A rejecting policy should produce zero hard state entries from user messages.
	ctx := backgroundContext(t)
	db := openTestDB(t)
	mem := newTestMemory(t, db)
	convID := "conv-reject-policy"

	// Override extraction policy to always-reject
	mem.extractionPolicy = &rejectAllPolicy{}

	if err := mem.AppendTurn(ctx, ConversationTurn{
		ID: "turn-r1", ConversationID: convID, Role: "user",
		Content: "I prefer terse responses.", CreatedAt: testTime(1),
	}); err != nil {
		t.Fatalf("append turn: %v", err)
	}
	if err := mem.InsertEvent(ctx, &ConversationEvent{
		ConversationID: convID, EventType: EventTypeUserMessage,
		Content: "I prefer terse responses.", TurnID: "turn-r1",
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if err := mem.EnsureHybridMode(ctx, convID); err != nil {
		t.Fatalf("ensure hybrid mode: %v", err)
	}
	if err := mem.BuildHybridContextLayers(ctx, convID); err != nil {
		t.Fatalf("build hybrid context layers: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_hard_state_entries WHERE conversation_id = $1
	`, convID).Scan(&count); err != nil {
		t.Fatalf("count hard state: %v", err)
	}
	if count != 0 {
		t.Fatalf("rejecting policy should produce 0 hard state entries, got %d", count)
	}
}

type rejectAllPolicy struct{}

func (r *rejectAllPolicy) ShouldExtract(_ ConversationEvent) (bool, []string) {
	return false, nil
}

func (r *rejectAllPolicy) ExtractEntries(_ string, _ []string) []ExtractedEntry {
	return nil
}

// --- Episode boundary detection uses typed signals ---

func TestEpisodeBoundary_RetractionSignalSeals(t *testing.T) {
	ctx := backgroundContext(t)
	db := openTestDB(t)
	mem := newTestMemory(t, db)
	convID := "conv-boundary-retraction"

	turns := []ConversationTurn{
		{ID: "t-br-1", ConversationID: convID, Role: "user", Content: "I prefer short answers.", CreatedAt: testTime(1)},
		{ID: "t-br-2", ConversationID: convID, Role: "assistant", Content: "Noted.", CreatedAt: testTime(2)},
		{ID: "t-br-3", ConversationID: convID, Role: "user", Content: "Actually no, I was wrong about that.", CreatedAt: testTime(3)},
	}
	for _, turn := range turns {
		if err := mem.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("append turn %s: %v", turn.ID, err)
		}
	}
	events := []ConversationEvent{
		{ConversationID: convID, EventType: EventTypeUserMessage, Content: "I prefer short answers.", TurnID: "t-br-1"},
		{ConversationID: convID, EventType: EventTypeAssistantMessage, Content: "Noted.", TurnID: "t-br-2"},
		{ConversationID: convID, EventType: EventTypeUserMessage, Content: "Actually no, I was wrong about that.", TurnID: "t-br-3"},
	}
	for i := range events {
		if err := mem.InsertEvent(ctx, &events[i]); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	if err := mem.EnsureHybridMode(ctx, convID); err != nil {
		t.Fatalf("ensure hybrid mode: %v", err)
	}
	if err := mem.BuildHybridContextLayers(ctx, convID); err != nil {
		t.Fatalf("build layers: %v", err)
	}

	// The retraction signal should have triggered episode sealing
	var sealedCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_soft_episodes WHERE conversation_id = $1
	`, convID).Scan(&sealedCount); err != nil {
		t.Fatalf("count episodes: %v", err)
	}
	if sealedCount == 0 {
		t.Fatal("retraction signal should have sealed an episode")
	}
}

func TestEpisodeBoundary_NoSignalStaysOpen(t *testing.T) {
	ctx := backgroundContext(t)
	db := openTestDB(t)
	mem := newTestMemory(t, db)
	convID := "conv-boundary-nosignal"

	turns := []ConversationTurn{
		{ID: "t-bn-1", ConversationID: convID, Role: "user", Content: "Tell me about Go.", CreatedAt: testTime(1)},
		{ID: "t-bn-2", ConversationID: convID, Role: "assistant", Content: "Go is a statically typed language.", CreatedAt: testTime(2)},
	}
	for _, turn := range turns {
		if err := mem.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("append turn %s: %v", turn.ID, err)
		}
	}
	events := []ConversationEvent{
		{ConversationID: convID, EventType: EventTypeUserMessage, Content: "Tell me about Go.", TurnID: "t-bn-1"},
		{ConversationID: convID, EventType: EventTypeAssistantMessage, Content: "Go is a statically typed language.", TurnID: "t-bn-2"},
	}
	for i := range events {
		if err := mem.InsertEvent(ctx, &events[i]); err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	if err := mem.EnsureHybridMode(ctx, convID); err != nil {
		t.Fatalf("ensure hybrid mode: %v", err)
	}
	if err := mem.BuildHybridContextLayers(ctx, convID); err != nil {
		t.Fatalf("build layers: %v", err)
	}

	// No retraction/redirect signal → episode should remain open
	var sealedCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_soft_episodes WHERE conversation_id = $1
	`, convID).Scan(&sealedCount); err != nil {
		t.Fatalf("count episodes: %v", err)
	}
	// Episodes may be sealed by other triggers (max_turns, tool_chain_complete),
	// but with only 2 events, retraction-based sealing should not occur.
	// We check that the open episode state exists.
	var openCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM companion_open_episode WHERE conversation_id = $1
	`, convID).Scan(&openCount); err != nil {
		t.Fatalf("count open episodes: %v", err)
	}
	if openCount != 1 {
		t.Fatalf("expected 1 open episode, got %d", openCount)
	}
}

// --- Helper functions ---

func backgroundContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestMemory(t *testing.T, db *sql.DB) *ConversationMemory {
	t.Helper()
	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new conversation memory: %v", err)
	}
	return mem
}

func testTime(sec int) time.Time {
	return time.Date(2026, 1, 15, 10, 0, sec, 0, time.UTC)
}
