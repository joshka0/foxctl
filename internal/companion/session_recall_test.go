package companion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

func TestSessionStoreRecallProvider_EnrichesTimeline(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspacePath := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	sessionStore, err := sessions.Open(ctx, root)
	if err != nil {
		t.Fatalf("open sessions store: %v", err)
	}
	defer sessionStore.Close()

	memoryStore, err := memorystore.Open(ctx, root, "")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memoryStore.Close()

	startedAt := time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC)
	sessionRecord, err := sessionStore.Save(ctx, sessions.Session{
		ID:            "sess-jido-1",
		WorkspacePath: workspacePath,
		ProjectName:   "agentctl",
		GitBranch:     "feat/jido",
		Summary:       "Wired Jido orchestration into agentctl runtime flows.",
		Decisions:     []string{"Keep agentctl as the semantic tool layer."},
		Gotchas:       []string{"Runtime child state must reconcile back into the board."},
		KeyFiles:      []string{"internal/v2/adapters/jido/orchestration_reconciler.go"},
		StartedAt:     startedAt,
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}

	for _, window := range []storage.ContextWindow{
		{
			ID:          "window-0",
			SessionID:   sessionRecord.ID,
			WindowIndex: 0,
			StartedAt:   startedAt,
			EndedAt:     startedAt.Add(10 * time.Minute),
			ChunkStart:  0,
			ChunkEnd:    4,
			Summary:     "Initial runtime bridge work",
		},
		{
			ID:          "window-1",
			SessionID:   sessionRecord.ID,
			WindowIndex: 1,
			StartedAt:   startedAt.Add(10 * time.Minute),
			EndedAt:     startedAt.Add(20 * time.Minute),
			ChunkStart:  5,
			ChunkEnd:    9,
			Summary:     "Child orchestration and board reconciliation",
		},
	} {
		if _, err := sessionStore.SaveContextWindow(ctx, window); err != nil {
			t.Fatalf("save context window %s: %v", window.ID, err)
		}
	}

	for _, summary := range []storage.SessionChunkSummary{
		{
			ID:            "summary-0",
			SessionID:     sessionRecord.ID,
			WindowIndex:   0,
			ChunkIndices:  []int{0, 1, 2, 3, 4},
			ChunkIndexMin: 0,
			ChunkIndexMax: 4,
			Summary:       "Built the initial runtime.start_agent bridge for Jido.",
			Tools:         []string{"runtime.start_agent"},
			Files:         []string{"internal/v2/adapters/jido/runtime_adapter.go"},
		},
		{
			ID:            "summary-1",
			SessionID:     sessionRecord.ID,
			WindowIndex:   1,
			ChunkIndices:  []int{5, 6, 7, 8, 9},
			ChunkIndexMin: 5,
			ChunkIndexMax: 9,
			Summary:       "Added child reconciliation so runtime outcomes drive kanban state.",
			Tools:         []string{"runtime.spawn_child", "runtime.signal"},
			Files:         []string{"internal/v2/adapters/jido/orchestration_reconciler.go"},
		},
	} {
		if _, err := sessionStore.SaveChunkSummary(ctx, summary); err != nil {
			t.Fatalf("save chunk summary %s: %v", summary.ID, err)
		}
	}

	for _, entry := range []storage.NamedEntry{
		{
			Name:      "learning:decision:jido:1",
			Type:      "decision",
			Workspace: workspacePath,
			SessionID: sessionRecord.ID,
			Summary:   "Use Jido for orchestration and keep agentctl authoritative for tool semantics.",
			Result:    []byte(`{"summary":"Use Jido for orchestration and keep agentctl authoritative for tool semantics.","window_index":1}`),
		},
		{
			Name:      "learning:gotcha:jido:1",
			Type:      "gotcha",
			Workspace: workspacePath,
			SessionID: sessionRecord.ID,
			Summary:   "Runtime children must publish terminal outcomes back into orchestration events.",
			Result:    []byte(`{"summary":"Runtime children must publish terminal outcomes back into orchestration events.","window_index":1}`),
		},
	} {
		if _, err := memoryStore.Save(ctx, entry); err != nil {
			t.Fatalf("save memory %s: %v", entry.Name, err)
		}
	}

	provider := &SessionStoreRecallProvider{
		Store:       sessionStore,
		MemoryStore: memoryStore,
		Workspace:   workspacePath,
	}

	matches, err := provider.RecallSessions(ctx, SessionRecallRequest{
		Query:                 "Jido orchestration into agentctl runtime",
		Workspace:             workspacePath,
		Limit:                 1,
		MinSimilarity:         0.2,
		IncludeTimeline:       true,
		TimelineSummaryLimit:  2,
		TimelineLearningLimit: 4,
	})
	if err != nil {
		t.Fatalf("recall sessions: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches=%d want 1", len(matches))
	}

	match := matches[0]
	if len(match.TimelineSummaryLines) == 0 {
		t.Fatal("expected timeline summary lines")
	}
	if !strings.Contains(strings.Join(match.TimelineSummaryLines, "\n"), "kanban state") {
		t.Fatalf("timeline summary lines=%v want kanban state detail", match.TimelineSummaryLines)
	}
	if len(match.TimelineDecisions) == 0 || !strings.Contains(match.TimelineDecisions[0], "agentctl authoritative") {
		t.Fatalf("timeline decisions=%v want enriched decision", match.TimelineDecisions)
	}
	if len(match.TimelineGotchas) == 0 || !strings.Contains(match.TimelineGotchas[0], "terminal outcomes") {
		t.Fatalf("timeline gotchas=%v want enriched gotcha", match.TimelineGotchas)
	}
	if len(match.TimelineTools) == 0 || !strings.Contains(strings.Join(match.TimelineTools, ","), "runtime.spawn_child") {
		t.Fatalf("timeline tools=%v want runtime.spawn_child", match.TimelineTools)
	}
}
