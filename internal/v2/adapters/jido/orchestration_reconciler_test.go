package jido

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	tursoorchestration "github.com/joshka0/foxctl/internal/v2/adapters/turso/orchestration"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
	v2orchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
	"github.com/joshka0/foxctl/internal/v2/core/spawn"
	v2runtimeorchestration "github.com/joshka0/foxctl/internal/v2/runtime/orchestration"
	v2services "github.com/joshka0/foxctl/internal/v2/services"
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

	now := time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       func() string { return "dispatch-1" },
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
	if evt.EventType == "" {
		t.Fatal("expected event to be appended")
	}
	if evt.EventType != "run.started" {
		t.Fatalf("event_type=%q want run.started", evt.EventType)
	}
	if evt.Command != commandDispatchIssue {
		t.Fatalf("command=%q want %q", evt.Command, commandDispatchIssue)
	}
	if len(events.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(events.events))
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-1",
	})
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
	if card.Card.RunID != "run-1" {
		t.Fatalf("run_id=%q want run-1", card.Card.RunID)
	}
	if card.Card.AgentID != "agent:worker-1" {
		t.Fatalf("agent_id=%q want agent:worker-1", card.Card.AgentID)
	}
}

func TestOrchestrationReconciler_RecordDispatchFailed_ProjectsBlockedLane(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_failed.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 12, 15, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       func() string { return "dispatch-2" },
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	evt, err := reconciler.RecordDispatchFailed(ctx, spawn.Request{
		RequestID:     "req-2",
		RunID:         "run-2",
		ActorID:       "actor:overseer",
		CorrelationID: "corr-2",
		CausationID:   "cause-2",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-2",
			"issue_identifier": "ABC-2",
			"title":            "Spawn child for code review",
		},
	}, &v2errors.V2Error{
		Kind:    v2errors.ErrPolicyViolation,
		Message: "depth exceeded",
		Details: map[string]any{
			"suggestion": "escalate to actor:system:overseer",
		},
		Fatal: true,
	})
	if err != nil {
		t.Fatalf("RecordDispatchFailed() error = %v", err)
	}
	if evt.EventType != "run.failed" {
		t.Fatalf("event_type=%q want run.failed", evt.EventType)
	}
	if len(events.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(events.events))
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-2",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != v2orchestration.LaneBlocked {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneBlocked)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusDenied {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusDenied)
	}
	if card.Card.LastOutcome != v2orchestration.OutcomePolicyDenied {
		t.Fatalf("last_outcome=%q want %q", card.Card.LastOutcome, v2orchestration.OutcomePolicyDenied)
	}
	if card.Card.DenialReason != "depth exceeded" {
		t.Fatalf("denial_reason=%q want depth exceeded", card.Card.DenialReason)
	}
	if card.Card.Suggestion != "escalate to actor:system:overseer" {
		t.Fatalf("suggestion=%q unexpected", card.Card.Suggestion)
	}
}

func TestOrchestrationReconciler_RecordDispatchFailed_ProjectsRetryQueueForTransientFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_failed_retry.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 12, 20, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       func() string { return "dispatch-retry-1" },
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	_, err = reconciler.RecordDispatchFailed(ctx, spawn.Request{
		RequestID: "req-retry-1",
		RunID:     "run-retry-1",
		ActorID:   "actor:overseer",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-retry-1",
			"issue_identifier": "ABC-RETRY-1",
			"title":            "Retry child spawn",
			"attempt":          2,
		},
	}, fmt.Errorf("dial unix /tmp/foxctl-jido.sock: connect: connection refused"))
	if err != nil {
		t.Fatalf("RecordDispatchFailed() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-retry-1",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != v2orchestration.LaneRetryQueue {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneRetryQueue)
	}
	if card.Card.State != v2orchestration.StateRetryQueue {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRetryQueue)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusOK {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusOK)
	}
	if card.Card.Eligibility != v2orchestration.EligibilityEligible {
		t.Fatalf("eligibility=%q want %q", card.Card.Eligibility, v2orchestration.EligibilityEligible)
	}
	if card.Card.Attempt != 3 {
		t.Fatalf("attempt=%d want 3", card.Card.Attempt)
	}
	if card.Card.RetryDueAt == nil {
		t.Fatal("retry_due_at should be populated")
	}
}

func TestOrchestrationReconciler_SpawnResultCallback_IgnoresNonDispatchSpawn(t *testing.T) {
	t.Parallel()

	events := &fakeEventAppender{}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events: events,
		Now:    func() time.Time { return time.Date(2026, time.March, 6, 12, 30, 0, 0, time.UTC) },
		NewID:  func() string { return "dispatch-3" },
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	callback := reconciler.SpawnResultCallback()
	if callback == nil {
		t.Fatal("expected callback")
	}
	if err := callback(context.Background(), spawn.Request{
		RequestID: "req-3",
		Role:      "worker",
	}, spawn.Response{
		RunID:   "run-3",
		AgentID: "agent:worker-3",
	}, nil); err != nil {
		t.Fatalf("callback() error = %v", err)
	}
	if len(events.events) != 0 {
		t.Fatalf("appended events=%d want 0", len(events.events))
	}
}

func TestOrchestrationReconciler_SpawnResultCallback_ProjectsDispatchFailureToBlockedCard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "callback_dispatch_blocked.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 12, 35, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       sequentialTestID("callback-blocked"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	callback := reconciler.SpawnResultCallback()
	if callback == nil {
		t.Fatal("expected callback")
	}
	err = callback(ctx, spawn.Request{
		RequestID: "req-callback-blocked",
		RunID:     "run-callback-blocked",
		ActorID:   "actor:system:overseer",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-callback-blocked",
			"issue_identifier": "ABC-CALLBACK-BLOCKED",
			"title":            "Spawn blocked worker",
			"attempt":          1,
		},
	}, spawn.Response{}, &v2errors.V2Error{
		Kind:    v2errors.ErrPolicyViolation,
		Message: "depth exceeded",
		Details: map[string]any{
			"suggestion": "request overseer approval",
		},
		Fatal: true,
	})
	if err != nil {
		t.Fatalf("callback() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-callback-blocked",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.Lane != v2orchestration.LaneBlocked {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneBlocked)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusDenied {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusDenied)
	}
	if card.Card.LastOutcome != v2orchestration.OutcomePolicyDenied {
		t.Fatalf("last_outcome=%q want %q", card.Card.LastOutcome, v2orchestration.OutcomePolicyDenied)
	}
	if card.Card.DenialReason != "depth exceeded" {
		t.Fatalf("denial_reason=%q want depth exceeded", card.Card.DenialReason)
	}
	if card.Card.Suggestion != "request overseer approval" {
		t.Fatalf("suggestion=%q want request overseer approval", card.Card.Suggestion)
	}
	if len(events.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(events.events))
	}
}

func TestScheduler_JidoDispatchFailureProjectsRetryCardWithoutRetryQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "scheduler_dispatch_retry.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 12, 40, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:      events,
		Projections: projections,
		Now:         func() time.Time { return now },
		NewID:       sequentialTestID("scheduler-dispatch"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	client := &fakeClient{
		spawnChildErr: fmt.Errorf("dial unix /tmp/foxctl-jido.sock: connect: connection refused"),
	}
	childSpawner, err := NewChildSpawner(ChildSpawnerConfig{
		Client:        client,
		Now:           func() time.Time { return now },
		OnSpawnResult: reconciler.SpawnResultCallback(),
	})
	if err != nil {
		t.Fatalf("NewChildSpawner() error = %v", err)
	}
	spawnService := v2services.NewSpawnService(v2services.SpawnDependencies{
		RuntimeSpawner: childSpawner,
		Now:            func() time.Time { return now },
		NewID:          sequentialTestID("spawn"),
	})
	dispatchService := v2services.NewOrchestrationService(v2services.OrchestrationDependencies{
		Spawn:       spawnService,
		Reader:      projections,
		LaneOptions: v2orchestration.DefaultLaneOptions(),
		Now:         func() time.Time { return now },
	})
	scheduler := v2runtimeorchestration.NewScheduler(v2runtimeorchestration.SchedulerConfig{
		Source: &staticCandidateSource{candidates: []v2runtimeorchestration.Candidate{
			{
				WorkspaceID:     "ws-1",
				IssueID:         "issue-scheduler-retry",
				IssueIdentifier: "ABC-SCHED-RETRY",
				Title:           "Retry scheduler dispatch",
				ParentAgentID:   "agent:overseer",
				Role:            "coder",
				Prompt:          "handle the issue",
				Attempt:         2,
			},
		}},
		Service:            dispatchService,
		MaxDispatchPerTick: 1,
		NewRequestID:       sequentialTestID("scheduler"),
		Now:                func() time.Time { return now },
	})

	if err := scheduler.Tick(ctx); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-scheduler-retry",
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
	if card.Card.Attempt != 3 {
		t.Fatalf("attempt=%d want 3", card.Card.Attempt)
	}
	if card.Card.RetryDueAt == nil {
		t.Fatal("retry_due_at should be populated")
	}
	if len(events.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(events.events))
	}
	if client.spawnChildReq.AgentID != "agent:overseer" {
		t.Fatalf("spawn child parent=%q want agent:overseer", client.spawnChildReq.AgentID)
	}
}

func TestOrchestrationReconciler_Reconcile_ProjectsCompletedChildToReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_terminal_review.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 13, 0, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	newID := sequentialTestID("dispatch-review")
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-1": {
					Tag:     "agent:worker-1",
					AgentID: "agent:worker-1",
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
		stateResp: StateResponse{
			AgentID: "agent:worker-1",
			Status:  "ok",
			State: json.RawMessage(`{
				"foxctl": {
					"status": "completed",
					"last_result": {
						"tool": "code/context_ripgrep",
						"envelope": {"status":"ok"}
					}
				}
			}`),
		},
	}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:              events,
		Projections:         projections,
		Reader:              projections,
		Client:              client,
		ParentAgentIDs:      []string{"agent:overseer"},
		SuccessTrackerState: "Human Review",
		Now:                 func() time.Time { return now },
		NewID:               newID,
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
		ActorID: "actor:worker-1",
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-1",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.Lane != v2orchestration.LaneReview {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneReview)
	}
	if card.Card.TrackerState != "Human Review" {
		t.Fatalf("tracker_state=%q want Human Review", card.Card.TrackerState)
	}
	if client.getChildrenReq.AgentID != "agent:overseer" {
		t.Fatalf("get_children.agent_id=%q want agent:overseer", client.getChildrenReq.AgentID)
	}
	if client.stateReq.AgentID != "agent:worker-1" {
		t.Fatalf("state.agent_id=%q want agent:worker-1", client.stateReq.AgentID)
	}
}

func TestOrchestrationReconciler_Reconcile_ProjectsFailedChildToBlocked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_terminal_blocked.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 13, 15, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	newID := sequentialTestID("dispatch-blocked")
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-2": {
					Tag:     "agent:worker-2",
					AgentID: "agent:worker-2",
					Metadata: map[string]any{
						"workspace_id":     "ws-1",
						"issue_id":         "issue-2",
						"issue_identifier": "ABC-2",
						"title":            "Review storage layer",
						"request_id":       "req-2",
						"run_id":           "run-2",
						"actor_id":         "actor:worker-2",
					},
				},
			},
		},
		stateResp: StateResponse{
			AgentID: "agent:worker-2",
			Status:  "ok",
			State: json.RawMessage(`{
				"foxctl": {
					"status": "completed",
					"last_result": {
						"envelope": {"status":"error","error":{"message":"tool failed"}}
					}
				}
			}`),
		},
	}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         client,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          newID,
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
			"title":            "Review storage layer",
		},
	}, spawn.Response{
		RunID:   "run-2",
		ActorID: "actor:worker-2",
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-2",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.Lane != v2orchestration.LaneBlocked {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneBlocked)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusBlocked {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusBlocked)
	}
	if card.Card.LastOutcome != v2orchestration.OutcomeExecFailed {
		t.Fatalf("last_outcome=%q want %q", card.Card.LastOutcome, v2orchestration.OutcomeExecFailed)
	}
	if card.Card.DenialReason != "tool failed" {
		t.Fatalf("denial_reason=%q want tool failed", card.Card.DenialReason)
	}
}

func TestOrchestrationReconciler_Reconcile_ProjectsTransientFailedChildToRetryQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_terminal_retry.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 13, 20, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	newID := sequentialTestID("dispatch-retry-runtime")
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-retry": {
					Tag:     "agent:worker-retry",
					AgentID: "agent:worker-retry",
					Metadata: map[string]any{
						"workspace_id":     "ws-1",
						"issue_id":         "issue-retry-2",
						"issue_identifier": "ABC-RETRY-2",
						"title":            "Retry runtime child",
						"request_id":       "req-retry-2",
						"run_id":           "run-retry-2",
						"actor_id":         "actor:worker-retry",
						"attempt":          1,
					},
				},
			},
		},
		stateResp: StateResponse{
			AgentID: "agent:worker-retry",
			Status:  "ok",
			State: json.RawMessage(`{
				"foxctl": {
					"status": "completed",
					"last_result": {
						"envelope": {"status":"error","error":{"message":"database is locked"}}
					}
				}
			}`),
		},
	}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         client,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          newID,
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-retry-2",
		RunID:     "run-retry-2",
		ActorID:   "actor:worker-retry",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-retry-2",
			"issue_identifier": "ABC-RETRY-2",
			"title":            "Retry runtime child",
			"attempt":          1,
		},
	}, spawn.Response{
		RunID:   "run-retry-2",
		ActorID: "actor:worker-retry",
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-retry-2",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != v2orchestration.LaneRetryQueue {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneRetryQueue)
	}
	if card.Card.State != v2orchestration.StateRetryQueue {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRetryQueue)
	}
	if card.Card.PolicyStatus != v2orchestration.PolicyStatusOK {
		t.Fatalf("policy_status=%q want %q", card.Card.PolicyStatus, v2orchestration.PolicyStatusOK)
	}
	if card.Card.Attempt != 2 {
		t.Fatalf("attempt=%d want 2", card.Card.Attempt)
	}
	if card.Card.RetryDueAt == nil {
		t.Fatal("retry_due_at should be populated")
	}
}

func TestOrchestrationReconciler_Reconcile_SkipsNonRunningCard(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_terminal_skip.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 13, 30, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			TerminalTrackerStates: []string{"done"},
			ReviewTrackerStates:   []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	newID := sequentialTestID("dispatch-skip")
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-3": {
					Tag:     "agent:worker-3",
					AgentID: "agent:worker-3",
					Metadata: map[string]any{
						"workspace_id":     "ws-1",
						"issue_id":         "issue-3",
						"issue_identifier": "ABC-3",
						"title":            "Stale child",
						"request_id":       "req-3",
						"run_id":           "run-3",
						"actor_id":         "actor:worker-3",
					},
				},
			},
		},
		stateResp: StateResponse{
			AgentID: "agent:worker-3",
			Status:  "ok",
			State:   json.RawMessage(`{"foxctl":{"status":"completed"}}`),
		},
	}

	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         client,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now },
		NewID:          newID,
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	if err := projections.Apply(ctx, fakeOrchestrationEvent("evt-skip", "req-skip", "ws-1", "issue-3", "ABC-3", "Stale child", "Released", "eligible")); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if err := reconciler.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if client.stateReq.AgentID != "" {
		t.Fatalf("state should not be queried for non-running card, got %q", client.stateReq.AgentID)
	}
	if len(events.events) != 0 {
		t.Fatalf("appended events=%d want 0", len(events.events))
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_MissingChildAfterGraceProjectsBlocked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "startup_missing_blocked.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 14, 0, 0, 0, time.UTC)
	current := now
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         &fakeClient{getChildrenResp: GetChildrenResponse{AgentID: "agent:overseer"}},
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return current },
		NewID:          sequentialTestID("startup-missing"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}

	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-startup-missing",
		RunID:     "run-startup-missing",
		ActorID:   "actor:worker-missing",
		Metadata: map[string]any{
			"workspace_id":     "ws-1",
			"issue_id":         "issue-startup-missing",
			"issue_identifier": "ABC-MISSING",
			"title":            "Recover missing child",
			"attempt":          1,
		},
	}, spawn.Response{
		RunID:   "run-startup-missing",
		AgentID: "agent:worker-missing",
		ActorID: "actor:worker-missing",
	}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}
	events.events = nil
	current = now.Add(time.Minute)
	projections.SetNowForTest(func() time.Time { return now.Add(time.Minute) })

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}

	card, err := projections.Card(ctx, v2orchestration.CardRequest{
		WorkspaceID: "ws-1",
		IssueID:     "issue-startup-missing",
	})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.Lane != v2orchestration.LaneBlocked {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneBlocked)
	}
	if card.Card.DenialReason != "runtime child missing during startup recovery" {
		t.Fatalf("denial_reason=%q unexpected", card.Card.DenialReason)
	}
	if len(events.events) != 1 {
		t.Fatalf("appended events=%d want 1", len(events.events))
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_MissingChildWithinGraceStaysRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "startup_missing_grace.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 14, 10, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         &fakeClient{getChildrenResp: GetChildrenResponse{AgentID: "agent:overseer"}},
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now.Add(10 * time.Second) },
		NewID:          sequentialTestID("startup-grace"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-startup-grace",
		RunID:     "run-startup-grace",
		Metadata: map[string]any{
			"workspace_id": "ws-1",
			"issue_id":     "issue-startup-grace",
			"title":        "Grace period child",
		},
	}, spawn.Response{RunID: "run-startup-grace", AgentID: "agent:worker-grace"}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}
	events.events = nil

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}
	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-startup-grace"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if len(events.events) != 0 {
		t.Fatalf("appended events=%d want 0", len(events.events))
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_LiveChildStaysRunning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "startup_live_child.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 14, 20, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-live": {
					Tag:     "agent:worker-live",
					AgentID: "agent:worker-live",
					Metadata: map[string]any{
						"workspace_id": "ws-1",
						"issue_id":     "issue-startup-live",
						"run_id":       "run-startup-live",
					},
				},
			},
		},
		stateResp: StateResponse{
			AgentID: "agent:worker-live",
			Status:  "ok",
			State:   json.RawMessage(`{"foxctl":{"status":"running"}}`),
		},
	}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         client,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now.Add(time.Minute) },
		NewID:          sequentialTestID("startup-live"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-startup-live",
		RunID:     "run-startup-live",
		Metadata: map[string]any{
			"workspace_id": "ws-1",
			"issue_id":     "issue-startup-live",
			"title":        "Live child",
		},
	}, spawn.Response{RunID: "run-startup-live", AgentID: "agent:worker-live"}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}
	events.events = nil

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}
	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-startup-live"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if len(events.events) != 0 {
		t.Fatalf("appended events=%d want 0", len(events.events))
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_TerminalChildProjectsCompleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "startup_terminal_child.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 14, 25, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{
		LaneOptions: v2orchestration.LaneOptions{
			ReviewTrackerStates: []string{"human review"},
		},
	})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-terminal": {
					Tag:     "agent:worker-terminal",
					AgentID: "agent:worker-terminal",
					Metadata: map[string]any{
						"workspace_id": "ws-1",
						"issue_id":     "issue-startup-terminal",
						"run_id":       "run-startup-terminal",
						"request_id":   "req-startup-terminal",
					},
				},
			},
		},
		stateResp: StateResponse{
			AgentID: "agent:worker-terminal",
			Status:  "ok",
			State:   json.RawMessage(`{"foxctl":{"status":"completed","last_result":{"envelope":{"status":"ok"}}}}`),
		},
	}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:              events,
		Projections:         projections,
		Reader:              projections,
		Client:              client,
		ParentAgentIDs:      []string{"agent:overseer"},
		SuccessTrackerState: "Human Review",
		Now:                 func() time.Time { return now.Add(time.Minute) },
		NewID:               sequentialTestID("startup-terminal"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-startup-terminal",
		RunID:     "run-startup-terminal",
		Metadata: map[string]any{
			"workspace_id": "ws-1",
			"issue_id":     "issue-startup-terminal",
			"title":        "Terminal child",
		},
	}, spawn.Response{RunID: "run-startup-terminal", AgentID: "agent:worker-terminal"}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}
	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-startup-terminal"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateReleased {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateReleased)
	}
	if card.Card.LastEvent != string(v2events.EventRunCompleted) {
		t.Fatalf("last_event=%q want %q", card.Card.LastEvent, v2events.EventRunCompleted)
	}
	if card.Card.Lane != v2orchestration.LaneReview {
		t.Fatalf("lane=%q want %q", card.Card.Lane, v2orchestration.LaneReview)
	}
}

func TestOrchestrationReconciler_RecoverOrphanedRuns_TerminalFailedChildProjectsRetryQueue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "startup_failed_child.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 14, 27, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:overseer",
			Children: map[string]ChildRef{
				"agent:worker-failed": {
					Tag:     "agent:worker-failed",
					AgentID: "agent:worker-failed",
					Metadata: map[string]any{
						"workspace_id": "ws-1",
						"issue_id":     "issue-startup-failed",
						"run_id":       "run-startup-failed",
						"request_id":   "req-startup-failed",
						"attempt":      1,
					},
				},
			},
		},
		stateResp: StateResponse{
			AgentID: "agent:worker-failed",
			Status:  "ok",
			State:   json.RawMessage(`{"foxctl":{"status":"completed","last_result":{"envelope":{"status":"error","error":{"message":"database is locked"}}}}}`),
		},
	}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         client,
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now.Add(time.Minute) },
		NewID:          sequentialTestID("startup-failed"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-startup-failed",
		RunID:     "run-startup-failed",
		Metadata: map[string]any{
			"workspace_id": "ws-1",
			"issue_id":     "issue-startup-failed",
			"title":        "Failed child",
			"attempt":      1,
		},
	}, spawn.Response{RunID: "run-startup-failed", AgentID: "agent:worker-failed"}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}

	if err := reconciler.RecoverOrphanedRuns(ctx); err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}
	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-startup-failed"})
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

func TestOrchestrationReconciler_RecoverOrphanedRuns_RuntimeUnavailableReturnsErrorWithoutMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "startup_runtime_error.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := tursoorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.March, 6, 14, 30, 0, 0, time.UTC)
	projections := tursoorchestration.NewStore(db, tursoorchestration.StoreOptions{})
	projections.SetNowForTest(func() time.Time { return now })
	events := &fakeEventAppender{}
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:         events,
		Projections:    projections,
		Reader:         projections,
		Client:         &fakeClient{getChildrenErr: fmt.Errorf("connection refused")},
		ParentAgentIDs: []string{"agent:overseer"},
		Now:            func() time.Time { return now.Add(time.Minute) },
		NewID:          sequentialTestID("startup-runtime-error"),
	})
	if err != nil {
		t.Fatalf("NewOrchestrationReconciler() error = %v", err)
	}
	if _, err := reconciler.RecordDispatchSpawned(ctx, spawn.Request{
		RequestID: "req-startup-error",
		RunID:     "run-startup-error",
		Metadata: map[string]any{
			"workspace_id": "ws-1",
			"issue_id":     "issue-startup-error",
			"title":        "Runtime unavailable",
		},
	}, spawn.Response{RunID: "run-startup-error", AgentID: "agent:worker-error"}); err != nil {
		t.Fatalf("RecordDispatchSpawned() error = %v", err)
	}
	events.events = nil

	if err := reconciler.RecoverOrphanedRuns(ctx); err == nil {
		t.Fatal("RecoverOrphanedRuns() error = nil, want error")
	}
	card, err := projections.Card(ctx, v2orchestration.CardRequest{WorkspaceID: "ws-1", IssueID: "issue-startup-error"})
	if err != nil {
		t.Fatalf("Card() error = %v", err)
	}
	if card.Card.State != v2orchestration.StateRunning {
		t.Fatalf("state=%q want %q", card.Card.State, v2orchestration.StateRunning)
	}
	if len(events.events) != 0 {
		t.Fatalf("appended events=%d want 0", len(events.events))
	}
}

type staticCandidateSource struct {
	candidates []v2runtimeorchestration.Candidate
}

func (s *staticCandidateSource) ListCandidates(context.Context, int) ([]v2runtimeorchestration.Candidate, error) {
	out := make([]v2runtimeorchestration.Candidate, len(s.candidates))
	copy(out, s.candidates)
	return out, nil
}

func fakeOrchestrationEvent(id, requestID, workspaceID, issueID, issueIdentifier, title, state, eligibility string) v2events.Event {
	return v2events.Event{
		ID:            id,
		StreamID:      "run-" + issueID,
		StreamType:    v2events.StreamTypeRun,
		StreamVersion: 1,
		EventType:     v2events.EventRunStarted,
		OccurredAt:    time.Date(2026, time.March, 6, 13, 30, 0, 0, time.UTC),
		Command:       commandDispatchIssue,
		RequestID:     requestID,
		Payload: v2events.MustMarshalPayload(map[string]any{
			"workspace_id":     workspaceID,
			"issue_id":         issueID,
			"issue_identifier": issueIdentifier,
			"title":            title,
			"state":            state,
			"eligibility":      eligibility,
		}),
	}
}

func sequentialTestID(prefix string) func() string {
	seq := 0
	return func() string {
		seq++
		return fmt.Sprintf("%s-%d", prefix, seq)
	}
}
