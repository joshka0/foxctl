package contextplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

const (
	taskProposalAutoPolicyID      = "task-proposal-auto-low-risk-v1"
	taskProposalAutoApprovalActor = "system:contextplane:auto-task-proposal"
)

var taskProposalUnsafeConflictBoolKeys = []string{
	"unsafe",
	"unsafe_scope",
	"unsafe_side_effects",
	"has_unsafe_side_effects",
	"conflict",
	"conflicts",
	"has_conflicts",
	"conflicting_evidence",
	"has_conflicting_evidence",
}

var taskProposalHarnessBoolKeys = []string{
	"requires_harness",
	"needs_harness",
}

type TaskProposalControlProcessInput struct {
	TaskStore  taskstore.Store
	ProposalID string
	Limit      int
}

type TaskProposalControlProcessResult struct {
	Items []TaskProposalControlProcessItem
}

type TaskProposalControlProcessItem struct {
	Proposal         ControlProposal
	ProposalID       string
	Decision         *CoordinatorDecision
	Apply            *ApplyResult
	Task             *taskstore.Task
	DecisionRecorded bool
	ApplyRecorded    bool
}

type taskProposalPayload struct {
	Title         string `json:"title"`
	ScopePath     string `json:"scope_path"`
	WorkspaceRoot string `json:"workspace_root"`
	Intent        string `json:"intent"`
	Event         string `json:"event,omitempty"`
	ToolName      string `json:"tool_name,omitempty"`
	ToolCanonical string `json:"tool_canonical,omitempty"`
	ToolKind      string `json:"tool_kind,omitempty"`
	SourceTool    string `json:"source_tool,omitempty"`
	SourceEvent   string `json:"source_event,omitempty"`
}

type taskProposalAutoDecision struct {
	Decision        DecisionKind
	Reason          string
	NormalizedScope string
}

// ProcessControlProposals processes task_proposal states with explicit low-risk auto policy.
func (s *WorkspaceStore) ProcessControlProposals(ctx context.Context, input TaskProposalControlProcessInput) (TaskProposalControlProcessResult, error) {
	if input.TaskStore == nil {
		return TaskProposalControlProcessResult{}, fmt.Errorf("task store is required")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	if proposalID := strings.TrimSpace(input.ProposalID); proposalID != "" {
		state, err := s.GetControlProposalState(ctx, proposalID)
		if err != nil {
			return TaskProposalControlProcessResult{}, err
		}
		if state == nil {
			return TaskProposalControlProcessResult{}, fmt.Errorf("control proposal %s not found", proposalID)
		}
		item, ok, err := s.processTaskProposalState(ctx, input.TaskStore, *state)
		if err != nil {
			return TaskProposalControlProcessResult{}, err
		}
		if !ok {
			return TaskProposalControlProcessResult{}, nil
		}
		return TaskProposalControlProcessResult{Items: []TaskProposalControlProcessItem{item}}, nil
	}

	states, err := s.ListControlProposalStates(ctx, limit)
	if err != nil {
		return TaskProposalControlProcessResult{}, err
	}

	out := make([]TaskProposalControlProcessItem, 0, len(states))
	for _, state := range states {
		item, ok, err := s.processTaskProposalState(ctx, input.TaskStore, state)
		if err != nil {
			return TaskProposalControlProcessResult{}, err
		}
		if ok {
			out = append(out, item)
		}
	}
	return TaskProposalControlProcessResult{Items: out}, nil
}

func (s *WorkspaceStore) processTaskProposalState(ctx context.Context, taskStore taskstore.Store, state ControlProposalState) (TaskProposalControlProcessItem, bool, error) {
	proposal := state.Proposal
	if proposal.Kind != ProposalKindTaskProposal {
		return TaskProposalControlProcessItem{}, false, nil
	}

	item := TaskProposalControlProcessItem{Proposal: proposal, ProposalID: proposal.ID}
	if state.LatestApplyResult != nil {
		item.Decision = state.LatestDecision
		item.Apply = state.LatestApplyResult
		if state.LatestApplyResult.TargetKind == "task" && strings.TrimSpace(state.LatestApplyResult.TargetID) != "" {
			task, err := taskStore.Get(ctx, state.LatestApplyResult.TargetID)
			if err == nil {
				item.Task = &task
			}
		}
		if state.LatestApplyResult.Status == ApplyResultStatusApplied {
			return item, true, nil
		}
	}
	if state.LatestDecision != nil {
		item.Decision = state.LatestDecision
		if state.LatestDecision.Decision != DecisionKindApprove {
			return item, true, nil
		}
		apply, task, err := s.ApplyTaskProposal(ctx, taskStore, proposal.ID, state.LatestDecision.ID)
		if err != nil {
			return TaskProposalControlProcessItem{}, false, err
		}
		item.Apply = &apply
		item.Task = &task
		item.ApplyRecorded = true
		return item, true, nil
	}

	autoDecision, err := evaluateTaskProposalAutoDecision(proposal)
	if err != nil {
		return TaskProposalControlProcessItem{}, false, err
	}
	decision, err := s.RecordCoordinatorDecision(ctx, CoordinatorDecision{
		ProposalID:    proposal.ID,
		WorkspaceID:   proposal.WorkspaceID,
		Decision:      autoDecision.Decision,
		AuthorityMode: AuthorityModeCoordinatorPolicy,
		ApprovalActor: taskProposalAutoApprovalActor,
		PolicyID:      taskProposalAutoPolicyID,
		Reason:        autoDecision.Reason,
		EvidenceRefs:  taskProposalDecisionEvidenceRefs(proposal),
	})
	if err != nil {
		return TaskProposalControlProcessItem{}, false, err
	}
	item.Decision = &decision
	item.DecisionRecorded = true
	if decision.Decision != DecisionKindApprove {
		return item, true, nil
	}
	apply, task, err := s.ApplyTaskProposal(ctx, taskStore, proposal.ID, decision.ID)
	if err != nil {
		return TaskProposalControlProcessItem{}, false, err
	}
	item.Apply = &apply
	item.Task = &task
	item.ApplyRecorded = true
	return item, true, nil
}

// ApplyTaskProposal materializes one approved task_proposal into exactly one pending task.
//
// [[invariant:proposal-creates-one-workitem-after-decision]]
func (s *WorkspaceStore) ApplyTaskProposal(ctx context.Context, taskStore taskstore.Store, proposalID, decisionID string) (ApplyResult, taskstore.Task, error) {
	if taskStore == nil {
		return ApplyResult{}, taskstore.Task{}, fmt.Errorf("task store is required")
	}
	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return ApplyResult{}, taskstore.Task{}, fmt.Errorf("proposal_id is required")
	}
	decisionID = strings.TrimSpace(decisionID)
	if decisionID == "" {
		return ApplyResult{}, taskstore.Task{}, fmt.Errorf("decision_id is required")
	}

	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return ApplyResult{}, taskstore.Task{}, err
	}
	proposal, err := findControlProposalRow(ctx, db, proposalID)
	if err != nil {
		_ = closeFn()
		return ApplyResult{}, taskstore.Task{}, err
	}
	if proposal == nil {
		_ = closeFn()
		return ApplyResult{}, taskstore.Task{}, fmt.Errorf("control proposal %s not found", proposalID)
	}
	if proposal.Kind != ProposalKindTaskProposal {
		_ = closeFn()
		return ApplyResult{}, taskstore.Task{}, fmt.Errorf("proposal %s is not a task proposal", proposalID)
	}
	decision, err := findApprovedDecisionForApply(ctx, db, *proposal, decisionID)
	if err != nil {
		_ = closeFn()
		return ApplyResult{}, taskstore.Task{}, err
	}
	idempotencyKey := taskProposalApplyKey(proposal.ID, decision.ID)
	existingApply, err := findApplyResultRowByIdempotencyKey(ctx, db, idempotencyKey)
	if err != nil {
		_ = closeFn()
		return ApplyResult{}, taskstore.Task{}, err
	}
	if err := closeFn(); err != nil {
		return ApplyResult{}, taskstore.Task{}, err
	}
	if existingApply != nil {
		task, err := taskStore.Get(ctx, existingApply.TargetID)
		if err != nil {
			return ApplyResult{}, taskstore.Task{}, err
		}
		return *existingApply, task, nil
	}

	payload, err := decodeTaskProposalPayload(proposal.Payload)
	if err != nil {
		return ApplyResult{}, taskstore.Task{}, err
	}
	normalizedScope, err := normalizeTaskProposalScope(payload.ScopePath, payload.WorkspaceRoot)
	if err != nil {
		return ApplyResult{}, taskstore.Task{}, err
	}
	taskID := taskProposalTaskID(proposal.ID, decision.ID)
	task, err := addTaskWithStableID(ctx, taskStore, taskstore.Task{
		ID:          taskID,
		WorkspaceID: proposal.WorkspaceID,
		Title:       payload.Title,
		Description: taskProposalDescription(*proposal, payload),
		ScopePath:   normalizedScope,
		Status:      taskstore.StatusPending,
		SessionID:   proposal.SessionID,
	})
	if err != nil {
		return ApplyResult{}, taskstore.Task{}, err
	}

	apply, err := s.RecordApplyResult(ctx, ApplyResult{
		ProposalID:     proposal.ID,
		DecisionID:     decision.ID,
		IdempotencyKey: idempotencyKey,
		TargetKind:     "task",
		TargetID:       task.ID,
		Status:         ApplyResultStatusApplied,
		Summary:        fmt.Sprintf("Applied task proposal %s as task %s", proposal.ID, task.ID),
		Result: map[string]any{
			"task_id":         task.ID,
			"title":           task.Title,
			"scope_path":      task.ScopePath,
			"workspace_id":    task.WorkspaceID,
			"proposal_id":     proposal.ID,
			"decision_id":     decision.ID,
			"source_event":    payload.SourceEvent,
			"source_tool":     firstNonEmpty(payload.SourceTool, payload.ToolCanonical, payload.ToolName),
			"proposal_intent": payload.Intent,
		},
		EvidenceRefs: uniqueEvidenceRefs(append(append([]contextengine.EvidenceRef{}, proposal.EvidenceRefs...), proposal.SourceRefs...)),
	})
	if err != nil {
		return ApplyResult{}, taskstore.Task{}, err
	}
	if strings.TrimSpace(apply.TargetID) != "" && apply.TargetID != task.ID {
		task, err = taskStore.Get(ctx, apply.TargetID)
		if err != nil {
			return ApplyResult{}, taskstore.Task{}, err
		}
	}
	return apply, task, nil
}

func addTaskWithStableID(ctx context.Context, store taskstore.Store, task taskstore.Task) (taskstore.Task, error) {
	added, err := store.Add(ctx, task)
	if err == nil {
		return added, nil
	}
	existing, getErr := store.Get(ctx, task.ID)
	if getErr == nil {
		return existing, nil
	}
	return taskstore.Task{}, err
}

func evaluateTaskProposalAutoDecision(proposal ControlProposal) (taskProposalAutoDecision, error) {
	if strings.TrimSpace(proposal.WorkspaceID) == "" {
		return taskProposalAutoDecision{
			Decision: DecisionKindNeedsClarification,
			Reason:   "workspace_id is required for task proposal auto policy",
		}, nil
	}
	if len(proposal.EvidenceRefs) == 0 {
		return taskProposalAutoDecision{
			Decision: DecisionKindNeedsClarification,
			Reason:   "proposal evidence_refs are required for auto approval",
		}, nil
	}

	payload, err := decodeTaskProposalPayload(proposal.Payload)
	if err != nil {
		return taskProposalAutoDecision{
			Decision: DecisionKindNeedsClarification,
			Reason:   fmt.Sprintf("task proposal payload requires clarification: %v", err),
		}, nil
	}
	normalizedScope, err := normalizeTaskProposalScope(payload.ScopePath, payload.WorkspaceRoot)
	if err != nil {
		return taskProposalAutoDecision{
			Decision: DecisionKindNeedsClarification,
			Reason:   fmt.Sprintf("task proposal scope_path requires clarification: %v", err),
		}, nil
	}
	risk, err := readTaskProposalRiskFlags(proposal.Payload)
	if err != nil {
		return taskProposalAutoDecision{
			Decision: DecisionKindNeedsClarification,
			Reason:   fmt.Sprintf("task proposal payload requires clarification: %v", err),
		}, nil
	}
	needsHarness, err := readTaskProposalBoolFlags(proposal.Payload, taskProposalHarnessBoolKeys)
	if err != nil {
		return taskProposalAutoDecision{
			Decision: DecisionKindNeedsClarification,
			Reason:   fmt.Sprintf("task proposal payload requires clarification: %v", err),
		}, nil
	}
	switch proposal.Status {
	case ProposalStatusUnsafeSideEffects, ProposalStatusConflictingEvidence:
		return taskProposalAutoDecision{
			Decision:        DecisionKindEscalate,
			Reason:          fmt.Sprintf("proposal status %q requires authority review", proposal.Status),
			NormalizedScope: normalizedScope,
		}, nil
	case ProposalStatusNeedsHarness:
		return taskProposalAutoDecision{
			Decision:        DecisionKindRequestHarness,
			Reason:          "proposal status requires harness evidence",
			NormalizedScope: normalizedScope,
		}, nil
	}
	if risk {
		return taskProposalAutoDecision{
			Decision:        DecisionKindEscalate,
			Reason:          "task proposal contains explicit unsafe/conflict signals",
			NormalizedScope: normalizedScope,
		}, nil
	}
	if needsHarness {
		return taskProposalAutoDecision{
			Decision:        DecisionKindRequestHarness,
			Reason:          "task proposal requires harness evidence",
			NormalizedScope: normalizedScope,
		}, nil
	}

	blast := strings.ToLower(strings.TrimSpace(proposal.BlastRadius))
	if blast != "low" {
		return taskProposalAutoDecision{
			Decision:        DecisionKindEscalate,
			Reason:          fmt.Sprintf("blast_radius=%q requires authority review", firstNonEmpty(blast, "medium")),
			NormalizedScope: normalizedScope,
		}, nil
	}
	return taskProposalAutoDecision{
		Decision:        DecisionKindApprove,
		Reason:          "auto-approved low-risk task proposal",
		NormalizedScope: normalizedScope,
	}, nil
}

func decodeTaskProposalPayload(payload map[string]any) (taskProposalPayload, error) {
	if payload == nil {
		return taskProposalPayload{}, fmt.Errorf("payload is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return taskProposalPayload{}, fmt.Errorf("encode task proposal payload: %w", err)
	}
	var out taskProposalPayload
	if err := json.Unmarshal(body, &out); err != nil {
		return taskProposalPayload{}, fmt.Errorf("decode task proposal payload: %w", err)
	}
	out.Title = strings.TrimSpace(out.Title)
	out.ScopePath = strings.TrimSpace(out.ScopePath)
	out.WorkspaceRoot = strings.TrimSpace(out.WorkspaceRoot)
	out.Intent = strings.TrimSpace(out.Intent)
	out.Event = strings.TrimSpace(out.Event)
	out.ToolName = strings.TrimSpace(out.ToolName)
	out.ToolCanonical = strings.TrimSpace(out.ToolCanonical)
	out.ToolKind = strings.TrimSpace(out.ToolKind)
	out.SourceTool = strings.TrimSpace(out.SourceTool)
	out.SourceEvent = strings.TrimSpace(out.SourceEvent)

	if out.Title == "" {
		return taskProposalPayload{}, fmt.Errorf("payload.title is required")
	}
	if out.ScopePath == "" {
		return taskProposalPayload{}, fmt.Errorf("payload.scope_path is required")
	}
	if out.WorkspaceRoot == "" {
		return taskProposalPayload{}, fmt.Errorf("payload.workspace_root is required")
	}
	if out.Intent == "" {
		return taskProposalPayload{}, fmt.Errorf("payload.intent is required")
	}
	return out, nil
}

func normalizeTaskProposalScope(scopePath, workspaceRoot string) (string, error) {
	scopePath = strings.TrimSpace(scopePath)
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if scopePath == "" {
		return "", fmt.Errorf("scope_path is required")
	}
	if workspaceRoot == "" {
		return "", fmt.Errorf("workspace_root is required")
	}
	if !filepath.IsAbs(workspaceRoot) {
		return "", fmt.Errorf("workspace_root must be absolute")
	}
	workspaceRoot = filepath.Clean(workspaceRoot)
	scopeAbs := scopePath
	if !filepath.IsAbs(scopeAbs) {
		scopeAbs = filepath.Join(workspaceRoot, scopeAbs)
	}
	scopeAbs = filepath.Clean(scopeAbs)
	rel, err := filepath.Rel(workspaceRoot, scopeAbs)
	if err != nil {
		return "", fmt.Errorf("scope_path relation failed: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("scope_path is outside workspace")
	}
	return filepath.ToSlash(rel), nil
}

func readTaskProposalRiskFlags(payload map[string]any) (bool, error) {
	return readTaskProposalBoolFlags(payload, taskProposalUnsafeConflictBoolKeys)
}

func readTaskProposalBoolFlags(payload map[string]any, keys []string) (bool, error) {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		flag, isBool := value.(bool)
		if !isBool {
			return false, fmt.Errorf("payload.%s must be boolean when present", key)
		}
		if flag {
			return true, nil
		}
	}
	return false, nil
}

func taskProposalDecisionEvidenceRefs(proposal ControlProposal) []contextengine.EvidenceRef {
	refs := uniqueEvidenceRefs(append(append([]contextengine.EvidenceRef{}, proposal.EvidenceRefs...), proposal.SourceRefs...))
	if len(refs) > 0 {
		return refs
	}
	return []contextengine.EvidenceRef{
		{Type: contextengine.RefTypeEvent, Ref: "control_proposal:" + proposal.ID, WorkspaceID: proposal.WorkspaceID},
	}
}

func taskProposalApplyKey(proposalID, decisionID string) string {
	return "task_proposal_apply:" + stableTaskProposalDigest("apply", proposalID, decisionID)
}

func taskProposalTaskID(proposalID, decisionID string) string {
	return "TP-" + stableTaskProposalDigest("task", proposalID, decisionID)
}

func stableTaskProposalDigest(namespace, proposalID, decisionID string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(namespace) + "|" + strings.TrimSpace(proposalID) + "|" + strings.TrimSpace(decisionID))))
	return hex.EncodeToString(sum[:8])
}

func taskProposalDescription(proposal ControlProposal, payload taskProposalPayload) string {
	parts := make([]string, 0, 3)
	if payload.Intent != "" {
		parts = append(parts, payload.Intent)
	}
	sourceTool := firstNonEmpty(payload.SourceTool, payload.ToolCanonical, payload.ToolName)
	sourceEvent := firstNonEmpty(payload.SourceEvent, payload.Event)
	if sourceTool != "" || sourceEvent != "" {
		parts = append(parts, fmt.Sprintf("source: %s %s", strings.TrimSpace(sourceEvent), strings.TrimSpace(sourceTool)))
	}
	if summary := strings.TrimSpace(proposal.Summary); summary != "" {
		parts = append(parts, "proposal: "+summary)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
