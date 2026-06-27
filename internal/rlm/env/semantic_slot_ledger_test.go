package env

import (
	"strings"
	"testing"
)

// TestBuildEvidenceLedgerSemanticSlotMatchAcceptsSynonym covers the live
// LongMem "model-kit" miss: the question "Do I have any model kits?" should be
// answered from the memory "plastic model airplane kit", but substring slot
// matching never fires because "kits" is not a substring of "kit" and
// "model kits" is not a substring of the stored phrase.
//
// With a semantic slot matcher injected (the shell supplies an embedding-backed
// one), the concept slots are recognized semantically and the evidence is
// accepted. The matcher here is a deterministic stand-in for embeddings so the
// test exercises the merge/acceptance wiring without a live provider.
func TestBuildEvidenceLedgerSemanticSlotMatchAcceptsSynonym(t *testing.T) {
	t.Parallel()

	question := "Do I have any model kits?"
	plan, err := buildContextQueryPlan(planContextQueryInput{Question: question, Goal: "recall", Limit: 4})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	memory := "I keep a plastic model airplane kit on the shelf in my study."

	input := evidenceLedgerInput{
		Query:                question,
		Refs:                 []string{"named_memory:hobby"},
		RequiredEvidence:     plan.RequiredEvidence,
		CoverageRequirements: plan.CoverageRequirements,
		MaxTextChars:         200,
	}

	// Baseline: without the semantic matcher, substring matching rejects the
	// synonymous memory.
	baseline := buildEvidenceLedger(input, []aggregateLoadedEvidenceRef{{
		Ref:    "named_memory:hobby",
		Loaded: true,
		Text:   memory,
	}}, plan)
	if accepted := baseline["accepted_rows"].([]map[string]any); len(accepted) != 0 {
		t.Fatalf("baseline (substring-only) unexpectedly accepted: %v", accepted)
	}

	// Deterministic stand-in for embedding similarity: a concept slot about
	// models/kits is satisfied by the "model airplane kit" memory.
	input.semanticSlotMatch = func(text string, slot aggregateEvidenceSlot) bool {
		if !strings.Contains(strings.ToLower(text), "model airplane kit") {
			return false
		}
		label := strings.ToLower(slot.Label)
		return strings.Contains(label, "model") || strings.Contains(label, "kit")
	}

	out := buildEvidenceLedger(input, []aggregateLoadedEvidenceRef{{
		Ref:    "named_memory:hobby",
		Loaded: true,
		Text:   memory,
	}}, plan)

	accepted := out["accepted_rows"].([]map[string]any)
	if len(accepted) == 0 {
		t.Fatalf("expected semantic slot matching to accept the synonymous memory, got none: %v", out)
	}
	if accepted[0]["ref"] != "named_memory:hobby" {
		t.Fatalf("accepted wrong ref: %v", accepted[0]["ref"])
	}
}
