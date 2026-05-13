package main

import (
	"reflect"
	"testing"
)

func TestReviewTargetsForFindingReasonOrderStable(t *testing.T) {
	item := finding{RuleID: "function_hotspot", Category: "function", Score: 91}

	targets, reasons := reviewTargetsForFinding(item)
	wantTargets := []string{
		targetSmallComposableCode,
		targetSemanticCommenting,
		targetImproveCodebaseArchitecture,
	}
	wantReasons := []string{
		targetSmallComposableCode + ": reduce sprawl through a smaller, behavior-preserving module shape",
		targetSemanticCommenting + ": inspect the owner for one or two evidence-only anchors or an Index block",
		targetImproveCodebaseArchitecture + ": evaluate whether this shallow module should become a deeper module with a clearer seam",
	}

	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("targets=%#v want %#v", targets, wantTargets)
	}
	if !reflect.DeepEqual(reasons, wantReasons) {
		t.Fatalf("reasons=%#v want %#v", reasons, wantReasons)
	}
}

func TestReviewTargetsForFindingDeadCodeMapsToSmallComposableOnly(t *testing.T) {
	item := finding{RuleID: "orphan_file", Category: "dead_code", Score: 90}

	targets, reasons := reviewTargetsForFinding(item)
	if len(targets) != 1 || targets[0] != targetSmallComposableCode {
		t.Fatalf("targets=%#v want only %q", targets, targetSmallComposableCode)
	}
	if len(reasons) != 1 {
		t.Fatalf("reasons=%#v want one reason", reasons)
	}
}

func TestApplyTargetTrimsInputAndFiltersDeterministically(t *testing.T) {
	items := applyReviewTargets([]finding{
		{RuleID: "function_hotspot", Category: "function", File: "a.go", Symbol: "DoThing", Score: 91},
		{RuleID: "semantic_simplification_candidate", Category: "function", File: "b.go", Symbol: "CleanBool", Score: 75},
	})

	filtered := applyTarget(items, "  "+targetImproveCodebaseArchitecture+"  ")
	if len(filtered) != 1 {
		t.Fatalf("len(filtered)=%d want 1", len(filtered))
	}
	if filtered[0].RuleID != "function_hotspot" {
		t.Fatalf("rule=%q want function_hotspot", filtered[0].RuleID)
	}
}

func TestBuildSkillTargetLanesUsesTargetReasonsAsSamples(t *testing.T) {
	items := applyReviewTargets([]finding{
		{
			RuleID:   "function_hotspot",
			Category: "function",
			File:     "reader/index.ts",
			Line:     10,
			Symbol:   "readerAPI",
			Score:    94,
			Detail:   "readerAPI triggers multiple refactoring signals.",
		},
	})

	lanes := buildSkillTargetLanes(items, 10)
	architecture := lanes[targetImproveCodebaseArchitecture]
	if len(architecture) != 1 {
		t.Fatalf("architecture lane=%#v want 1 entry", architecture)
	}
	if len(architecture[0].Samples) == 0 {
		t.Fatalf("architecture samples=%#v want reason sample", architecture[0].Samples)
	}
}
