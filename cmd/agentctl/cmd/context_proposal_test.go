package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
)

func TestContextProposalCommands(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	proposal, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
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

	listCmd := newContextProposalsCommand()
	listOut := &bytes.Buffer{}
	listCmd.SetOut(listOut)
	listCmd.SetErr(&bytes.Buffer{})
	listCmd.SetArgs([]string{"--workspace", workspace})
	if err := listCmd.Execute(); err != nil {
		t.Fatalf("list execute: %v", err)
	}
	listEnv := decodeTestEnvelope(t, listOut.Bytes())
	listData := envelopeDataMap(t, listEnv)
	items, ok := listData["proposals"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("proposals=%T %v", listData["proposals"], listData["proposals"])
	}

	getCmd := newContextProposalCommand()
	getOut := &bytes.Buffer{}
	getCmd.SetOut(getOut)
	getCmd.SetErr(&bytes.Buffer{})
	getCmd.SetArgs([]string{"--workspace", workspace, proposal.ID})
	if err := getCmd.Execute(); err != nil {
		t.Fatalf("get execute: %v", err)
	}
	getEnv := decodeTestEnvelope(t, getOut.Bytes())
	getData := envelopeDataMap(t, getEnv)
	gotProposal := nestedMap(t, getData, "proposal")
	if gotProposal["id"] != proposal.ID {
		t.Fatalf("proposal.id=%v want %s", gotProposal["id"], proposal.ID)
	}

	applyCmd := newContextProposalApplyCommand()
	applyOut := &bytes.Buffer{}
	applyCmd.SetOut(applyOut)
	applyCmd.SetErr(&bytes.Buffer{})
	applyCmd.SetArgs([]string{"--workspace", workspace, proposal.ID})
	if err := applyCmd.Execute(); err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	applyEnv := decodeTestEnvelope(t, applyOut.Bytes())
	applyData := envelopeDataMap(t, applyEnv)
	appliedProposal := nestedMap(t, applyData, "proposal")
	if appliedProposal["status"] != "applied" {
		t.Fatalf("proposal.status=%v want applied", appliedProposal["status"])
	}

	manual, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
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
	rejectCmd := newContextProposalRejectCommand()
	rejectOut := &bytes.Buffer{}
	rejectCmd.SetOut(rejectOut)
	rejectCmd.SetErr(&bytes.Buffer{})
	rejectCmd.SetArgs([]string{"--workspace", workspace, manual.ID})
	if err := rejectCmd.Execute(); err != nil {
		t.Fatalf("reject execute: %v", err)
	}
	rejectEnv := decodeTestEnvelope(t, rejectOut.Bytes())
	rejectData := envelopeDataMap(t, rejectEnv)
	rejectedProposal := nestedMap(t, rejectData, "proposal")
	if rejectedProposal["status"] != "rejected" {
		t.Fatalf("proposal.status=%v want rejected", rejectedProposal["status"])
	}

	evidence, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "methodology_draft|aca-vocabulary",
		Kind:           "methodology_draft",
		Classification: "external_evidence",
		Status:         "open",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "high",
		Summary:        "Review imported evidence for a methodology or doctrine update: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"evidence_import_id":         "E-123",
			"title":                      "ACA Vocabulary Review",
			"draft_path":                 "inbox/drafted-from-agentctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Record evidence proposal: %v", err)
	}
	evidenceApplyCmd := newContextProposalApplyCommand()
	evidenceApplyOut := &bytes.Buffer{}
	evidenceApplyCmd.SetOut(evidenceApplyOut)
	evidenceApplyCmd.SetErr(&bytes.Buffer{})
	evidenceApplyCmd.SetArgs([]string{"--workspace", workspace, evidence.ID})
	if err := evidenceApplyCmd.Execute(); err != nil {
		t.Fatalf("evidence apply execute: %v", err)
	}
	evidenceApplyEnv := decodeTestEnvelope(t, evidenceApplyOut.Bytes())
	evidenceApplyData := envelopeDataMap(t, evidenceApplyEnv)
	evidenceAppliedProposal := nestedMap(t, evidenceApplyData, "proposal")
	if evidenceAppliedProposal["status"] != "prepared" {
		t.Fatalf("proposal.status=%v want prepared", evidenceAppliedProposal["status"])
	}
	resultMap := nestedMap(t, evidenceApplyData, "result")
	if resultMap["target_path"] != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("target_path=%v", resultMap["target_path"])
	}
	workPacket := nestedMap(t, evidenceApplyData, "work_packet")
	if workPacket["action"] != "merge_promotion" || workPacket["target_path"] != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("unexpected work_packet=%v", workPacket)
	}

	vaultRoot := filepath.Join(t.TempDir(), "vault")
	targetAbs := filepath.Join(vaultRoot, "notes", "repo", "aca-inspect", "semantic-and-memory.md")
	if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(targetAbs, []byte(`---
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
	draftRel := "inbox/drafted-from-agentctl/external-evidence/aca-inspect/aca-vocabulary-review.md"
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
	mergeProposal, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "methodology_draft|aca-vocabulary-merge",
		Kind:           "methodology_draft",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "high",
		Summary:        "Review imported evidence for a methodology or doctrine update: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"evidence_import_id":         "E-456",
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
		t.Fatalf("Record merge proposal: %v", err)
	}
	mergeCmd := newContextProposalMergeCommand()
	mergeOut := &bytes.Buffer{}
	mergeCmd.SetOut(mergeOut)
	mergeCmd.SetErr(&bytes.Buffer{})
	mergeCmd.SetArgs([]string{"--workspace", workspace, "--vault-path", vaultRoot, mergeProposal.ID})
	if err := mergeCmd.Execute(); err != nil {
		t.Fatalf("merge execute: %v", err)
	}
	mergeEnv := decodeTestEnvelope(t, mergeOut.Bytes())
	mergeData := envelopeDataMap(t, mergeEnv)
	mergedProposal := nestedMap(t, mergeData, "proposal")
	if mergedProposal["status"] != "merged" {
		t.Fatalf("proposal.status=%v want merged", mergedProposal["status"])
	}
	mergeResult := nestedMap(t, mergeData, "merge")
	if mergeResult["merged_as"] != "append" {
		t.Fatalf("merged_as=%v want append", mergeResult["merged_as"])
	}
	mergePacket := nestedMap(t, mergeData, "work_packet")
	if mergePacket["status"] != "merged" || mergePacket["target_path"] != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("unexpected work_packet=%v", mergePacket)
	}
}
