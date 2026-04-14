package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
)

func TestContextNextProposalMergeCommand(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-vocabulary-next",
		Kind:           "external_evidence_import",
		Classification: "external_evidence",
		Status:         "prepared",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "medium",
		Summary:        "Review imported evidence draft for merge consideration: ACA Vocabulary Review. Suggested target: notes/repo/aca-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"draft_path":                 "inbox/drafted-from-agentctl/external-evidence/aca-inspect/aca-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/aca-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		EvaluationStatus: "accepted",
		ApplyStatus:      "review_prepared",
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	cmd := newContextNextProposalMergeCommand()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspace})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	env := decodeTestEnvelope(t, out.Bytes())
	data := envelopeDataMap(t, env)
	if data["found"] != true {
		t.Fatalf("found=%v want true", data["found"])
	}
	packet := nestedMap(t, data, "work_packet")
	if packet["action"] != "merge_promotion" || packet["target_path"] != "notes/repo/aca-inspect/semantic-and-memory.md" {
		t.Fatalf("unexpected work_packet=%v", packet)
	}

	claimCmd := newContextNextProposalMergeCommand()
	claimOut := &bytes.Buffer{}
	claimCmd.SetOut(claimOut)
	claimCmd.SetErr(&bytes.Buffer{})
	claimCmd.SetArgs([]string{"--workspace", workspace, "--claim"})
	if err := claimCmd.Execute(); err != nil {
		t.Fatalf("claim execute: %v", err)
	}
	claimEnv := decodeTestEnvelope(t, claimOut.Bytes())
	claimData := envelopeDataMap(t, claimEnv)
	claimTask := nestedMap(t, claimData, "task")
	if claimTask["status"] != "claimed" {
		t.Fatalf("task.status=%v want claimed", claimTask["status"])
	}
}
