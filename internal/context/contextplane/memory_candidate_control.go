package contextplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/storage"
)

// RecordMemoryCandidateProposal stores a typed memory-candidate control proposal.
func (s *WorkspaceStore) RecordMemoryCandidateProposal(ctx context.Context, input MemoryCandidateInput) (ControlProposal, error) {
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if workspaceID == "" {
		return ControlProposal{}, fmt.Errorf("workspace_id is required")
	}
	payload, err := normalizeMemoryCandidatePayload(MemoryCandidatePayload{
		Name:           input.Name,
		Kind:           input.Kind,
		Summary:        input.Summary,
		Content:        input.Content,
		ResultArtifact: input.ResultArtifact,
		FileRefs:       input.FileRefs,
		TemporalScope:  input.TemporalScope,
		SourceRefs:     input.SourceRefs,
		EvidenceRefs:   input.EvidenceRefs,
		EvidenceOnly:   input.EvidenceOnly,
	}, true)
	if err != nil {
		return ControlProposal{}, err
	}
	payloadMap, err := memoryCandidatePayloadToMap(payload)
	if err != nil {
		return ControlProposal{}, err
	}
	reviewRequired := true
	if input.ReviewRequired != nil {
		reviewRequired = *input.ReviewRequired
	}
	proposal := ControlProposal{
		DedupeKey:      memoryCandidateProposalKey(workspaceID, payload),
		Kind:           ProposalKindMemoryCandidate,
		WorkspaceID:    workspaceID,
		SessionID:      strings.TrimSpace(input.SessionID),
		AgentID:        strings.TrimSpace(input.AgentID),
		RoomID:         strings.TrimSpace(input.RoomID),
		Summary:        payload.Summary,
		SourceRefs:     payload.SourceRefs,
		EvidenceRefs:   payload.EvidenceRefs,
		Payload:        payloadMap,
		Confidence:     firstNonZeroFloat64(input.Confidence, 0.65),
		BlastRadius:    firstNonEmpty(strings.TrimSpace(input.BlastRadius), "medium"),
		ReviewRequired: reviewRequired,
	}
	return s.RecordControlProposal(ctx, proposal)
}

// ApplyMemoryCandidate materializes one approved memory_candidate proposal into named memory.
//
// [[invariant:generated-memory-starts-evidence-only]]
// [[test:internal/context/contextplane/control_plane_test.go#TestApplyMemoryCandidateAppliesOneNamedMemoryIdempotently]]
func (s *WorkspaceStore) ApplyMemoryCandidate(ctx context.Context, memStore storage.MemoryStore, proposalID, decisionID string) (ApplyResult, storage.NamedEntry, error) {
	if memStore == nil {
		return ApplyResult{}, storage.NamedEntry{}, fmt.Errorf("memory store is required")
	}
	proposalID = strings.TrimSpace(proposalID)
	if proposalID == "" {
		return ApplyResult{}, storage.NamedEntry{}, fmt.Errorf("proposal_id is required")
	}
	decisionID = strings.TrimSpace(decisionID)
	if decisionID == "" {
		return ApplyResult{}, storage.NamedEntry{}, fmt.Errorf("decision_id is required")
	}

	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	proposal, err := findControlProposalRow(ctx, db, proposalID)
	if err != nil {
		_ = closeFn()
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	if proposal == nil {
		_ = closeFn()
		return ApplyResult{}, storage.NamedEntry{}, fmt.Errorf("control proposal %s not found", proposalID)
	}
	if proposal.Kind != ProposalKindMemoryCandidate {
		_ = closeFn()
		return ApplyResult{}, storage.NamedEntry{}, fmt.Errorf("proposal %s is not a memory candidate", proposalID)
	}
	decision, err := findApprovedDecisionForApply(ctx, db, *proposal, decisionID)
	if err != nil {
		_ = closeFn()
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	idempotencyKey := memoryCandidateApplyKey(proposal.ID, decision.ID)
	existingApply, err := findApplyResultRowByIdempotencyKey(ctx, db, idempotencyKey)
	if err != nil {
		_ = closeFn()
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	if err := closeFn(); err != nil {
		return ApplyResult{}, storage.NamedEntry{}, err
	}

	payload, err := decodeMemoryCandidatePayload(proposal.Payload)
	if err != nil {
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	if existingApply != nil {
		entry, err := memStore.Get(ctx, existingApply.TargetID, proposal.WorkspaceID)
		if err != nil {
			return ApplyResult{}, storage.NamedEntry{}, err
		}
		return *existingApply, entry, nil
	}
	resultJSON, err := json.Marshal(map[string]any{
		"name":                 payload.Name,
		"kind":                 payload.Kind,
		"summary":              payload.Summary,
		"content":              payload.Content,
		"requested_name":       payload.Name,
		"result_artifact":      payload.ResultArtifact,
		"temporal_scope":       payload.TemporalScope,
		"file_refs":            evidenceRefsToStrings(payload.FileRefs),
		"source_refs":          evidenceRefsToStrings(payload.SourceRefs),
		"evidence_refs":        evidenceRefsToStrings(payload.EvidenceRefs),
		"instruction_eligible": false,
		"evidence_only":        true,
		"proposal_id":          proposal.ID,
		"decision_id":          decision.ID,
	})
	if err != nil {
		return ApplyResult{}, storage.NamedEntry{}, fmt.Errorf("encode memory candidate result: %w", err)
	}

	reviewStatus := "unreviewed"
	if proposal.ReviewRequired {
		reviewStatus = "needs_review"
	}
	candidateName := memoryCandidateNamedMemoryName(proposal.ID, payload.Name)
	entry, err := memStore.Save(ctx, storage.NamedEntry{
		ID:             memoryCandidateNamedMemoryID(proposal.ID),
		Name:           candidateName,
		Type:           payload.Kind,
		Workspace:      proposal.WorkspaceID,
		Summary:        payload.Summary,
		Result:         resultJSON,
		SessionID:      proposal.SessionID,
		LifecycleState: "candidate",
		ReviewStatus:   reviewStatus,
	})
	if err != nil {
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	entry, err = memStore.UpdateLifecycle(ctx, entry.Name, entry.Workspace, storage.MemoryLifecycleUpdate{
		LifecycleState: "candidate",
		ReviewStatus:   reviewStatus,
	})
	if err != nil {
		return ApplyResult{}, storage.NamedEntry{}, err
	}

	apply, err := s.RecordApplyResult(ctx, ApplyResult{
		ProposalID:     proposal.ID,
		DecisionID:     decision.ID,
		IdempotencyKey: idempotencyKey,
		TargetKind:     "named_memory",
		TargetID:       entry.Name,
		Status:         ApplyResultStatusApplied,
		Summary:        fmt.Sprintf("Applied memory candidate %s as named memory %s", proposal.ID, entry.Name),
		Result: map[string]any{
			"name":            entry.Name,
			"id":              entry.ID,
			"workspace_id":    entry.Workspace,
			"lifecycle_state": entry.LifecycleState,
			"review_status":   entry.ReviewStatus,
		},
		EvidenceRefs: uniqueEvidenceRefs(append(append([]contextengine.EvidenceRef{}, proposal.EvidenceRefs...), payload.EvidenceRefs...)),
	})
	if err != nil {
		return ApplyResult{}, storage.NamedEntry{}, err
	}
	return apply, entry, nil
}

func normalizeMemoryCandidatePayload(payload MemoryCandidatePayload, requireRefs bool) (MemoryCandidatePayload, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	if payload.Name == "" {
		return MemoryCandidatePayload{}, fmt.Errorf("name is required")
	}
	payload.Kind = strings.TrimSpace(payload.Kind)
	if payload.Kind == "" {
		return MemoryCandidatePayload{}, fmt.Errorf("kind is required")
	}
	if !memorycore.Kind(payload.Kind).IsValid() {
		return MemoryCandidatePayload{}, fmt.Errorf("invalid memory candidate kind %q", payload.Kind)
	}
	payload.Summary = strings.TrimSpace(payload.Summary)
	if payload.Summary == "" {
		return MemoryCandidatePayload{}, fmt.Errorf("summary is required")
	}
	payload.Content = strings.TrimSpace(payload.Content)
	payload.ResultArtifact = strings.TrimSpace(payload.ResultArtifact)
	payload.TemporalScope = firstNonEmpty(strings.TrimSpace(payload.TemporalScope), "unknown")
	payload.FileRefs = uniqueEvidenceRefs(payload.FileRefs)
	payload.SourceRefs = uniqueEvidenceRefs(payload.SourceRefs)
	payload.EvidenceRefs = uniqueEvidenceRefs(payload.EvidenceRefs)
	if err := validateControlEvidenceRefs("file_refs", payload.FileRefs, false); err != nil {
		return MemoryCandidatePayload{}, err
	}
	if err := validateControlEvidenceRefs("source_refs", payload.SourceRefs, requireRefs); err != nil {
		return MemoryCandidatePayload{}, err
	}
	if err := validateControlEvidenceRefs("evidence_refs", payload.EvidenceRefs, requireRefs); err != nil {
		return MemoryCandidatePayload{}, err
	}
	payload.InstructionEligible = false
	payload.EvidenceOnly = true
	return payload, nil
}

func memoryCandidatePayloadToMap(payload MemoryCandidatePayload) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode memory candidate payload: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode memory candidate payload map: %w", err)
	}
	return out, nil
}

func decodeMemoryCandidatePayload(payload map[string]any) (MemoryCandidatePayload, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return MemoryCandidatePayload{}, fmt.Errorf("encode memory candidate payload: %w", err)
	}
	var out MemoryCandidatePayload
	if err := json.Unmarshal(body, &out); err != nil {
		return MemoryCandidatePayload{}, fmt.Errorf("decode memory candidate payload: %w", err)
	}
	return normalizeMemoryCandidatePayload(out, true)
}

func memoryCandidateProposalKey(workspaceID string, payload MemoryCandidatePayload) string {
	keyParts := []string{
		"memory_candidate",
		strings.ToLower(strings.TrimSpace(workspaceID)),
		strings.ToLower(strings.TrimSpace(payload.Name)),
		strings.ToLower(strings.TrimSpace(payload.Kind)),
		strings.ToLower(strings.TrimSpace(payload.Summary)),
		strings.ToLower(strings.TrimSpace(payload.Content)),
		strings.ToLower(strings.TrimSpace(payload.ResultArtifact)),
		strings.Join(evidenceRefsToStrings(payload.FileRefs), "|"),
		strings.Join(evidenceRefsToStrings(payload.SourceRefs), "|"),
		strings.Join(evidenceRefsToStrings(payload.EvidenceRefs), "|"),
	}
	return strings.Join(keyParts, "|")
}

func memoryCandidateNamedMemoryID(proposalID string) string {
	return "MC-" + safeFileSlug(strings.ToLower(strings.TrimSpace(proposalID)), "candidate")
}

func memoryCandidateNamedMemoryName(proposalID, requestedName string) string {
	return "memory-candidate:" + safeFileSlug(strings.ToLower(strings.TrimSpace(proposalID)), "candidate") + ":" + safeFileSlug(requestedName, "memory")
}

func memoryCandidateApplyKey(proposalID, decisionID string) string {
	return strings.ToLower(strings.TrimSpace(fmt.Sprintf("memory-candidate:%s:%s", proposalID, decisionID)))
}
