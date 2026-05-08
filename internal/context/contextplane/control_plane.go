package contextplane

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
)

// RecordControlProposal persists one deduplicated signal for coordinator review.
//
// [[domain:coordinator-control-plane-ledger]]
func (s *WorkspaceStore) RecordControlProposal(ctx context.Context, proposal ControlProposal) (ControlProposal, error) {
	normalized, err := normalizeControlProposal(proposal)
	if err != nil {
		return ControlProposal{}, err
	}
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return ControlProposal{}, err
	}
	defer func() { _ = closeFn() }()
	if err := upsertControlProposalRow(ctx, db, normalized); err != nil {
		return ControlProposal{}, fmt.Errorf("record control proposal: %w", err)
	}
	stored, err := findControlProposalRowByKey(ctx, db, normalized.DedupeKey)
	if err != nil {
		return ControlProposal{}, err
	}
	if stored == nil {
		return ControlProposal{}, fmt.Errorf("control proposal persisted but could not be reloaded")
	}
	return *stored, nil
}

// GetControlProposalState returns the proposal plus its latest decision/apply projection.
func (s *WorkspaceStore) GetControlProposalState(ctx context.Context, id string) (*ControlProposalState, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	proposal, err := findControlProposalRow(ctx, db, strings.TrimSpace(id))
	if err != nil || proposal == nil {
		return nil, err
	}
	state, err := buildControlProposalState(ctx, db, *proposal)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// ListControlProposalStates lists proposal read models ordered by proposal freshness.
func (s *WorkspaceStore) ListControlProposalStates(ctx context.Context, limit int) ([]ControlProposalState, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	proposals, err := listControlProposalRows(ctx, db, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ControlProposalState, 0, len(proposals))
	for _, proposal := range proposals {
		state, err := buildControlProposalState(ctx, db, proposal)
		if err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	return out, nil
}

// RecordCoordinatorDecision appends one authority decision for a control proposal.
func (s *WorkspaceStore) RecordCoordinatorDecision(ctx context.Context, decision CoordinatorDecision) (CoordinatorDecision, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return CoordinatorDecision{}, err
	}
	defer func() { _ = closeFn() }()
	proposal, err := findControlProposalRow(ctx, db, decision.ProposalID)
	if err != nil {
		return CoordinatorDecision{}, err
	}
	if proposal == nil {
		return CoordinatorDecision{}, fmt.Errorf("control proposal %s not found", strings.TrimSpace(decision.ProposalID))
	}
	normalized, err := normalizeCoordinatorDecision(decision, *proposal)
	if err != nil {
		return CoordinatorDecision{}, err
	}
	if err := insertCoordinatorDecisionRow(ctx, db, normalized); err != nil {
		return CoordinatorDecision{}, fmt.Errorf("record coordinator decision: %w", err)
	}
	decisions, err := listCoordinatorDecisionRows(ctx, db, normalized.ProposalID, 0)
	if err != nil {
		return CoordinatorDecision{}, err
	}
	for _, stored := range decisions {
		if stored.ID == normalized.ID {
			return stored, nil
		}
	}
	return CoordinatorDecision{}, fmt.Errorf("coordinator decision persisted but could not be reloaded")
}

// ListCoordinatorDecisions returns append-only decisions for one proposal.
func (s *WorkspaceStore) ListCoordinatorDecisions(ctx context.Context, proposalID string, limit int) ([]CoordinatorDecision, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	if strings.TrimSpace(proposalID) == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	return listCoordinatorDecisionRows(ctx, db, proposalID, limit)
}

// RecordApplyResult stores an idempotent apply result for an approved decision.
//
// [[invariant:control-proposal-apply-requires-approval]]
func (s *WorkspaceStore) RecordApplyResult(ctx context.Context, result ApplyResult) (ApplyResult, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	defer func() { _ = closeFn() }()
	result.IdempotencyKey = strings.ToLower(strings.TrimSpace(result.IdempotencyKey))
	existing, err := findApplyResultRowByIdempotencyKey(ctx, db, result.IdempotencyKey)
	if err != nil {
		return ApplyResult{}, err
	}
	if existing != nil {
		if strings.TrimSpace(result.ProposalID) != "" && existing.ProposalID != strings.TrimSpace(result.ProposalID) {
			return ApplyResult{}, fmt.Errorf("idempotency_key %s already belongs to proposal %s", result.IdempotencyKey, existing.ProposalID)
		}
		return *existing, nil
	}
	proposal, err := findControlProposalRow(ctx, db, result.ProposalID)
	if err != nil {
		return ApplyResult{}, err
	}
	if proposal == nil {
		return ApplyResult{}, fmt.Errorf("control proposal %s not found", strings.TrimSpace(result.ProposalID))
	}
	decision, err := findApprovedDecisionForApply(ctx, db, *proposal, result.DecisionID)
	if err != nil {
		return ApplyResult{}, err
	}
	normalized, err := normalizeApplyResult(result, *proposal, decision)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := insertApplyResultRow(ctx, db, normalized); err != nil {
		return ApplyResult{}, fmt.Errorf("record apply result: %w", err)
	}
	stored, err := findApplyResultRowByIdempotencyKey(ctx, db, normalized.IdempotencyKey)
	if err != nil {
		return ApplyResult{}, err
	}
	if stored == nil {
		return ApplyResult{}, fmt.Errorf("apply result persisted but could not be reloaded")
	}
	if stored.ProposalID != normalized.ProposalID {
		return ApplyResult{}, fmt.Errorf("idempotency_key %s belongs to proposal %s", normalized.IdempotencyKey, stored.ProposalID)
	}
	return *stored, nil
}

func normalizeControlProposal(proposal ControlProposal) (ControlProposal, error) {
	proposal.DedupeKey = effectiveControlProposalKey(proposal)
	if proposal.DedupeKey == "" {
		return ControlProposal{}, fmt.Errorf("dedupe_key is required")
	}
	proposal.WorkspaceID = strings.TrimSpace(proposal.WorkspaceID)
	if proposal.WorkspaceID == "" {
		return ControlProposal{}, fmt.Errorf("workspace_id is required")
	}
	if !proposal.Kind.IsValid() {
		return ControlProposal{}, fmt.Errorf("invalid proposal kind %q", proposal.Kind)
	}
	if proposal.Status == "" {
		proposal.Status = ProposalStatusOpen
	}
	if !proposal.Status.IsValid() {
		return ControlProposal{}, fmt.Errorf("invalid proposal status %q", proposal.Status)
	}
	proposal.Summary = strings.TrimSpace(proposal.Summary)
	if proposal.Summary == "" {
		return ControlProposal{}, fmt.Errorf("summary is required")
	}
	if proposal.Count <= 0 {
		proposal.Count = 1
	}
	now := timeutil.NowUTC()
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = now
	}
	if proposal.UpdatedAt.IsZero() {
		proposal.UpdatedAt = proposal.CreatedAt
	}
	proposal.SessionID = strings.TrimSpace(proposal.SessionID)
	proposal.AgentID = strings.TrimSpace(proposal.AgentID)
	proposal.RoomID = strings.TrimSpace(proposal.RoomID)
	proposal.BlastRadius = firstNonEmpty(strings.TrimSpace(proposal.BlastRadius), "medium")
	proposal.SourceRefs = uniqueEvidenceRefs(proposal.SourceRefs)
	proposal.EvidenceRefs = uniqueEvidenceRefs(proposal.EvidenceRefs)
	if proposal.Payload == nil {
		proposal.Payload = map[string]any{}
	}
	return proposal, nil
}

func normalizeCoordinatorDecision(decision CoordinatorDecision, proposal ControlProposal) (CoordinatorDecision, error) {
	decision.ProposalID = strings.TrimSpace(decision.ProposalID)
	if decision.ProposalID == "" {
		return CoordinatorDecision{}, fmt.Errorf("proposal_id is required")
	}
	if !decision.Decision.IsValid() {
		return CoordinatorDecision{}, fmt.Errorf("invalid decision %q", decision.Decision)
	}
	if !decision.AuthorityMode.IsValid() {
		return CoordinatorDecision{}, fmt.Errorf("invalid authority mode %q", decision.AuthorityMode)
	}
	if len(uniqueEvidenceRefs(decision.EvidenceRefs)) == 0 {
		return CoordinatorDecision{}, fmt.Errorf("evidence_refs are required")
	}
	if decision.Decision == DecisionKindApprove &&
		decision.AuthorityMode == AuthorityModeCoordinatorPolicy &&
		strings.TrimSpace(decision.PolicyID) == "" &&
		strings.TrimSpace(decision.PolicyHash) == "" {
		return CoordinatorDecision{}, fmt.Errorf("coordinator_policy approvals require policy_id or policy_hash")
	}
	if decision.StatusAfter == "" {
		decision.StatusAfter = statusAfterDecision(decision.Decision)
	}
	if !decision.StatusAfter.IsValid() {
		return CoordinatorDecision{}, fmt.Errorf("invalid status_after %q", decision.StatusAfter)
	}
	if decision.ID == "" {
		decision.ID = buildRecordID("CD", timeutil.NowUTC())
	}
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = timeutil.NowUTC()
	}
	decision.WorkspaceID = firstNonEmpty(strings.TrimSpace(decision.WorkspaceID), proposal.WorkspaceID)
	decision.ApprovalActor = strings.TrimSpace(decision.ApprovalActor)
	decision.PolicyID = strings.TrimSpace(decision.PolicyID)
	decision.PolicyVersion = strings.TrimSpace(decision.PolicyVersion)
	decision.PolicyHash = strings.TrimSpace(decision.PolicyHash)
	decision.HarnessRunIDs = uniqueStrings(decision.HarnessRunIDs)
	decision.RoomConsensusID = strings.TrimSpace(decision.RoomConsensusID)
	decision.Reason = strings.TrimSpace(decision.Reason)
	decision.EvidenceRefs = uniqueEvidenceRefs(decision.EvidenceRefs)
	if decision.Constraints == nil {
		decision.Constraints = map[string]any{}
	}
	return decision, nil
}

func normalizeApplyResult(result ApplyResult, proposal ControlProposal, decision CoordinatorDecision) (ApplyResult, error) {
	result.ProposalID = strings.TrimSpace(result.ProposalID)
	if result.ProposalID == "" {
		return ApplyResult{}, fmt.Errorf("proposal_id is required")
	}
	result.DecisionID = strings.TrimSpace(result.DecisionID)
	if result.DecisionID == "" {
		return ApplyResult{}, fmt.Errorf("decision_id is required")
	}
	result.IdempotencyKey = strings.ToLower(strings.TrimSpace(result.IdempotencyKey))
	if result.IdempotencyKey == "" {
		return ApplyResult{}, fmt.Errorf("idempotency_key is required")
	}
	result.TargetKind = strings.TrimSpace(result.TargetKind)
	if result.TargetKind == "" {
		return ApplyResult{}, fmt.Errorf("target_kind is required")
	}
	if result.Status == "" {
		result.Status = ApplyResultStatusApplied
	}
	if !result.Status.IsValid() {
		return ApplyResult{}, fmt.Errorf("invalid apply result status %q", result.Status)
	}
	if result.ID == "" {
		result.ID = buildRecordID("AR", timeutil.NowUTC())
	}
	if result.CreatedAt.IsZero() {
		result.CreatedAt = timeutil.NowUTC()
	}
	result.TargetID = strings.TrimSpace(result.TargetID)
	result.Summary = strings.TrimSpace(result.Summary)
	result.ErrorMessage = strings.TrimSpace(result.ErrorMessage)
	result.EvidenceRefs = uniqueEvidenceRefs(result.EvidenceRefs)
	if result.Result == nil {
		result.Result = map[string]any{}
	}
	if proposal.ID != decision.ProposalID {
		return ApplyResult{}, fmt.Errorf("decision %s does not belong to proposal %s", decision.ID, proposal.ID)
	}
	return result, nil
}

func buildControlProposalState(ctx context.Context, db *sql.DB, proposal ControlProposal) (ControlProposalState, error) {
	decisions, err := listCoordinatorDecisionRows(ctx, db, proposal.ID, 1)
	if err != nil {
		return ControlProposalState{}, err
	}
	applies, err := listApplyResultRows(ctx, db, proposal.ID, 1)
	if err != nil {
		return ControlProposalState{}, err
	}
	state := ControlProposalState{Proposal: proposal, DerivedStatus: proposal.Status}
	if len(decisions) > 0 {
		state.LatestDecision = &decisions[0]
		state.DerivedStatus = decisions[0].StatusAfter
	}
	if len(applies) > 0 {
		state.LatestApplyResult = &applies[0]
		switch applies[0].Status {
		case ApplyResultStatusApplied:
			state.DerivedStatus = ProposalStatusApplied
		case ApplyResultStatusFailed:
			state.DerivedStatus = ProposalStatusFailed
		}
	}
	if state.DerivedStatus == "" {
		state.DerivedStatus = ProposalStatusOpen
	}
	return state, nil
}

func findApprovedDecisionForApply(ctx context.Context, db *sql.DB, proposal ControlProposal, decisionID string) (CoordinatorDecision, error) {
	decisionID = strings.TrimSpace(decisionID)
	if decisionID == "" {
		return CoordinatorDecision{}, fmt.Errorf("decision_id is required")
	}
	decisions, err := listCoordinatorDecisionRows(ctx, db, proposal.ID, 0)
	if err != nil {
		return CoordinatorDecision{}, err
	}
	if len(decisions) == 0 {
		return CoordinatorDecision{}, fmt.Errorf("proposal %s has no approving decision", proposal.ID)
	}
	latest := decisions[0]
	if latest.ID != decisionID {
		return CoordinatorDecision{}, fmt.Errorf("decision %s is not the latest decision for proposal %s", decisionID, proposal.ID)
	}
	for _, decision := range decisions {
		if decision.ID != decisionID {
			continue
		}
		if decision.Decision != DecisionKindApprove || decision.StatusAfter != ProposalStatusApproved {
			return CoordinatorDecision{}, fmt.Errorf("decision %s does not approve proposal %s", decision.ID, proposal.ID)
		}
		return decision, nil
	}
	return CoordinatorDecision{}, fmt.Errorf("decision %s not found for proposal %s", decisionID, proposal.ID)
}

func effectiveControlProposalKey(proposal ControlProposal) string {
	return strings.ToLower(strings.TrimSpace(proposal.DedupeKey))
}

func statusAfterDecision(decision DecisionKind) ProposalStatus {
	switch decision {
	case DecisionKindApprove:
		return ProposalStatusApproved
	case DecisionKindReject:
		return ProposalStatusRejected
	case DecisionKindNeedsClarification:
		return ProposalStatusNeedsClarification
	case DecisionKindRequestHarness:
		return ProposalStatusNeedsHarness
	case DecisionKindEscalate:
		return ProposalStatusNeedsAuthority
	case DecisionKindDefer:
		return ProposalStatusEvaluating
	default:
		return ProposalStatusOpen
	}
}
