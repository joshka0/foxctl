package contextplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/storage"
	memorystore "github.com/joshka0/foxctl/internal/storage/memory"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestRecordControlProposalDedupesByDedupeKey(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()

	first, err := store.RecordControlProposal(ctx, testControlProposal("WS-1", "Task:Write", "Add a task for untracked write"))
	if err != nil {
		t.Fatalf("RecordControlProposal first: %v", err)
	}
	second, err := store.RecordControlProposal(ctx, testControlProposal("WS-1", "task:write", "Add a task for the same write"))
	if err != nil {
		t.Fatalf("RecordControlProposal second: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("proposal IDs differ: %s vs %s", first.ID, second.ID)
	}
	if second.Count != 2 {
		t.Fatalf("proposal count=%d want 2", second.Count)
	}
	if second.DedupeKey != "task:write" {
		t.Fatalf("dedupe key=%q want normalized task:write", second.DedupeKey)
	}
	states, err := store.ListControlProposalStates(ctx, 10)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("states=%d want 1", len(states))
	}
	if states[0].DerivedStatus != ProposalStatusOpen {
		t.Fatalf("derived status=%q want open", states[0].DerivedStatus)
	}
}

func TestCoordinatorDecisionsAreAppendOnly(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	proposal := mustRecordControlProposal(t, store, testControlProposal("ws-append", "task:append", "Append decisions"))

	approved := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-approve",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:1"}},
		CreatedAt:     time.Now().UTC(),
	})
	if approved.StatusAfter != ProposalStatusApproved {
		t.Fatalf("approved status_after=%q want approved", approved.StatusAfter)
	}

	deferred := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-defer",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindDefer,
		AuthorityMode: AuthorityModeHumanApproval,
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:2"}},
		CreatedAt:     time.Now().UTC().Add(time.Second),
	})
	if deferred.StatusAfter != ProposalStatusEvaluating {
		t.Fatalf("deferred status_after=%q want evaluating", deferred.StatusAfter)
	}

	decisions, err := store.ListCoordinatorDecisions(ctx, proposal.ID, 0)
	if err != nil {
		t.Fatalf("ListCoordinatorDecisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("decisions=%d want 2", len(decisions))
	}
	if decisions[0].ID != "decision-defer" || decisions[1].ID != "decision-approve" {
		t.Fatalf("decisions not append-only/latest ordered: %+v", decisions)
	}
	state, err := store.GetControlProposalState(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("GetControlProposalState: %v", err)
	}
	if state.DerivedStatus != ProposalStatusEvaluating {
		t.Fatalf("derived status=%q want evaluating", state.DerivedStatus)
	}
}

func TestRecordApplyResultIsIdempotentByKey(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	proposal := mustRecordControlProposal(t, store, testControlProposal("ws-apply", "task:apply", "Apply once"))
	decision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-apply",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:apply"}},
	})

	first, err := store.RecordApplyResult(ctx, ApplyResult{
		ID:             "apply-first",
		ProposalID:     proposal.ID,
		DecisionID:     decision.ID,
		IdempotencyKey: "TASK:123",
		TargetKind:     "task",
		TargetID:       "task-123",
		Status:         ApplyResultStatusApplied,
		Result:         map[string]any{"task_id": "task-123"},
	})
	if err != nil {
		t.Fatalf("RecordApplyResult first: %v", err)
	}
	second, err := store.RecordApplyResult(ctx, ApplyResult{
		ID:             "apply-second",
		ProposalID:     proposal.ID,
		DecisionID:     decision.ID,
		IdempotencyKey: "task:123",
		TargetKind:     "task",
		TargetID:       "task-should-not-exist",
		Status:         ApplyResultStatusApplied,
	})
	if err != nil {
		t.Fatalf("RecordApplyResult duplicate: %v", err)
	}
	if second.ID != first.ID || second.TargetID != "task-123" {
		t.Fatalf("idempotent result not reused: first=%+v second=%+v", first, second)
	}
	state, err := store.GetControlProposalState(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("GetControlProposalState: %v", err)
	}
	if state.DerivedStatus != ProposalStatusApplied {
		t.Fatalf("derived status=%q want applied", state.DerivedStatus)
	}
}

func TestRecordApplyResultHandlesReplayConflict(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	proposal := mustRecordControlProposal(t, store, testControlProposal("ws-replay", "task:replay", "Replay apply"))
	decision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-replay",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:replay"}},
	})
	db, closeFn, err := store.openMutableDB(ctx)
	if err != nil {
		t.Fatalf("openMutableDB: %v", err)
	}
	defer func() { _ = closeFn() }()
	preexisting := ApplyResult{
		ID:             "apply-preexisting",
		ProposalID:     proposal.ID,
		DecisionID:     decision.ID,
		IdempotencyKey: "task:race",
		TargetKind:     "task",
		TargetID:       "task-race",
		Status:         ApplyResultStatusApplied,
		CreatedAt:      time.Now().UTC(),
	}
	if err := insertApplyResultRow(ctx, db, preexisting); err != nil {
		t.Fatalf("insertApplyResultRow preexisting: %v", err)
	}

	got, err := store.RecordApplyResult(ctx, ApplyResult{
		ID:             "apply-replayed",
		ProposalID:     proposal.ID,
		DecisionID:     decision.ID,
		IdempotencyKey: "task:race",
		TargetKind:     "task",
		TargetID:       "task-new",
		Status:         ApplyResultStatusApplied,
	})
	if err != nil {
		t.Fatalf("RecordApplyResult replay: %v", err)
	}
	if got.ID != preexisting.ID || got.TargetID != "task-race" {
		t.Fatalf("replay did not return preexisting result: %+v", got)
	}
}

func TestRejectsInvalidControlTransitions(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()

	if _, err := store.RecordControlProposal(ctx, testControlProposal("", "task:missing-workspace", "Missing workspace")); err == nil {
		t.Fatalf("RecordControlProposal missing workspace succeeded")
	}
	if _, err := store.RecordCoordinatorDecision(ctx, CoordinatorDecision{
		ProposalID:    "missing",
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:missing"}},
	}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("RecordCoordinatorDecision missing proposal err=%v", err)
	}

	proposal := mustRecordControlProposal(t, store, testControlProposal("ws-invalid", "task:invalid", "Invalid transitions"))
	if _, err := store.RecordCoordinatorDecision(ctx, CoordinatorDecision{
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
	}); err == nil || !strings.Contains(err.Error(), "evidence_refs") {
		t.Fatalf("approval without evidence err=%v", err)
	}
	if _, err := store.RecordCoordinatorDecision(ctx, CoordinatorDecision{
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: "", Ref: ""}},
	}); err == nil || !strings.Contains(err.Error(), "invalid ref type") {
		t.Fatalf("approval with invalid evidence err=%v", err)
	}
	if _, err := store.RecordCoordinatorDecision(ctx, CoordinatorDecision{
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:no-policy"}},
	}); err == nil || !strings.Contains(err.Error(), "policy_id") {
		t.Fatalf("coordinator approval without policy err=%v", err)
	}
	if _, err := store.RecordApplyResult(ctx, ApplyResult{
		ProposalID:     proposal.ID,
		DecisionID:     "missing-decision",
		IdempotencyKey: "task:invalid",
		TargetKind:     "task",
	}); err == nil || !strings.Contains(err.Error(), "no approving decision") {
		t.Fatalf("apply without approval err=%v", err)
	}

	approval := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-stale-approval",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:approval"}},
		CreatedAt:     time.Now().UTC(),
	})
	mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-reject",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindReject,
		AuthorityMode: AuthorityModeHumanApproval,
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:reject"}},
		CreatedAt:     time.Now().UTC().Add(time.Second),
	})
	if _, err := store.RecordApplyResult(ctx, ApplyResult{
		ProposalID:     proposal.ID,
		DecisionID:     approval.ID,
		IdempotencyKey: "task:rejected",
		TargetKind:     "task",
	}); err == nil || !strings.Contains(err.Error(), "not the latest decision") {
		t.Fatalf("apply after rejection err=%v", err)
	}
}

func TestLatestDecisionOrderingIsDeterministic(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	proposal := mustRecordControlProposal(t, store, testControlProposal("ws-order", "task:order", "Order same timestamps"))
	stamp := time.Now().UTC()
	mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-a",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:order-a"}},
		CreatedAt:     stamp,
	})
	mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-z",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindReject,
		AuthorityMode: AuthorityModeHumanApproval,
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:order-z"}},
		CreatedAt:     stamp,
	})

	decisions, err := store.ListCoordinatorDecisions(ctx, proposal.ID, 0)
	if err != nil {
		t.Fatalf("ListCoordinatorDecisions: %v", err)
	}
	if decisions[0].ID != "decision-z" {
		t.Fatalf("latest decision=%s want deterministic id desc tie-break", decisions[0].ID)
	}
	state, err := store.GetControlProposalState(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("GetControlProposalState: %v", err)
	}
	if state.DerivedStatus != ProposalStatusRejected {
		t.Fatalf("derived status=%q want rejected", state.DerivedStatus)
	}
}

func TestListControlProposalStatesDerivesLatestState(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	now := time.Now().UTC()
	open := testControlProposal("ws-list", "task:open", "Open proposal")
	open.CreatedAt = now
	open.UpdatedAt = now
	approved := testControlProposal("ws-list", "task:approved", "Approved proposal")
	approved.CreatedAt = now.Add(time.Second)
	approved.UpdatedAt = now.Add(time.Second)
	applied := testControlProposal("ws-list", "task:applied", "Applied proposal")
	applied.CreatedAt = now.Add(2 * time.Second)
	applied.UpdatedAt = now.Add(2 * time.Second)

	openStored := mustRecordControlProposal(t, store, open)
	approvedStored := mustRecordControlProposal(t, store, approved)
	appliedStored := mustRecordControlProposal(t, store, applied)

	mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-approved-list",
		ProposalID:    approvedStored.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:approved-list"}},
	})
	applyDecision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-applied-list",
		ProposalID:    appliedStored.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "low-risk-task-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:applied-list"}},
	})
	if _, err := store.RecordApplyResult(ctx, ApplyResult{
		ProposalID:     appliedStored.ID,
		DecisionID:     applyDecision.ID,
		IdempotencyKey: "task:applied-list",
		TargetKind:     "task",
		TargetID:       "task-applied",
		Status:         ApplyResultStatusApplied,
	}); err != nil {
		t.Fatalf("RecordApplyResult: %v", err)
	}

	states, err := store.ListControlProposalStates(ctx, 0)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("states=%d want 3", len(states))
	}
	if states[0].Proposal.ID != appliedStored.ID || states[0].DerivedStatus != ProposalStatusApplied {
		t.Fatalf("state[0]=%+v want applied latest", states[0])
	}
	if states[1].Proposal.ID != approvedStored.ID || states[1].DerivedStatus != ProposalStatusApproved {
		t.Fatalf("state[1]=%+v want approved", states[1])
	}
	if states[2].Proposal.ID != openStored.ID || states[2].DerivedStatus != ProposalStatusOpen {
		t.Fatalf("state[2]=%+v want open", states[2])
	}
}

// [[test:internal/context/contextplane/control_plane_test.go#TestApplyTaskProposalAppliesOnePendingTaskIdempotently]]
func TestApplyTaskProposalAppliesOnePendingTaskIdempotently(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	taskDB := mustOpenTaskStore(t)
	defer func() { _ = taskDB.Close() }()

	proposal := mustRecordControlProposal(t, store, testTaskProposal("ws-task-apply", workspaceRoot, "pkg/main.go", "Edit pkg/main.go", "low"))
	decision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-task-apply",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "task-proposal-auto-low-risk-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:task-apply"}},
	})

	firstApply, firstTask, err := store.ApplyTaskProposal(ctx, taskDB, proposal.ID, decision.ID)
	if err != nil {
		t.Fatalf("ApplyTaskProposal first: %v", err)
	}
	secondApply, secondTask, err := store.ApplyTaskProposal(ctx, taskDB, proposal.ID, decision.ID)
	if err != nil {
		t.Fatalf("ApplyTaskProposal replay: %v", err)
	}
	if firstApply.ID != secondApply.ID {
		t.Fatalf("apply replay id mismatch: %s vs %s", firstApply.ID, secondApply.ID)
	}
	if firstTask.ID != secondTask.ID {
		t.Fatalf("task replay id mismatch: %s vs %s", firstTask.ID, secondTask.ID)
	}
	if firstApply.TargetKind != "task" {
		t.Fatalf("target_kind=%q want task", firstApply.TargetKind)
	}
	if firstTask.Status != taskstore.StatusPending {
		t.Fatalf("task status=%q want pending", firstTask.Status)
	}
	tasks, err := taskDB.ListByWorkspace(ctx, proposal.WorkspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count=%d want 1", len(tasks))
	}
}

func TestApplyTaskProposalWithoutApprovalFailsNoTask(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	taskDB := mustOpenTaskStore(t)
	defer func() { _ = taskDB.Close() }()

	proposal := mustRecordControlProposal(t, store, testTaskProposal("ws-task-no-approval", workspaceRoot, "pkg/main.go", "Edit pkg/main.go", "low"))
	if _, _, err := store.ApplyTaskProposal(ctx, taskDB, proposal.ID, "missing-decision"); err == nil || !strings.Contains(err.Error(), "no approving decision") {
		t.Fatalf("ApplyTaskProposal without approval err=%v", err)
	}
	tasks, err := taskDB.ListByWorkspace(ctx, proposal.WorkspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("task count=%d want 0", len(tasks))
	}
}

func TestApplyTaskProposalRejectDecisionCannotApply(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	taskDB := mustOpenTaskStore(t)
	defer func() { _ = taskDB.Close() }()

	proposal := mustRecordControlProposal(t, store, testTaskProposal("ws-task-reject", workspaceRoot, "pkg/main.go", "Edit pkg/main.go", "low"))
	mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-task-approve-before-reject",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "task-proposal-auto-low-risk-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:task-approve-before-reject"}},
		CreatedAt:     time.Now().UTC(),
	})
	reject := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-task-reject",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindReject,
		AuthorityMode: AuthorityModeHumanApproval,
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:task-reject"}},
		CreatedAt:     time.Now().UTC().Add(time.Second),
	})

	if _, _, err := store.ApplyTaskProposal(ctx, taskDB, proposal.ID, reject.ID); err == nil || !strings.Contains(err.Error(), "does not approve proposal") {
		t.Fatalf("ApplyTaskProposal rejected decision err=%v", err)
	}
	tasks, err := taskDB.ListByWorkspace(ctx, proposal.WorkspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("task count=%d want 0", len(tasks))
	}
}

func TestProcessControlProposalsAutoApprovesLowRiskTaskProposal(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	taskDB := mustOpenTaskStore(t)
	defer func() { _ = taskDB.Close() }()

	proposal := mustRecordControlProposal(t, store, testTaskProposal("ws-task-process-low", workspaceRoot, "pkg/main.go", "Edit pkg/main.go", "low"))
	result, err := store.ProcessControlProposals(ctx, TaskProposalControlProcessInput{
		TaskStore:  taskDB,
		ProposalID: proposal.ID,
	})
	if err != nil {
		t.Fatalf("ProcessControlProposals: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("processed items=%d want 1", len(result.Items))
	}
	item := result.Items[0]
	if item.Decision == nil || item.Decision.Decision != DecisionKindApprove {
		t.Fatalf("decision=%+v want approve", item.Decision)
	}
	if item.Apply == nil || item.Apply.TargetKind != "task" {
		t.Fatalf("apply=%+v want task apply", item.Apply)
	}
	if item.Task == nil || item.Task.Status != taskstore.StatusPending {
		t.Fatalf("task=%+v want pending task", item.Task)
	}

	state, err := store.GetControlProposalState(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("GetControlProposalState: %v", err)
	}
	if state.LatestDecision == nil || state.LatestDecision.Decision != DecisionKindApprove {
		t.Fatalf("latest decision=%+v", state.LatestDecision)
	}
	if state.LatestApplyResult == nil || state.LatestApplyResult.TargetKind != "task" {
		t.Fatalf("latest apply=%+v", state.LatestApplyResult)
	}
	tasks, err := taskDB.ListByWorkspace(ctx, proposal.WorkspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count=%d want 1", len(tasks))
	}
}

func TestProcessControlProposalsHighBlastRadiusNeedsAuthorityNoTask(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	taskDB := mustOpenTaskStore(t)
	defer func() { _ = taskDB.Close() }()

	proposal := mustRecordControlProposal(t, store, testTaskProposal("ws-task-process-high", workspaceRoot, "pkg/main.go", "Edit pkg/main.go", "high"))
	result, err := store.ProcessControlProposals(ctx, TaskProposalControlProcessInput{
		TaskStore:  taskDB,
		ProposalID: proposal.ID,
	})
	if err != nil {
		t.Fatalf("ProcessControlProposals: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("processed items=%d want 1", len(result.Items))
	}
	item := result.Items[0]
	if item.Decision == nil || item.Decision.Decision != DecisionKindEscalate {
		t.Fatalf("decision=%+v want escalate", item.Decision)
	}
	if item.Decision.StatusAfter != ProposalStatusNeedsAuthority {
		t.Fatalf("status_after=%q want needs_authority", item.Decision.StatusAfter)
	}
	if item.Apply != nil {
		t.Fatalf("unexpected apply=%+v", item.Apply)
	}
	tasks, err := taskDB.ListByWorkspace(ctx, proposal.WorkspaceID)
	if err != nil {
		t.Fatalf("ListByWorkspace: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("task count=%d want 0", len(tasks))
	}
}

func TestRecordMemoryCandidateProposalRequiresCoreFields(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	valid := MemoryCandidateInput{
		WorkspaceID: "ws-candidate",
		Name:        "memory.candidate.test",
		Kind:        "decision",
		Summary:     "Candidate summary",
		FileRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/context/contextplane/control_plane.go"},
		},
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "evt:candidate"},
		},
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "docs/candidate.md"},
		},
	}

	if _, err := store.RecordMemoryCandidateProposal(ctx, MemoryCandidateInput{}); err == nil || !strings.Contains(err.Error(), "workspace_id") {
		t.Fatalf("missing workspace err=%v", err)
	}
	missingName := valid
	missingName.Name = ""
	if _, err := store.RecordMemoryCandidateProposal(ctx, missingName); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("missing name err=%v", err)
	}
	missingKind := valid
	missingKind.Kind = ""
	if _, err := store.RecordMemoryCandidateProposal(ctx, missingKind); err == nil || !strings.Contains(err.Error(), "kind is required") {
		t.Fatalf("missing kind err=%v", err)
	}
	invalidKind := valid
	invalidKind.Kind = "not_a_memory_kind"
	if _, err := store.RecordMemoryCandidateProposal(ctx, invalidKind); err == nil || !strings.Contains(err.Error(), "invalid memory candidate kind") {
		t.Fatalf("invalid kind err=%v", err)
	}
	missingSummary := valid
	missingSummary.Summary = ""
	if _, err := store.RecordMemoryCandidateProposal(ctx, missingSummary); err == nil || !strings.Contains(err.Error(), "summary is required") {
		t.Fatalf("missing summary err=%v", err)
	}
	missingSource := valid
	missingSource.SourceRefs = nil
	if _, err := store.RecordMemoryCandidateProposal(ctx, missingSource); err == nil || !strings.Contains(err.Error(), "source_refs") {
		t.Fatalf("missing source refs err=%v", err)
	}
	missingEvidence := valid
	missingEvidence.EvidenceRefs = nil
	if _, err := store.RecordMemoryCandidateProposal(ctx, missingEvidence); err == nil || !strings.Contains(err.Error(), "evidence_refs") {
		t.Fatalf("missing evidence refs err=%v", err)
	}

	stored, err := store.RecordMemoryCandidateProposal(ctx, valid)
	if err != nil {
		t.Fatalf("RecordMemoryCandidateProposal valid: %v", err)
	}
	if stored.Kind != ProposalKindMemoryCandidate {
		t.Fatalf("kind=%q want memory_candidate", stored.Kind)
	}
	if stored.Payload["name"] != valid.Name {
		t.Fatalf("payload name=%v want %s", stored.Payload["name"], valid.Name)
	}
	if stored.Payload["instruction_eligible"] != false || stored.Payload["evidence_only"] != true {
		t.Fatalf("candidate usage payload=%v", stored.Payload)
	}
}

func TestApplyMemoryCandidateRequiresApprovalDecision(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	mem := mustOpenMemoryStore(t)
	defer func() { _ = mem.Close() }()

	proposal, err := store.RecordMemoryCandidateProposal(ctx, testMemoryCandidateInput("ws-apply-no-approval"))
	if err != nil {
		t.Fatalf("RecordMemoryCandidateProposal: %v", err)
	}
	if _, _, err := store.ApplyMemoryCandidate(ctx, mem, proposal.ID, "missing-decision"); err == nil || !strings.Contains(err.Error(), "no approving decision") {
		t.Fatalf("ApplyMemoryCandidate without approval err=%v", err)
	}
	entries, err := mem.List(ctx, proposal.WorkspaceID, 10)
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected memories written without approval: %d", len(entries))
	}
}

func TestApplyMemoryCandidateAppliesOneNamedMemoryIdempotently(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	mem := mustOpenMemoryStore(t)
	defer func() { _ = mem.Close() }()

	in := testMemoryCandidateInput("ws-apply-candidate")
	proposal, err := store.RecordMemoryCandidateProposal(ctx, in)
	if err != nil {
		t.Fatalf("RecordMemoryCandidateProposal: %v", err)
	}
	decision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-apply-candidate",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "memory-candidate-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:approve-candidate"}},
	})

	firstApply, firstEntry, err := store.ApplyMemoryCandidate(ctx, mem, proposal.ID, decision.ID)
	if err != nil {
		t.Fatalf("ApplyMemoryCandidate first: %v", err)
	}
	secondApply, secondEntry, err := store.ApplyMemoryCandidate(ctx, mem, proposal.ID, decision.ID)
	if err != nil {
		t.Fatalf("ApplyMemoryCandidate replay: %v", err)
	}
	if firstApply.ID != secondApply.ID {
		t.Fatalf("apply replay ID mismatch: %s vs %s", firstApply.ID, secondApply.ID)
	}
	if firstEntry.ID != secondEntry.ID || firstEntry.Name != secondEntry.Name {
		t.Fatalf("entry replay mismatch: first=%+v second=%+v", firstEntry, secondEntry)
	}
	if firstApply.TargetKind != "named_memory" {
		t.Fatalf("target_kind=%q want named_memory", firstApply.TargetKind)
	}

	entries, err := mem.List(ctx, proposal.WorkspaceID, 10)
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("named memory count=%d want 1", len(entries))
	}
	if entries[0].LifecycleState != "candidate" {
		t.Fatalf("lifecycle=%q want candidate", entries[0].LifecycleState)
	}
	if entries[0].ReviewStatus != "needs_review" {
		t.Fatalf("review_status=%q want needs_review", entries[0].ReviewStatus)
	}
	record := memorycore.RecordFromNamedEntry(entries[0], memorycore.NamedEntryOptions{})
	if record.Usage.InstructionEligible {
		t.Fatalf("instruction eligibility should remain false for named memory projection")
	}
	if !record.Usage.EvidenceOnly {
		t.Fatalf("evidence_only should remain true for named memory projection")
	}
	if entries[0].Name == in.Name {
		t.Fatalf("candidate apply should not overwrite requested active memory name")
	}
}

func TestApplyMemoryCandidateDoesNotOverwriteExistingNamedMemory(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	mem := mustOpenMemoryStore(t)
	defer func() { _ = mem.Close() }()

	in := testMemoryCandidateInput("ws-apply-no-overwrite")
	if _, err := mem.Save(ctx, storage.NamedEntry{
		Name:           in.Name,
		Type:           "decision",
		Workspace:      in.WorkspaceID,
		Summary:        "Existing trusted memory",
		Result:         []byte(`{"version":1,"status":"ok","command":"manual","data":{},"meta":{"ts":"2026-05-08T00:00:00Z"},"error":{}}`),
		LifecycleState: "active",
		ReviewStatus:   "reviewed",
	}); err != nil {
		t.Fatalf("save existing memory: %v", err)
	}

	proposal, err := store.RecordMemoryCandidateProposal(ctx, in)
	if err != nil {
		t.Fatalf("RecordMemoryCandidateProposal: %v", err)
	}
	decision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-apply-no-overwrite",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "memory-candidate-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:approve-no-overwrite"}},
	})

	_, candidate, err := store.ApplyMemoryCandidate(ctx, mem, proposal.ID, decision.ID)
	if err != nil {
		t.Fatalf("ApplyMemoryCandidate: %v", err)
	}
	if candidate.Name == in.Name {
		t.Fatalf("candidate name should be isolated from existing memory name")
	}
	existing, err := mem.Get(ctx, in.Name, in.WorkspaceID)
	if err != nil {
		t.Fatalf("get existing memory: %v", err)
	}
	if existing.LifecycleState != "active" || existing.ReviewStatus != "reviewed" {
		t.Fatalf("existing memory lifecycle changed: %+v", existing)
	}
	entries, err := mem.List(ctx, in.WorkspaceID, 10)
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("named memory count=%d want 2", len(entries))
	}
}

func TestApplyMemoryCandidateSupportsUnreviewedCandidates(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	mem := mustOpenMemoryStore(t)
	defer func() { _ = mem.Close() }()

	reviewRequired := false
	in := testMemoryCandidateInput("ws-apply-unreviewed")
	in.ReviewRequired = &reviewRequired
	proposal, err := store.RecordMemoryCandidateProposal(ctx, in)
	if err != nil {
		t.Fatalf("RecordMemoryCandidateProposal: %v", err)
	}
	decision := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-apply-unreviewed",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "memory-candidate-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:approve-unreviewed"}},
	})

	if _, _, err := store.ApplyMemoryCandidate(ctx, mem, proposal.ID, decision.ID); err != nil {
		t.Fatalf("ApplyMemoryCandidate: %v", err)
	}
	entries, err := mem.List(ctx, proposal.WorkspaceID, 10)
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("named memory count=%d want 1", len(entries))
	}
	if entries[0].ReviewStatus != "unreviewed" {
		t.Fatalf("review_status=%q want unreviewed", entries[0].ReviewStatus)
	}
}

func TestApplyMemoryCandidateRejectDecisionCannotApply(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	ctx := context.Background()
	mem := mustOpenMemoryStore(t)
	defer func() { _ = mem.Close() }()

	proposal, err := store.RecordMemoryCandidateProposal(ctx, testMemoryCandidateInput("ws-apply-reject"))
	if err != nil {
		t.Fatalf("RecordMemoryCandidateProposal: %v", err)
	}
	mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-approve-before-reject",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindApprove,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		PolicyID:      "memory-candidate-v1",
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:approve-before-reject"}},
		CreatedAt:     time.Now().UTC(),
	})
	reject := mustRecordCoordinatorDecision(t, store, CoordinatorDecision{
		ID:            "decision-reject-candidate",
		ProposalID:    proposal.ID,
		Decision:      DecisionKindReject,
		AuthorityMode: AuthorityModeHumanApproval,
		EvidenceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "evt:reject-candidate"}},
		CreatedAt:     time.Now().UTC().Add(time.Second),
	})

	if _, _, err := store.ApplyMemoryCandidate(ctx, mem, proposal.ID, reject.ID); err == nil || !strings.Contains(err.Error(), "does not approve proposal") {
		t.Fatalf("ApplyMemoryCandidate rejected decision err=%v", err)
	}
	entries, err := mem.List(ctx, proposal.WorkspaceID, 10)
	if err != nil {
		t.Fatalf("memory list: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected memories written after rejection: %d", len(entries))
	}
}

func testControlProposal(workspaceID, dedupeKey, summary string) ControlProposal {
	return ControlProposal{
		DedupeKey:   dedupeKey,
		Kind:        ProposalKindTaskProposal,
		WorkspaceID: workspaceID,
		Summary:     summary,
		SourceRefs:  []contextengine.EvidenceRef{{Type: contextengine.RefTypeEvent, Ref: "hook:write"}},
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/context/contextplane/control_plane.go"},
		},
		Payload: map[string]any{"title": summary},
	}
}

func testMemoryCandidateInput(workspaceID string) MemoryCandidateInput {
	return MemoryCandidateInput{
		WorkspaceID:   workspaceID,
		Name:          "memory.candidate.example",
		Kind:          "decision",
		Summary:       "Capture the reviewed memory candidate for follow-up.",
		Content:       "Candidate memory body for review.",
		TemporalScope: "sprint",
		FileRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "internal/context/contextplane/control_plane.go"},
		},
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "evt:memory-candidate"},
		},
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "docs/spec/memory-candidate.md"},
		},
	}
}

func testTaskProposal(workspaceID, workspaceRoot, scopePath, title, blastRadius string) ControlProposal {
	return ControlProposal{
		DedupeKey:      "task-proposal:" + workspaceID + ":" + scopePath + ":" + title + ":" + blastRadius,
		Kind:           ProposalKindTaskProposal,
		WorkspaceID:    workspaceID,
		Summary:        "task proposal: " + title,
		BlastRadius:    blastRadius,
		ReviewRequired: true,
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "evt:task-proposal-source"},
		},
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: scopePath},
		},
		Payload: map[string]any{
			"title":          title,
			"scope_path":     scopePath,
			"workspace_root": workspaceRoot,
			"intent":         "pre-tool-use write guard proposal",
			"event":          "PreToolUse",
			"tool_name":      "Edit",
			"tool_canonical": "edit.apply_patch",
			"tool_kind":      "write",
			"source_tool":    "edit.apply_patch",
			"source_event":   "PreToolUse",
		},
	}
}

func mustOpenMemoryStore(t *testing.T) *memorystore.Store {
	t.Helper()
	store, err := memorystore.Open(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatalf("memory.Open: %v", err)
	}
	return store
}

func mustOpenTaskStore(t *testing.T) taskstore.Store {
	t.Helper()
	store, err := taskstore.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("tasks.Open: %v", err)
	}
	return store
}

func mustRecordControlProposal(t *testing.T, store *WorkspaceStore, proposal ControlProposal) ControlProposal {
	t.Helper()
	stored, err := store.RecordControlProposal(context.Background(), proposal)
	if err != nil {
		t.Fatalf("RecordControlProposal: %v", err)
	}
	return stored
}

func mustRecordCoordinatorDecision(t *testing.T, store *WorkspaceStore, decision CoordinatorDecision) CoordinatorDecision {
	t.Helper()
	stored, err := store.RecordCoordinatorDecision(context.Background(), decision)
	if err != nil {
		t.Fatalf("RecordCoordinatorDecision: %v", err)
	}
	return stored
}
