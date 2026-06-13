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
		t.Fatalf("expected cross-ref composition to accept the answer-bearing ref, got none: %v", out)
	}
	foundCheckout := false
	for _, row := range accepted {
		if row["ref"] == "named_memory:checkout-turn" {
			foundCheckout = true
			values := row["answer_values"].(map[string]any)
			if !containsString(values["locations"].([]string), "Target") {
				t.Fatalf("accepted checkout row missing Target location: %v", values["locations"])
			}
		}
	}
	if !foundCheckout {
		t.Fatalf("expected named_memory:checkout-turn accepted, accepted=%v", accepted)
	}
	outline := out["answer_outline"].(map[string]any)
	if !containsString(outline["supported_values"].([]string), "Target") {
		t.Fatalf("supported_values=%v missing Target", outline["supported_values"])
	}
	if out["ready"] != true || out["needs_fallback"] != false {
		t.Fatalf("expected ready after composition: ready=%v needs_fallback=%v", out["ready"], out["needs_fallback"])
	}
}
