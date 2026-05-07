package contextplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
)

func TestSetRetrievalPackageNoteFallback(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	path, err := store.SetRetrievalPackageNoteFallback(true)
	if err != nil {
		t.Fatalf("SetRetrievalPackageNoteFallback: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(body), "package_note_fallback: true") {
		t.Fatalf("retrieval policy missing enabled fallback:\n%s", string(body))
	}
	if !store.CurrentRetrievalOptions().UsePackageNoteFallback {
		t.Fatal("expected package_note_fallback to be enabled")
	}
}

func TestInspectRetrievalClassifiesPackageNoteFallbackDisabled(t *testing.T) {
	ctx := context.Background()
	workspacePath := filepath.Join(t.TempDir(), "praze")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	storageRoot := t.TempDir()
	vaultRoot := t.TempDir()

	store := NewWorkspaceStore(workspacePath)
	expectedPath := "internal/storage/memory/store.go"
	candidateNote := firstPackageNoteCandidate(filepath.Base(workspacePath), []string{expectedPath})
	writeVaultNote(t, filepath.Join(vaultRoot, candidateNote), `---
title: Storage Memory
type: map
trust: canonical
paths:
  - internal/storage/memory/store.go
---
Canonical package note for storage memory.
`)

	index, err := obsidianindex.Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, vaultRoot); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	inspection, err := store.InspectRetrieval(ctx, index, vaultRoot, "storage memory package", []string{expectedPath}, RetrievalResult{}, DefaultRetrievalOptions(), 5)
	if err != nil {
		t.Fatalf("InspectRetrieval: %v", err)
	}
	if inspection.Classification != "package_note_fallback_disabled" {
		t.Fatalf("classification=%q want package_note_fallback_disabled", inspection.Classification)
	}
	if inspection.Proposal.Kind != "policy_patch" {
		t.Fatalf("proposal.kind=%q want policy_patch", inspection.Proposal.Kind)
	}
	if inspection.CandidateNote != candidateNote {
		t.Fatalf("candidate_note=%q want %q", inspection.CandidateNote, candidateNote)
	}
}

func TestInspectRetrievalClassifiesBridgeMetadataGap(t *testing.T) {
	ctx := context.Background()
	workspacePath := filepath.Join(t.TempDir(), "praze")
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	storageRoot := t.TempDir()
	vaultRoot := t.TempDir()

	store := NewWorkspaceStore(workspacePath)
	expectedPath := "internal/interfaces/web/api/handlers.go"
	candidateNote := firstPackageNoteCandidate(filepath.Base(workspacePath), []string{expectedPath})
	writeVaultNote(t, filepath.Join(vaultRoot, candidateNote), `---
title: Web API
type: map
trust: canonical
---
Canonical package note without repo path metadata.
`)

	index, err := obsidianindex.Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	defer index.Close()
	if _, err := index.Rebuild(ctx, vaultRoot); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	opts := DefaultRetrievalOptions()
	opts.UsePackageNoteFallback = true
	inspection, err := store.InspectRetrieval(ctx, index, vaultRoot, "web api handlers", []string{expectedPath}, RetrievalResult{}, opts, 5)
	if err != nil {
		t.Fatalf("InspectRetrieval: %v", err)
	}
	if inspection.Classification != "bridge_metadata_gap" {
		t.Fatalf("classification=%q want bridge_metadata_gap", inspection.Classification)
	}
	if inspection.Proposal.Kind != "metadata_patch" {
		t.Fatalf("proposal.kind=%q want metadata_patch", inspection.Proposal.Kind)
	}
	if !strings.Contains(inspection.Proposal.MetadataPatch, expectedPath) {
		t.Fatalf("metadata_patch=%q want %q", inspection.Proposal.MetadataPatch, expectedPath)
	}
}

func TestInspectRetrievalClassifiesMissingSemanticAnchorAndBuildsReviewProposal(t *testing.T) {
	store := NewWorkspaceStore(filepath.Join(t.TempDir(), "foxctl"))
	opts := DefaultRetrievalOptions()
	opts.UseSemanticAnchors = true
	expectedPath := "internal/runtime/terminal/tmuxbridge/client.go"

	inspection, err := store.InspectRetrieval(context.Background(), nil, "", "read before write enforced", []string{expectedPath}, RetrievalResult{}, opts, 5)
	if err != nil {
		t.Fatalf("InspectRetrieval: %v", err)
	}
	if inspection.Classification != "missing_semantic_anchor" {
		t.Fatalf("classification=%q want missing_semantic_anchor", inspection.Classification)
	}
	if inspection.Proposal.Kind != "semantic_anchor_patch" {
		t.Fatalf("proposal.kind=%q want semantic_anchor_patch", inspection.Proposal.Kind)
	}
	proposal := memoryProposalFromRetrievalInspection(inspection)
	if proposal.Kind != PolicyKindSemanticAnchorPatch {
		t.Fatalf("proposal.Kind=%q want %q", proposal.Kind, PolicyKindSemanticAnchorPatch)
	}
	if !proposal.ReviewRequired {
		t.Fatalf("semantic anchor proposal must be review required")
	}
	if proposal.ApplyStatus != "pending" {
		t.Fatalf("apply_status=%q want pending", proposal.ApplyStatus)
	}
	if got := proposal.ProposedChange["expected_repo_paths"]; got == nil {
		t.Fatalf("expected_repo_paths missing from proposed change: %+v", proposal.ProposedChange)
	}
}

func TestSummarizeRetrievalInspections(t *testing.T) {
	summary := SummarizeRetrievalInspections([]RetrievalInspection{
		{
			Matched:        false,
			Classification: "package_note_fallback_disabled",
			Proposal: RetrievalCorrectionAction{
				Kind: "policy_patch",
			},
		},
		{
			Matched:        false,
			Classification: "missing_package_note",
			Proposal: RetrievalCorrectionAction{
				Kind:           "draft_package_note",
				TargetNotePath: "notes/repo/praze/storage-memory.md",
			},
		},
		{
			Matched:        true,
			Classification: "matched",
			Proposal: RetrievalCorrectionAction{
				Kind: "none",
			},
		},
	})

	if summary.Queries != 3 || summary.Matched != 1 || summary.Misses != 2 {
		t.Fatalf("summary counts=%+v", summary)
	}
	if !summary.PolicyPatchCandidate {
		t.Fatal("expected policy patch candidate")
	}
	if got := summary.Classifications["missing_package_note"]; got != 1 {
		t.Fatalf("missing_package_note=%d want 1", got)
	}
	if len(summary.PackageNoteCandidates) != 1 || summary.PackageNoteCandidates[0] != "notes/repo/praze/storage-memory.md" {
		t.Fatalf("package candidates=%v", summary.PackageNoteCandidates)
	}
	if len(summary.RecommendedActions) == 0 {
		t.Fatal("expected recommended actions")
	}
}

func TestRecordAndReadRetrievalCorrectionRuns(t *testing.T) {
	store := NewWorkspaceStore(t.TempDir())
	run := RetrievalCorrectionRun{
		Suite:          "foxctl-mixed",
		ControlSuite:   "praze-mixed",
		ArtifactDigest: "sha256:test",
		Summary: RetrievalInspectionBatchSummary{
			Queries:         3,
			Matched:         2,
			Misses:          1,
			Classifications: map[string]int{"missing_package_note": 1},
		},
		PolicyCandidate: true,
		PolicyApplied:   true,
		PolicyAccepted:  true,
		DraftCount:      1,
	}
	if err := store.RecordRetrievalCorrectionRun(run); err != nil {
		t.Fatalf("RecordRetrievalCorrectionRun: %v", err)
	}
	runs, err := store.ListRetrievalCorrectionRuns(10)
	if err != nil {
		t.Fatalf("ListRetrievalCorrectionRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs=%d want 1", len(runs))
	}
	if runs[0].ArtifactDigest != "sha256:test" {
		t.Fatalf("artifact=%q", runs[0].ArtifactDigest)
	}
	got, err := store.GetRetrievalCorrectionRun(runs[0].ID)
	if err != nil {
		t.Fatalf("GetRetrievalCorrectionRun: %v", err)
	}
	if got == nil || got.Suite != "foxctl-mixed" {
		t.Fatalf("run=%+v", got)
	}
	if got.Summary.Classifications["missing_package_note"] != 1 {
		t.Fatalf("summary=%+v", got.Summary)
	}
}

func writeVaultNote(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
