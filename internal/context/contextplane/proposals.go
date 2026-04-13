package contextplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
)

func (s *WorkspaceStore) RecordMemoryProposal(ctx context.Context, proposal MemoryProposal) (MemoryProposal, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return MemoryProposal{}, err
	}
	defer func() { _ = closeFn() }()
	if strings.TrimSpace(proposal.Status) == "" {
		proposal.Status = "open"
	}
	if strings.TrimSpace(proposal.EvaluationStatus) == "" {
		proposal.EvaluationStatus = "not_evaluated"
	}
	if strings.TrimSpace(proposal.ApplyStatus) == "" {
		proposal.ApplyStatus = "pending"
	}
	now := timeutil.NowUTC()
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = now
	}
	if proposal.UpdatedAt.IsZero() {
		proposal.UpdatedAt = proposal.CreatedAt
	}
	if proposal.Count <= 0 {
		proposal.Count = 1
	}
	if err := upsertMemoryProposalRow(ctx, db, proposal); err != nil {
		return MemoryProposal{}, fmt.Errorf("record memory proposal: %w", err)
	}
	stored, err := findMemoryProposalRowByKey(ctx, db, effectiveMemoryProposalKey(proposal))
	if err != nil {
		return MemoryProposal{}, err
	}
	if stored == nil {
		return MemoryProposal{}, fmt.Errorf("proposal persisted but could not be reloaded")
	}
	return *stored, nil
}

func (s *WorkspaceStore) ListMemoryProposals(ctx context.Context, limit int) ([]MemoryProposal, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return listMemoryProposalRows(ctx, db, limit)
}

func (s *WorkspaceStore) GetMemoryProposal(ctx context.Context, id string) (*MemoryProposal, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	return findMemoryProposalRow(ctx, db, id)
}

func (s *WorkspaceStore) ApplyMemoryProposal(ctx context.Context, id string) (*MemoryProposal, map[string]any, ProposalWorkPacket, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, nil, ProposalWorkPacket{}, err
	}
	defer func() { _ = closeFn() }()
	proposal, err := findMemoryProposalRow(ctx, db, id)
	if err != nil {
		return nil, nil, ProposalWorkPacket{}, err
	}
	if proposal == nil {
		return nil, nil, ProposalWorkPacket{}, fmt.Errorf("no proposal found for %s", strings.TrimSpace(id))
	}
	if strings.EqualFold(strings.TrimSpace(proposal.Status), "rejected") {
		return nil, nil, ProposalWorkPacket{}, fmt.Errorf("proposal %s is rejected", proposal.ID)
	}

	var result map[string]any
	switch strings.TrimSpace(proposal.Kind) {
	case "retrieval_policy_patch":
		policyPath, err := s.SetRetrievalPackageNoteFallback(true)
		if err != nil {
			return nil, nil, ProposalWorkPacket{}, fmt.Errorf("apply retrieval policy patch: %w", err)
		}
		result = map[string]any{
			"policy_path": policyPath,
		}
	case "external_evidence_import", "methodology_draft":
		draftPath := firstNonEmpty(changeString(proposal.ProposedChange, "draft_path"))
		targetPath := firstNonEmpty(changeString(proposal.ProposedChange, "suggested_target_note_path"))
		heading := firstNonEmpty(changeString(proposal.ProposedChange, "suggested_target_heading"), "Review")
		if strings.TrimSpace(draftPath) == "" {
			return nil, nil, ProposalWorkPacket{}, fmt.Errorf("proposal %s has no draft_path", proposal.ID)
		}
		sourceRef := "draft:" + draftPath
		if importID := changeString(proposal.ProposedChange, "evidence_import_id"); importID != "" {
			sourceRef = "evidence_import:" + importID
		}
		sourceKind := "evidence_import"
		noteType := "evidence"
		if strings.TrimSpace(proposal.Kind) == "methodology_draft" {
			noteType = "pattern"
		}
		job, err := ensurePromotionJobRow(ctx, db, sourceRef, sourceKind, noteType, changeString(proposal.ProposedChange, "title"), draftPath)
		if err != nil {
			return nil, nil, ProposalWorkPacket{}, fmt.Errorf("prepare review merge job: %w", err)
		}
		result = map[string]any{
			"promotion_job": job,
			"draft_path":    draftPath,
			"target_path":   targetPath,
			"heading":       heading,
			"next_action":   "context proposal merge",
		}
	default:
		return nil, nil, ProposalWorkPacket{}, fmt.Errorf("proposal kind %s is not auto-applicable in phase 1", proposal.Kind)
	}

	nextStatus := "applied"
	nextApplyStatus := "applied"
	if proposal.Kind == "external_evidence_import" || proposal.Kind == "methodology_draft" {
		nextStatus = "prepared"
		nextApplyStatus = "review_prepared"
	}
	if err := updateMemoryProposalRowStatus(ctx, db, proposal.ID, nextStatus, "accepted", nextApplyStatus); err != nil {
		return nil, nil, ProposalWorkPacket{}, fmt.Errorf("mark proposal applied: %w", err)
	}
	updated, err := findMemoryProposalRow(ctx, db, proposal.ID)
	if err != nil {
		return nil, nil, ProposalWorkPacket{}, err
	}
	if updated == nil {
		return nil, nil, ProposalWorkPacket{}, fmt.Errorf("proposal %s disappeared after apply", proposal.ID)
	}
	return updated, result, buildApplyProposalWorkPacket(updated, result), nil
}

func (s *WorkspaceStore) RejectMemoryProposal(ctx context.Context, id string) (*MemoryProposal, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	proposal, err := findMemoryProposalRow(ctx, db, id)
	if err != nil {
		return nil, err
	}
	if proposal == nil {
		return nil, fmt.Errorf("no proposal found for %s", strings.TrimSpace(id))
	}
	if err := updateMemoryProposalRowStatus(ctx, db, proposal.ID, "rejected", proposal.EvaluationStatus, "rejected"); err != nil {
		return nil, fmt.Errorf("mark proposal rejected: %w", err)
	}
	return findMemoryProposalRow(ctx, db, proposal.ID)
}

func (s *WorkspaceStore) MergeMemoryProposal(ctx context.Context, vaultName, vaultPath, id, draftPathOverride, targetPathOverride, headingOverride string) (*MemoryProposal, PromotionMergeResult, ProposalWorkPacket, error) {
	proposal, err := s.GetMemoryProposal(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, err
	}
	if proposal == nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("no proposal found for %s", strings.TrimSpace(id))
	}
	if !strings.EqualFold(strings.TrimSpace(proposal.Status), "prepared") {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("proposal %s is not prepared for merge", proposal.ID)
	}
	switch strings.TrimSpace(proposal.ApplyStatus) {
	case "review_prepared", "review_claimed":
	default:
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("proposal %s is not prepared for merge", proposal.ID)
	}
	switch strings.TrimSpace(proposal.Kind) {
	case "external_evidence_import", "methodology_draft":
	default:
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("proposal kind %s does not support direct merge", proposal.Kind)
	}

	draftPath := firstNonEmpty(strings.TrimSpace(draftPathOverride), changeString(proposal.ProposedChange, "draft_path"))
	targetPath := firstNonEmpty(strings.TrimSpace(targetPathOverride), changeString(proposal.ProposedChange, "suggested_target_note_path"))
	heading := firstNonEmpty(strings.TrimSpace(headingOverride), changeString(proposal.ProposedChange, "suggested_target_heading"), "Review")
	if draftPath == "" {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("proposal %s has no draft_path", proposal.ID)
	}
	if targetPath == "" {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("proposal %s has no target path", proposal.ID)
	}

	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, err
	}
	sourceRef := "draft:" + draftPath
	if importID := changeString(proposal.ProposedChange, "evidence_import_id"); importID != "" {
		sourceRef = "evidence_import:" + importID
	}
	sourceKind := "evidence_import"
	noteType := "evidence"
	if proposal.Kind == "methodology_draft" {
		noteType = "pattern"
	}
	if _, err := ensurePromotionJobRow(ctx, db, sourceRef, sourceKind, noteType, changeString(proposal.ProposedChange, "title"), draftPath); err != nil {
		_ = closeFn()
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("prepare review merge job: %w", err)
	}
	if err := closeFn(); err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, err
	}

	merge, err := s.MergePromotionDraft(ctx, vaultName, vaultPath, draftPath, targetPath, heading)
	if err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, err
	}

	db, closeFn, err = s.openMutableDB(ctx)
	if err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, err
	}
	defer func() { _ = closeFn() }()
	if err := updateMemoryProposalRowStatus(ctx, db, proposal.ID, "merged", "accepted", "reviewed_merged"); err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("mark proposal merged: %w", err)
	}
	updated, err := findMemoryProposalRow(ctx, db, proposal.ID)
	if err != nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, err
	}
	if updated == nil {
		return nil, PromotionMergeResult{}, ProposalWorkPacket{}, fmt.Errorf("proposal %s disappeared after merge", proposal.ID)
	}
	return updated, merge, buildMergeProposalWorkPacket(updated, merge, vaultPath), nil
}

func (s *WorkspaceStore) ClaimNextProposalMergeTask(ctx context.Context, limit int) (*MaintenanceTask, error) {
	task, err := s.NextProposalMergeTask(ctx, limit)
	if err != nil || task == nil || task.WorkPacket == nil {
		return task, err
	}
	proposalID := strings.TrimSpace(task.WorkPacket.ProposalID)
	if proposalID == "" {
		return nil, fmt.Errorf("proposal merge task %s has no proposal id", strings.TrimSpace(task.ID))
	}

	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	proposal, err := findMemoryProposalRow(ctx, db, proposalID)
	if err != nil {
		return nil, err
	}
	if proposal == nil {
		return nil, fmt.Errorf("no proposal found for %s", proposalID)
	}
	if !strings.EqualFold(strings.TrimSpace(proposal.ApplyStatus), "review_prepared") {
		return nil, fmt.Errorf("proposal %s is not available to claim", proposal.ID)
	}
	if err := updateMemoryProposalRowStatus(ctx, db, proposal.ID, proposal.Status, proposal.EvaluationStatus, "review_claimed"); err != nil {
		return nil, fmt.Errorf("mark proposal claimed: %w", err)
	}
	if err := updateMaintenanceTaskStatus(ctx, db, task.ID, "claimed"); err != nil {
		_ = updateMemoryProposalRowStatus(ctx, db, proposal.ID, proposal.Status, proposal.EvaluationStatus, "review_prepared")
		return nil, fmt.Errorf("claim task: %w", err)
	}
	claimedTask := *task
	claimedTask.Status = "claimed"
	packet := *task.WorkPacket
	packet.Status = "claimed"
	claimedTask.WorkPacket = &packet
	return &claimedTask, nil
}

func (s *WorkspaceStore) ClaimedProposalMergeTask(ctx context.Context) (*MaintenanceTask, error) {
	proposals, err := s.ListMemoryProposals(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, proposal := range proposals {
		packet, ok := buildStoredClaimedProposalWorkPacket(&proposal)
		if !ok {
			continue
		}
		task := MaintenanceTask{
			ID:       "proposal:" + proposal.ID,
			Title:    summarizeProposalMaintenanceTitle(proposal, packet),
			Kind:     "proposal_merge",
			Priority: proposalMaintenancePriority(proposal),
			Reason:   proposal.Summary,
			SourceRefs: uniqueStrings([]string{
				"proposal:" + proposal.ID,
				"draft:" + packet.DraftPath,
				"target:" + packet.TargetPath,
			}),
			WorkPacket: &packet,
			Status:     "claimed",
			CreatedAt:  proposal.UpdatedAt,
		}
		return &task, nil
	}
	return nil, nil
}

func (s *WorkspaceStore) ReleaseProposalMergeClaim(ctx context.Context, id string) (*MemoryProposal, error) {
	db, closeFn, err := s.openMutableDB(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closeFn() }()
	proposal, err := findMemoryProposalRow(ctx, db, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if proposal == nil {
		return nil, fmt.Errorf("no proposal found for %s", strings.TrimSpace(id))
	}
	if !strings.EqualFold(strings.TrimSpace(proposal.Status), "prepared") {
		return nil, fmt.Errorf("proposal %s is not in prepared state", proposal.ID)
	}
	if !strings.EqualFold(strings.TrimSpace(proposal.ApplyStatus), "review_claimed") {
		return nil, fmt.Errorf("proposal %s is not claimed", proposal.ID)
	}
	if err := updateMemoryProposalRowStatus(ctx, db, proposal.ID, proposal.Status, proposal.EvaluationStatus, "review_prepared"); err != nil {
		return nil, fmt.Errorf("release proposal claim: %w", err)
	}
	return findMemoryProposalRow(ctx, db, proposal.ID)
}

func (s *WorkspaceStore) RecordRetrievalProposal(ctx context.Context, inspection RetrievalInspection) (MemoryProposal, error) {
	return s.RecordMemoryProposal(ctx, memoryProposalFromRetrievalInspection(inspection))
}

func (s *WorkspaceStore) RecordEvidenceImportProposal(ctx context.Context, run EvidenceImportRun, extraction EvidenceExtraction, target EvidenceTargetSuggestion) (MemoryProposal, error) {
	return s.RecordMemoryProposal(ctx, memoryProposalFromEvidenceImport(run, extraction, target))
}

func memoryProposalFromRetrievalInspection(inspection RetrievalInspection) MemoryProposal {
	kind, blastRadius, reviewRequired := proposalShapeFromRetrievalAction(inspection.Proposal.Kind)
	change := map[string]any{
		"proposal_kind": inspection.Proposal.Kind,
		"summary":       inspection.Proposal.Summary,
	}
	if inspection.Proposal.PolicyPath != "" {
		change["policy_path"] = inspection.Proposal.PolicyPath
	}
	if inspection.Proposal.PolicyPatch != "" {
		change["policy_patch"] = inspection.Proposal.PolicyPatch
		change["package_note_fallback"] = strings.Contains(inspection.Proposal.PolicyPatch, "package_note_fallback: true")
	}
	if inspection.Proposal.TargetNotePath != "" {
		change["target_note_path"] = inspection.Proposal.TargetNotePath
	}
	if inspection.Proposal.NoteType != "" {
		change["note_type"] = inspection.Proposal.NoteType
	}
	if inspection.Proposal.NoteTitle != "" {
		change["note_title"] = inspection.Proposal.NoteTitle
	}
	if inspection.Proposal.MetadataPatch != "" {
		change["metadata_patch"] = inspection.Proposal.MetadataPatch
	}
	if inspection.Proposal.SupportingNote != "" {
		change["supporting_note"] = inspection.Proposal.SupportingNote
	}
	if len(inspection.Proposal.ExpectedRepoPaths) > 0 {
		change["expected_repo_paths"] = append([]string(nil), inspection.Proposal.ExpectedRepoPaths...)
	}
	if inspection.Query != "" {
		change["query"] = inspection.Query
	}
	if len(inspection.ExpectedPaths) > 0 {
		change["expected_paths"] = append([]string(nil), inspection.ExpectedPaths...)
	}
	if len(inspection.RetrievedPaths) > 0 {
		change["retrieved_paths"] = append([]string(nil), inspection.RetrievedPaths...)
	}
	return MemoryProposal{
		Kind:             kind,
		Classification:   inspection.Classification,
		Status:           "open",
		ReviewRequired:   reviewRequired,
		Confidence:       inspection.Observation.Confidence,
		BlastRadius:      blastRadius,
		Summary:          firstNonEmpty(strings.TrimSpace(inspection.Proposal.Summary), strings.TrimSpace(inspection.Observation.Statement), fmt.Sprintf("ACA proposal for %s", inspection.Classification)),
		SourceRefs:       memoryProposalSourceRefs(inspection),
		ProposedChange:   change,
		EvaluationStatus: "not_evaluated",
		ApplyStatus:      "pending",
		Count:            1,
		CreatedAt:        inspection.GeneratedAt,
		UpdatedAt:        inspection.GeneratedAt,
	}
}

func proposalShapeFromRetrievalAction(kind string) (proposalKind, blastRadius string, reviewRequired bool) {
	switch strings.TrimSpace(kind) {
	case "policy_patch":
		return "retrieval_policy_patch", "low", false
	case "metadata_patch":
		return "bridge_metadata_patch", "medium", true
	case "draft_package_note":
		return "missing_note_draft", "medium", true
	default:
		return "manual_review", "high", true
	}
}

func memoryProposalFromEvidenceImport(run EvidenceImportRun, extraction EvidenceExtraction, target EvidenceTargetSuggestion) MemoryProposal {
	proposalKind := "external_evidence_import"
	blastRadius := "medium"
	reviewRequired := true
	reviewAction := "review_and_merge"
	topicKey := normalizeEvidenceTopic(firstNonEmpty(strings.TrimSpace(run.Title), strings.TrimSpace(run.SourceRef), strings.TrimSpace(extraction.Summary)))
	if looksMethodologyEvidence(run, extraction) {
		proposalKind = "methodology_draft"
		blastRadius = "high"
		reviewAction = "draft_methodology_update"
	}
	change := map[string]any{
		"review_action":      reviewAction,
		"evidence_import_id": run.ID,
		"draft_path":         run.DraftPath,
		"source_kind":        run.SourceKind,
		"source_ref":         run.SourceRef,
		"title":              run.Title,
		"summary":            extraction.Summary,
		"claims":             append([]string(nil), extraction.Claims...),
		"frameworks":         append([]string(nil), extraction.Frameworks...),
		"action_items":       append([]string(nil), extraction.ActionItems...),
		"open_questions":     append([]string(nil), extraction.OpenQuestions...),
		"artifact_digest":    run.ArtifactDigest,
		"processor_kind":     extraction.ProcessorKind,
		"processor_model":    extraction.ProcessorModel,
	}
	if strings.TrimSpace(target.Path) != "" {
		change["suggested_target_note_path"] = target.Path
		change["suggested_target_heading"] = firstNonEmpty(strings.TrimSpace(target.Heading), "Review")
		change["suggested_target_reason"] = target.Reason
	}
	summary := fmt.Sprintf("Review imported evidence draft for merge consideration: %s.", firstNonEmpty(strings.TrimSpace(run.Title), "Imported External Evidence"))
	if proposalKind == "methodology_draft" {
		summary = fmt.Sprintf("Review imported evidence for a methodology or doctrine update: %s.", firstNonEmpty(strings.TrimSpace(run.Title), "Imported External Evidence"))
	}
	if strings.TrimSpace(target.Path) != "" {
		summary = strings.TrimSuffix(summary, ".") + fmt.Sprintf(" Suggested target: %s.", target.Path)
	}
	return MemoryProposal{
		DedupeKey:        fmt.Sprintf("%s|%s", proposalKind, topicKey),
		Kind:             proposalKind,
		Classification:   "external_evidence",
		Status:           "open",
		ReviewRequired:   reviewRequired,
		Confidence:       0.72,
		BlastRadius:      blastRadius,
		Summary:          summary,
		SourceRefs:       uniqueStrings([]string{"draft:" + run.DraftPath, "external:" + run.SourceKind + ":" + run.SourceRef, "topic:" + topicKey}),
		ProposedChange:   change,
		EvaluationStatus: "not_evaluated",
		ApplyStatus:      "pending",
		Count:            1,
		CreatedAt:        run.CreatedAt,
		UpdatedAt:        run.CreatedAt,
	}
}

func looksMethodologyEvidence(run EvidenceImportRun, extraction EvidenceExtraction) bool {
	text := strings.ToLower(strings.Join([]string{
		run.Title,
		run.Summary,
		extraction.Summary,
		strings.Join(extraction.Claims, " "),
		strings.Join(extraction.Frameworks, " "),
		strings.Join(extraction.ActionItems, " "),
	}, " "))
	keywords := []string{
		"methodology", "doctrine", "policy", "workflow", "guideline",
		"convention", "prompt", "naming", "architecture", "operating model",
		"vocabulary", "rule", "layer",
	}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func memoryProposalSourceRefs(inspection RetrievalInspection) []string {
	refs := append([]string(nil), inspection.Observation.EvidenceRefs...)
	if q := strings.TrimSpace(inspection.Query); q != "" {
		refs = append(refs, "query:"+q)
	}
	for _, item := range inspection.ExpectedPaths {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(item), ".md") {
			refs = append(refs, "note:"+item)
			continue
		}
		refs = append(refs, "path:"+item)
	}
	if candidate := strings.TrimSpace(inspection.CandidateNote); candidate != "" {
		refs = append(refs, "note:"+candidate)
	}
	return uniqueStrings(refs)
}

func memoryProposalKey(proposal MemoryProposal) string {
	changeJSON, _ := json.Marshal(proposal.ProposedChange)
	keyParts := []string{
		strings.ToLower(strings.TrimSpace(proposal.Kind)),
		strings.ToLower(strings.TrimSpace(proposal.Classification)),
		strings.ToLower(strings.TrimSpace(proposal.Summary)),
		string(changeJSON),
	}
	return strings.Join(keyParts, "|")
}

func effectiveMemoryProposalKey(proposal MemoryProposal) string {
	if strings.TrimSpace(proposal.DedupeKey) != "" {
		return strings.ToLower(strings.TrimSpace(proposal.DedupeKey))
	}
	return memoryProposalKey(proposal)
}

func changeString(change map[string]any, key string) string {
	if change == nil {
		return ""
	}
	if raw, ok := change[key]; ok {
		if text, ok := raw.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func buildApplyProposalWorkPacket(proposal *MemoryProposal, result map[string]any) ProposalWorkPacket {
	if proposal == nil {
		return ProposalWorkPacket{}
	}
	packet := ProposalWorkPacket{
		ProposalID:     proposal.ID,
		ProposalKind:   proposal.Kind,
		Status:         proposal.Status,
		ReviewRequired: proposal.ReviewRequired,
	}
	switch strings.TrimSpace(proposal.Kind) {
	case "retrieval_policy_patch":
		packet.Action = "retrieval_policy_patch"
		packet.PolicyPath = changeString(result, "policy_path")
		packet.NextCommand = ""
	case "external_evidence_import", "methodology_draft":
		packet.Action = "merge_promotion"
		packet.Status = "prepared"
		packet.DraftPath = changeString(result, "draft_path")
		packet.TargetPath = changeString(result, "target_path")
		packet.Heading = firstNonEmpty(changeString(result, "heading"), "Review")
		packet.RequiresVaultPath = true
		if job, ok := result["promotion_job"].(PromotionJob); ok {
			packet.PromotionJobID = job.ID
		}
		packet.NextCommand = "agentctl context proposal merge " + proposal.ID + " --vault-path <vault-path>"
	}
	return packet
}

func buildMergeProposalWorkPacket(proposal *MemoryProposal, merge PromotionMergeResult, vaultPath string) ProposalWorkPacket {
	if proposal == nil {
		return ProposalWorkPacket{}
	}
	return ProposalWorkPacket{
		ProposalID:     proposal.ID,
		ProposalKind:   proposal.Kind,
		Action:         "merge_promotion",
		Status:         "merged",
		ReviewRequired: proposal.ReviewRequired,
		DraftPath:      merge.DraftPath,
		TargetPath:     merge.TargetPath,
		Heading:        merge.Heading,
		PromotionJobID: merge.Job.ID,
		VaultPath:      strings.TrimSpace(vaultPath),
		NextCommand:    "",
	}
}

func buildStoredPreparedProposalWorkPacket(proposal *MemoryProposal) (ProposalWorkPacket, bool) {
	if proposal == nil {
		return ProposalWorkPacket{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(proposal.Status), "prepared") || !strings.EqualFold(strings.TrimSpace(proposal.ApplyStatus), "review_prepared") {
		return ProposalWorkPacket{}, false
	}
	switch strings.TrimSpace(proposal.Kind) {
	case "external_evidence_import", "methodology_draft":
	default:
		return ProposalWorkPacket{}, false
	}
	draftPath := changeString(proposal.ProposedChange, "draft_path")
	targetPath := changeString(proposal.ProposedChange, "suggested_target_note_path")
	if draftPath == "" || targetPath == "" {
		return ProposalWorkPacket{}, false
	}
	packet := ProposalWorkPacket{
		ProposalID:        proposal.ID,
		ProposalKind:      proposal.Kind,
		Action:            "merge_promotion",
		Status:            "prepared",
		ReviewRequired:    proposal.ReviewRequired,
		DraftPath:         draftPath,
		TargetPath:        targetPath,
		Heading:           firstNonEmpty(changeString(proposal.ProposedChange, "suggested_target_heading"), "Review"),
		RequiresVaultPath: true,
		NextCommand:       "agentctl context proposal merge " + proposal.ID + " --vault-path <vault-path>",
	}
	return packet, true
}

func buildStoredClaimedProposalWorkPacket(proposal *MemoryProposal) (ProposalWorkPacket, bool) {
	if proposal == nil {
		return ProposalWorkPacket{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(proposal.Status), "prepared") || !strings.EqualFold(strings.TrimSpace(proposal.ApplyStatus), "review_claimed") {
		return ProposalWorkPacket{}, false
	}
	packet, ok := buildStoredPreparedProposalWorkPacket(&MemoryProposal{
		ID:             proposal.ID,
		Kind:           proposal.Kind,
		Status:         proposal.Status,
		ReviewRequired: proposal.ReviewRequired,
		ProposedChange: proposal.ProposedChange,
		ApplyStatus:    "review_prepared",
	})
	if !ok {
		return ProposalWorkPacket{}, false
	}
	packet.Status = "claimed"
	return packet, true
}

func isLowRiskPreparedProposal(proposal *MemoryProposal) bool {
	if proposal == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(proposal.BlastRadius)) {
	case "", "low", "medium":
		return true
	default:
		return false
	}
}
