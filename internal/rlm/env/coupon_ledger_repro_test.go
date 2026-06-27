package env

import "testing"

// TestBuildEvidenceLedgerComposesCouponSplitAcrossRefs covers the live LongMem
// "coupon" false negative described in
// docs/analysis/long-term-agent-worktree-status-2026-06-11.md: the expected
// memory is surfaced, but the answer depends on "separated conversational
// context".
//
// The same facts are split across two refs/turns — one names the redeemed
// coupon, the other names the store. The per-ref pass accepts neither (no
// single ref carries both the required anchors and the location), so cross-ref
// composition must accept the answer-bearing ref while crediting the sibling
// ref's anchor coverage. The single-ref variant is covered by
// TestBuildEvidenceLedgerAcceptsDirectLocationEvidence.
//
// Slice 3 regression: when the location regex cannot extract the answer
// value (conversational phrasing without preposition), but the required
// concept slots are strongly matched, the row should still be accepted.
func TestBuildEvidenceLedgerAcceptsNonExtractableLocationWithStrongSlots(t *testing.T) {
	t.Parallel()

	question := "Where did I redeem a $5 coupon on coffee creamer?"
	plan, err := buildContextQueryPlan(planContextQueryInput{Question: question, Goal: "recall", Limit: 4})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	// "Target" appears without a preposition — the location regex requires
	// at|in|from|through|near|inside|outside|via|to|by before a capitalized
	// word. This phrasing should NOT match the regex, so directAnswer=false.
	// But the concept slots (coupon, creamer) are covered, so strongSlotMatch
	// should rescue the row via the Slice 3 fix.
	out := buildEvidenceLedger(evidenceLedgerInput{
		Query:                question,
		Refs:                 []string{"named_memory:conversational-coupon"},
		RequiredEvidence:     plan.RequiredEvidence,
		CoverageRequirements: plan.CoverageRequirements,
		MaxTextChars:         200,
	}, []aggregateLoadedEvidenceRef{
		{
			Ref:    "named_memory:conversational-coupon",
			Loaded: true,
			Text:   "Target had a great coupon deal — I redeemed a $5 coupon on coffee creamer there.",
		},
	}, plan)

	accepted := out["accepted_rows"].([]map[string]any)
	if len(accepted) == 0 {
		t.Fatalf("expected strong-slot evidence to be accepted despite non-extractable location, got 0 accepted rows. Output: %v", out)
	}
}

func TestBuildEvidenceLedgerComposesCouponSplitAcrossRefs(t *testing.T) {
	t.Parallel()

	question := "Where did I redeem a $5 coupon on coffee creamer?"
	plan, err := buildContextQueryPlan(planContextQueryInput{Question: question, Goal: "recall", Limit: 4})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	out := buildEvidenceLedger(evidenceLedgerInput{
		Query:                question,
		Refs:                 []string{"named_memory:coupon-turn", "named_memory:checkout-turn"},
		RequiredEvidence:     plan.RequiredEvidence,
		CoverageRequirements: plan.CoverageRequirements,
		MaxTextChars:         200,
	}, []aggregateLoadedEvidenceRef{
		{
			Ref:    "named_memory:coupon-turn",
			Loaded: true,
			Text:   "I redeemed a $5 coupon on coffee creamer earlier this week.",
		},
		{
			Ref:    "named_memory:checkout-turn",
			Loaded: true,
			Text:   "Later that day I wrapped up my grocery run and checked out at Target.",
		},
	}, plan)

	accepted := out["accepted_rows"].([]map[string]any)
	if len(accepted) == 0 {
		rejected := out["rejected_rows"].([]map[string]any)
		t.Fatalf("expected cross-ref composition to accept the answer-bearing ref, got none. Accepted: %v Rejected: %v", accepted, rejected)
	}
	foundCheckout := false
	for _, row := range accepted {
		if row["ref"] == "named_memory:checkout-turn" {
			foundCheckout = true
		}
	}
	if !foundCheckout {
		rejected := out["rejected_rows"].([]map[string]any)
		t.Fatalf("expected named_memory:checkout-turn among accepted=%v\nrejected=%v", accepted, rejected)
	}
	outline := out["answer_outline"].(map[string]any)
	if !containsString(outline["supported_values"].([]string), "Target") {
		t.Fatalf("supported_values=%v missing Target", outline["supported_values"])
	}
	if out["ready"] != true || out["needs_fallback"] != false {
		t.Fatalf("expected ready after composition: ready=%v needs_fallback=%v", out["ready"], out["needs_fallback"])
	}
}
