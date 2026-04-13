package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
)

func TestHooksProposalNextMergeCommand(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	if _, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "external_evidence_import|aca-vocabulary-hook-next",
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

	cmd := newHooksProposalNextMergeCommand()
	cmd.SetIn(bytes.NewBufferString(`{}`))
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--workspace", workspace})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	env := decodeTestEnvelope(t, out.Bytes())
	data := envelopeDataMap(t, env)
	response := nestedMap(t, data, "response")
	metadata := nestedMap(t, response, "metadata")
	workPacket := nestedMap(t, metadata, "proposal_work_packet")
	if workPacket["action"] != "merge_promotion" {
		t.Fatalf("work_packet.action=%v want merge_promotion", workPacket["action"])
	}
	if workPacket["status"] != "prepared" {
		t.Fatalf("work_packet.status=%v want prepared", workPacket["status"])
	}

	claimCmd := newHooksProposalNextMergeCommand()
	claimCmd.SetIn(bytes.NewBufferString(`{"claim":true}`))
	claimOut := &bytes.Buffer{}
	claimCmd.SetOut(claimOut)
	claimCmd.SetErr(&bytes.Buffer{})
	claimCmd.SetArgs([]string{"--workspace", workspace})
	if err := claimCmd.Execute(); err != nil {
		t.Fatalf("claim execute: %v", err)
	}
	claimEnv := decodeTestEnvelope(t, claimOut.Bytes())
	claimData := envelopeDataMap(t, claimEnv)
	claimResponse := nestedMap(t, claimData, "response")
	claimMetadata := nestedMap(t, claimResponse, "metadata")
	claimedPacket := nestedMap(t, claimMetadata, "proposal_work_packet")
	if claimedPacket["status"] != "claimed" {
		t.Fatalf("work_packet.status=%v want claimed", claimedPacket["status"])
	}
}
