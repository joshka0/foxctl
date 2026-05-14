package cmd

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestHooksProposalPacketCommand(t *testing.T) {
	workspace := t.TempDir()
	store := contextplane.NewWorkspaceStore(workspace)
	proposal, err := store.RecordMemoryProposal(context.Background(), contextplane.MemoryProposal{
		DedupeKey:      "methodology_draft|contextwiki-vocabulary-hook",
		Kind:           "methodology_draft",
		Classification: "external_evidence",
		Status:         "open",
		ReviewRequired: true,
		Confidence:     0.72,
		BlastRadius:    "high",
		Summary:        "Review imported evidence for a methodology or doctrine update: ContextWiki Vocabulary Review. Suggested target: notes/repo/contextwiki-inspect/semantic-and-memory.md.",
		ProposedChange: map[string]any{
			"evidence_import_id":         "E-789",
			"title":                      "ContextWiki Vocabulary Review",
			"draft_path":                 "inbox/drafted-from-foxctl/external-evidence/contextwiki-inspect/contextwiki-vocabulary-review.md",
			"suggested_target_note_path": "notes/repo/contextwiki-inspect/semantic-and-memory.md",
			"suggested_target_heading":   "Review",
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordMemoryProposal: %v", err)
	}

	cmd := newHooksProposalPacketCommand()
	cmd.SetIn(bytes.NewBufferString(`{"proposal_id":"` + proposal.ID + `","action":"apply"}`))
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
	if _, ok := response["context"].(string); !ok {
		t.Fatalf("response.context=%T", response["context"])
	}
	metadata := nestedMap(t, response, "metadata")
	workPacket := nestedMap(t, metadata, "proposal_work_packet")
	if workPacket["action"] != "merge_promotion" {
		t.Fatalf("work_packet.action=%v want merge_promotion", workPacket["action"])
	}
	if workPacket["proposal_id"] != proposal.ID {
		t.Fatalf("work_packet.proposal_id=%v want %s", workPacket["proposal_id"], proposal.ID)
	}
}
