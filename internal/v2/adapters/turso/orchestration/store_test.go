package orchestration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
)

func TestStore_ApplyAndBoard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_store.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{
		LaneOptions: coreorchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"human review"},
		},
	})
	store.SetNowForTest(func() time.Time { return time.Date(2026, time.March, 5, 14, 0, 0, 0, time.UTC) })

	err = store.Apply(ctx, coreevents.Event{
		ID:            "evt-001",
		StreamID:      "run-001",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 5, 14, 0, 1, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-001",
		ActorID:       "actor:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-1",
			"issue_identifier": "ABC-1",
			"title":            "Implement scheduler",
			"state":            "Running",
			"eligibility":      "eligible",
			"agent_id":         "agent:worker-1",
			"tracker_state":    "In Progress",
		}),
	})
	if err != nil {
		t.Fatalf("apply event 1: %v", err)
	}

	err = store.Apply(ctx, coreevents.Event{
		ID:            "evt-002",
		StreamID:      "run-002",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunCompleted,
		OccurredAt:    time.Date(2026, time.March, 5, 14, 0, 2, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-002",
		ActorID:       "actor:overseer",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-2",
			"issue_identifier": "ABC-2",
			"title":            "Close issue",
			"state":            "Released",
			"eligibility":      "eligible",
			"tracker_state":    "Done",
		}),
	})
	if err != nil {
		t.Fatalf("apply event 2: %v", err)
	}

	board, err := store.Board(ctx, coreorchestration.BoardRequest{
		WorkspaceID: "ws-1",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if got := board.Counts[coreorchestration.LaneRunning]; got != 1 {
		t.Fatalf("running count=%d want 1", got)
	}
	if got := board.Counts[coreorchestration.LaneDone]; got != 1 {
		t.Fatalf("done count=%d want 1", got)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-2"})
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if card.Card.Lane != coreorchestration.LaneDone {
		t.Fatalf("issue-2 lane=%q want %q", card.Card.Lane, coreorchestration.LaneDone)
	}

	cardRunning, err := store.Card(ctx, coreorchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-1"})
	if err != nil {
		t.Fatalf("running card: %v", err)
	}
	if cardRunning.Card.AgentID != "agent:worker-1" {
		t.Fatalf("issue-1 agent_id=%q want agent:worker-1", cardRunning.Card.AgentID)
	}
}

func TestStore_DeleteCards_RemovesProjectionAndAppliedEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_delete.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	store.SetNowForTest(func() time.Time { return time.Date(2026, time.April, 6, 13, 0, 0, 0, time.UTC) })

	apply := func(eventID, issueID string) {
		t.Helper()
		if err := store.Apply(ctx, coreevents.Event{
			ID:            eventID,
			StreamID:      "run-" + issueID,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    time.Date(2026, time.April, 6, 13, 0, 1, 0, time.UTC),
			Command:       "orchestration/dispatch-issue",
			RequestID:     "req-" + issueID,
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id": "ws-delete",
				"issue_id":     issueID,
				"title":        issueID,
				"state":        "Released",
				"eligibility":  "eligible",
			}),
		}); err != nil {
			t.Fatalf("apply %s: %v", issueID, err)
		}
	}

	apply("evt-del-1", "issue-delete-1")
	apply("evt-del-2", "issue-delete-2")

	deleted, err := store.DeleteCards(ctx, "ws-delete", []string{"issue-delete-1", "issue-delete-2"}, []string{"evt-del-1", "evt-del-2"})
	if err != nil {
		t.Fatalf("DeleteCards() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d want 2", deleted)
	}

	board, err := store.Board(ctx, coreorchestration.BoardRequest{WorkspaceID: "ws-delete", Limit: 10})
	if err != nil {
		t.Fatalf("Board() error = %v", err)
	}
	total := 0
	for _, count := range board.Counts {
		total += count
	}
	if total != 0 {
		t.Fatalf("board count=%d want 0", total)
	}
}

func TestStore_ArchiveAndRestoreCards(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_archive.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	store.SetNowForTest(func() time.Time { return time.Date(2026, time.April, 6, 14, 0, 0, 0, time.UTC) })

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-arch-1",
		StreamID:      "run-arch-1",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.April, 6, 14, 0, 1, 0, time.UTC),
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-arch-1",
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id": "ws-archive",
			"issue_id":     "issue-archive-1",
			"title":        "Archive me",
			"state":        "Released",
			"eligibility":  "eligible",
		}),
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	updated, err := store.ArchiveCards(ctx, "ws-archive", []string{"issue-archive-1"})
	if err != nil {
		t.Fatalf("ArchiveCards() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("updated=%d want 1", updated)
	}

	activeBoard, err := store.Board(ctx, coreorchestration.BoardRequest{WorkspaceID: "ws-archive", Limit: 10})
	if err != nil {
		t.Fatalf("Board(active) error = %v", err)
	}
	activeTotal := 0
	for _, count := range activeBoard.Counts {
		activeTotal += count
	}
	if activeTotal != 0 {
		t.Fatalf("active board count=%d want 0", activeTotal)
	}

	archivedBoard, err := store.Board(ctx, coreorchestration.BoardRequest{WorkspaceID: "ws-archive", Limit: 10, ArchivedOnly: true})
	if err != nil {
		t.Fatalf("Board(archived) error = %v", err)
	}
	archivedTotal := 0
	for _, count := range archivedBoard.Counts {
		archivedTotal += count
	}
	if archivedTotal != 1 {
		t.Fatalf("archived board count=%d want 1", archivedTotal)
	}

	updated, err = store.RestoreCards(ctx, "ws-archive", []string{"issue-archive-1"})
	if err != nil {
		t.Fatalf("RestoreCards() error = %v", err)
	}
	if updated != 1 {
		t.Fatalf("restored=%d want 1", updated)
	}
}

func TestMigrateSchema_AddsAgentIDColumnToLegacyCards(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_legacy_agent_id.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })

	for _, stmt := range []string{
		`CREATE TABLE v2_orchestration_cards (
			issue_id TEXT PRIMARY KEY,
			workspace_id TEXT,
			issue_identifier TEXT,
			title TEXT,
			state TEXT NOT NULL,
			lane TEXT NOT NULL,
			tracker_state TEXT,
			policy_status TEXT,
			last_outcome TEXT,
			eligibility TEXT,
			denial_reason TEXT,
			suggestion TEXT,
			run_id TEXT,
			actor_id TEXT,
			attempt INTEGER,
			retry_due_at TEXT,
			last_event_type TEXT,
			last_event_at TEXT,
			last_request_id TEXT,
			last_event_id TEXT NOT NULL,
			last_stream_version INTEGER NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE v2_orchestration_applied_events (
			event_id TEXT PRIMARY KEY,
			command TEXT,
			scope_id TEXT,
			request_id TEXT,
			applied_at TEXT NOT NULL
		)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}

	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("MigrateSchema() error = %v", err)
	}

	store := NewStore(db, StoreOptions{})
	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-legacy-1",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-legacy-1",
		StreamID:      "run-legacy-1",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 6, 10, 0, 0, 0, time.UTC),
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"issue_id":    "issue-legacy-1",
			"state":       "Running",
			"eligibility": "eligible",
			"agent_id":    "agent:legacy-worker",
		}),
	}); err != nil {
		t.Fatalf("apply after migrate: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{IssueID: "issue-legacy-1"})
	if err != nil {
		t.Fatalf("card after migrate: %v", err)
	}
	if card.Card.AgentID != "agent:legacy-worker" {
		t.Fatalf("agent_id=%q want agent:legacy-worker", card.Card.AgentID)
	}
}

func TestStore_Apply_IdempotentByEventAndRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_idempotent.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	store.SetNowForTest(func() time.Time { return time.Date(2026, time.March, 5, 14, 10, 0, 0, time.UTC) })

	first := coreevents.Event{
		ID:            "evt-101",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-101",
		StreamID:      "run-101",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 5, 14, 10, 1, 0, time.UTC),
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"issue_id":    "issue-101",
			"state":       "Claimed",
			"eligibility": "eligible",
		}),
	}
	if err := store.Apply(ctx, first); err != nil {
		t.Fatalf("apply first: %v", err)
	}
	// Duplicate by event_id should be ignored.
	dupEventID := first
	dupEventID.Payload = coreevents.MustMarshalPayload(map[string]any{
		"issue_id": "issue-101",
		"state":    "Running",
	})
	if err := store.Apply(ctx, dupEventID); err != nil {
		t.Fatalf("apply duplicate event id: %v", err)
	}
	// Duplicate by (command, scope_id, request_id) should be ignored.
	dupRequest := first
	dupRequest.ID = "evt-102"
	dupRequest.Payload = coreevents.MustMarshalPayload(map[string]any{
		"issue_id": "issue-101",
		"state":    "Running",
	})
	if err := store.Apply(ctx, dupRequest); err != nil {
		t.Fatalf("apply duplicate request: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{IssueID: "issue-101"})
	if err != nil {
		t.Fatalf("card after duplicates: %v", err)
	}
	if card.Card.State != coreorchestration.StateClaimed {
		t.Fatalf("state after duplicates=%q want %q", card.Card.State, coreorchestration.StateClaimed)
	}

	third := first
	third.ID = "evt-103"
	third.RequestID = "req-103"
	third.Payload = coreevents.MustMarshalPayload(map[string]any{
		"issue_id": "issue-101",
		"state":    "Running",
	})
	if err := store.Apply(ctx, third); err != nil {
		t.Fatalf("apply third: %v", err)
	}
	card, err = store.Card(ctx, coreorchestration.CardRequest{IssueID: "issue-101"})
	if err != nil {
		t.Fatalf("card after third: %v", err)
	}
	if card.Card.State != coreorchestration.StateRunning {
		t.Fatalf("state after third=%q want %q", card.Card.State, coreorchestration.StateRunning)
	}
}

func TestStore_BoardCursorAndLaneFilter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_cursor.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	store.SetNowForTest(func() time.Time { return time.Date(2026, time.March, 5, 14, 20, 0, 0, time.UTC) })

	apply := func(id, issue, state string) {
		t.Helper()
		if err := store.Apply(ctx, coreevents.Event{
			ID:            id,
			Command:       "orchestration/dispatch-issue",
			RequestID:     id,
			StreamID:      "run-" + issue,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    time.Date(2026, time.March, 5, 14, 20, 0, 0, time.UTC),
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id": "ws-1",
				"issue_id":     issue,
				"state":        state,
				"eligibility":  "eligible",
			}),
		}); err != nil {
			t.Fatalf("apply %s: %v", id, err)
		}
	}

	apply("evt-a", "issue-a", "Running")
	apply("evt-b", "issue-b", "Running")
	apply("evt-c", "issue-c", "Released")

	page1, err := store.Board(ctx, coreorchestration.BoardRequest{WorkspaceID: "ws-1", Limit: 2})
	if err != nil {
		t.Fatalf("board page1: %v", err)
	}
	if len(page1.Lanes) == 0 {
		t.Fatal("expected lane columns")
	}
	if page1.NextCursor == "" {
		t.Fatal("expected next cursor for first page")
	}

	page2, err := store.Board(ctx, coreorchestration.BoardRequest{
		WorkspaceID: "ws-1",
		Limit:       2,
		Cursor:      page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("board page2: %v", err)
	}
	totalCards := 0
	for _, lane := range page2.Lanes {
		totalCards += len(lane.Cards)
	}
	if totalCards != 1 {
		t.Fatalf("page2 cards=%d want 1", totalCards)
	}

	filtered, err := store.Board(ctx, coreorchestration.BoardRequest{
		WorkspaceID: "ws-1",
		Limit:       10,
		Lane:        coreorchestration.LaneRunning,
	})
	if err != nil {
		t.Fatalf("board filtered: %v", err)
	}
	if len(filtered.Lanes) != 1 || filtered.Lanes[0].ID != coreorchestration.LaneRunning {
		t.Fatalf("filtered lanes=%+v want only running", filtered.Lanes)
	}
}

func TestStore_ListRunningCards_FiltersWorkspaceArchivedAndCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_running_cards.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	apply := func(id, workspaceID, issueID, state string) {
		t.Helper()
		if err := store.Apply(ctx, coreevents.Event{
			ID:            id,
			Command:       "orchestration/dispatch-issue",
			RequestID:     "req-" + issueID,
			StreamID:      "run-" + issueID,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    time.Date(2026, time.March, 5, 14, 25, 0, 0, time.UTC),
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id": workspaceID,
				"issue_id":     issueID,
				"state":        state,
				"eligibility":  "eligible",
				"agent_id":     "agent:" + issueID,
			}),
		}); err != nil {
			t.Fatalf("apply %s: %v", issueID, err)
		}
	}

	apply("evt-running-a", "ws-1", "issue-a", "Running")
	apply("evt-running-b", "ws-1", "issue-b", "Running")
	apply("evt-released-c", "ws-1", "issue-c", "Released")
	apply("evt-running-d", "ws-2", "issue-d", "Running")

	if _, err := store.ArchiveCards(ctx, "ws-1", []string{"issue-b"}); err != nil {
		t.Fatalf("ArchiveCards() error = %v", err)
	}

	cards, err := store.ListRunningCards(ctx, coreorchestration.RunningCardsRequest{
		WorkspaceID: "ws-1",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListRunningCards() error = %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards=%d want 1: %+v", len(cards), cards)
	}
	if cards[0].IssueID != "issue-a" {
		t.Fatalf("issue_id=%q want issue-a", cards[0].IssueID)
	}
	if cards[0].State != coreorchestration.StateRunning {
		t.Fatalf("state=%q want %q", cards[0].State, coreorchestration.StateRunning)
	}
	if cards[0].AgentID != "agent:issue-a" {
		t.Fatalf("agent_id=%q want agent:issue-a", cards[0].AgentID)
	}

	afterCursor, err := store.ListRunningCards(ctx, coreorchestration.RunningCardsRequest{
		WorkspaceID: "ws-1",
		Cursor:      "issue-a",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListRunningCards(cursor) error = %v", err)
	}
	if len(afterCursor) != 0 {
		t.Fatalf("cursor cards=%d want 0: %+v", len(afterCursor), afterCursor)
	}
}

func TestStore_Apply_IdempotencyScopeUsesIssueForIssueCommands(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_scope.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	event := func(id, reqID, issueID string) coreevents.Event {
		return coreevents.Event{
			ID:            id,
			Command:       "orchestration/dispatch-issue",
			RequestID:     reqID,
			StreamID:      "run-" + issueID,
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    time.Date(2026, time.March, 5, 14, 30, 0, 0, time.UTC),
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id": "ws-scope",
				"issue_id":     issueID,
				"state":        "Claimed",
				"eligibility":  "eligible",
			}),
		}
	}

	if err := store.Apply(ctx, event("evt-201", "req-same", "issue-201")); err != nil {
		t.Fatalf("apply issue-201: %v", err)
	}
	// Same request_id and workspace, but different issue must still apply.
	if err := store.Apply(ctx, event("evt-202", "req-same", "issue-202")); err != nil {
		t.Fatalf("apply issue-202: %v", err)
	}

	if _, err := store.Card(ctx, coreorchestration.CardRequest{WorkspaceID: "ws-scope", IssueID: "issue-201"}); err != nil {
		t.Fatalf("missing card issue-201: %v", err)
	}
	if _, err := store.Card(ctx, coreorchestration.CardRequest{WorkspaceID: "ws-scope", IssueID: "issue-202"}); err != nil {
		t.Fatalf("missing card issue-202: %v", err)
	}
}

func TestStore_Apply_SparsePayloadPreservesIdentityFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_sparse.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	firstOccurred := time.Date(2026, time.March, 5, 14, 40, 0, 0, time.UTC)
	secondOccurred := time.Date(2026, time.March, 5, 14, 40, 1, 0, time.UTC)

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-301",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-301",
		StreamID:      "run-301",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    firstOccurred,
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":     "ws-sparse",
			"issue_id":         "issue-301",
			"issue_identifier": "ABC-301",
			"title":            "Initial title",
			"state":            "Claimed",
			"eligibility":      "eligible",
		}),
	}); err != nil {
		t.Fatalf("apply first: %v", err)
	}

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-302",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-302",
		StreamID:      "run-301",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 2,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    secondOccurred,
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"issue_id": "issue-301",
			"state":    "Running",
		}),
	}); err != nil {
		t.Fatalf("apply second: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{IssueID: "issue-301"})
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if card.Card.IssueIdentifier != "ABC-301" {
		t.Fatalf("issue_identifier=%q want ABC-301", card.Card.IssueIdentifier)
	}
	if card.Card.Title != "Initial title" {
		t.Fatalf("title=%q want Initial title", card.Card.Title)
	}
}

func TestStore_Apply_IgnoresPayloadLaneAndUsesDerivedLane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_lane_authority.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-401",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-401",
		StreamID:      "run-401",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 5, 14, 50, 0, 0, time.UTC),
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"issue_id":    "issue-401",
			"state":       "Running",
			"eligibility": "eligible",
			"lane":        "Done",
		}),
	}); err != nil {
		t.Fatalf("apply event: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{IssueID: "issue-401"})
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if card.Card.Lane != coreorchestration.LaneRunning {
		t.Fatalf("lane=%q want %q", card.Card.Lane, coreorchestration.LaneRunning)
	}
}

func TestStore_Apply_UsesEventOccurredAtForLastEventAt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_event_time.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{})
	store.SetNowForTest(func() time.Time { return time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC) })
	occurredAt := time.Date(2026, time.March, 5, 15, 0, 0, 0, time.UTC)

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-501",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-501",
		StreamID:      "run-501",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		OccurredAt:    occurredAt,
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"issue_id": "issue-501",
			"state":    "Running",
		}),
	}); err != nil {
		t.Fatalf("apply event: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{IssueID: "issue-501"})
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if card.Card.LastEventAt == nil {
		t.Fatal("last_event_at is nil")
	}
	if !card.Card.LastEventAt.Equal(occurredAt) {
		t.Fatalf("last_event_at=%s want %s", card.Card.LastEventAt.UTC(), occurredAt.UTC())
	}
}

func TestStore_Apply_ClearsExplicitFieldsWhenPayloadProvidesEmptyValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "orchestration_clear_fields.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db, StoreOptions{
		LaneOptions: coreorchestration.DefaultLaneOptions(),
	})

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-clear-1",
		Command:       "orchestration/dispatch-issue",
		RequestID:     "req-clear-1",
		StreamID:      "run-clear-1",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunCompleted,
		OccurredAt:    time.Date(2026, time.March, 6, 14, 0, 0, 0, time.UTC),
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":  "ws-1",
			"issue_id":      "issue-clear-1",
			"state":         "Released",
			"eligibility":   "eligible",
			"policy_status": "ok",
			"tracker_state": "Done",
			"last_outcome":  "execution_failed",
			"denial_reason": "tool failed",
			"suggestion":    "inspect runtime state",
			"retry_due_at":  time.Date(2026, time.March, 6, 14, 5, 0, 0, time.UTC).Format(time.RFC3339Nano),
		}),
	}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	if err := store.Apply(ctx, coreevents.Event{
		ID:            "evt-clear-2",
		Command:       "orchestration/card-action",
		RequestID:     "req-clear-2",
		StreamID:      "run-clear-1",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 2,
		EventType:     coreevents.EventOrchestrationUpdated,
		OccurredAt:    time.Date(2026, time.March, 6, 14, 1, 0, 0, time.UTC),
		Payload: coreevents.MustMarshalPayload(map[string]any{
			"workspace_id":  "ws-1",
			"issue_id":      "issue-clear-1",
			"state":         "Released",
			"eligibility":   "eligible",
			"policy_status": "ok",
			"tracker_state": "",
			"last_outcome":  "",
			"denial_reason": "",
			"suggestion":    "",
			"retry_due_at":  "",
		}),
	}); err != nil {
		t.Fatalf("clear apply: %v", err)
	}

	card, err := store.Card(ctx, coreorchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-clear-1"})
	if err != nil {
		t.Fatalf("card: %v", err)
	}
	if card.Card.TrackerState != "" {
		t.Fatalf("tracker_state=%q want empty", card.Card.TrackerState)
	}
	if card.Card.LastOutcome != "" {
		t.Fatalf("last_outcome=%q want empty", card.Card.LastOutcome)
	}
	if card.Card.DenialReason != "" {
		t.Fatalf("denial_reason=%q want empty", card.Card.DenialReason)
	}
	if card.Card.Suggestion != "" {
		t.Fatalf("suggestion=%q want empty", card.Card.Suggestion)
	}
	if card.Card.RetryDueAt != nil {
		t.Fatalf("retry_due_at=%v want nil", card.Card.RetryDueAt)
	}
	if card.Card.Lane != coreorchestration.LaneTodo {
		t.Fatalf("lane=%q want %q", card.Card.Lane, coreorchestration.LaneTodo)
	}
}
