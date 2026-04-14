package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestContextImportEvidenceCommand(t *testing.T) {
	h := newACAInspectHarness(t)
	targetPath := filepath.Join(h.vaultRoot, "notes", "repo", filepath.Base(h.workspacePath), "semantic-and-memory.md")
	writeTestVaultNote(t, targetPath, `---
title: semantic and memory
type: map
status: reviewed
trust: canonical
---
# semantic and memory

ACA vocabulary, retrieval, and memory conventions live here.
`)

	cmd := newContextImportEvidenceCommand()
	cmd.SetContext(config.WithContext(context.Background(), h.cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"--workspace", h.workspacePath,
		"--vault-path", h.vaultRoot,
		"--title", "ACA Vocabulary Review",
		"--text", "We should unify ACA vocabulary.\nAction: add an evidence intake lane.\nQuestion: Should local summarization be a separate layer?",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	env := decodeTestEnvelope(t, stdout.Bytes())
	data := envelopeDataMap(t, env)
	result := nestedMap(t, data, "result")
	run := nestedMap(t, result, "run")
	draftPath, _ := run["draft_path"].(string)
	if draftPath == "" {
		t.Fatal("expected draft_path")
	}
	if _, err := os.Stat(filepath.Join(h.vaultRoot, filepath.FromSlash(draftPath))); err != nil {
		t.Fatalf("draft path missing: %v", err)
	}
	extraction := nestedMap(t, result, "extraction")
	if extraction["processor_kind"] != "deterministic" {
		t.Fatalf("processor_kind=%v want deterministic", extraction["processor_kind"])
	}
	proposal := nestedMap(t, result, "proposal")
	if proposal["kind"] != "methodology_draft" {
		t.Fatalf("proposal.kind=%v want methodology_draft", proposal["kind"])
	}
	change := nestedMap(t, proposal, "proposed_change")
	if change["suggested_target_note_path"] != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("suggested_target_note_path=%v", change["suggested_target_note_path"])
	}
}
