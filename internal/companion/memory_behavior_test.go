package companion

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
)

type fakeSessionRecallProvider struct {
	matches []SessionRecallMatch
}

func (f fakeSessionRecallProvider) RecallSessions(context.Context, SessionRecallRequest) ([]SessionRecallMatch, error) {
	return append([]SessionRecallMatch(nil), f.matches...), nil
}

func TestMemoryBehaviorForRetention(t *testing.T) {
	companionBehavior := MemoryBehaviorForRetention(agent.MemoryRetentionCompanion)
	taskBehavior := MemoryBehaviorForRetention(agent.MemoryRetentionTask)
	ephemeralBehavior := MemoryBehaviorForRetention(agent.MemoryRetentionEphemeral)

	if companionBehavior.ImplicitRecallLimit <= taskBehavior.ImplicitRecallLimit {
		t.Fatalf("companion recall=%d want > task=%d", companionBehavior.ImplicitRecallLimit, taskBehavior.ImplicitRecallLimit)
	}
	if taskBehavior.HistoryTurnLimit <= ephemeralBehavior.HistoryTurnLimit {
		t.Fatalf("task history=%d want > ephemeral=%d", taskBehavior.HistoryTurnLimit, ephemeralBehavior.HistoryTurnLimit)
	}
	if !companionBehavior.RequireContextQueryWhenMemorySparse {
		t.Fatal("companion behavior should require context query when memory is sparse")
	}
	if taskBehavior.RequireContextQueryWhenMemorySparse {
		t.Fatal("task behavior should not require sparse-memory context query")
	}
	if companionBehavior.SessionRecallLimit <= taskBehavior.SessionRecallLimit {
		t.Fatalf("companion session recall=%d want > task=%d", companionBehavior.SessionRecallLimit, taskBehavior.SessionRecallLimit)
	}
	if companionBehavior.SessionTimelineSummaryLimit <= taskBehavior.SessionTimelineSummaryLimit {
		t.Fatalf("companion session timeline summaries=%d want > task=%d", companionBehavior.SessionTimelineSummaryLimit, taskBehavior.SessionTimelineSummaryLimit)
	}
	if taskBehavior.SessionTimelineLearningLimit <= ephemeralBehavior.SessionTimelineLearningLimit {
		t.Fatalf("task session timeline learnings=%d want > ephemeral=%d", taskBehavior.SessionTimelineLearningLimit, ephemeralBehavior.SessionTimelineLearningLimit)
	}
}

func TestShouldTriggerAutoCompress(t *testing.T) {
	behavior := MemoryBehavior{
		AutoCompressMinTurns:   4,
		AutoCompressEveryTurns: 6,
	}

	if shouldTriggerAutoCompress(behavior, 2) {
		t.Fatal("should not auto compress before min turns")
	}
	if shouldTriggerAutoCompress(behavior, 4) {
		t.Fatal("should not auto compress on non-cadence boundary")
	}
	if !shouldTriggerAutoCompress(behavior, 6) {
		t.Fatal("should auto compress on cadence boundary")
	}
}

func TestBuildChatMessages_UsesRetentionHistoryLimit(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	mem, err := NewConversationMemory(db)
	if err != nil {
		t.Fatalf("new memory: %v", err)
	}

	turns := []ConversationTurn{
		{ConversationID: "conv-history", Role: "user", Content: "first", CreatedAt: time.Date(2026, time.March, 6, 10, 0, 0, 0, time.UTC)},
		{ConversationID: "conv-history", Role: "assistant", Content: "second", CreatedAt: time.Date(2026, time.March, 6, 10, 1, 0, 0, time.UTC)},
		{ConversationID: "conv-history", Role: "user", Content: "third", CreatedAt: time.Date(2026, time.March, 6, 10, 2, 0, 0, time.UTC)},
	}
	for _, turn := range turns {
		if err := mem.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("append turn: %v", err)
		}
	}

	svc := &Service{
		memory: mem,
		config: ServiceConfig{
			MemoryBehavior: normalizeMemoryBehavior(MemoryBehavior{HistoryTurnLimit: 2}),
		},
	}

	messages, hasHistory := svc.buildChatMessages(ctx, ChatRequest{
		ConversationID: "conv-history",
		Message:        "current",
	})
	if !hasHistory {
		t.Fatal("expected history flag to be true")
	}
	if len(messages) != 3 {
		t.Fatalf("messages=%d want 3", len(messages))
	}
	if messages[0].Content != "second" || messages[1].Content != "third" {
		t.Fatalf("history contents=%q, %q want second, third", messages[0].Content, messages[1].Content)
	}
}

func TestBuildSystemPrompt_InjectsImplicitMemoryRecall(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	store, err := contextvar.Open(ctx, tmp)
	if err != nil {
		t.Fatalf("open context store: %v", err)
	}
	defer store.Close()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	svc := NewService(store, ServiceConfig{
		MemoryDB: db,
		MemoryBehavior: MemoryBehavior{
			ImplicitRecallLimit:    3,
			ImplicitRecallMinScore: 0.1,
		},
	}, nil)
	if svc.memory == nil {
		t.Fatal("expected memory to be initialized")
	}

	if _, err := svc.memory.db.ExecContext(ctx, `
		INSERT INTO companion_hard_state_entries
			(conversation_id, source_event_id, entry_type, key, value_json, confidence, status, created_at)
		VALUES
			($1, 1, 'fact', 'favorite_stack', '"prefers Go and Elixir orchestration work"', 0.95, 'active', CURRENT_TIMESTAMP)
	`, "conv-recall"); err != nil {
		t.Fatalf("seed hard state entry: %v", err)
	}

	prompt, meta, err := svc.buildSystemPrompt(ctx, ChatRequest{
		ConversationID: "conv-recall",
		Message:        "Can you help with Go orchestration again?",
	})
	if err != nil {
		t.Fatalf("build system prompt: %v", err)
	}
	if meta.ImplicitRecallCount == 0 {
		t.Fatal("expected implicit recall count to be populated")
	}
	if !strings.Contains(prompt, "# Relevant Recalled Memory") {
		t.Fatalf("prompt missing recalled memory section: %s", prompt)
	}
	if !strings.Contains(prompt, "prefers Go and Elixir orchestration work") {
		t.Fatalf("prompt missing recalled memory summary: %s", prompt)
	}
}

func TestBuildSystemPrompt_InjectsSessionRecall(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()

	store, err := contextvar.Open(ctx, tmp)
	if err != nil {
		t.Fatalf("open context store: %v", err)
	}
	defer store.Close()

	svc := NewService(store, ServiceConfig{
		MemoryBehavior: MemoryBehavior{
			SessionRecallLimit:    2,
			SessionRecallMinScore: 0.2,
		},
		SessionRecallProvider: fakeSessionRecallProvider{
			matches: []SessionRecallMatch{
				{
					SessionID:            "sess-123",
					ProjectName:          "agentctl",
					Summary:              "Implemented the Jido runtime bridge for companion orchestration.",
					Decisions:            []string{"Use Jido for orchestration and keep agentctl as the semantic tool layer."},
					Gotchas:              []string{"Per-agent runtime config must be injected at start time."},
					KeyFiles:             []string{"internal/v2/adapters/jido/child_spawner.go"},
					Similarity:           0.92,
					StartedAt:            time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC),
					TimelineSummaryLines: []string{"W3 [12-16]: Child spawns now reconcile into kanban state."},
					TimelineDecisions:    []string{"Keep Jido for orchestration and agentctl for semantic tools."},
					TimelineLearnings:    []string{"Per-agent plugin config has to be injected at runtime start."},
					TimelineTools:        []string{"runtime.spawn_child", "runtime.signal"},
				},
			},
		},
	}, nil)

	prompt, meta, err := svc.buildSystemPrompt(ctx, ChatRequest{
		ConversationID: "conv-session",
		Message:        "How should we wire Jido orchestration into agentctl?",
	})
	if err != nil {
		t.Fatalf("build system prompt: %v", err)
	}
	if meta.SessionRecallCount != 1 {
		t.Fatalf("session recall count=%d want 1", meta.SessionRecallCount)
	}
	if !strings.Contains(prompt, "# Related Past Sessions") {
		t.Fatalf("prompt missing related past sessions section: %s", prompt)
	}
	if !strings.Contains(prompt, "Implemented the Jido runtime bridge") {
		t.Fatalf("prompt missing recalled session summary: %s", prompt)
	}
	if !strings.Contains(prompt, "Child spawns now reconcile into kanban state") {
		t.Fatalf("prompt missing session timeline summary: %s", prompt)
	}
	if !strings.Contains(prompt, "runtime.spawn_child") {
		t.Fatalf("prompt missing session timeline tools: %s", prompt)
	}
}
