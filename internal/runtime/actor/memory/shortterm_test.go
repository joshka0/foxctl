package memory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO required)
)

func setupTestMemoryDB(t *testing.T) *sql.DB {
	// Use file::memory:?cache=shared to ensure all connections in the pool
	// share the same in-memory database. Without this, each connection would
	// get its own empty database, causing schema to "disappear" between queries.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	return db
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.RawBufferSize != 5 {
		t.Errorf("RawBufferSize = %d, want 5", cfg.RawBufferSize)
	}
	if cfg.RawTokenBudget != 8000 {
		t.Errorf("RawTokenBudget = %d, want 8000", cfg.RawTokenBudget)
	}
	if cfg.RecentSummarySize != 3 {
		t.Errorf("RecentSummarySize = %d, want 3", cfg.RecentSummarySize)
	}
	if cfg.L1TokenBudget != 6000 {
		t.Errorf("L1TokenBudget = %d, want 6000", cfg.L1TokenBudget)
	}
	if cfg.L2TokenBudget != 4000 {
		t.Errorf("L2TokenBudget = %d, want 4000", cfg.L2TokenBudget)
	}
	if cfg.TotalTokenBudget != 18000 {
		t.Errorf("TotalTokenBudget = %d, want 18000", cfg.TotalTokenBudget)
	}
	if cfg.SummarizerModel != "gemini-flash" {
		t.Errorf("SummarizerModel = %q, want gemini-flash", cfg.SummarizerModel)
	}
}

func TestNew(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_WithOptions(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	cfg := Config{
		RawBufferSize:  10,
		RawTokenBudget: 16000,
	}

	redactor := NewSecretRedactor()

	m, err := New(
		context.Background(), db,
		WithConfig(cfg),
		WithRedactor(redactor),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if m.config.RawBufferSize != 10 {
		t.Errorf("config.RawBufferSize = %d, want 10", m.config.RawBufferSize)
	}
}

func TestShortTermMemory_ensureSchema(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify table exists
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='actor_memory_state'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to check schema: %v", err)
	}
	if count != 1 {
		t.Error("actor_memory_state table not created")
	}

	_ = m // use m
}

func TestShortTermMemory_InitState(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	err = m.InitState(ctx, "actor-1", "session-123")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Verify state was created
	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state == nil {
		t.Fatal("GetState() returned nil")
		return
	}
	if state.ActorID != "actor-1" {
		t.Errorf("ActorID = %q, want actor-1", state.ActorID)
	}
	if state.SessionID != "session-123" {
		t.Errorf("SessionID = %q, want session-123", state.SessionID)
	}
}

func TestShortTermMemory_InitState_Upsert(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// First init
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Second init with different session (should upsert)
	err = m.InitState(ctx, "actor-1", "session-2")
	if err != nil {
		t.Fatalf("InitState() second error = %v", err)
	}

	// Verify session was updated
	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.SessionID != "session-2" {
		t.Errorf("SessionID = %q, want session-2", state.SessionID)
	}
}

func TestShortTermMemory_GetState_NotFound(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	state, err := m.GetState(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state != nil {
		t.Error("GetState() should return nil for nonexistent actor")
	}
}

func TestShortTermMemory_SetTaskContext(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state first
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Set task context
	err = m.SetTaskContext(ctx, "actor-1", "Implement feature X")
	if err != nil {
		t.Fatalf("SetTaskContext() error = %v", err)
	}

	// Verify
	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.Task != "Implement feature X" {
		t.Errorf("Task = %q, want 'Implement feature X'", state.Task)
	}
}

func TestShortTermMemory_AppendTurn(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Append turn
	turn := Turn{
		Index:      0,
		Role:       "user",
		Content:    "Hello, world!",
		Timestamp:  time.Now(),
		TokenCount: 5,
	}

	err = m.AppendTurn(ctx, "actor-1", turn)
	if err != nil {
		t.Fatalf("AppendTurn() error = %v", err)
	}

	// Verify state updated
	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.TotalTurns != 1 {
		t.Errorf("TotalTurns = %d, want 1", state.TotalTurns)
	}
	if state.TokenEstimate != 5 {
		t.Errorf("TokenEstimate = %d, want 5", state.TokenEstimate)
	}
}

func TestShortTermMemory_Clear(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Clear
	err = m.Clear(ctx, "actor-1")
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	// Verify deleted
	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state != nil {
		t.Error("GetState() should return nil after Clear")
	}
}

func TestShortTermMemory_Export(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Export (same as GetState)
	state, err := m.Export(ctx, "actor-1")
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if state == nil {
		t.Fatal("Export() returned nil")
		return
	}
	if state.ActorID != "actor-1" {
		t.Errorf("ActorID = %q, want actor-1", state.ActorID)
	}
}

func TestShortTermMemory_GetContext_Empty(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// GetContext returns empty for uninitialized actor
	content, err := m.GetContext(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}
	if content != "" {
		t.Errorf("GetContext() = %q, want empty string", content)
	}
}

func TestTurn(t *testing.T) {
	now := time.Now()
	turn := Turn{
		Index:   0,
		Role:    "assistant",
		Content: "Hello!",
		ToolCalls: []ToolCall{
			{Name: "read_file", Input: `{"path": "test.go"}`, Output: "content"},
		},
		Timestamp:  now,
		TokenCount: 10,
	}

	if turn.Index != 0 {
		t.Errorf("Index = %d, want 0", turn.Index)
	}
	if turn.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", turn.Role)
	}
	if len(turn.ToolCalls) != 1 {
		t.Errorf("len(ToolCalls) = %d, want 1", len(turn.ToolCalls))
	}
	if turn.ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want read_file", turn.ToolCalls[0].Name)
	}
}

func TestSummary(t *testing.T) {
	now := time.Now()
	summary := Summary{
		TurnRange:  TurnRange{Start: 0, End: 5},
		Content:    "Discussion about feature X",
		KeyPoints:  []string{"Decided on approach A", "Need to handle edge cases"},
		Decisions:  []string{"Use pattern X"},
		TokenCount: 50,
		CreatedAt:  now,
	}

	if summary.TurnRange.Start != 0 || summary.TurnRange.End != 5 {
		t.Errorf("TurnRange = %v, want {0, 5}", summary.TurnRange)
	}
	if len(summary.KeyPoints) != 2 {
		t.Errorf("len(KeyPoints) = %d, want 2", len(summary.KeyPoints))
	}
	if len(summary.Decisions) != 1 {
		t.Errorf("len(Decisions) = %d, want 1", len(summary.Decisions))
	}
}

func TestState(t *testing.T) {
	now := time.Now()
	state := State{
		ActorID:              "actor-1",
		SessionID:            "session-123",
		Task:                 "Implement feature",
		NextTurnToSummarize:  5,
		NextSummaryToDistill: 2,
		L1ArtifactID:         "sha256:abc123",
		L2ArtifactID:         "sha256:def456",
		TotalTurns:           10,
		TokenEstimate:        500,
		LastSummarizeAt:      now,
		LastDistillAt:        now,
		UpdatedAt:            now,
	}

	if state.ActorID != "actor-1" {
		t.Errorf("ActorID = %q, want actor-1", state.ActorID)
	}
	if state.NextTurnToSummarize != 5 {
		t.Errorf("NextTurnToSummarize = %d, want 5", state.NextTurnToSummarize)
	}
	if state.L1ArtifactID != "sha256:abc123" {
		t.Errorf("L1ArtifactID = %q, want sha256:abc123", state.L1ArtifactID)
	}
}

func TestShortTermMemory_TurnPersistence(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Append multiple turns
	for i := 0; i < 3; i++ {
		turn := Turn{
			Index:      i,
			Role:       "user",
			Content:    "Message " + string(rune('A'+i)),
			Timestamp:  time.Now(),
			TokenCount: 10,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn(%d) error = %v", i, err)
		}
	}

	// Verify state
	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.TotalTurns != 3 {
		t.Errorf("TotalTurns = %d, want 3", state.TotalTurns)
	}
	if state.TokenEstimate != 30 {
		t.Errorf("TokenEstimate = %d, want 30", state.TokenEstimate)
	}

	// Retrieve turns
	turns, err := m.GetTurns(ctx, "actor-1", 0, 3)
	if err != nil {
		t.Fatalf("GetTurns() error = %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("len(turns) = %d, want 3", len(turns))
	}
	if turns[0].Content != "Message A" {
		t.Errorf("turns[0].Content = %q, want 'Message A'", turns[0].Content)
	}
	if turns[2].Content != "Message C" {
		t.Errorf("turns[2].Content = %q, want 'Message C'", turns[2].Content)
	}
}

func TestShortTermMemory_GetRecentTurns(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Append 5 turns
	for i := 0; i < 5; i++ {
		turn := Turn{
			Index:      i,
			Role:       "user",
			Content:    "Message " + string(rune('A'+i)),
			Timestamp:  time.Now(),
			TokenCount: 10,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn(%d) error = %v", i, err)
		}
	}

	// Get last 2 turns
	turns, err := m.GetRecentTurns(ctx, "actor-1", 2)
	if err != nil {
		t.Fatalf("GetRecentTurns() error = %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("len(turns) = %d, want 2", len(turns))
	}
	// Should be in oldest-first order
	if turns[0].Content != "Message D" {
		t.Errorf("turns[0].Content = %q, want 'Message D'", turns[0].Content)
	}
	if turns[1].Content != "Message E" {
		t.Errorf("turns[1].Content = %q, want 'Message E'", turns[1].Content)
	}
}

func TestShortTermMemory_TurnWithToolCalls(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Append turn with tool calls
	turn := Turn{
		Index:   0,
		Role:    "assistant",
		Content: "Let me read that file",
		ToolCalls: []ToolCall{
			{Name: "read_file", Input: `{"path": "test.go"}`, Output: "package test"},
			{Name: "write_file", Input: `{"path": "out.txt"}`, Output: "ok"},
		},
		Timestamp:  time.Now(),
		TokenCount: 20,
	}
	if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
		t.Fatalf("AppendTurn() error = %v", err)
	}

	// Retrieve and verify tool calls persisted
	turns, err := m.GetTurns(ctx, "actor-1", 0, 1)
	if err != nil {
		t.Fatalf("GetTurns() error = %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("len(turns) = %d, want 1", len(turns))
	}
	if len(turns[0].ToolCalls) != 2 {
		t.Fatalf("len(ToolCalls) = %d, want 2", len(turns[0].ToolCalls))
	}
	if turns[0].ToolCalls[0].Name != "read_file" {
		t.Errorf("ToolCalls[0].Name = %q, want 'read_file'", turns[0].ToolCalls[0].Name)
	}
	if turns[0].ToolCalls[1].Name != "write_file" {
		t.Errorf("ToolCalls[1].Name = %q, want 'write_file'", turns[0].ToolCalls[1].Name)
	}
}

func TestShortTermMemory_SecretRedaction(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Append turn with secret (OpenAI API keys are sk- followed by 48 chars)
	turn := Turn{
		Index:      0,
		Role:       "user",
		Content:    "My API key is sk-1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL",
		Timestamp:  time.Now(),
		TokenCount: 15,
	}
	if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
		t.Fatalf("AppendTurn() error = %v", err)
	}

	// Retrieve and verify secret was redacted
	turns, err := m.GetTurns(ctx, "actor-1", 0, 1)
	if err != nil {
		t.Fatalf("GetTurns() error = %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("len(turns) = %d, want 1", len(turns))
	}
	// The secret should be redacted
	if turns[0].Content == turn.Content {
		t.Error("Secret was not redacted from persisted turn")
	}
	if !containsRedaction(turns[0].Content) {
		t.Errorf("Expected redaction marker in content: %q", turns[0].Content)
	}
}

func containsRedaction(s string) bool {
	return len(s) > 0 && s != "" && (contains(s, "[REDACTED") || contains(s, "***"))
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestShortTermMemory_ClearDeletesTurns(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state and add turns
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		turn := Turn{
			Index:      i,
			Role:       "user",
			Content:    "Message",
			Timestamp:  time.Now(),
			TokenCount: 5,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn() error = %v", err)
		}
	}

	// Verify turns exist
	turns, err := m.GetTurns(ctx, "actor-1", 0, 10)
	if err != nil {
		t.Fatalf("GetTurns() error = %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("len(turns) = %d, want 3", len(turns))
	}

	// Clear
	if err := m.Clear(ctx, "actor-1"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	// Verify turns deleted
	turns, err = m.GetTurns(ctx, "actor-1", 0, 10)
	if err != nil {
		t.Fatalf("GetTurns() error = %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("len(turns) = %d, want 0 after Clear", len(turns))
	}
}

func TestShortTermMemory_GetContext_WithTurns(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state and add turns
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		turn := Turn{
			Index:      i,
			Role:       "user",
			Content:    fmt.Sprintf("Message %d", i),
			Timestamp:  time.Now(),
			TokenCount: 10,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn() error = %v", err)
		}
	}

	// Get context - should include raw turns
	content, err := m.GetContext(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}

	if !strings.Contains(content, "Recent Conversation") {
		t.Error("context should contain Recent Conversation section")
	}
	if !strings.Contains(content, "Message 0") {
		t.Error("context should contain Message 0")
	}
}

func TestShortTermMemory_GetContextWithSummaries(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Manually insert a summary
	_, err = db.ExecContext(ctx, `
		INSERT INTO actor_summaries (actor_id, session_id, summary_index, turn_start, turn_end, content, token_count)
		VALUES ('actor-1', 'session-1', 0, 0, 4, 'Summary of turns 0-4', 50)
	`)
	if err != nil {
		t.Fatalf("insert summary: %v", err)
	}

	// Get context - should include summary
	content, err := m.GetContext(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}

	if !strings.Contains(content, "Recent Summaries") {
		t.Error("context should contain Recent Summaries section")
	}
	if !strings.Contains(content, "[Turns 0-4]") {
		t.Error("context should contain turn range")
	}
}

func TestShortTermMemory_GetContextWithL2(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state with L2 content
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Set L2 artifact (distilled summary)
	_, err = db.ExecContext(ctx, `
		UPDATE actor_memory_state SET l2_artifact_id = 'Distilled history content' WHERE actor_id = 'actor-1'
	`)
	if err != nil {
		t.Fatalf("update l2_artifact_id: %v", err)
	}

	// Get context - should include L2
	content, err := m.GetContext(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetContext() error = %v", err)
	}

	if !strings.Contains(content, "Session History (Distilled)") {
		t.Error("context should contain Session History section")
	}
	if !strings.Contains(content, "Distilled history content") {
		t.Error("context should contain distilled content")
	}
}

type stubSummarizer struct {
	called int
}

func (s *stubSummarizer) SummarizeTurns(ctx context.Context, task string, turns []Turn) (*Summary, error) {
	s.called++
	return &Summary{
		TurnRange: TurnRange{Start: turns[0].Index, End: turns[len(turns)-1].Index},
		Content:   "summary",
		CreatedAt: time.Now(),
	}, nil
}

func (s *stubSummarizer) DistillSummaries(ctx context.Context, task string, summaries []Summary) (string, error) {
	return "distilled", nil
}

func (s *stubSummarizer) FilterByRelevance(ctx context.Context, task string, items []string) ([]string, error) {
	return items, nil
}

func TestShortTermMemory_CompactionAssignsMonotonicSummaryIndex(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	sum := &stubSummarizer{}
	m, err := New(context.Background(), db, WithSummarizer(sum), WithConfig(Config{
		RawBufferSize: 1, // force compaction on every turn
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := m.InitState(ctx, "actor-1", "session-1"); err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Add first turn and wait for its compaction to complete before adding second
	// This ensures each turn's compaction completes in isolation, avoiding races
	for turnIdx := 0; turnIdx < 2; turnIdx++ {
		turn := Turn{
			Index:      turnIdx,
			Role:       "user",
			Content:    fmt.Sprintf("message %d", turnIdx),
			Timestamp:  time.Now(),
			TokenCount: 5,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn(%d) error = %v", turnIdx, err)
		}

		// Wait for this turn's compaction to complete before adding the next
		expectedSummaries := turnIdx + 1
		for i := 0; i < 100; i++ {
			var count int
			err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actor_summaries WHERE actor_id = ?`, "actor-1").Scan(&count)
			if err != nil {
				t.Fatalf("count summaries: %v", err)
			}
			if count >= expectedSummaries {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Verify final state
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actor_summaries WHERE actor_id = ?`, "actor-1").Scan(&count)
	if err != nil {
		t.Fatalf("count summaries: %v", err)
	}
	if count != 2 {
		t.Fatalf("summary count = %d, want 2", count)
	}

	var maxIdx int
	err = db.QueryRowContext(ctx, `SELECT MAX(summary_index) FROM actor_summaries WHERE actor_id = ?`, "actor-1").Scan(&maxIdx)
	if err != nil {
		t.Fatalf("max summary_index: %v", err)
	}
	if maxIdx != 1 {
		t.Fatalf("max summary_index = %d, want 1 (monotonic)", maxIdx)
	}

	state, err := m.GetState(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetState() error = %v", err)
	}
	if state.NextTurnToSummarize != 2 {
		t.Fatalf("NextTurnToSummarize = %d, want 2", state.NextTurnToSummarize)
	}
	if sum.called < 2 {
		t.Fatalf("summarizer called %d times, want at least 2", sum.called)
	}
}

func TestShortTermMemory_RunCompaction(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	mockSummarizer := &MockSummarizer{}
	m, err := New(context.Background(), db, WithSummarizer(mockSummarizer))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	// Add enough turns to trigger compaction
	for i := 0; i < 6; i++ {
		turn := Turn{
			Index:      i,
			Role:       "user",
			Content:    fmt.Sprintf("Message %d", i),
			Timestamp:  time.Now(),
			TokenCount: 10,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn() error = %v", err)
		}
	}

	// Manually run compaction
	err = m.RunCompaction(ctx, "actor-1")
	if err != nil {
		t.Fatalf("RunCompaction() error = %v", err)
	}

	// Verify summary was created - use polling to handle async compaction race
	// AppendTurn triggers async compaction via goroutine, which may complete
	// before or after RunCompaction
	var count int
	for i := 0; i < 50; i++ {
		err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actor_summaries WHERE actor_id = 'actor-1'`).Scan(&count)
		if err != nil {
			t.Fatalf("query summaries: %v", err)
		}
		if count > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if count == 0 {
		t.Error("expected summary to be created")
	}
}

func TestShortTermMemory_RunCompactionNoSummarizer(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db, WithConfig(Config{})) // No summarizer
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	err = m.RunCompaction(ctx, "actor-1")
	if err == nil {
		t.Error("expected error when no summarizer configured")
	}
}

func TestCompactionRunner_StartStop(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runner := NewCompactionRunner(m, 10*time.Millisecond)

	ctx := context.Background()
	runner.Start(ctx)

	// Let it run briefly
	time.Sleep(25 * time.Millisecond)

	// Stop should not block
	done := make(chan struct{})
	go func() {
		runner.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good
	case <-time.After(time.Second):
		t.Error("Stop() timed out")
	}
}

func TestCompactionRunner_DefaultInterval(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	m, err := New(context.Background(), db)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Zero interval should use default
	runner := NewCompactionRunner(m, 0)
	if runner.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", runner.interval)
	}
}

func TestCompactionRunner_CompactsActors(t *testing.T) {
	db := setupTestMemoryDB(t)
	defer db.Close()

	mockSummarizer := &MockSummarizer{}
	m, err := New(context.Background(), db, WithSummarizer(mockSummarizer), WithConfig(Config{
		RawBufferSize:     3, // Lower threshold for testing
		RawTokenBudget:    8000,
		RecentSummarySize: 3,
		L1TokenBudget:     6000,
		L2TokenBudget:     4000,
		TotalTokenBudget:  18000,
	}))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Init state and add turns
	err = m.InitState(ctx, "actor-1", "session-1")
	if err != nil {
		t.Fatalf("InitState() error = %v", err)
	}

	for i := 0; i < 4; i++ {
		turn := Turn{
			Index:      i,
			Role:       "user",
			Content:    fmt.Sprintf("Message %d", i),
			Timestamp:  time.Now(),
			TokenCount: 10,
		}
		if err := m.AppendTurn(ctx, "actor-1", turn); err != nil {
			t.Fatalf("AppendTurn() error = %v", err)
		}
	}

	// Create and run compaction
	runner := NewCompactionRunner(m, 10*time.Millisecond)
	runner.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	runner.Stop()

	// Verify summary was created
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM actor_summaries WHERE actor_id = 'actor-1'`).Scan(&count)
	if err != nil {
		t.Fatalf("query summaries: %v", err)
	}
	if count == 0 {
		t.Error("expected summary to be created by compaction runner")
	}
}
