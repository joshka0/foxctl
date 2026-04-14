package main

import (
	"strings"

	refstatus "github.com/joshka0/foxctl/internal/intelligence/refactor/status"
)

func applyConfidenceScores(findings []finding, mode refstatus.Mode) []finding {
	out := append([]finding(nil), findings...)
	for i := range out {
		score, factors := scoreFindingConfidence(out[i], mode)
		if out[i].Evidence == nil {
			out[i].Evidence = map[string]any{}
		}
		out[i].Evidence["confidence_score"] = score
		out[i].Evidence["confidence_factors"] = factors
		out[i].Confidence = confidenceLabel(score)
	}
	return out
}

func scoreFindingConfidence(item finding, mode refstatus.Mode) (int, map[string]int) {
	score := baseConfidenceScore(item.Confidence)
	factors := map[string]int{
		"base": score,
	}

	if mode == refstatus.ModeIndexBacked {
		score += 6
		factors["index_mode"] = 6
	}
	if item.Evidence != nil {
		if evidenceString(item.Evidence["scope_snapshot_id"]) != "" {
			score += 3
			factors["snapshot"] = 3
		}
	}

	switch item.RuleID {
	case "function_hotspot":
		rules := evidenceStrings(item.Evidence["rules"])
		if structural := minConfidenceInt(len(rules)*2, 10); structural > 0 {
			score += structural
			factors["rule_mix"] = structural
		}
		if seed := evidenceString(item.Evidence["seed_node_id"]); seed != "" {
			score += 4
			factors["graph_seed"] = 4
		}
		if boundary := evidenceString(item.Evidence["suggested_boundary_kind"]); boundary != "" {
			score += 2
			factors["boundary"] = 2
		}
		if cochange := evidenceInt(item.Evidence["cochange_count"]); cochange > 0 {
			bonus := minConfidenceInt(cochange, 3)
			score += bonus
			factors["cochange"] = bonus
		}
		if opportunity := evidenceInt(item.Evidence["opportunity_bonus"]); opportunity > 0 {
			bonus := minConfidenceInt(opportunity, 8)
			score += bonus
			factors["opportunity"] = bonus
		}
	case "unreachable_private_symbol":
		score += 12
		factors["reachability"] = 12
	case "test_only_helper":
		score += 8
		factors["reachability"] = 8
	case "stale_export_candidate":
		score += 4
		factors["reachability"] = 4
	}

	if item.Evidence != nil {
		if incoming := evidenceInt(item.Evidence["incoming_ref_count"]); incoming == 0 && isDeadRuleID(item.RuleID) {
			score += 4
			factors["zero_inbound_refs"] = 4
		}
		if external := evidenceInt(item.Evidence["external_non_test_refs"]); external == 0 && isDeadRuleID(item.RuleID) {
			score += 4
			factors["zero_external_refs"] = 4
		}
		if sourceSample := evidenceStrings(item.Evidence["source_sample"]); len(sourceSample) > 0 && isDeadRuleID(item.RuleID) {
			score -= 4
			factors["source_sample_penalty"] = -4
		}
	}

	return clampScore(score), factors
}

func isDeadRuleID(ruleID string) bool {
	switch strings.TrimSpace(ruleID) {
	case "unreachable_private_symbol", "test_only_helper", "stale_export_candidate", "orphan_file", "test_only_file", "stale_package_candidate", "test_only_package":
		return true
	default:
		return false
	}
}

func baseConfidenceScore(label string) int {
	switch strings.TrimSpace(label) {
	case "high":
		return 78
	case "medium":
		return 64
	case "low":
		return 48
	default:
		return 60
	}
}

func confidenceLabel(score int) string {
	switch {
	case score >= 80:
		return "high"
	case score >= 60:
		return "medium"
	default:
		return "low"
	}
}

func minConfidenceInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
