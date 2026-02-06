package trajectory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const testWorkspaceID = "0123456789abcdef0123456789abcdef"

func TestOpenAndClose(t *testing.T) {
	store := openTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestTrajectory_InsertAndGet(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	traj := trajectory.Trajectory{
		WorkspaceID:   testWorkspaceID,
		RootRequestID: "req-456",
		TaskIDs:       []string{"task-1", "task-2"},
		EpicID:        "epic-789",
		AgentRole:     "coder",
		JobID:         "job-abc",
		TraceID:       "trace-def",
		Status:        trajectory.StatusOK,
		Summary:       "Test trajectory",
	}

	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if inserted.ID == "" {
		t.Error("expected ID to be generated")
	}
	if inserted.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	got, err := store.GetTrajectory(ctx, testWorkspaceID, inserted.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorkspaceID != testWorkspaceID {
		t.Errorf("workspace_id: got %q, want %q", got.WorkspaceID, testWorkspaceID)
	}
	if got.RootRequestID != "req-456" {
		t.Errorf("root_request_id: got %q, want %q", got.RootRequestID, "req-456")
	}
	if len(got.TaskIDs) != 2 {
		t.Errorf("task_ids: got %d, want 2", len(got.TaskIDs))
	}
	if got.Status != trajectory.StatusOK {
		t.Errorf("status: got %q, want %q", got.Status, trajectory.StatusOK)
	}
}

func TestTrajectory_Update(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusPartial,
		Summary:     "Initial summary",
	}

	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	inserted.Status = trajectory.StatusOK
	inserted.Summary = "Updated summary"
	inserted.ArtifactDigest = "sha256:abc123"

	if err := store.UpdateTrajectory(ctx, inserted); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := store.GetTrajectory(ctx, testWorkspaceID, inserted.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != trajectory.StatusOK {
		t.Errorf("status: got %q, want %q", got.Status, trajectory.StatusOK)
	}
	if got.Summary != "Updated summary" {
		t.Errorf("summary: got %q, want %q", got.Summary, "Updated summary")
	}
	if got.ArtifactDigest != "sha256:abc123" {
		t.Errorf("artifact_digest: got %q, want %q", got.ArtifactDigest, "sha256:abc123")
	}
}

func TestTrajectory_List(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Insert multiple trajectories.
	for i := 0; i < 3; i++ {
		traj := trajectory.Trajectory{
			WorkspaceID: testWorkspaceID,
			Status:      trajectory.StatusOK,
			AgentRole:   "coder",
		}
		if _, err := store.InsertTrajectory(ctx, traj); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	// Insert one with different role.
	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
		AgentRole:   "planner",
	}
	if _, err := store.InsertTrajectory(ctx, traj); err != nil {
		t.Fatalf("insert planner: %v", err)
	}

	// List all.
	all, err := store.ListTrajectories(ctx, trajectory.ListFilter{WorkspaceID: testWorkspaceID})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("list all: got %d, want 4", len(all))
	}

	// List by role.
	coders, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: testWorkspaceID,
		AgentRole:   "coder",
	})
	if err != nil {
		t.Fatalf("list coders: %v", err)
	}
	if len(coders) != 3 {
		t.Errorf("list coders: got %d, want 3", len(coders))
	}
}

func TestTrajectory_Delete(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
	}
	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := store.DeleteTrajectory(ctx, testWorkspaceID, inserted.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err = store.GetTrajectory(ctx, testWorkspaceID, inserted.ID)
	if !errors.Is(err, trajectory.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestTrajectory_List_TaskIDExactMatch(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	_, err := store.InsertTrajectory(ctx, trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
		TaskIDs:     []string{"task1"},
	})
	if err != nil {
		t.Fatalf("insert task1: %v", err)
	}
	_, err = store.InsertTrajectory(ctx, trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
		TaskIDs:     []string{"task10"},
	})
	if err != nil {
		t.Fatalf("insert task10: %v", err)
	}

	results, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: testWorkspaceID,
		TaskID:      "task1",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].TaskIDs) != 1 || results[0].TaskIDs[0] != "task1" {
		t.Fatalf("unexpected task_ids: %+v", results[0].TaskIDs)
	}
}

func TestListReturnsEmptySlices(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	trajectories, err := store.ListTrajectories(ctx, trajectory.ListFilter{WorkspaceID: testWorkspaceID})
	if err != nil {
		t.Fatalf("list trajectories: %v", err)
	}
	if trajectories == nil {
		t.Fatalf("expected non-nil trajectories slice")
	}
	if len(trajectories) != 0 {
		t.Fatalf("expected 0 trajectories, got %d", len(trajectories))
	}

	requests, err := store.ListUserRequests(ctx, testWorkspaceID, 10)
	if err != nil {
		t.Fatalf("list user_requests: %v", err)
	}
	if requests == nil {
		t.Fatalf("expected non-nil user_requests slice")
	}
	if len(requests) != 0 {
		t.Fatalf("expected 0 user_requests, got %d", len(requests))
	}

	traj, err := store.InsertTrajectory(ctx, trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
	})
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	events, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: traj.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if events == nil {
		t.Fatalf("expected non-nil events slice")
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	byTrace, err := store.GetEventsByTraceID(ctx, testWorkspaceID, "trace-empty")
	if err != nil {
		t.Fatalf("get events by trace_id: %v", err)
	}
	if byTrace == nil {
		t.Fatalf("expected non-nil events-by-trace slice")
	}
	if len(byTrace) != 0 {
		t.Fatalf("expected 0 events-by-trace, got %d", len(byTrace))
	}
}

func TestUserRequest_InsertAndGet(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	ur := trajectory.UserRequestCapture{
		WorkspaceID: testWorkspaceID,
		Actor:       "actor:human:user1",
		Source:      trajectory.SourceCLI,
		Text:        "Fix the bug in auth.go",
		CommandContext: &trajectory.CommandContext{
			CLICommand:      "agentctl agent spawn --role coder",
			ProtocolCommand: "agent/spawn",
			TraceID:         "trace-xyz",
		},
		TaskHints: &trajectory.TaskHints{
			TaskID:     "task-123",
			ScopePaths: []string{"internal/auth/"},
		},
	}

	inserted, err := store.InsertUserRequest(ctx, ur)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if inserted.ID == "" {
		t.Error("expected ID to be generated")
	}
	if inserted.TS.IsZero() {
		t.Error("expected TS to be set")
	}

	got, err := store.GetUserRequest(ctx, testWorkspaceID, inserted.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Actor != "actor:human:user1" {
		t.Errorf("actor: got %q, want %q", got.Actor, "actor:human:user1")
	}
	if got.Source != trajectory.SourceCLI {
		t.Errorf("source: got %q, want %q", got.Source, trajectory.SourceCLI)
	}
	if got.Text != "Fix the bug in auth.go" {
		t.Errorf("text: got %q, want %q", got.Text, "Fix the bug in auth.go")
	}
	if got.CommandContext == nil {
		t.Fatal("expected command_context to be set")
	}
	if got.CommandContext.CLICommand != "agentctl agent spawn --role coder" {
		t.Errorf("cli_command: got %q, want %q", got.CommandContext.CLICommand, "agentctl agent spawn --role coder")
	}
	if got.TaskHints == nil {
		t.Fatal("expected task_hints to be set")
	}
	if got.TaskHints.TaskID != "task-123" {
		t.Errorf("task_id: got %q, want %q", got.TaskHints.TaskID, "task-123")
	}
}

func TestUserRequest_List(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	for i := 0; i < 5; i++ {
		ur := trajectory.UserRequestCapture{
			WorkspaceID: testWorkspaceID,
			Actor:       "actor:human:user1",
			Source:      trajectory.SourceCLI,
			Text:        "Request " + string(rune('A'+i)),
		}
		if _, err := store.InsertUserRequest(ctx, ur); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	list, err := store.ListUserRequests(ctx, testWorkspaceID, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 5 {
		t.Errorf("list: got %d, want 5", len(list))
	}
}

func TestEvent_InsertAndList(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create a trajectory first.
	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
		TraceID:     "trace-events",
	}
	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Insert events.
	event1 := trajectory.Event{
		TrajectoryID: inserted.ID,
		Kind:         trajectory.EventKindUserRequest,
		Actor:        "actor:human:user1",
		Status:       "ok",
		DataInline: map[string]any{
			"text": "Fix the bug",
		},
		Meta: &trajectory.EventMeta{
			TraceID: "trace-events",
		},
	}
	insertedEvent, err := store.InsertEvent(ctx, event1)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if insertedEvent.ID == "" {
		t.Error("expected event ID to be generated")
	}

	event2 := trajectory.Event{
		TrajectoryID: inserted.ID,
		Kind:         trajectory.EventKindToolCall,
		Actor:        "actor:agent:dspy:coder",
		Command:      "code.symbol_search",
		Status:       "ok",
		Meta: &trajectory.EventMeta{
			TraceID: "trace-events",
			JobID:   "job-xyz",
		},
	}
	if _, err := store.InsertEvent(ctx, event2); err != nil {
		t.Fatalf("insert event 2: %v", err)
	}

	// List events.
	events, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: inserted.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("list events: got %d, want 2", len(events))
	}

	// Verify first event.
	if events[0].Kind != trajectory.EventKindUserRequest {
		t.Errorf("event 0 kind: got %q, want %q", events[0].Kind, trajectory.EventKindUserRequest)
	}
	if events[0].DataInline["text"] != "Fix the bug" {
		t.Errorf("event 0 data_inline: got %v", events[0].DataInline)
	}
	if events[0].Meta == nil || events[0].Meta.TraceID != "trace-events" {
		t.Errorf("event 0 meta.trace_id: got %v", events[0].Meta)
	}
}

func TestEvent_InsertBatch(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create trajectory.
	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
	}
	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Insert batch of events.
	events := []trajectory.Event{
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall},
	}
	if err := store.InsertEvents(ctx, events); err != nil {
		t.Fatalf("insert batch: %v", err)
	}

	// Verify all inserted.
	list, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: inserted.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("list: got %d, want 3", len(list))
	}
}

func TestEvent_FilterByKind(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create trajectory.
	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
	}
	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	// Insert mixed events.
	events := []trajectory.Event{
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindSWEGrep},
	}
	if err := store.InsertEvents(ctx, events); err != nil {
		t.Fatalf("insert batch: %v", err)
	}

	// Filter by kind.
	toolCalls, err := store.ListEvents(ctx, trajectory.EventFilter{
		TrajectoryID: inserted.ID,
		Kind:         trajectory.EventKindToolCall,
	})
	if err != nil {
		t.Fatalf("filter by kind: %v", err)
	}
	if len(toolCalls) != 2 {
		t.Errorf("tool_call events: got %d, want 2", len(toolCalls))
	}
}

func TestTrajectory_CascadeDelete(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create trajectory with events.
	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
	}
	inserted, err := store.InsertTrajectory(ctx, traj)
	if err != nil {
		t.Fatalf("insert trajectory: %v", err)
	}

	events := []trajectory.Event{
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolCall},
		{TrajectoryID: inserted.ID, Kind: trajectory.EventKindToolResult},
	}
	if err := store.InsertEvents(ctx, events); err != nil {
		t.Fatalf("insert events: %v", err)
	}

	// Delete trajectory.
	if err := store.DeleteTrajectory(ctx, testWorkspaceID, inserted.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify events are also deleted (via CASCADE).
	list, err := store.ListEvents(ctx, trajectory.EventFilter{TrajectoryID: inserted.ID})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("events after cascade delete: got %d, want 0", len(list))
	}
}

func TestTrajectory_TimeFilter(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	now := time.Now().UTC()

	// Insert trajectory with explicit timestamps.
	traj := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
		CreatedAt:   now.Add(-2 * time.Hour),
	}
	if _, err := store.InsertTrajectory(ctx, traj); err != nil {
		t.Fatalf("insert old: %v", err)
	}

	traj2 := trajectory.Trajectory{
		WorkspaceID: testWorkspaceID,
		Status:      trajectory.StatusOK,
		CreatedAt:   now.Add(-30 * time.Minute),
	}
	if _, err := store.InsertTrajectory(ctx, traj2); err != nil {
		t.Fatalf("insert recent: %v", err)
	}

	// Filter since 1 hour ago.
	recent, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: testWorkspaceID,
		Since:       now.Add(-1 * time.Hour),
	})
	if err != nil {
		t.Fatalf("filter since: %v", err)
	}
	if len(recent) != 1 {
		t.Errorf("recent trajectories: got %d, want 1", len(recent))
	}
}

func openTestStore(t *testing.T) trajectory.Store {
	t.Helper()
	store, err := trajectory.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}
