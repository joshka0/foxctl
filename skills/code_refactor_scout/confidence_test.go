package main

import (
	"testing"

	refstatus "github.com/jkatigb/agentctl/internal/refactor/status"
)

func TestApplyConfidenceScoresAddsStructuredConfidenceToHotspots(t *testing.T) {
	items := []finding{
		{
			RuleID:     "function_hotspot",
			Confidence: "high",
			Evidence: map[string]any{
				"rules":                   []string{"duplicate_orchestration_fingerprint", "fan_out_dependency_spread", "same_file_extraction_candidate"},
				"scope_snapshot_id":       "refsnap-1",
				"seed_node_id":            "sym:alpha",
				"opportunity_bonus":       6,
				"suggested_boundary_kind": "extract_workflow_step",
			},
		},
	}

	got := applyConfidenceScores(items, refstatus.ModeIndexBacked)
	score, ok := got[0].Evidence["confidence_score"].(int)
	if !ok {
		t.Fatalf("confidence_score=%T want int", got[0].Evidence["confidence_score"])
	}
	if score < 80 {
		t.Fatalf("confidence_score=%d want >= 80", score)
	}
	factors, ok := got[0].Evidence["confidence_factors"].(map[string]int)
	if !ok {
		t.Fatalf("confidence_factors=%T want map[string]int", got[0].Evidence["confidence_factors"])
	}
	if factors["index_mode"] == 0 || factors["graph_seed"] == 0 || factors["opportunity"] == 0 {
		t.Fatalf("factors=%v want index_mode, graph_seed, and opportunity bonuses", factors)
	}
	if got[0].Confidence != "high" {
		t.Fatalf("confidence=%q want high", got[0].Confidence)
	}
}

func TestApplyConfidenceScoresDifferentiatesDeadCodeRules(t *testing.T) {
	items := []finding{
		{
			RuleID:     "unreachable_private_symbol",
			Confidence: "high",
			Evidence: map[string]any{
				"incoming_ref_count":    0,
				"external_non_test_refs": 0,
			},
		},
		{
			RuleID:     "stale_export_candidate",
			Confidence: "medium",
			Evidence: map[string]any{
				"incoming_ref_count":    0,
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
