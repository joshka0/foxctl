package contextplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordRetrievalProposalDedupes(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	inspection := RetrievalInspection{
		Query:          "storage memory package",
		ExpectedPaths:  []string{"internal/storage/memory/store.go"},
		RetrievedPaths: []string{"notes/repo/foxctl/semantic-and-memory.md"},
		Classification: "package_note_fallback_disabled",
		Observation: Observation{
			Statement:    "ACA retrieval missed deterministic package-note fallback.",
			Confidence:   0.82,
			Count:        1,
			Project:      "foxctl",
			Area:         "aca-retrieval",
			EvidenceRefs: []string{"query:storage memory package"},
			FirstSeen:    time.Now().UTC(),
			LastSeen:     time.Now().UTC(),
		},
		Proposal: RetrievalCorrectionAction{
			Kind:       "policy_patch",
			Summary:    "Enable deterministic ACA package-note fallback for this workspace.",
			PolicyPath: ".foxctl/policy/retrieval.yaml",
			PolicyPatch: "aca:\n" +
				"  package_note_fallback: true\n",
		},
		GeneratedAt: time.Now().UTC(),
	}

	first, err := store.RecordRetrievalProposal(context.Background(), inspection)
	if err != nil {
		t.Fatalf("RecordRetrievalProposal first: %v", err)
	}
	second, err := store.RecordRetrievalProposal(context.Background(), inspection)
	if err != nil {
		t.Fatalf("RecordRetrievalProposal second: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("proposal IDs differ: %s vs %s", first.ID, second.ID)
	}
	if second.Count != 2 {
		t.Fatalf("proposal count=%d want 2", second.Count)
	}
	items, err := store.ListMemoryProposals(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListMemoryProposals: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("proposals=%d want 1", len(items))
	}
}

func TestApplyAndRejectMemoryProposal(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	proposal, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		Kind:           "retrieval_policy_patch",
		Classification: "package_note_fallback_disabled",
		Status:         "open",
		Confidence:     0.82,
		BlastRadius:    "low",
		Summary:        "Enable deterministic ACA package-note fallback for this workspace.",
		ProposedChange: map[string]any{
			"policy_patch":          "aca:\n  package_note_fallback: true\n",
			"package_note_fallback": true,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	applied, result, packet, err := store.ApplyMemoryProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("ApplyMemoryProposal: %v", err)
	}
	if applied.Status != "applied" || applied.ApplyStatus != "applied" {
		t.Fatalf("applied proposal=%+v", applied)
	}
	if strings.TrimSpace(result["policy_path"].(string)) == "" {
		t.Fatalf("expected policy_path result, got %#v", result)
	}
	if packet.Action != "retrieval_policy_patch" || packet.PolicyPath == "" {
		t.Fatalf("unexpected work packet: %+v", packet)
	}
	body, err := store.ReadRetrievalPolicy()
	if err != nil {
		t.Fatalf("ReadRetrievalPolicy: %v", err)
	}
	if !strings.Contains(string(body), "package_note_fallback: true") {
		t.Fatalf("retrieval policy missing fallback enable:\n%s", string(body))
	}

	manual, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		Kind:           "manual_review",
		Classification: "ranking_mismatch",
		Status:         "open",
		Confidence:     0.68,
		BlastRadius:    "high",
		Summary:        "ACA retrieved notes, but ranking did not surface the expected path set.",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Record manual proposal: %v", err)
	}
	rejected, err := store.RejectMemoryProposal(context.Background(), manual.ID)
	if err != nil {
		t.Fatalf("RejectMemoryProposal: %v", err)
	}
	if rejected.Status != "rejected" || rejected.ApplyStatus != "rejected" {
		t.Fatalf("rejected proposal=%+v", rejected)
	}
}

func TestApplyEvidenceProposalPreparesReviewJob(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	proposal, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		DedupeKey:      "methodology_draft|aca-vocabulary",
		Kind:           "methodology_draft",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "high",
		Summary:        "Review imported evidence for a methodology or doctrine update: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"evidence_import_id":         "E-123",
			"title":                      "ACA Vocabulary Review",
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	applied, result, packet, err := store.ApplyMemoryProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("ApplyMemoryProposal: %v", err)
	}
	if applied.Status != "prepared" || applied.ApplyStatus != "review_prepared" {
		t.Fatalf("applied evidence proposal=%+v", applied)
	}
	jobMap, ok := result["promotion_job"].(PromotionJob)
	if !ok {
		t.Fatalf("promotion_job=%T %#v", result["promotion_job"], result["promotion_job"])
	}
	if jobMap.SourceKind != "evidence_import" {
		t.Fatalf("source_kind=%q want evidence_import", jobMap.SourceKind)
	}
	if got := result["target_path"]; got != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("target_path=%v", got)
	}
	if got := result["heading"]; got != "Review" {
		t.Fatalf("heading=%v", got)
	}
	if packet.Action != "merge_promotion" || packet.TargetPath != "notes/repo/aca-inspect/semantic-and-memory.md" || !packet.RequiresVaultPath {
		t.Fatalf("unexpected work packet: %+v", packet)
	}
	jobs, err := store.ListPromotionJobs(10)
	if err != nil {
		t.Fatalf("ListPromotionJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d want 1", len(jobs))
	}
}

func TestMergeMemoryProposal(t *testing.T) {
	workspace := t.TempDir()
	store := NewWorkspaceStore(workspace)
	vaultRoot := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "repo", "aca-inspect"), 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	targetPath := filepath.Join(vaultRoot, "notes", "repo", "aca-inspect", "semantic-and-memory.md")
	if err := os.WriteFile(targetPath, []byte(`---
title: semantic and memory
type: map
status: reviewed
trust: canonical
---

# semantic and memory

## Review

Existing review block.
`), 0o644); err != nil {
		t.Fatalf("write target note: %v", err)
	}
	draftRel := "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-vocabulary-review.md"
	draftAbs := filepath.Join(vaultRoot, filepath.FromSlash(draftRel))
	if err := os.MkdirAll(filepath.Dir(draftAbs), 0o755); err != nil {
		t.Fatalf("mkdir draft dir: %v", err)
	}
	if err := os.WriteFile(draftAbs, []byte(`---
title: ACA Vocabulary Review
type: evidence
status: draft
trust: raw
---

# ACA Vocabulary Review

Imported evidence says we should unify ACA vocabulary.
`), 0o644); err != nil {
		t.Fatalf("write draft note: %v", err)
	}
	proposal, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		DedupeKey:      "methodology_draft|aca-vocabulary",
		Kind:           "methodology_draft",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "high",
		Summary:        "Review imported evidence for a methodology or doctrine update: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"evidence_import_id":         "E-123",
			"title":                      "ACA Vocabulary Review",
			"draft_path":                 draftRel,
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}
	updated, merge, packet, err := store.MergeMemoryProposal(context.Background(), "", vaultRoot, proposal.ID, "", "", "")
	if err != nil {
		t.Fatalf("MergeMemoryProposal: %v", err)
	}
	if updated.Status != "merged" || updated.ApplyStatus != "reviewed_merged" {
		t.Fatalf("updated proposal=%+v", updated)
	}
	if merge.MergedAs != "append" {
		t.Fatalf("merged_as=%q want append", merge.MergedAs)
	}
	if packet.Status != "merged" || packet.TargetPath != "notes/repo/aca-inspect/semantic-and-memory.md" || packet.PromotionJobID == "" {
		t.Fatalf("unexpected merge packet: %+v", packet)
	}
	body, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target note: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "### Reviewed Merge") || !strings.Contains(text, "Imported evidence says we should unify ACA vocabulary.") {
		t.Fatalf("unexpected merged note:\n%s", text)
	}
}

func TestClaimAndReleaseProposalMergeTask(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	proposal, err := store.RecordMemoryProposal(context.Background(), MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-vocabulary-claim",
		Kind:           "external_evidence_import",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "medium",
		Summary:        "Review imported evidence draft for merge consideration: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}
	if _, err := store.GenerateMaintenanceTasks(context.Background(), 10); err != nil {
		t.Fatalf("GenerateMaintenanceTasks: %v", err)
	}
	claimed, err := store.ClaimNextProposalMergeTask(context.Background(), 10)
	if err != nil {
		t.Fatalf("ClaimNextProposalMergeTask: %v", err)
	}
	if claimed == nil || claimed.Status != "claimed" || claimed.WorkPacket == nil || claimed.WorkPacket.Status != "claimed" {
		t.Fatalf("claimed task=%+v", claimed)
	}
	next, err := store.NextProposalMergeTask(context.Background(), 10)
	if err != nil {
		t.Fatalf("NextProposalMergeTask after claim: %v", err)
	}
	if next != nil {
		t.Fatalf("expected no next task after claim, got %+v", next)
	}
	released, err := store.ReleaseProposalMergeClaim(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("ReleaseProposalMergeClaim: %v", err)
	}
	if released.ApplyStatus != "review_prepared" {
		t.Fatalf("apply_status=%q want review_prepared", released.ApplyStatus)
	}
	if _, err := store.GenerateMaintenanceTasks(context.Background(), 10); err != nil {
		t.Fatalf("GenerateMaintenanceTasks after release: %v", err)
	}
	next, err = store.NextProposalMergeTask(context.Background(), 10)
	if err != nil {
		t.Fatalf("NextProposalMergeTask after release: %v", err)
	}
	if next == nil || next.WorkPacket == nil || next.WorkPacket.ProposalID != proposal.ID {
		t.Fatalf("expected released task to reappear, got %+v", next)
	}
}
