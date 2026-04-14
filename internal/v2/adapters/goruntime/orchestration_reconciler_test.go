package goruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/dbutil"
	libsqlorchestration "github.com/joshka0/foxctl/internal/v2/adapters/libsql/orchestration"
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
	if err := libsqlorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.April, 6, 20, 0, 0, 0, time.UTC)
	projections := libsqlorchestration.NewStore(db, libsqlorchestration.StoreOptions{})
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

func TestOrchestrationReconciler_Reconcile_ProjectsCompletedWorkerToReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "dispatch_completed.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := libsqlorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.April, 6, 20, 15, 0, 0, time.UTC)
	projections := libsqlorchestration.NewStore(db, libsqlorchestration.StoreOptions{
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
	if err := libsqlorchestration.MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	now := time.Date(2026, time.April, 6, 20, 30, 0, 0, time.UTC)
	projections := libsqlorchestration.NewStore(db, libsqlorchestration.StoreOptions{})
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
}

func (f *fakeWorkerReader) Worker(context.Context, coreworker.LookupRequest) (coreworker.Record, error) {
	return coreworker.Record{}, fmt.Errorf("not implemented")
}

func (f *fakeWorkerReader) Children(_ context.Context, req coreworker.ChildrenRequest) ([]coreworker.Record, error) {
	f.lastParentAgentID = req.ParentAgentID
	return append([]coreworker.Record(nil), f.childrenByParent[req.ParentAgentID]...), nil
}

func sequentialTestID(prefix string) func() string {
	seq := 0
	return func() string {
		seq++
		return fmt.Sprintf("%s-%d", prefix, seq)
	}
}
