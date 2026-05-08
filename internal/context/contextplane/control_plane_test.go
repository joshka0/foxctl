package contextplane

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
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
