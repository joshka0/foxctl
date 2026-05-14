package contextplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportEvidenceDeterministicDraftsIntoVaultInbox(t *testing.T) {
	store := NewWorkspaceStore(filepath.Join(t.TempDir(), "workspace"))
	vaultRoot := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	targetPath := filepath.Join(vaultRoot, "notes", "repo", "workspace", "semantic-and-memory.md")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte(`---
title: semantic and memory
type: map
status: reviewed
trust: canonical
---
# semantic and memory

ContextWiki vocabulary, retrieval, and memory conventions live here.
`), 0o644); err != nil {
		t.Fatalf("write target note: %v", err)
	}

	result, err := store.ImportEvidence(context.Background(), filepath.Join(t.TempDir(), "cas"), vaultRoot, EvidenceImportInput{
		Title:      "ContextWiki Vocabulary Evidence Intake",
		SourceKind: "transcript",
		SourceRef:  "/tmp/retro.txt",
		Content: "We should unify ContextWiki vocabulary.\n" +
			"Action: add a proposal store for memory changes.\n" +
			"Question: Should L5 ingest live under context or obsidian?\n" +
			"This pattern helps keep retrieval fixes reviewable and bounded.\n",
	})
	if err != nil {
		t.Fatalf("ImportEvidence: %v", err)
	}
	if result.Run.SourceKind != "transcript" {
		t.Fatalf("source_kind=%q want transcript", result.Run.SourceKind)
	}
	if result.Proposal.Kind != "methodology_draft" {
		t.Fatalf("proposal.kind=%q want methodology_draft", result.Proposal.Kind)
	}
	if got := result.Proposal.ProposedChange["suggested_target_note_path"]; got != "notes/repo/workspace/semantic-and-memory.md" {
		t.Fatalf("suggested_target_note_path=%v", got)
	}
	if got := result.Proposal.ProposedChange["suggested_target_heading"]; got != "Review" {
		t.Fatalf("suggested_target_heading=%v", got)
	}
	if result.Extraction.ProcessorKind != "deterministic" {
		t.Fatalf("processor_kind=%q want deterministic", result.Extraction.ProcessorKind)
	}
	if strings.TrimSpace(result.Run.DraftPath) == "" {
		t.Fatal("expected draft path")
	}
	body, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(result.Run.DraftPath)))
	if err != nil {
		t.Fatalf("read draft: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "## Summary") || !strings.Contains(text, "## Action Items") {
		t.Fatalf("unexpected draft body:\n%s", text)
	}
	if !strings.Contains(text, "transcript:/tmp/retro.txt") || !strings.Contains(text, "type: note") {
		t.Fatalf("missing provenance ref:\n%s", text)
	}

	second, err := store.ImportEvidence(context.Background(), filepath.Join(t.TempDir(), "cas-2"), vaultRoot, EvidenceImportInput{
		Title:      "ContextWiki Vocabulary Evidence Intake",
		SourceKind: "transcript",
		SourceRef:  "/tmp/retro-2.txt",
		Content:    "Policy update suggestion.\nWorkflow change.\nConvention should be documented.\n",
	})
	if err != nil {
		t.Fatalf("ImportEvidence second: %v", err)
	}
	if second.Proposal.ID != result.Proposal.ID {
		t.Fatalf("proposal IDs differ: %s vs %s", result.Proposal.ID, second.Proposal.ID)
	}
	if second.Proposal.Count != 2 {
		t.Fatalf("proposal count=%d want 2", second.Proposal.Count)
	}
}
