package runtime

import (
	"fmt"
	"strings"
)

const BraidGraphPolicyLongCoTController = "longcot_controller"
const maxControllerCycleClusterSize = 6

func normalizeLongCoTControllerBraidGraph(g BraidGraph, maxNodes int) BraidGraph {
	g = normalizeLongCoTControllerVerify(g)
	g = normalizeLongCoTControllerFinalReduce(g)
	g = normalizeLongCoTControllerNodeCap(g, maxNodes)
	g = normalizeBraidGraphDependencies(g)
	byID := make(map[string]BraidNode, len(g.Nodes))
	for _, node := range g.Nodes {
		byID[node.ID] = node
	}
	for idx := range g.Nodes {
		if isBraidSolveKind(g.Nodes[idx].Kind) || g.Nodes[idx].Kind == "verify" {
			if g.Nodes[idx].HelperPolicy == "" || g.Nodes[idx].HelperPolicy == BraidNodeHelperPolicyAuto {
				g.Nodes[idx].HelperPolicy = BraidNodeHelperPolicyPreferred
			}
		}
		if g.Nodes[idx].Kind == "cycle_solve" {
			normalizeLongCoTCycleSolveNode(&g.Nodes[idx])
		}
		if g.Nodes[idx].Kind == "verify" {
			normalizeLongCoTVerifyNode(&g.Nodes[idx])
		}
		if g.Nodes[idx].Kind == "reduce" && g.Nodes[idx].ID == g.FinalNode {
			normalizeLongCoTFinalReduceDeps(&g.Nodes[idx], byID)
		}
	}
	g = normalizeBraidGraphDependencies(g)
	return g
}

func normalizeLongCoTCycleSolveNode(node *BraidNode) {
	if node == nil {
		return
	}
	if len(extractBraidCycleClustersFromAny(node.InputSchema["cycle_clusters"])) > 0 {
		return
	}
	targets := stringSliceFromAny(node.InputSchema["target_nodes"])
	if len(targets) < 2 {
		return
	}
	if node.InputSchema == nil {
		node.InputSchema = map[string]any{}
	}
	node.InputSchema["cycle_clusters"] = []any{stringSliceToAny(targets)}
}

func normalizeLongCoTControllerVerify(g BraidGraph) BraidGraph {
	if firstBraidNodeByKind(g.Nodes, "verify").ID != "" {
		return g
	}
	solve := lastBraidSolveNode(g.Nodes)
	if solve.ID == "" {
		return g
	}
	byID := make(map[string]BraidNode, len(g.Nodes)+1)
	for _, node := range g.Nodes {
		byID[node.ID] = node
	}
	verifyID := uniqueBraidNodeID(byID, "n_verify")
	g.Nodes = append(g.Nodes, BraidNode{
		ID:              verifyID,
		Kind:            "verify",
		Question:        "Substitute the candidate answer into the original constraints; report pass true or the first concrete failed constraint.",
		DependsOn:       []string{solve.ID},
		ExpectedOutput:  "pass true or first concrete failed constraint",
		MaxSummaryChars: 1200,
		HelperPolicy:    BraidNodeHelperPolicyPreferred,
		Archetype:       BraidScaffoldClassCandidateVerify,
		ScaffoldClass:   BraidScaffoldClassCandidateVerify,
		ScaffoldID:      BraidScaffoldIDPropertyCheckV1,
		InputSchema: map[string]any{
			"candidates": "candidate answers",
			"predicates": "original verification constraints",
		},
	})
	return g
}

func normalizeLongCoTControllerFinalReduce(g BraidGraph) BraidGraph {
	byID := make(map[string]BraidNode, len(g.Nodes))
	for _, node := range g.Nodes {
		byID[node.ID] = node
	}
	if final, ok := byID[g.FinalNode]; ok && final.Kind == "reduce" {
		return g
	}

	verify := firstBraidNodeByKind(g.Nodes, "verify")
	solve := firstBraidSolveNode(g.Nodes)
	if verify.ID == "" || solve.ID == "" {
		return g
	}

	if reduce := firstBraidNodeByKind(g.Nodes, "reduce"); reduce.ID != "" {
		for idx := range g.Nodes {
			if g.Nodes[idx].ID != reduce.ID {
				continue
			}
			if len(g.Nodes[idx].DependsOn) == 0 {
				g.Nodes[idx].DependsOn = []string{solve.ID, verify.ID}
			}
			g.FinalNode = reduce.ID
			return g
		}
	}

	reduceID := uniqueBraidNodeID(byID, "n_reduce")
	deps := []string{solve.ID, verify.ID}
	if len(verify.DependsOn) > 0 {
		deps = []string{verify.ID}
		for _, dep := range verify.DependsOn {
			if isBraidSolveKind(byID[dep].Kind) {
				deps = append(deps, dep)
			}
		}
	}
	g.Nodes = append(g.Nodes, BraidNode{
		ID:              reduceID,
		Kind:            "reduce",
		Question:        "Return the final answer only if verification passed; otherwise return concrete failed constraints.",
		DependsOn:       deps,
		ExpectedOutput:  "solution line or concrete failed constraints",
		MaxSummaryChars: 300,
		HelperPolicy:    BraidNodeHelperPolicyNever,
	})
	g.FinalNode = reduceID
	return g
}

func normalizeLongCoTControllerNodeCap(g BraidGraph, maxNodes int) BraidGraph {
	if maxNodes <= 0 || len(g.Nodes) <= maxNodes || maxNodes < 4 {
		return g
	}
	extract := firstBraidNodeByKind(g.Nodes, "extract")
	solve := firstBraidSolveNode(g.Nodes)
	verify := firstBraidNodeByKind(g.Nodes, "verify")
	reduce := firstBraidReduceNode(g.Nodes, g.FinalNode)
	if extract.ID == "" || solve.ID == "" || verify.ID == "" || reduce.ID == "" {
		return g
	}
	solve.DependsOn = filterBraidDepsToSelected(solve.DependsOn, map[string]bool{extract.ID: true})
	if len(solve.DependsOn) == 0 {
		solve.DependsOn = []string{extract.ID}
	}
	verify.DependsOn = []string{solve.ID}
	reduce.DependsOn = []string{solve.ID, verify.ID}
	g.Nodes = []BraidNode{extract, solve, verify, reduce}
	g.FinalNode = reduce.ID
	return g
}

func normalizeLongCoTFinalReduceDeps(node *BraidNode, byID map[string]BraidNode) {
	if node == nil || len(node.DependsOn) == 0 {
		return
	}
	seen := make(map[string]bool, len(node.DependsOn)+4)
	deps := make([]string, 0, len(node.DependsOn)+4)
	for _, depID := range node.DependsOn {
		if !seen[depID] {
			deps = append(deps, depID)
			seen[depID] = true
		}
		dep := byID[depID]
		if dep.Kind != "verify" {
			continue
		}
		for _, verifyDepID := range dep.DependsOn {
			if seen[verifyDepID] {
				continue
			}
			if isBraidSolveKind(byID[verifyDepID].Kind) {
				deps = append(deps, verifyDepID)
				seen[verifyDepID] = true
			}
		}
	}
	node.DependsOn = deps
}

func normalizeLongCoTVerifyNode(node *BraidNode) {
	if node == nil {
		return
	}
	if mentionsOriginalConstraints(node.Question + " " + node.ExpectedOutput) {
		return
	}
	const verifyQuestion = "Verify the candidate against the original task constraints by simulation or substitution."
	const verifyExpected = "pass true only if every original constraint, rule, and goal is satisfied"
	if strings.TrimSpace(node.Question) == "" || len(node.Question) > maxBraidNodeQuestionChars-len(" "+verifyQuestion) {
		node.Question = verifyQuestion
	} else {
		node.Question = strings.TrimSpace(node.Question) + " " + verifyQuestion
	}
	if strings.TrimSpace(node.ExpectedOutput) == "" || len(node.ExpectedOutput) > maxBraidNodeExpectedChars-len("; "+verifyExpected) {
		node.ExpectedOutput = verifyExpected
	} else {
		node.ExpectedOutput = strings.TrimSpace(node.ExpectedOutput) + "; " + verifyExpected
	}
	if len(node.Question) > maxBraidNodeQuestionChars {
		node.Question = verifyQuestion
	}
	if len(node.ExpectedOutput) > maxBraidNodeExpectedChars {
		node.ExpectedOutput = verifyExpected
	}
}

func validateLongCoTControllerBraidGraph(g BraidGraph) error {
	if len(g.Nodes) < 4 {
		return fmt.Errorf("braid graph: longcot_controller requires at least 4 nodes")
	}
	byID := make(map[string]BraidNode, len(g.Nodes))
	byKind := map[string][]BraidNode{}
	for _, node := range g.Nodes {
		byID[node.ID] = node
		byKind[node.Kind] = append(byKind[node.Kind], node)
	}
	for _, kind := range []string{"extract", "verify", "reduce"} {
		if len(byKind[kind]) == 0 {
			return fmt.Errorf("braid graph: longcot_controller requires a %s node", kind)
		}
	}
	if countBraidSolveNodes(byKind) < 1 {
		return fmt.Errorf("braid graph: longcot_controller requires at least one solve-like node")
	}
	for _, solve := range append(append([]BraidNode{}, byKind["solve"]...), byKind["cycle_solve"]...) {
		if err := validateLongCoTControllerSolveTargetContract(solve); err != nil {
			return err
		}
	}
	final, ok := byID[g.FinalNode]
	if !ok || final.Kind != "reduce" {
		return fmt.Errorf("braid graph: longcot_controller final_node must be a reduce node")
	}
	if len(final.DependsOn) == 0 {
		return fmt.Errorf("braid graph: longcot_controller reduce node must depend on verification")
	}
	if !anyDepKind(final.DependsOn, byID, "verify") {
		return fmt.Errorf("braid graph: longcot_controller reduce node must depend on a verify node")
	}
	for _, verify := range byKind["verify"] {
		if len(verify.DependsOn) == 0 {
			return fmt.Errorf("braid graph: longcot_controller verify node %q must depend on a solve-like node", verify.ID)
		}
		if !anyDepSolveKind(verify.DependsOn, byID) {
			return fmt.Errorf("braid graph: longcot_controller verify node %q must depend on a solve-like node", verify.ID)
		}
		if !mentionsOriginalConstraints(verify.Question + " " + verify.ExpectedOutput) {
			return fmt.Errorf("braid graph: longcot_controller verify node %q must check original constraints, not prior summary only", verify.ID)
		}
	}
	return nil
}

func validateLongCoTControllerSolveTargetContract(node BraidNode) error {
	if node.Kind == "cycle_solve" {
		clusters := extractBraidCycleClustersFromAny(node.InputSchema["cycle_clusters"])
		if len(clusters) == 0 {
			return fmt.Errorf("braid graph: longcot_controller cycle_solve node %q must declare input_schema.cycle_clusters", node.ID)
		}
		for _, cluster := range clusters {
			if len(cluster) > maxControllerCycleClusterSize {
				return fmt.Errorf("braid graph: longcot_controller cycle_solve node %q declares overbroad cycle cluster of %d targets; split into smaller strongly connected clusters or independent solve_targets", node.ID, len(cluster))
			}
		}
		clusterTargets := map[string]struct{}{}
		for _, cluster := range clusters {
			for _, target := range cluster {
				target = strings.TrimSpace(target)
				if target != "" {
					clusterTargets[target] = struct{}{}
				}
			}
		}
		for _, target := range stringSliceFromAny(node.InputSchema["target_nodes"]) {
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			if _, ok := clusterTargets[target]; !ok {
				return fmt.Errorf("braid graph: longcot_controller cycle_solve node %q targets non-cycle node %q; cycle_solve target_nodes must be limited to declared cycle_clusters", node.ID, target)
			}
		}
	}
	if node.Kind == "solve" && len(stringSliceFromAny(node.InputSchema["target_nodes"])) > 1 {
		if len(stringSliceFromAny(node.InputSchema["solve_targets"])) == 0 &&
			len(stringSliceFromAny(node.InputSchema["nodes_to_solve"])) == 0 &&
			len(extractBraidCycleClustersFromAny(node.InputSchema["cycle_clusters"])) == 0 &&
			!braidExplicitDAGInputHasRuntimeCheck(node.InputSchema) {
			return fmt.Errorf("braid graph: longcot_controller solve node %q with multiple target_nodes must declare solve_targets, nodes_to_solve, cycle_clusters, or a runtime-checkable input schema", node.ID)
		}
	}
	if node.Kind == "solve" &&
		strings.TrimSpace(node.ScaffoldClass) == BraidScaffoldClassExplicitDAG &&
		strings.TrimSpace(node.ScaffoldID) == BraidScaffoldIDSearchBacktrackV1 &&
		len(stringSliceFromAny(node.InputSchema["target_nodes"])) == 0 &&
		len(stringSliceFromAny(node.InputSchema["solve_targets"])) == 0 &&
		len(stringSliceFromAny(node.InputSchema["nodes_to_solve"])) == 0 &&
		len(extractBraidCycleClustersFromAny(node.InputSchema["cycle_clusters"])) == 0 &&
		!braidExplicitDAGInputHasRuntimeCheck(node.InputSchema) {
		mentionedTargets := extractBraidNodeIDsFromAny(map[string]any{
			"question":        node.Question,
			"expected_output": node.ExpectedOutput,
			"input_schema":    node.InputSchema,
		})
		if len(mentionedTargets) > 1 {
			return fmt.Errorf("braid graph: longcot_controller explicit_dag solve node %q mentions multiple work items but must declare target_nodes, solve_targets, nodes_to_solve, cycle_clusters, or a runtime-checkable input schema", node.ID)
		}
	}
	return nil
}
