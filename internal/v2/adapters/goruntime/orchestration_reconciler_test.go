package goruntime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	tursoorchestration "github.com/joshka0/foxctl/internal/v2/adapters/turso/orchestration"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
	v2orchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
	"github.com/joshka0/foxctl/internal/v2/core/spawn"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

func TestOrchestrationReconciler_RecordDispatchSpawned_ProjectsRunningLane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_spawned.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.April, 6, 20, 0, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       sequentialTestID("dispatch-1"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	evt, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID:     "req-1",
		RunID:         "run-1",
		ActorID:       "actor:worker-1",
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-1",
			"issue_identifier": "ABC-1",
			"title":            "Investigate scheduler drift",
			"attempt":          2,
		},
	}, spawn.Response{
		RunID:   "run-1",
		AgentID: "agent:worker-1",
		ActorID: "actor:worker-1",
		Status:  "spawned",
	})
	if err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}
	if evt.EventType != v2events.EventRunStarted {
		t.Fatalf("event_type=%q want %q", evt.EventType, v2events.EventRunStarted)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-1"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != v2orchestration.LaneRunning {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneRunning)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if card.Card.Attempt != 2 {
		t.Fatalf("attempt=%d want 2", card.Card.Attempt)
	}
	if card.Card.AgentID != "agent:worker-1" {
		t.Fatalf("agent_id=%q want agent:worker-1", card.Card.AgentID)
	}
}

func TestOrchestrationReconciler_SpawnResultCallback_ProjectsDispatchFailureToRetryQueueCard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.April, 6, 20, 5, 0, 0, time.UTC)
	projections, events := newGoruntimeReconcilerStore(t, ctx, "callback_dispatch_retry.db", now)
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       sequentialTestID("callback-retry"),
	})

	callback := reconciler.SpawnResultCallback()
	if callback == nil {
		t.Fatal("expected callback")
	}
	err := callback(ctx, spawn.Request{
		RequestID: "req-callback-retry",
		RunID:     "run-callback-retry",
		ActorID:   "actor:system:overseer",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-callback-retry",
			"issue_identifier": "ABC-CALLBACK-RETRY",
			"title":            "Retry callback worker spawn",
			"attempt":          2,
		},
	}, spawn.Response{}, fmt.Errorf("dial unix /tmp/foxctl-jido.sock: connect: connection refused"))
	if err != nil {
		t.Fatalf("callback() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-callback-retry",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRetryQueue {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRetryQueue)
	}
	if card.Card.Lane != v2orchestration.LaneRetryQueue {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneRetryQueue)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusOK {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusOK)
	}
	if card.Card.Attempt != 3 {
		t.Fatalf("attempt=%d want 3", card.Card.Attempt)
	}
	if card.Card.RetryDueAt == nil {
		t.Fatal("retry_due_at should be populated")
	}
	if len(events.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(events.events))
	}
}

func TestOrchestrationReconciler_Reconcile_ProjectsCompletedWorkerToReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_completed.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.April, 6, 20, 15, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	workers := &fakeWorkerReader{
		childrenByParent: map[string][]coreworker.Record{
			"agent:overseer": {
				{
					WorkerID:      "subprocess:agent:worker-1",
					AgentID:       "agent:worker-1",
					ParentAgentID: "agent:overseer",
					RunID:         "run-1",
					Status:        coreworker.StatusCompleted,
					Metadata: map[string]any{
						"workspace_id":     "ws-1",
						"issue_id":         "issue-1",
						"issue_identifier": "ABC-1",
						"title":            "Investigate scheduler drift",
						"request_id":       "req-1",
						"run_id":           "run-1",
						"actor_id":         "actor:worker-1",
					},
				},
			},
		},
	}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:              events,
		Projections:         projections,
		Reader:              projections,
		Workers:             workers,
		ParentAgentIDs:      []string{"agent:overseer"},
		SuccessTrackerState: "Human Review",
		Now:                 func() time.Time { return now },
		NewID:               sequentialTestID("dispatch-complete"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-1",
		RunID:     "run-1",
		ActorID:   "actor:worker-1",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-1",
			"issue_identifier": "ABC-1",
			"title":            "Investigate scheduler drift",
		},
	}, spawn.Response{
		RunID:   "run-1",
		AgentID: "agent:worker-1",
		ActorID: "actor:worker-1",
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-1"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.Lane != v2orchestration.LaneReview {
		t.Fatalf("lane=%q want %q (tracker_state=%q eligibility=%q)", card.Card.Lane, v2orchestration.LaneReview, card.Card.TrackerState, card.Card.Eligibility)
	}
	if card.Card.TrackerState != "Human Review" {
		t.Fatalf("tracker_state=%q want Human Review", card.Card.TrackerState)
	}
	if workers.lastParentAgentID != "agent:overseer" {
		t.Fatalf("parent_agent_id=%q want agent:overseer", workers.lastParentAgentID)
	}
}

func TestOrchestrationReconciler_Reconcile_ProjectsTransientWorkerFailureToRetryQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_retry.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.April, 6, 20, 30, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	workers := &fakeWorkerReader{
		childrenByParent: map[string][]coreworker.Record{
			"agent:overseer": {
				{
					WorkerID:      "subprocess:agent:worker-2",
					AgentID:       "agent:worker-2",
					ParentAgentID: "agent:overseer",
					RunID:         "run-2",
					Status:        coreworker.StatusFailed,
					StopReason:    "dial unix /tmp/foxctl-jido.sock: connect: connection refused",
					Metadata: map[string]any{
						"workspace_id":     "ws-1",
						"issue_id":         "issue-2",
						"issue_identifier": "ABC-2",
						"title":            "Retry child spawn",
						"request_id":       "req-2",
						"run_id":           "run-2",
						"actor_id":         "actor:worker-2",
						"attempt":          2,
					},
				},
			},
		},
	}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Workers:        workers,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          sequentialTestID("dispatch-retry"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-2",
		RunID:     "run-2",
		ActorID:   "actor:worker-2",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-2",
			"issue_identifier": "ABC-2",
			"title":            "Retry child spawn",
			"attempt":          2,
		},
	}, spawn.Response{
		RunID:   "run-2",
		AgentID: "agent:worker-2",
		ActorID: "actor:worker-2",
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-2"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRetryQueue {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRetryQueue)
	}
	if card.Card.Lane != v2orchestration.LaneRetryQueue {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneRetryQueue)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusOK {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusOK)
	}
	if card.Card.Attempt != 3 {
		t.Fatalf("attempt=%d want 3", card.Card.Attempt)
	}
	if card.Card.RetryDueAt == nil {
		t.Fatal("retry_due_at should be populated")
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_ProjectsMissingOldCardToBlocked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	startedAt := time.Date(2026, time.April, 6, 21, 0, 0, 0, time.UTC)
	currentTime := startedAt
	projections, events := newGoruntimeReconcilerStore(t, ctx, "recover_orphaned.db", startedAt)
	now := time.Date(2026, time.April, 6, 21, 1, 0, 0, time.UTC)
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Workers:        &fakeWorkerReader{childrenByParent: map[string][]coreworker.Record{"agent:overseer": nil}},
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return currentTime },
		NewID:          sequentialTestID("recover-orphaned"),
	})

	recordRunningDispatch(t, ctx, reconciler, "ws-1", "issue-orphaned", "ABC-9", "run-orphaned", "agent:worker-orphaned", 2)
	currentTime = now

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-orphaned"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.Lane != v2orchestration.LaneBlocked {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneBlocked)
	}
	if card.Card.DenialReason != "runtime worker missing during startup recovery" {
		t.Fatalf("denial_reason=%q", card.Card.DenialReason)
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_LeavesRecentMissingCardRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, time.April, 6, 21, 5, 0, 0, time.UTC)
	projections, events := newGoruntimeReconcilerStore(t, ctx, "recover_recent.db", now.Add(-10*time.Second))
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Workers:        &fakeWorkerReader{childrenByParent: map[string][]coreworker.Record{"agent:overseer": nil}},
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          sequentialTestID("recover-recent"),
	})

	recordRunningDispatch(t, ctx, reconciler, "ws-1", "issue-recent", "ABC-10", "run-recent", "agent:worker-recent", 1)

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-recent"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if len(events.events) != 1 {
		t.Fatalf("events=%d want only original run.started", len(events.events))
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_ProjectsTerminalWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projections, events := newGoruntimeReconcilerStore(t, ctx, "recover_terminal.db", time.Date(2026, time.April, 6, 21, 10, 0, 0, time.UTC))
	now := time.Date(2026, time.April, 6, 21, 11, 0, 0, time.UTC)
	workers := &fakeWorkerReader{childrenByParent: map[string][]coreworker.Record{
		"agent:overseer": {
			{
				WorkerID:      "subprocess:agent:worker-terminal",
				AgentID:       "agent:worker-terminal",
				ParentAgentID: "agent:overseer",
				RunID:         "run-terminal",
				Status:        coreworker.StatusCompleted,
				Metadata: map[string]any{
					"workspace_id":     "ws-1",
					"issue_id":         "issue-terminal",
					"issue_identifier": "ABC-11",
					"title":            "Complete recovered work",
					"request_id":       "req-issue-terminal",
					"run_id":           "run-terminal",
					"actor_id":         "actor:worker-terminal",
				},
			},
		},
	}}
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:              events,
		Projections:         projections,
		Reader:              projections,
		Workers:             workers,
		ParentAgentIDs:      []string{"agent:overseer"},
		SuccessTrackerState: "Human Review",
		Now:                 func() time.Time { return now },
		NewID:               sequentialTestID("recover-terminal"),
	})

	recordRunningDispatch(t, ctx, reconciler, "ws-1", "issue-terminal", "ABC-11", "run-terminal", "agent:worker-terminal", 1)

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-terminal"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.LastEvent != string(v2events.EventRunCompleted) {
		t.Fatalf("last_event=%q want %q", card.Card.LastEvent, v2events.EventRunCompleted)
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_LeavesLiveWorkerRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projections, events := newGoruntimeReconcilerStore(t, ctx, "recover_live.db", time.Date(2026, time.April, 6, 21, 12, 0, 0, time.UTC))
	now := time.Date(2026, time.April, 6, 21, 13, 0, 0, time.UTC)
	workers := &fakeWorkerReader{childrenByParent: map[string][]coreworker.Record{
		"agent:overseer": {
			{
				WorkerID:      "subprocess:agent:worker-live",
				AgentID:       "agent:worker-live",
				ParentAgentID: "agent:overseer",
				RunID:         "run-live",
				Status:        coreworker.StatusRunning,
				Metadata: map[string]any{
					"workspace_id": "ws-1",
					"issue_id":     "issue-live",
					"request_id":   "req-issue-live",
					"run_id":       "run-live",
				},
			},
		},
	}}
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Workers:        workers,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          sequentialTestID("recover-live"),
	})

	recordRunningDispatch(t, ctx, reconciler, "ws-1", "issue-live", "ABC-13", "run-live", "agent:worker-live", 1)

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-live"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if len(events.events) != 1 {
		t.Fatalf("events=%d want only original run.started", len(events.events))
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_ProjectsFailedWorkerToRetryQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projections, events := newGoruntimeReconcilerStore(t, ctx, "recover_failed.db", time.Date(2026, time.April, 6, 21, 14, 0, 0, time.UTC))
	now := time.Date(2026, time.April, 6, 21, 15, 0, 0, time.UTC)
	workers := &fakeWorkerReader{childrenByParent: map[string][]coreworker.Record{
		"agent:overseer": {
			{
				WorkerID:      "subprocess:agent:worker-failed",
				AgentID:       "agent:worker-failed",
				ParentAgentID: "agent:overseer",
				RunID:         "run-failed",
				Status:        coreworker.StatusFailed,
				StopReason:    "database is locked",
				Metadata: map[string]any{
					"workspace_id": "ws-1",
					"issue_id":     "issue-failed",
					"request_id":   "req-issue-failed",
					"run_id":       "run-failed",
					"attempt":      1,
				},
			},
		},
	}}
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Workers:        workers,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          sequentialTestID("recover-failed"),
	})

	recordRunningDispatch(t, ctx, reconciler, "ws-1", "issue-failed", "ABC-14", "run-failed", "agent:worker-failed", 1)

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-failed"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRetryQueue {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRetryQueue)
	}
	if card.Card.Lane != v2orchestration.LaneRetryQueue {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneRetryQueue)
	}
	if card.Card.Attempt != 2 {
		t.Fatalf("attempt=%d want 2", card.Card.Attempt)
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_WorkerListErrorDoesNotMutateCards(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	projections, events := newGoruntimeReconcilerStore(t, ctx, "recover_worker_error.db", time.Date(2026, time.April, 6, 21, 15, 0, 0, time.UTC))
	now := time.Date(2026, time.April, 6, 21, 16, 0, 0, time.UTC)
	reconciler := newTestGoruntimeReconciler(t, OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Workers:        &fakeWorkerReader{err: errors.New("worker store unavailable")},
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          sequentialTestID("recover-worker-error"),
	})

	recordRunningDispatch(t, ctx, reconciler, "ws-1", "issue-worker-error", "ABC-12", "run-worker-error", "agent:worker-error", 1)

	if err := reconciler.RecoverOrphanedRuns(ctx); err == nil {
		t.Fatal("RecoverOrphanedRuns() error = nil, want worker store error")
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-worker-error"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if len(events.events) != 1 {
		t.Fatalf("events=%d want only original run.started", len(events.events))
	}
}

type fakeEventAppender struct {
	events []v2events.Event
}

func (f *fakeEventAppender) Append(_ context.Context, event v2events.Event) error {
	f.events = append(f.events, event)
	return nil
}

type fakeWorkerReader struct {
	childrenByParent  map[string][]coreworker.Record
	lastParentAgentID string
	err               error
}

func (f *fakeWorkerReader) Worker(context.Context, coreworker.LookupRequest) (coreworker.Record, error) {
	return coreworker.Record{}, fmt.Errorf("not implemented")
}

func (f *fakeWorkerReader) Children(_ context.Context, req coreworker.ChildrenRequest) ([]coreworker.Record, error) {
	f.lastParentAgentID = req.ParentAgentID
	if f.err != nil {
		return nil, f.err
	}
	return append([]coreworker.Record(nil), f.childrenByParent[req.ParentAgentID]...), nil
}

func newGoruntimeReconcilerStore(t *testing.T, ctx context.Context, name string, now time.Time) (*tursoorchestration.Store, *fakeEventAppender) {
	t.Helper()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), name), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			ReviewTrackerStates: []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	return projections, &fakeEventAppender{}
}

func newTestGoruntimeReconciler(t *testing.T, cfg OrchestrationReconcilerConfig) *OrchestrationReconciler {
	t.Helper()
	reconciler, err := NewOrchestrationReconciler(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	return reconciler
}

func recordRunningDispatch(t *testing.T, ctx context.Context, reconciler *OrchestrationReconciler, workspaceID, issueID, issueIdentifier, runID, agentID string, attempt int) {
	t.Helper()
	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-" + issueID,
		RunID:     runID,
		ActorID:   "actor:" + agentID,
		Metadata: map[string]any{
			"workspace_id":     workspaceID,
			"issue_id":         issueID,
			"issue_identifier": issueIdentifier,
			"title":            "Recover " + issueID,
			"attempt":          attempt,
		},
	}, spawn.Response{
		RunID:   runID,
		AgentID: agentID,
		ActorID: "actor:" + agentID,
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}
}

func sequentialTestID(prefix string) func() string {
	seq := 0
	return func() string {
		seq++
		return fmt.Sprintf("%s-%d", prefix, seq)
	}
}
