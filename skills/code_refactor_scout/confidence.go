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

	score += scoreIndexModeConfidence(mode, factors)
	score += scoreSnapshotConfidence(item.Evidence, factors)
	score += scoreRuleSpecificConfidence(item, factors)
	score += scoreDeadRuleEvidenceConfidence(item.RuleID, item.Evidence, factors)

	return clampScore(score), factors
}

func scoreIndexModeConfidence(mode refstatus.Mode, factors map[string]int) int {
	if mode != refstatus.ModeIndexBacked {
		return 0
	}
	factors["index_mode"] = 6
	return 6
}

func scoreSnapshotConfidence(evidence map[string]any, factors map[string]int) int {
	if evidenceString(evidence["scope_snapshot_id"]) == "" {
		return 0
	}
	factors["snapshot"] = 3
	return 3
}

func scoreRuleSpecificConfidence(item finding, factors map[string]int) int {
	switch item.RuleID {
	case "function_hotspot":
		return scoreHotspotRuleConfidence(item.Evidence, factors)
	case "unreachable_private_symbol":
		factors["reachability"] = 12
		return 12
	case "test_only_helper":
		factors["reachability"] = 8
		return 8
	case "stale_export_candidate":
		factors["reachability"] = 4
		return 4
	default:
		return 0
	}
}

func scoreHotspotRuleConfidence(evidence map[string]any, factors map[string]int) int {
	score := 0
	rules := evidenceStrings(evidence["rules"])
	if structural := minConfidenceInt(len(rules)*2, 10); structural > 0 {
		score += structural
		factors["rule_mix"] = structural
	}
	if seed := evidenceString(evidence["seed_node_id"]); seed != "" {
		score += 4
		factors["graph_seed"] = 4
	}
	if boundary := evidenceString(evidence["suggested_boundary_kind"]); boundary != "" {
		score += 2
		factors["boundary"] = 2
	}
	if cochange := evidenceInt(evidence["cochange_count"]); cochange > 0 {
		bonus := minConfidenceInt(cochange, 3)
		score += bonus
		factors["cochange"] = bonus
	}
	if opportunity := evidenceInt(evidence["opportunity_bonus"]); opportunity > 0 {
		bonus := minConfidenceInt(opportunity, 8)
		score += bonus
		factors["opportunity"] = bonus
	}
	return score
}

func scoreDeadRuleEvidenceConfidence(ruleID string, evidence map[string]any, factors map[string]int) int {
	if evidence == nil || !isDeadRuleID(ruleID) {
		return 0
	}
	score := 0
	if incoming := evidenceInt(evidence["incoming_ref_count"]); incoming == 0 {
		score += 4
		factors["zero_inbound_refs"] = 4
	}
	if external := evidenceInt(evidence["external_non_test_refs"]); external == 0 {
		score += 4
		factors["zero_external_refs"] = 4
	}
	if sourceSample := evidenceStrings(evidence["source_sample"]); len(sourceSample) > 0 {
		score -= 4
		factors["source_sample_penalty"] = -4
	}
	return score
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
