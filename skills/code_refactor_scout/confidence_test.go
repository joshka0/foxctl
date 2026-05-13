package main

import (
	"reflect"
	"testing"

	refstatus "github.com/joshka0/foxctl/internal/intelligence/refactor/status"
)

func TestApplyConfidenceScoresAddsDeterministicConfidenceForHotspots(t *testing.T) {
	items := []finding{
		{
			RuleID:     "function_hotspot",
			Confidence: "medium",
			Evidence: map[string]any{
				"rules":                   []string{"duplicate_orchestration_fingerprint", "fan_out_dependency_spread"},
				"scope_snapshot_id":       "refsnap-1",
				"seed_node_id":            "sym:alpha",
				"cochange_count":          2,
				"opportunity_bonus":       5,
				"suggested_boundary_kind": "extract_workflow_step",
			},
		},
	}

	got := applyConfidenceScores(items, refstatus.ModeIndexBacked)
	score, ok := got[0].Evidence["confidence_score"].(int)
	if !ok {
		t.Fatalf("confidence_score=%T want int", got[0].Evidence["confidence_score"])
	}
	if score != 90 {
		t.Fatalf("confidence_score=%d want 90", score)
	}
	factors, ok := got[0].Evidence["confidence_factors"].(map[string]int)
	if !ok {
		t.Fatalf("confidence_factors=%T want map[string]int", got[0].Evidence["confidence_factors"])
	}
	wantFactors := map[string]int{
		"base":        64,
		"index_mode":  6,
		"snapshot":    3,
		"rule_mix":    4,
		"graph_seed":  4,
		"boundary":    2,
		"cochange":    2,
		"opportunity": 5,
	}
	if !reflect.DeepEqual(factors, wantFactors) {
		t.Fatalf("factors=%v want %v", factors, wantFactors)
	}
	if got[0].Confidence != "high" {
		t.Fatalf("confidence=%q want high", got[0].Confidence)
	}
}

func TestApplyConfidenceScoresHandlesMissingAndPartialEvidence(t *testing.T) {
	items := []finding{
		{
			RuleID:     "function_hotspot",
			Confidence: "",
			Evidence:   nil,
		},
		{
			RuleID:     "unreachable_private_symbol",
			Confidence: "low",
			Evidence: map[string]any{
				"source_sample": "not-a-list",
			},
		},
	}

	got := applyConfidenceScores(items, refstatus.ModeParserOnly)

	firstScore, ok := got[0].Evidence["confidence_score"].(int)
	if !ok {
		t.Fatalf("first confidence_score=%T want int", got[0].Evidence["confidence_score"])
	}
	if firstScore != 60 {
		t.Fatalf("first confidence_score=%d want 60", firstScore)
	}
	firstFactors, ok := got[0].Evidence["confidence_factors"].(map[string]int)
	if !ok {
		t.Fatalf("first confidence_factors=%T want map[string]int", got[0].Evidence["confidence_factors"])
	}
	if !reflect.DeepEqual(firstFactors, map[string]int{"base": 60}) {
		t.Fatalf("first confidence_factors=%v want base-only map", firstFactors)
	}
	if got[0].Confidence != "medium" {
		t.Fatalf("first confidence=%q want medium", got[0].Confidence)
	}

	secondScore, ok := got[1].Evidence["confidence_score"].(int)
	if !ok {
		t.Fatalf("second confidence_score=%T want int", got[1].Evidence["confidence_score"])
	}
	if secondScore != 68 {
		t.Fatalf("second confidence_score=%d want 68", secondScore)
	}
	secondFactors, ok := got[1].Evidence["confidence_factors"].(map[string]int)
	if !ok {
		t.Fatalf("second confidence_factors=%T want map[string]int", got[1].Evidence["confidence_factors"])
	}
	wantSecondFactors := map[string]int{
		"base":               48,
		"reachability":       12,
		"zero_inbound_refs":  4,
		"zero_external_refs": 4,
	}
	if !reflect.DeepEqual(secondFactors, wantSecondFactors) {
		t.Fatalf("second confidence_factors=%v want %v", secondFactors, wantSecondFactors)
	}
	if got[1].Confidence != "medium" {
		t.Fatalf("second confidence=%q want medium", got[1].Confidence)
	}
}

func TestApplyConfidenceScoresDifferentiatesDeadCodeRules(t *testing.T) {
	items := []finding{
		{
			RuleID:     "unreachable_private_symbol",
			Confidence: "high",
			Evidence: map[string]any{
				"incoming_ref_count":     0,
				"external_non_test_refs": 0,
			},
		},
		{
			RuleID:     "stale_export_candidate",
			Confidence: "medium",
			Evidence: map[string]any{
				"incoming_ref_count":     0,
				"external_non_test_refs": 0,
			},
		},
	}

	got := applyConfidenceScores(items, refstatus.ModeIndexBacked)
	first := got[0].Evidence["confidence_score"].(int)
	second := got[1].Evidence["confidence_score"].(int)
	if first <= second {
		t.Fatalf("unreachable_private_symbol score=%d want > stale_export_candidate score=%d", first, second)
	}
}
