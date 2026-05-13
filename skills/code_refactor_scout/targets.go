package main

import "strings"

const (
	targetAll                         = "all"
	targetSmallComposableCode         = "small-composable-code"
	targetSemanticCommenting          = "semantic-commenting"
	targetImproveCodebaseArchitecture = "improve-codebase-architecture"
)

func reviewTargetNames() []string {
	return []string{
		targetSmallComposableCode,
		targetSemanticCommenting,
		targetImproveCodebaseArchitecture,
	}
}

// applyReviewTargets annotates deterministic scout findings with follow-up skill lanes.
//
// Index:
//
//	Purpose: Routes refactor scout findings to follow-up review skills without changing discovery rules.
//	Keywords: refactor scout targets, small composable code, semantic commenting, architecture deepening
//	Related: reviewTargetsForFinding, buildSkillTargetLanes, applyTarget
//	OutputFields: targets, target_reasons, review_targets
//
// [[domain:refactor-scout-targeting]]
// [[decision:target-lenses-derive-from-rule-ids]]
// [[doc:docs/general/refactor-scout.md#Scout Output Contract]]
// [[test:skills/code_refactor_scout/targets_test.go#TestReviewTargetsForFindingReasonOrderStable]]
func applyReviewTargets(items []finding) []finding {
	out := append([]finding(nil), items...)
	for i := range out {
		targets, reasons := reviewTargetsForFinding(out[i])
		out[i].Targets = targets
		out[i].TargetReasons = reasons
		if len(targets) == 0 {
			continue
		}
		if out[i].Evidence == nil {
			out[i].Evidence = map[string]any{}
		}
		out[i].Evidence["review_targets"] = targets
		out[i].Evidence["review_target_reasons"] = reasons
	}
	return out
}

func applyTarget(items []finding, target string) []finding {
	target = strings.TrimSpace(target)
	if target == "" || target == targetAll || len(items) == 0 {
		return items
	}
	filtered := make([]finding, 0, len(items))
	for _, item := range items {
		if findingTargetsReview(item, target) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func reviewTargetsForFinding(item finding) ([]string, []string) {
	var targets []string
	var reasons []string

	add := func(target, reason string) {
		target = strings.TrimSpace(target)
		reason = strings.TrimSpace(reason)
		if target == "" {
			return
		}
		for _, existing := range targets {
			if existing == target {
				return
			}
		}
		targets = append(targets, target)
		if reason != "" {
			reasons = append(reasons, target+": "+reason)
		}
	}

	if isSmallComposableTarget(item) {
		add(targetSmallComposableCode, "reduce sprawl through a smaller, behavior-preserving module shape")
	}
	if isSemanticCommentingTarget(item) {
		add(targetSemanticCommenting, "inspect the owner for one or two evidence-only anchors or an Index block")
	}
	if isArchitectureTarget(item) {
		add(targetImproveCodebaseArchitecture, "evaluate whether this shallow module should become a deeper module with a clearer seam")
	}

	return targets, reasons
}

func findingTargetsReview(item finding, target string) bool {
	for _, current := range item.Targets {
		if current == target {
			return true
		}
	}
	return false
}

func isSmallComposableTarget(item finding) bool {
	switch item.RuleID {
	case "long_parameter_list", "boolean_parameter", "wide_return_tuple",
		"oversized_function", "high_cyclomatic_complexity", "deep_nesting",
		"oversized_symbol", "fan_out_dependency_spread", "duplicate_recovery_block",
		"duplicated_error_remap", "repeated_guard_ladder", "semantic_simplification_candidate",
		"duplicate_orchestration_fingerprint", "same_file_extraction_candidate",
		"function_hotspot", "complexity_cluster", "receiver_hotspot", "god_file",
		"preload_after_get_chain", "post_transaction_preload", "transaction_script_hotspot":
		return true
	default:
		return isDeadFinding(item)
	}
}

func isSemanticCommentingTarget(item finding) bool {
	switch item.RuleID {
	case "function_hotspot", "complexity_cluster", "receiver_hotspot", "wide_interface",
		"god_file", "structural_similarity_cluster", "structural_similarity_module_cluster",
		"call_family_cluster", "call_family_module_cluster", "fan_out_dependency_spread",
		"preload_after_get_chain", "post_transaction_preload", "transaction_script_hotspot":
		return true
	default:
		return item.Score >= 80 && (item.Category == "function" || item.Category == "type" || item.Category == "file")
	}
}

func isArchitectureTarget(item finding) bool {
	switch item.RuleID {
	case "function_hotspot", "fan_out_dependency_spread", "same_file_extraction_candidate",
		"complexity_cluster", "receiver_hotspot", "wide_interface", "god_file",
		"structural_similarity_cluster", "structural_similarity_module_cluster",
		"call_family_cluster", "call_family_module_cluster", "preload_after_get_chain",
		"post_transaction_preload", "transaction_script_hotspot":
		return true
	default:
		return false
	}
}
