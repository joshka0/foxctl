package runtime

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/rlm/runtime/generalsolver"
	"github.com/joshka0/foxctl/internal/runtime/engine"
)

func TestBraidValidateGraphAcceptsValidGraph(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract constraints."},
			{ID: "n2", Kind: "cycle_solve", Question: "Solve mutually dependent constraints.", DependsOn: []string{"n1"}},
			{ID: "n3", Kind: "reduce", DependsOn: []string{"n2"}},
		},
		FinalNode: "n3",
	}

	if err := ValidateBraidGraph(graph, 8); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
}

func TestBraidValidateGraphRejectsInvalidGraphs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		graph   BraidGraph
		max     int
		wantErr string
	}{
		{
			name: "duplicate ids",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "extract", Question: "a"},
					{ID: "n1", Kind: "solve", Question: "b"},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "duplicate node id",
		},
		{
			name: "unknown dep",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: "a", DependsOn: []string{"missing"}},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "depends on unknown node",
		},
		{
			name: "cycle",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "extract", Question: "a", DependsOn: []string{"n2"}},
					{ID: "n2", Kind: "solve", Question: "b", DependsOn: []string{"n1"}},
				},
				FinalNode: "n2",
			},
			max:     8,
			wantErr: "cycle detected",
		},
		{
			name: "too many nodes",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "extract", Question: "a"},
					{ID: "n2", Kind: "solve", Question: "b"},
				},
				FinalNode: "n2",
			},
			max:     1,
			wantErr: "exceeds max",
		},
		{
			name: "invalid kind",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "plan", Question: "a"},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "invalid kind",
		},
		{
			name: "summary cap",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: "a", MaxSummaryChars: maxBraidNodeSummaryChars + 1},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "max_summary_chars",
		},
		{
			name: "invalid helper policy",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: "a", HelperPolicy: "sometimes"},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "helper_policy",
		},
		{
			name: "required helper policy on extract",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "extract", Question: "a", HelperPolicy: BraidNodeHelperPolicyRequired},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "helper_policy required",
		},
		{
			name: "question cap",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: strings.Repeat("q", maxBraidNodeQuestionChars+1)},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "question length",
		},
		{
			name: "expected output cap",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: "Solve.", ExpectedOutput: strings.Repeat("e", maxBraidNodeExpectedChars+1)},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "expected_output length",
		},
		{
			name: "runtime token in question",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: "Call rlm_query to solve the dependency cluster."},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "forbidden runtime token",
		},
		{
			name: "runtime token in expected output",
			graph: BraidGraph{
				Version: 1,
				Nodes: []BraidNode{
					{ID: "n1", Kind: "solve", Question: "Solve cluster.", ExpectedOutput: "blocked if remaining depth is zero"},
				},
				FinalNode: "n1",
			},
			max:     8,
			wantErr: "forbidden runtime token",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBraidGraph(tc.graph, tc.max)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateBraidGraph() err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestBraidValidateLongCoTControllerTreatsCycleSolveAsSolveLike(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{ID: "n_cycle", Kind: "cycle_solve", Question: "Solve mutual constraints.", DependsOn: []string{"n_extract"}, InputSchema: map[string]any{"cycle_clusters": []any{[]any{"node_2", "node_5"}}}},
			{ID: "n_solve", Kind: "solve", Question: "Solve target.", DependsOn: []string{"n_cycle"}},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_cycle", "n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	if err := ValidateBraidGraph(graph, 6); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestNormalizeLongCoTControllerFillsCycleClustersFromCycleTargets(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{
				ID:        "n_cycle",
				Kind:      "cycle_solve",
				Question:  "Solve mutual constraints.",
				DependsOn: []string{"n_extract"},
				InputSchema: map[string]any{
					"target_nodes": []any{"node_2", "node_5", "node_6", "node_7"},
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_cycle"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_cycle", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	graph = NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	cycle, ok := braidGraphNodeByID(graph, "n_cycle")
	if !ok {
		t.Fatal("n_cycle missing")
	}
	if got := extractBraidCycleClustersFromAny(cycle.InputSchema["cycle_clusters"]); !reflect.DeepEqual(got, [][]string{{"node_2", "node_5", "node_6", "node_7"}}) {
		t.Fatalf("cycle_clusters=%v", got)
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestBraidValidateLongCoTControllerRejectsOverbroadCycleCluster(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{
				ID:        "n_cycle",
				Kind:      "cycle_solve",
				Question:  "Solve the entire dependency graph as one cluster.",
				DependsOn: []string{"n_extract"},
				InputSchema: map[string]any{
					"cycle_clusters": []any{[]any{"node_0", "node_1", "node_2", "node_3", "node_4", "node_5", "node_6", "node_7"}},
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_cycle"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_cycle", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController)
	if err == nil {
		t.Fatal("ValidateBraidGraphPolicy() accepted overbroad cycle cluster")
	}
	if !strings.Contains(err.Error(), "overbroad cycle cluster") {
		t.Fatalf("err=%v, want overbroad cycle cluster", err)
	}
}

func TestBraidValidateLongCoTControllerRejectsCycleSolveNonClusterTargets(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{
				ID:        "n_cycle",
				Kind:      "cycle_solve",
				Question:  "Solve one fixed-point cluster.",
				DependsOn: []string{"n_extract"},
				InputSchema: map[string]any{
					"target_nodes":   []any{"node_0", "node_2", "node_5"},
					"cycle_clusters": []any{[]any{"node_2", "node_5"}},
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_cycle"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_cycle", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController)
	if err == nil {
		t.Fatal("ValidateBraidGraphPolicy() accepted cycle_solve targeting non-cycle node")
	}
	if !strings.Contains(err.Error(), "targets non-cycle node") {
		t.Fatalf("err=%v, want targets non-cycle node", err)
	}
}

func TestBraidValidateLongCoTControllerRejectsUndeclaredMultiTargetSolve(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{
				ID:          "n_solve",
				Kind:        "solve",
				Question:    "Solve multiple dependency nodes.",
				DependsOn:   []string{"n_extract"},
				InputSchema: map[string]any{"target_nodes": []any{"node_0", "node_1", "node_2"}},
			},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController)
	if err == nil {
		t.Fatal("ValidateBraidGraphPolicy() succeeded for undeclared multi-target solve")
	}
	if !strings.Contains(err.Error(), "must declare solve_targets") {
		t.Fatalf("ValidateBraidGraphPolicy() err=%v, want solve_targets contract", err)
	}
}

func TestBraidValidateLongCoTControllerRejectsImplicitMultiWorkItemSolve(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract facts."},
			{
				ID:            "n_solve",
				Kind:          "solve",
				Question:      "Solve node_0, node_1, node_2, and node_3 sequentially.",
				DependsOn:     []string{"n_extract"},
				ScaffoldClass: BraidScaffoldClassExplicitDAG,
				ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"prompt": "Evaluate node_0 through node_3.",
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify against original constraints.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController)
	if err == nil || !strings.Contains(err.Error(), "mentions multiple work items") {
		t.Fatalf("err=%v want implicit multi-work-item contract error", err)
	}
}

func TestBraidValidateLongCoTControllerAllowsDeclaredMultiTargetSolveTargets(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{
				ID:          "n_solve",
				Kind:        "solve",
				Question:    "Solve declared independent work items.",
				DependsOn:   []string{"n_extract"},
				InputSchema: map[string]any{"target_nodes": []any{"node_0", "node_1"}, "solve_targets": []any{"node_0", "node_1"}},
			},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestBraidValidateLongCoTControllerAllowsMissingCycleSolve(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{ID: "n_solve_a", Kind: "solve", Question: "Solve candidate a.", DependsOn: []string{"n_extract"}},
			{ID: "n_solve_b", Kind: "solve", Question: "Solve candidate b.", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_solve_a", "n_solve_b"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve_a", "n_solve_b", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	if err := ValidateBraidGraph(graph, 5); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestBraidValidateLongCoTControllerAllowsStateSimulationVerify(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract states."},
			{ID: "n_solve_a", Kind: "solve", Question: "Plan clearing moves.", DependsOn: []string{"n_extract"}},
			{ID: "n_solve_b", Kind: "solve", Question: "Plan build moves.", DependsOn: []string{"n_extract", "n_solve_a"}},
			{ID: "n_verify", Kind: "verify", Question: "Simulate full move sequence on initial state and check final state matches goal state.", DependsOn: []string{"n_solve_a", "n_solve_b"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve_b", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	if err := ValidateBraidGraph(graph, 5); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestBraidValidateLongCoTControllerAllowsSingleSolve(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{ID: "n_solve", Kind: "solve", Question: "Solve candidate.", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestNormalizeBraidGraphForPolicyKeepsSingleSolveAndClampsSummary(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints.", MaxSummaryChars: maxBraidNodeSummaryChars + 1},
			{ID: "n_solve", Kind: "solve", Question: "Solve candidate.", DependsOn: []string{"n_extract"}, MaxSummaryChars: maxBraidNodeSummaryChars + 1},
			{ID: "n_verify", Kind: "verify", Question: "Simulate candidate on initial state and check final state matches goal state.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	if len(normalized.Nodes) != 4 {
		t.Fatalf("normalized node count = %d, want 4", len(normalized.Nodes))
	}
	if normalized.Nodes[0].MaxSummaryChars != maxBraidNodeSummaryChars {
		t.Fatalf("summary cap was not clamped: %d", normalized.Nodes[0].MaxSummaryChars)
	}
	for _, idx := range []int{1, 2} {
		if normalized.Nodes[idx].HelperPolicy != BraidNodeHelperPolicyPreferred {
			t.Fatalf("node %s helper_policy=%q want preferred", normalized.Nodes[idx].ID, normalized.Nodes[idx].HelperPolicy)
		}
	}
	if err := ValidateBraidGraph(normalized, 8); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(normalized, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestNormalizeBraidGraphForPolicyRepairsWeakVerifyContract(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract compact facts."},
			{ID: "n_solve", Kind: "solve", Question: "Solve candidate.", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", Question: "Check n_solve summary.", ExpectedOutput: "pass or fail", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err == nil {
		t.Fatal("ValidateBraidGraphPolicy() succeeded before normalization")
	}
	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	if err := ValidateBraidGraph(normalized, 8); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(normalized, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
	verify := normalized.Nodes[2]
	if !strings.Contains(strings.ToLower(verify.Question+" "+verify.ExpectedOutput), "original") {
		t.Fatalf("verify node was not rewritten with original-constraint contract: %#v", verify)
	}
}

func TestNormalizeBraidGraphForPolicyAddsSolveDependencyToFinalReduce(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract state."},
			{ID: "n_solve", Kind: "solve", Question: "Build candidate.", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", Question: "Simulate candidate against original constraints.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Format final answer.", DependsOn: []string{"n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	reduce := normalized.Nodes[3]
	if !dependsOnBraidNode(reduce, "n_verify") {
		t.Fatalf("reduce lost verify dependency: %#v", reduce.DependsOn)
	}
	if !dependsOnBraidNode(reduce, "n_solve") {
		t.Fatalf("reduce did not inherit verified solve dependency: %#v", reduce.DependsOn)
	}
	if err := ValidateBraidGraph(normalized, 8); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(normalized, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestNormalizeBraidGraphForPolicySynthesizesMissingFinalReduce(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract facts."},
			{ID: "n_solve_a", Kind: "solve", Question: "Solve dependency A.", DependsOn: []string{"n_extract"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "a"}},
			{ID: "n_solve_b", Kind: "solve", Question: "Solve dependency B.", DependsOn: []string{"n_solve_a"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "b"}},
			{ID: "n_verify", Kind: "verify", Question: "Verify candidate against original constraints.", DependsOn: []string{"n_solve_b"}, Archetype: "candidate_verify", ScaffoldClass: BraidScaffoldClassCandidateVerify, ScaffoldID: BraidScaffoldIDPropertyCheckV1, InputSchema: map[string]any{"candidates": "candidate"}},
		},
		FinalNode: "n_verify",
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 12)
	if normalized.FinalNode != "n_reduce" {
		t.Fatalf("FinalNode=%q want n_reduce", normalized.FinalNode)
	}
	reduce := BraidNode{}
	for _, node := range normalized.Nodes {
		if node.ID == normalized.FinalNode {
			reduce = node
		}
	}
	if reduce.Kind != "reduce" {
		t.Fatalf("final node = %#v, want reduce", reduce)
	}
	if !dependsOnBraidNode(reduce, "n_verify") || !dependsOnBraidNode(reduce, "n_solve_b") {
		t.Fatalf("reduce deps=%v want verify and verified solve", reduce.DependsOn)
	}
	if err := ValidateBraidGraph(normalized, 12); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(normalized, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestNormalizeBraidGraphForPolicySynthesizesMissingVerifyAndReduce(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract facts."},
			{ID: "n_solve_a", Kind: "solve", Question: "Solve A.", DependsOn: []string{"n_extract"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "a"}},
			{ID: "n_solve_b", Kind: "solve", Question: "Solve B.", DependsOn: []string{"n_solve_a"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "b"}},
		},
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	verify, ok := braidGraphNodeByID(normalized, "n_verify")
	if !ok {
		t.Fatal("missing synthesized verify node")
	}
	if !reflect.DeepEqual(verify.DependsOn, []string{"n_solve_b"}) {
		t.Fatalf("verify deps=%v want [n_solve_b]", verify.DependsOn)
	}
	reduce, ok := braidGraphNodeByID(normalized, normalized.FinalNode)
	if !ok || reduce.Kind != "reduce" {
		t.Fatalf("final node=%q node=%#v want reduce", normalized.FinalNode, reduce)
	}
	if !dependsOnBraidNode(reduce, "n_verify") || !dependsOnBraidNode(reduce, "n_solve_b") {
		t.Fatalf("reduce deps=%v want verify and last solve", reduce.DependsOn)
	}
	if err := ValidateBraidGraph(normalized, 8); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(normalized, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestNormalizeBraidGraphForPolicyPrunesUnknownDependencies(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract recurrence constraints."},
			{ID: "n_solve_5", Kind: "solve", Question: "Solve node 5.", DependsOn: []string{"n_extract"}},
			{ID: "n_solve_7", Kind: "solve", Question: "Solve node 7.", DependsOn: []string{"n_solve_6", "n_solve_5", "n_solve_5", "n_solve_7"}},
			{ID: "n_verify", Kind: "verify", Question: "Verify solution against original constraints.", DependsOn: []string{"n_solve_7"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Format final answer.", DependsOn: []string{"n_solve_7", "n_verify", "missing_reduce_dep"}},
		},
		FinalNode: "n_reduce",
	}
	if err := ValidateBraidGraph(graph, 8); err == nil {
		t.Fatal("ValidateBraidGraph() succeeded before normalization")
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	solve := normalized.Nodes[2]
	if !reflect.DeepEqual(solve.DependsOn, []string{"n_solve_5"}) {
		t.Fatalf("n_solve_7 deps = %#v, want only n_solve_5", solve.DependsOn)
	}
	reduce := normalized.Nodes[4]
	if dependsOnBraidNode(reduce, "missing_reduce_dep") {
		t.Fatalf("reduce kept unknown dependency: %#v", reduce.DependsOn)
	}
	if err := ValidateBraidGraph(normalized, 8); err != nil {
		t.Fatalf("ValidateBraidGraph() error = %v", err)
	}
	if err := ValidateBraidGraphPolicy(normalized, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("ValidateBraidGraphPolicy() error = %v", err)
	}
}

func TestBraidValidateLongCoTControllerRejectsShallowRubberStamp(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "solve", Question: "Solve all."},
			{ID: "n2", Kind: "verify", Question: "Check consistency with n1 summary.", DependsOn: []string{"n1"}},
		},
		FinalNode: "n2",
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err == nil {
		t.Fatal("ValidateBraidGraphPolicy() succeeded for shallow solve/verify graph")
	}
}

func TestBraidNodeShouldForceHelperForBlockedStateSearch(t *testing.T) {
	t.Parallel()

	if !braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "solve"}, "status: blocked checks: requires state-space search and simulation") {
		t.Fatal("expected blocked solve search summary to force helper")
	}
	if !braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "verify"}, "status: blocked checks: needs executable code to simulate dynamic state") {
		t.Fatal("expected blocked verify simulation summary to force helper")
	}
	if !braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "verify"}, "status: solved answer: solution = [[1,0,1]] checks: echoed candidate") {
		t.Fatal("expected verify summary without pass signal to force helper")
	}
	if braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "verify"}, "status: pass answer: pass: true checks: final state matches") {
		t.Fatal("verified pass summary should not force helper")
	}
	if !braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "solve"}, "status: partial answer: candidate prefix checks: cannot generate exact move sequence without computational planning assistance") {
		t.Fatal("expected partial solve requesting computational planning to force helper")
	}
	if braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "solve"}, "status: solved answer: solution = [] checks: generated by a search procedure") {
		t.Fatal("solved summary that merely mentions search should not force helper")
	}
	if braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "solve", HelperPolicy: BraidNodeHelperPolicyNever}, "status: blocked checks: requires state-space search") {
		t.Fatal("helper_policy=never should suppress helper recovery")
	}
	if braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "extract"}, "status: blocked checks: requires search") {
		t.Fatal("extract nodes should not force helper")
	}
	if braidNodeShouldForceHelper(BraidNode{ID: "n", Kind: "solve"}, "status: partial answer: candidate prefix") {
		t.Fatal("non-blocked solve should not force helper")
	}
}

func TestBraidCycleSolveUsesHelperBeforeChildREPL(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_cycle",
		Kind:           "cycle_solve",
		ScaffoldClass:  BraidScaffoldClassExplicitDAG,
		ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
		ExpectedOutput: "consistent node values",
	}
	if braidNodeShouldUsePythonREPLFirst(node) {
		t.Fatal("cycle_solve should not use target-extraction python_repl shortcut")
	}
	if braidNodeShouldUseChildREPLInsteadOfHelper(node, "root", nil, "") {
		t.Fatal("cycle_solve should not bypass helper-first for generic explicit_dag")
	}
}

func TestBraidNodeCanFallbackFromPreferredCycleHelperFailure(t *testing.T) {
	t.Parallel()

	if !braidNodeCanFallbackFromHelperFailure(BraidNode{Kind: "cycle_solve"}, BraidNodeHelperPolicyPreferred) {
		t.Fatal("preferred cycle_solve helper failure should allow child fallback")
	}
	if braidNodeCanFallbackFromHelperFailure(BraidNode{Kind: "cycle_solve"}, BraidNodeHelperPolicyRequired) {
		t.Fatal("required cycle_solve helper failure should remain terminal")
	}
	if !braidNodeCanFallbackFromHelperFailure(BraidNode{Kind: "solve"}, BraidNodeHelperPolicyPreferred) {
		t.Fatal("preferred solve helper failure should allow child fallback")
	}
}

func TestBraidVerifySummaryRequiresPassSignal(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_verify", Kind: "verify"},
		"status: completed summary: status: solved answer: solution = [[1,0,1]] checks: echoed candidate",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for verify without pass signal")
	}
	if !strings.Contains(err.Error(), "did not report verification pass") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want missing pass signal", err)
	}

	err = validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_verify", Kind: "verify"},
		"status: completed summary: status: pass answer: pass checks: all moves valid and final state matches goal",
		"n_reduce",
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() pass summary error = %v", err)
	}

	err = validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_verify", Kind: "verify"},
		"status: completed summary: status: solved answer: solution = 42 checks: verified 17+25=42",
		"n_reduce",
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() verified-check summary error = %v", err)
	}
}

func TestBraidNodeArtifactValidation(t *testing.T) {
	t.Parallel()

	verifyArtifact := `{"status":"pass","answer":"pass: true","checks":["verified original constraints"],"confidence":0.95}`
	if err := validateBraidNodeExecutionSummary("graph_fanout", BraidNode{ID: "n_verify", Kind: "verify"}, verifyArtifact, "n_reduce"); err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() structured verify error = %v", err)
	}

	solveArtifact := `{"status":"solved","answer":"solution = {\"node_2\":1071}","checks":["computed deterministically"],"confidence":0.9}`
	node := BraidNode{ID: "n_solve__adaptive_00_node_2", Kind: "solve"}
	if err := validateBraidNodeExecutionSummary("graph_fanout", node, solveArtifact, "n_reduce"); err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() structured solve error = %v", err)
	}

	blockedArtifact := `{"status":"blocked","answer":"","checks":["missing dependency"],"confidence":0.2}`
	if err := validateBraidNodeExecutionSummary("graph_fanout", BraidNode{ID: "n_solve", Kind: "solve"}, blockedArtifact, "n_reduce"); err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() accepted structured blocked artifact")
	}
}

func TestBraidCycleSolveArtifactValidation(t *testing.T) {
	t.Parallel()

	artifact := `{"status":"solved","answer":"cycle_json: {\"pass\":true,\"candidates\":{\"node_2\":1071},\"checks\":[{\"name\":\"fixed_point\",\"ok\":true,\"observed\":6,\"expected\":6}]}","checks":["bounded fixed-point search passed"],"confidence":0.9}`
	if err := validateBraidNodeExecutionSummary("graph_fanout", BraidNode{ID: "n_cycle", Kind: "cycle_solve"}, artifact, "n_reduce"); err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() structured cycle_solve error = %v", err)
	}
}

func TestHelperAnswerFromToolResultRequiresOKAnswer(t *testing.T) {
	t.Parallel()

	if got := helperAnswerFromToolResult(`{"ok":true,"answer":"solution = []"}`); got != "solution = []" {
		t.Fatalf("helperAnswerFromToolResult()=%q", got)
	}
	if got := helperAnswerFromToolResult(`{"ok":false,"error":"compile failed"}`); got != "" {
		t.Fatalf("helperAnswerFromToolResult()=%q, want empty for failed helper", got)
	}
}

func TestHelperFactoryVerifierAnswerDoesNotRequireSolutionLine(t *testing.T) {
	t.Parallel()

	answer, ok := helperFactoryAnswer(map[string]any{
		"ok":     true,
		"answer": "pass: false first_failure: candidate is scalar but expected tuple",
	}, false)
	if !ok {
		t.Fatal("helperFactoryAnswer() rejected verifier diagnostic")
	}
	if !strings.Contains(answer, "pass: false") {
		t.Fatalf("answer=%q, want verifier diagnostic", answer)
	}
	if _, ok := helperFactoryAnswer(map[string]any{
		"ok":     true,
		"answer": "pass: false first_failure: candidate is scalar but expected tuple",
	}, true); ok {
		t.Fatal("helperFactoryAnswer() accepted verifier diagnostic as final solution")
	}
}

func TestFormatBraidHelperVerifyMismatchBlocksRepair(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummary(BraidNode{ID: "n_verify", Kind: "verify"}, "Final state mismatch")
	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "pass: false") {
		t.Fatalf("summary=%q, want pass false", summary)
	}
	if strings.Contains(summary, "pass: true") {
		t.Fatalf("summary=%q, must not contain pass true", summary)
	}
	if err := validateBraidNodeExecutionSummary("graph_fanout", BraidNode{ID: "n_verify", Kind: "verify"}, summary, "n_reduce"); err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for verify mismatch")
	}
}

func TestFormatBraidHelperSolveSelfVerificationFailureBlocksRepair(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummary(BraidNode{ID: "n_solve", Kind: "solve"}, "pass: false first_failure: move 3 is illegal")
	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "self-verified candidate") {
		t.Fatalf("summary=%q, want self-verification marker", summary)
	}
	if err := validateBraidNodeExecutionSummary("graph_fanout", BraidNode{ID: "n_solve", Kind: "solve"}, summary, "n_reduce"); err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for solve self-verification failure")
	}
}

func TestBuildBraidHelperRecoveryInstructionsRequireConcreteVerifierFailure(t *testing.T) {
	t.Parallel()

	verify := buildBraidHelperRecoveryInstructions(BraidNode{ID: "n_verify", Kind: "verify"}, "failed")
	for _, want := range []string{"pass: false first_failure", "failed step index"} {
		if !strings.Contains(verify, want) {
			t.Fatalf("verify instructions missing %q:\n%s", want, verify)
		}
	}
	cycle := buildBraidHelperRecoveryInstructions(BraidNode{ID: "n_cycle", Kind: "cycle_solve"}, "failed")
	for _, want := range []string{"cycle_json:", "Do not return `solution =`", "Do not return the candidate map directly", `"pass":true`, `"candidates"`, `"checks"`, `{"ok": True, "answer": "cycle_json:`} {
		if !strings.Contains(cycle, want) {
			t.Fatalf("cycle_solve instructions missing %q:\n%s", want, cycle)
		}
	}
	solve := buildBraidHelperRecoveryInstructions(BraidNode{ID: "n_solve", Kind: "solve"}, "failed")
	for _, want := range []string{"internal deterministic check", "partial action list", "state explicitly", "do not run exhaustive BFS/DFS"} {
		if !strings.Contains(solve, want) {
			t.Fatalf("solve instructions missing %q:\n%s", want, solve)
		}
	}
}

func TestTransitionSystemHelperContract(t *testing.T) {
	t.Parallel()

	if !braidHelperInputLooksLikeTransitionSystem(map[string]any{
		"initial_state": []any{},
		"goal_state":    []any{},
	}) {
		t.Fatal("expected initial_state/goal_state input to look like a transition system")
	}
	if braidHelperInputLooksLikeTransitionSystem(map[string]any{"initial_state": []any{}}) {
		t.Fatal("expected missing goal_state to not look like a transition system")
	}
	contract := buildTransitionSystemHelperContract()
	for _, want := range []string{"syntactically valid Python", "parse_state", "legal_actions", "apply", "is_goal", "verify_plan", "search_or_construct", "move(src, dst)", "src == dst", "ok:false with first_failure"} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract missing %q:\n%s", want, contract)
		}
	}
}

func TestGraphSearchHelperContractCoversExplicitShortestPath(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"nodes":      []any{"A", "B", "C", "D"},
		"edges":      []any{[]any{"A", "B"}, []any{"B", "D"}, []any{"A", "C"}, []any{"C", "D"}},
		"start_node": "A",
		"goal_node":  "D",
		"objective":  "shortest_path_length",
	}
	if !braidHelperInputLooksLikeExplicitShortestPath(input) {
		t.Fatal("expected explicit nodes/edges/start/goal input to look like shortest-path graph search")
	}
	diag, applicable := explicitShortestPathAnswerVerifier("solution = 3", input)
	if !applicable || diag.Pass || diag.ExpectedFinal != 2 {
		t.Fatalf("wrong candidate diagnostic=%#v applicable=%v", diag, applicable)
	}
	diag, applicable = explicitShortestPathAnswerVerifier("solution = 2", input)
	if !applicable || !diag.Pass {
		t.Fatalf("valid candidate diagnostic=%#v applicable=%v", diag, applicable)
	}
	contract := buildGraphSearchHelperContract()
	for _, want := range []string{"nodes", "directed edges", "BFS", "shortest path length", "-1 if unreachable"} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract missing %q:\n%s", want, contract)
		}
	}
}

func TestVerifyStackMoveCandidateFromInput(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"initial_state": []any{[]any{float64(0)}, []any{float64(1), float64(2)}, []any{}},
		"goal_state":    []any{[]any{}, []any{float64(1)}, []any{float64(2), float64(0)}},
	}
	ok, detail, applicable := verifyStackMoveCandidateFromInput("solution = [[2,1,2],[0,0,2]]", input)
	if !applicable || !ok || detail != "" {
		t.Fatalf("valid candidate ok=%v applicable=%v detail=%q", ok, applicable, detail)
	}
	ok, detail, applicable = verifyStackMoveCandidateFromInput("solution = [[0,0,0]]", input)
	if !applicable || ok || !strings.Contains(detail, "same stack") {
		t.Fatalf("same-stack candidate ok=%v applicable=%v detail=%q", ok, applicable, detail)
	}
	ok, detail, applicable = verifyStackMoveCandidateFromInput("solution = [[1,1,2]]", input)
	if !applicable || ok || !strings.Contains(detail, "top is 2") {
		t.Fatalf("non-top candidate ok=%v applicable=%v detail=%q", ok, applicable, detail)
	}
	diag, applicable := stackMoveAnswerVerifier("solution = [[2,1,2],[2,1,0]]", input)
	if !applicable || diag.Pass || diag.Score <= 0 || diag.Progress["valid_prefix_moves"] != 1 || len(diag.ValidPrefix) != 1 {
		t.Fatalf("diagnostic=%#v applicable=%v", diag, applicable)
	}
	diag, applicable = stackMoveAnswerVerifier("solution = [[2,1,2]]", input)
	if !applicable || diag.Pass || diag.Score <= 0.8 || diag.Progress["state_similarity"] == nil {
		t.Fatalf("goal mismatch diagnostic=%#v applicable=%v", diag, applicable)
	}
	ok, detail, applicable = verifyStackMoveCandidateFromInput("solution = []", input)
	if !applicable || ok || !strings.Contains(detail, "does not match goal") {
		t.Fatalf("empty move candidate ok=%v applicable=%v detail=%q", ok, applicable, detail)
	}
}

func TestBraidHelperInputIncludesDependencySummaries(t *testing.T) {
	t.Parallel()

	input := braidHelperInput(
		"Puzzle instance:\n\nInitial state: [[0], [1, 2], []]\nGoal state: [[], [1], [2, 0]]\nNumber of blocks: 3\n",
		map[string]string{"n_solve": "status: solved answer: solution = [[2,1,2],[0,0,2]]"},
	)
	if input["n_solve"] == "" {
		t.Fatalf("input missing dependency key: %#v", input)
	}
	packet, ok := input["n_solve"].(map[string]any)
	if !ok {
		t.Fatalf("dependency key should be a typed handoff packet: %#v", input["n_solve"])
	}
	if packet["answer"] != "solution = [[2,1,2],[0,0,2]]" {
		t.Fatalf("dependency answer=%#v", packet["answer"])
	}
	if packet["solution_text"] != "[[2,1,2],[0,0,2]]" {
		t.Fatalf("dependency solution_text=%#v", packet["solution_text"])
	}
	if got, ok := packet["solution"].([]any); !ok || len(got) != 2 {
		t.Fatalf("dependency solution=%#v", packet["solution"])
	}
	deps, ok := input["dependency_summaries"].(map[string]any)
	if !ok || deps["n_solve"] == "" {
		t.Fatalf("input missing dependency summaries: %#v", input)
	}
	depPacket, ok := deps["n_solve"].(map[string]any)
	if !ok || depPacket["answer"] != "solution = [[2,1,2],[0,0,2]]" {
		t.Fatalf("dependency summaries should contain typed packets: %#v", deps)
	}
	depText, ok := input["dependency_summary_text"].(map[string]any)
	if !ok || depText["n_solve"] == "" {
		t.Fatalf("input missing dependency summary text: %#v", input)
	}
	if _, ok := input["initial_state"]; !ok {
		t.Fatalf("input missing parsed official fields: %#v", input)
	}
}

func TestNormalizeBraidVerifyHelperInputReplacesPlaceholderCandidates(t *testing.T) {
	t.Parallel()

	input := braidHelperInput(
		"",
		map[string]string{
			"n_solve_a": `status: completed summary: status: solved answer: solution = {"node_2":4} checks: ok`,
			"n_solve_b": `status: completed summary: status: solved answer: solution = 1232 checks: ok`,
		},
	)
	input["candidates"] = "candidate answers for node_2 and node_1"
	input["predicates"] = "problem constraint checks"

	normalized := normalizeBraidVerifyHelperInput(input)
	candidates, ok := normalized["candidates"].(map[string]any)
	if !ok {
		t.Fatalf("candidates=%#v, want map", normalized["candidates"])
	}
	nodeValue, ok := candidates["n_solve_a"].(map[string]any)
	if !ok || nodeValue["node_2"] != float64(4) {
		t.Fatalf("n_solve_a candidate=%#v", candidates["n_solve_a"])
	}
	if candidates["n_solve_b"] != float64(1232) {
		t.Fatalf("candidates=%#v", candidates)
	}
	if _, exists := normalized["predicates"]; exists {
		t.Fatalf("placeholder predicates should be removed: %#v", normalized["predicates"])
	}
}

func TestNormalizeBraidVerifyHelperInputFlattensSingleDependencyCandidate(t *testing.T) {
	t.Parallel()

	input := braidHelperInput(
		"",
		map[string]string{
			"n_solve": `status: completed summary: status: solved answer: solution = {"node_4":1,"node_2":46,"node_7":121} checks: ok`,
		},
	)
	input["candidates"] = "node_4, node_2, node_7 values"

	normalized := normalizeBraidVerifyHelperInput(input)
	candidates, ok := normalized["candidates"].(map[string]any)
	if !ok {
		t.Fatalf("candidates=%#v, want flattened map", normalized["candidates"])
	}
	if candidates["node_2"] != float64(46) || candidates["node_4"] != float64(1) || candidates["node_7"] != float64(121) {
		t.Fatalf("candidates=%#v", candidates)
	}
}

func TestBraidHelperInputIncludesDependencyAliases(t *testing.T) {
	t.Parallel()

	input := braidHelperInput(
		"",
		map[string]string{"n_solve_chain__adaptive_merge": "status: solved answer: solution = 1"},
	)
	if _, ok := input["n_solve_chain__adaptive_merge"]; !ok {
		t.Fatalf("input missing exact dependency key: %#v", input)
	}
	if _, ok := input["n_solve_chain_adaptive_merge"]; !ok {
		t.Fatalf("input missing collapsed dependency alias: %#v", input)
	}
}

func TestBraidNodeExecutionSummaryRejectsNestedBlockedStatus(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_reduce", Kind: "reduce"},
		"status: completed summary: status: blocked answer: checks failed",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for nested blocked status")
	}
	if !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want did not complete", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsNestedSolvedStatus(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_reduce", Kind: "reduce"},
		"status: completed summary: status: solved answer: solution = 42 checks: verified",
		"n_reduce",
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryRejectsPartialExtract(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_extract", Kind: "extract"},
		"status: completed summary: status: partial answer: extracted some constraints",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for partial extract")
	}
	if !strings.Contains(err.Error(), "returned partial status") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want partial status", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsPartialGeneratedParseNode(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_solve__parse", Kind: "extract"},
			{ID: "n_solve__solve_00", Kind: "solve", DependsOn: []string{"n_solve__parse"}},
			{ID: "n_solve__merge", Kind: "reduce", DependsOn: []string{"n_solve__solve_00"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve__merge"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve__merge", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	err := validateBraidNodeExecutionSummaryInGraph(
		"graph_fanout",
		BraidNode{ID: "n_solve__parse", Kind: "extract"},
		"status: completed summary: status: partial answer: parsed dependency packet for downstream chunk solvers",
		"n_reduce",
		graph,
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsPartialRootExtractWithDownstream(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve"}},
		},
		FinalNode: "n_reduce",
	}
	err := validateBraidNodeExecutionSummaryInGraph(
		"graph_fanout",
		BraidNode{ID: "n_extract", Kind: "extract"},
		"status: completed summary: status: partial answer: dependency packet sufficient for downstream solve nodes",
		"n_reduce",
		graph,
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryRejectsPartialSolve(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_solve", Kind: "solve"},
		"status: completed summary: status: partial answer: missing cyclic branch",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for partial solve")
	}
	if !strings.Contains(err.Error(), "returned partial status") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want partial status", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsPartialSolveBeforeCycleSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve_context", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_solve_context"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_cycle"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_cycle", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	err := validateBraidNodeExecutionSummaryInGraph(
		"graph_fanout",
		BraidNode{ID: "n_solve_context", Kind: "solve"},
		"status: completed summary: status: partial answer: fixed-point cluster deferred to cycle_solve",
		"n_reduce",
		graph,
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsPartialSolveBeforeDownstreamSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve_plan", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_solve_moves", Kind: "solve", DependsOn: []string{"n_solve_plan"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve_moves"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve_moves", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	err := validateBraidNodeExecutionSummaryInGraph(
		"graph_fanout",
		BraidNode{ID: "n_solve_plan", Kind: "solve"},
		"status: completed summary: status: partial answer: high-level plan for downstream move generation",
		"n_reduce",
		graph,
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryRejectsPartialFinalSolveEvenBeforeCycleSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_solve_final", Kind: "solve"},
			{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_solve_final"}},
		},
		FinalNode: "n_solve_final",
	}
	err := validateBraidNodeExecutionSummaryInGraph(
		"graph_fanout",
		BraidNode{ID: "n_solve_final", Kind: "solve"},
		"status: completed summary: status: partial answer: not final",
		"n_solve_final",
		graph,
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummaryInGraph() succeeded for partial final solve")
	}
	if !strings.Contains(err.Error(), "returned partial status") {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() err=%v, want partial status", err)
	}
}

func TestBraidNodeExecutionSummaryRequiresCycleSolvePassTrue(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		"status: completed summary: status: solved answer: [1,2,3] checks: searched bounds 0..10",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for cycle_solve without pass=true")
	}
	if !strings.Contains(err.Error(), "missing cycle_json") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want missing cycle_json", err)
	}
}

func TestBraidNodeExecutionSummaryRequiresCycleJSON(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		"status: completed summary: status: solved answer: [1,2,3] checks: pass=true searched bounds 0..10",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for cycle_solve without cycle_json")
	}
	if !strings.Contains(err.Error(), "missing cycle_json") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want missing cycle_json", err)
	}
}

func TestBraidHelperNodeSummaryBlocksMalformedCycleSolve(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummary(
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		"solution = {\"node_2\": 1}",
	)
	if !strings.Contains(summary, "status: blocked") || !strings.Contains(summary, "missing cycle_json") {
		t.Fatalf("summary=%q, want blocked missing cycle_json", summary)
	}
}

func TestBraidNodeExecutionSummaryRejectsCycleJSONFailedCheck(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		`status: completed summary: status: solved answer: cycle_json: {"pass":true,"candidates":{"x":1},"checks":[{"name":"fixed_point","ok":false,"observed":5,"expected":6}]} checks: pass=true`,
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for cycle_solve failed cycle_json check")
	}
	if !strings.Contains(err.Error(), "checks[0].ok") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want failed check", err)
	}
}

func TestBraidNodeExecutionSummaryRejectsCycleSolvePassFalse(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		"status: completed summary: status: solved answer: [1,2,3] checks: pass=false mismatch at candidate 2",
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for cycle_solve pass=false")
	}
	if !strings.Contains(err.Error(), "pass=false") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want pass=false", err)
	}
}

func TestBraidNodeExecutionSummaryRejectsCycleJSONMissingObservedExpected(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		`status: completed summary: status: solved answer: cycle_json: {"pass":true,"candidates":{"x":1},"checks":[{"name":"fixed_point","ok":true}]}`,
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for cycle_solve check without observed/expected")
	}
	if !strings.Contains(err.Error(), "observed and expected") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want observed and expected", err)
	}
}

func TestBraidNodeExecutionSummaryRejectsCycleJSONObservedExpectedMismatch(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		`status: completed summary: status: solved answer: cycle_json: {"pass":true,"candidates":{"x":1},"checks":[{"name":"prime_sum","ok":true,"observed":9,"expected":6}]}`,
		"n_reduce",
	)
	if err == nil {
		t.Fatal("validateBraidNodeExecutionSummary() succeeded for cycle_solve mismatched observed/expected")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("validateBraidNodeExecutionSummary() err=%v, want mismatch", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsCycleSolvePassTrue(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		`status: completed summary: status: solved answer: cycle_json: {"pass":true,"candidates":{"x":1},"checks":[{"name":"fixed_point","ok":true,"observed":6,"expected":6}]} checks: pass=true searched bounds 0..10`,
		"n_reduce",
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsCycleSolveJSONPassWithoutTextLabel(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		`status: completed summary: status: solved answer: cycle_json: {"pass":true,"candidates":{"node_2":1071,"node_5":7},"checks":[{"name":"fixed_point","ok":true,"observed":"stable","expected":"stable"}]}`,
		"n_reduce",
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() error = %v", err)
	}
}

func TestBraidNodeExecutionSummaryAllowsCycleJSONWithTrailingChecksText(t *testing.T) {
	t.Parallel()

	err := validateBraidNodeExecutionSummary(
		"graph_fanout",
		BraidNode{ID: "n_cycle", Kind: "cycle_solve"},
		`status: completed summary: status: solved answer: cycle_json: {"pass":true,"candidates":{"node_2":1430},"checks":[{"name":"prime_factor_sum","ok":true,"observed":6,"expected":6}]} checks: pass=true; fixed point found`,
		"n_reduce",
	)
	if err != nil {
		t.Fatalf("validateBraidNodeExecutionSummary() error = %v", err)
	}
}

func TestBraidPrepareRepairResetsSolveDescendants(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract": "status: solved",
		"n_solve":   "status: solved answer: bad",
		"n_verify":  "status: blocked checks: failed substitution",
	}
	executed := map[string]struct{}{
		"n_extract": {},
		"n_solve":   {},
		"n_verify":  {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 1},
		graph,
		BraidNode{ID: "n_verify", Kind: "verify"},
		"status: completed summary: status: blocked checks: failed substitution",
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want 1", attempts)
	}
	if _, ok := summaries["n_extract"]; !ok {
		t.Fatalf("extract summary should be preserved: %#v", summaries)
	}
	for _, id := range []string{"n_solve", "n_verify", "n_reduce"} {
		if _, ok := summaries[id]; ok {
			t.Fatalf("summary for %s should be reset: %#v", id, summaries)
		}
		if _, ok := executed[id]; ok {
			t.Fatalf("executed for %s should be reset: %#v", id, executed)
		}
	}
	if !strings.Contains(feedback["n_solve"], "failed substitution") {
		t.Fatalf("repair feedback=%q, want failed substitution", feedback["n_solve"])
	}
}

func TestBraidPrepareRepairAcceptsPartialCycleSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_cycle"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_cycle", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract": "status: solved",
		"n_cycle":   "status: completed summary: status: partial answer: {\"unknowns\":[\"node_2\"],\"constraints\":[\"sum factors = 6\"]} checks: extracted fixed-point constraints",
	}
	executed := map[string]struct{}{
		"n_extract": {},
		"n_cycle":   {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 1},
		graph,
		BraidNode{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_extract"}},
		summaries["n_cycle"],
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false for partial cycle_solve")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want 1", attempts)
	}
	if _, ok := summaries["n_extract"]; !ok {
		t.Fatalf("extract summary should be preserved: %#v", summaries)
	}
	for _, id := range []string{"n_cycle", "n_verify", "n_reduce"} {
		if _, ok := summaries[id]; ok {
			t.Fatalf("summary for %s should be reset: %#v", id, summaries)
		}
	}
	if !strings.Contains(feedback["n_cycle"], "sum factors") {
		t.Fatalf("repair feedback=%q, want partial constraints", feedback["n_cycle"])
	}
}

func TestBraidPrepareRepairDoesNotRepeatHelperContractFailure(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_cycle"}},
		},
		FinalNode: "n_verify",
	}
	summary := "status: blocked summary: answer: checks: ephemeral_helper_solve failed before producing a usable candidate. detail: helper factory failed after 5 attempts: python skill validation/run failed: IndentationError"
	attempts := 0
	if prepareBraidRepair(REPLRunnerPhase{BraidRepairAttempts: 2}, graph, BraidNode{ID: "n_cycle", Kind: "cycle_solve"}, summary, map[string]string{}, map[string]struct{}{}, map[string]string{}, &attempts) {
		t.Fatal("prepareBraidRepair() returned true for helper contract failure")
	}
	if attempts != 0 {
		t.Fatalf("attempts=%d want 0", attempts)
	}
}

func TestNormalizeBraidVerificationFailureSummaryFeedsRepair(t *testing.T) {
	t.Parallel()

	raw := "status: completed summary: node_2 verification: sum of prime factors is 17 != 6. This is a critical constraint failure."
	normalized, ok := normalizeBraidVerificationFailureSummary(BraidNode{ID: "n_verify", Kind: "verify"}, raw)
	if !ok {
		t.Fatal("normalizeBraidVerificationFailureSummary() returned false")
	}
	for _, want := range []string{"status: blocked", "pass: false", "first_failure", "17 != 6", "repair the upstream candidate"} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("normalized summary missing %q:\n%s", want, normalized)
		}
	}

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract": "status: solved",
		"n_solve":   "status: solved answer: bad candidate",
		"n_verify":  normalized,
	}
	executed := map[string]struct{}{
		"n_extract": {},
		"n_solve":   {},
		"n_verify":  {},
	}
	feedback := map[string]string{}
	attempts := 0

	if !prepareBraidRepair(REPLRunnerPhase{BraidRepairAttempts: 1}, graph, BraidNode{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve"}}, normalized, summaries, executed, feedback, &attempts) {
		t.Fatal("prepareBraidRepair() returned false for normalized concrete verifier failure")
	}
	if !strings.Contains(feedback["n_solve"], "17 != 6") {
		t.Fatalf("repair feedback=%q, want concrete verifier failure", feedback["n_solve"])
	}
}

func TestBraidPrepareRepairTargetsFailedVerifierSolveAncestors(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve_a", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_solve_b", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_verify_b", Kind: "verify", DependsOn: []string{"n_solve_b"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve_a", "n_solve_b", "n_verify_b"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract":  "status: solved",
		"n_solve_a":  "status: solved answer: keep",
		"n_solve_b":  "status: solved answer: bad",
		"n_verify_b": "status: blocked checks: failed b",
		"n_reduce":   "status: blocked",
	}
	executed := map[string]struct{}{
		"n_extract":  {},
		"n_solve_a":  {},
		"n_solve_b":  {},
		"n_verify_b": {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 1},
		graph,
		BraidNode{ID: "n_verify_b", Kind: "verify", DependsOn: []string{"n_solve_b"}},
		"status: completed summary: status: blocked checks: failed b",
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if _, ok := summaries["n_solve_a"]; !ok {
		t.Fatalf("unrelated solve summary should be preserved: %#v", summaries)
	}
	for _, id := range []string{"n_solve_b", "n_verify_b", "n_reduce"} {
		if _, ok := summaries[id]; ok {
			t.Fatalf("summary for %s should be reset: %#v", id, summaries)
		}
	}
	if feedback["n_solve_a"] != "" {
		t.Fatalf("unexpected feedback for n_solve_a: %q", feedback["n_solve_a"])
	}
	if !strings.Contains(feedback["n_solve_b"], "failed b") {
		t.Fatalf("repair feedback=%q, want failed b", feedback["n_solve_b"])
	}
}

func TestBraidPrepareRepairRetriesBlockedSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_solve_a", Kind: "solve", DependsOn: []string{"n_extract"}},
			{ID: "n_solve_b", Kind: "solve", DependsOn: []string{"n_solve_a"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve_b"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve_b", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract": "status: solved",
		"n_solve_a": "status: solved answer: keep",
		"n_solve_b": "status: blocked checks: mutual dependency",
	}
	executed := map[string]struct{}{
		"n_extract": {},
		"n_solve_a": {},
		"n_solve_b": {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 1},
		graph,
		BraidNode{ID: "n_solve_b", Kind: "solve", DependsOn: []string{"n_solve_a"}},
		"status: completed summary: status: blocked checks: mutual dependency",
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if _, ok := summaries["n_solve_a"]; !ok {
		t.Fatalf("upstream solve summary should be preserved: %#v", summaries)
	}
	for _, id := range []string{"n_solve_b", "n_verify", "n_reduce"} {
		if _, ok := summaries[id]; ok {
			t.Fatalf("summary for %s should be reset: %#v", id, summaries)
		}
	}
	if !strings.Contains(feedback["n_solve_b"], "fixed-point") {
		t.Fatalf("repair feedback=%q, want fixed-point guidance", feedback["n_solve_b"])
	}
}

func TestBraidPrepareRepairRetriesBlockedCycleSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_extract"}},
			{ID: "n_solve", Kind: "solve", DependsOn: []string{"n_cycle"}},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract": "status: solved",
		"n_cycle":   "status: blocked checks: no fixed point found",
	}
	executed := map[string]struct{}{
		"n_extract": {},
		"n_cycle":   {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 1},
		graph,
		BraidNode{ID: "n_cycle", Kind: "cycle_solve", DependsOn: []string{"n_extract"}},
		"status: completed summary: status: blocked checks: no fixed point found",
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if _, ok := summaries["n_extract"]; !ok {
		t.Fatalf("extract summary should be preserved: %#v", summaries)
	}
	for _, id := range []string{"n_cycle", "n_solve", "n_verify", "n_reduce"} {
		if _, ok := summaries[id]; ok {
			t.Fatalf("summary for %s should be reset: %#v", id, summaries)
		}
	}
	if !strings.Contains(feedback["n_cycle"], "fixed-point") {
		t.Fatalf("repair feedback=%q, want fixed-point guidance", feedback["n_cycle"])
	}
}

func TestBraidPrepareRepairAdaptivelySplitsUnresolvedSolveTargets(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{
				ID:            "n_solve_wave",
				Kind:          "solve",
				DependsOn:     []string{"n_extract"},
				HelperPolicy:  BraidNodeHelperPolicyPreferred,
				Archetype:     BraidScaffoldClassCandidateVerify,
				ScaffoldClass: BraidScaffoldClassCandidateVerify,
				ScaffoldID:    BraidScaffoldIDPropertyCheckV1,
				InputSchema: map[string]any{
					"prompt":        "solve explicit work items",
					"solve_targets": []any{"node_0", "node_1", "node_2"},
				},
			},
			{ID: "n_verify", Kind: "verify", DependsOn: []string{"n_solve_wave"}},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve_wave", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract":    "status: solved",
		"n_solve_wave": `status: blocked answer: solution = {"node_0":"UNSOLVED"} checks: failure_kind=search_backtrack first_failure="answer contains unresolved values" candidate_frontier=[]`,
	}
	executed := map[string]struct{}{
		"n_extract":    {},
		"n_solve_wave": {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 2},
		graph,
		BraidNode{ID: "n_solve_wave", Kind: "solve"},
		summaries["n_solve_wave"],
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d want 1", attempts)
	}
	if _, ok := summaries["n_extract"]; !ok {
		t.Fatalf("extract summary should be preserved: %#v", summaries)
	}
	if _, ok := summaries["n_solve_wave"]; ok {
		t.Fatalf("original solve summary should be cleared: %#v", summaries)
	}
	if _, ok := executed["n_solve_wave"]; ok {
		t.Fatalf("original solve execution should be cleared: %#v", executed)
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve_wave"); ok {
		t.Fatal("original solve node should be replaced")
	}
	mergeID := "n_solve_wave__adaptive_merge"
	if _, ok := braidGraphNodeByID(*graph, mergeID); !ok {
		t.Fatalf("merge node %q missing from graph: %#v", mergeID, graph.Nodes)
	}
	routerID := "n_solve_wave__adaptive_router"
	router, ok := braidGraphNodeByID(*graph, routerID)
	if !ok {
		t.Fatalf("router node %q missing from graph: %#v", routerID, graph.Nodes)
	}
	if router.Kind != "extract" || !reflect.DeepEqual(router.DependsOn, []string{"n_extract"}) {
		t.Fatalf("router=%#v, want extract depending on n_extract", router)
	}
	verify, ok := braidGraphNodeByID(*graph, "n_verify")
	if !ok {
		t.Fatal("verify node missing")
	}
	if !reflect.DeepEqual(verify.DependsOn, []string{mergeID}) {
		t.Fatalf("verify deps=%v want [%s]", verify.DependsOn, mergeID)
	}
	reduce, ok := braidGraphNodeByID(*graph, "n_reduce")
	if !ok {
		t.Fatal("reduce node missing")
	}
	if !reflect.DeepEqual(reduce.DependsOn, []string{mergeID, "n_verify"}) {
		t.Fatalf("reduce deps=%v want [%s n_verify]", reduce.DependsOn, mergeID)
	}
	firstID := "n_solve_wave__adaptive_00_node_0"
	secondID := "n_solve_wave__adaptive_01_node_1"
	thirdID := "n_solve_wave__adaptive_02_node_2"
	first, ok := braidGraphNodeByID(*graph, firstID)
	if !ok {
		t.Fatalf("first split node missing")
	}
	if !reflect.DeepEqual(first.DependsOn, []string{routerID}) {
		t.Fatalf("first deps=%v want [%s]", first.DependsOn, routerID)
	}
	if first.ExpectedOutput != "solution = <answer for node_0>" {
		t.Fatalf("first expected_output=%q", first.ExpectedOutput)
	}
	second, ok := braidGraphNodeByID(*graph, secondID)
	if !ok {
		t.Fatalf("second split node missing")
	}
	if !reflect.DeepEqual(second.DependsOn, []string{routerID, firstID}) {
		t.Fatalf("second deps=%v want [%s %s]", second.DependsOn, routerID, firstID)
	}
	third, ok := braidGraphNodeByID(*graph, thirdID)
	if !ok {
		t.Fatalf("third split node missing")
	}
	if !reflect.DeepEqual(third.DependsOn, []string{routerID, secondID}) {
		t.Fatalf("third deps=%v want [%s %s]", third.DependsOn, routerID, secondID)
	}
	if third.ScaffoldClass != BraidScaffoldClassExplicitDAG || third.ScaffoldID != BraidScaffoldIDSearchBacktrackV1 {
		t.Fatalf("third scaffold=%s/%s, want explicit_dag/search_backtrack_v1", third.ScaffoldClass, third.ScaffoldID)
	}
	if got := fmt.Sprintf("%v", third.InputSchema["target_node"]); got != "node_2" {
		t.Fatalf("third target_node=%q want node_2", got)
	}
}

func TestBraidPrepareRepairAdaptivelySplitsNodeRange(t *testing.T) {
	t.Parallel()

	if got := extractBraidNodeIDsFromText("node_0 to node_2"); !reflect.DeepEqual(got, []string{"node_0", "node_1", "node_2"}) {
		t.Fatalf("range extraction=%v", got)
	}
	if !braidFailureSignalsUnresolvedWork("status: blocked summary: answer: solution = 4 checks: ephemeral_helper_solve produced an unusable candidate. detail: answer does not contain structured node values required by expected_output") {
		t.Fatal("structured-output failure should signal unresolved work")
	}

	rangeNode := BraidNode{ID: "n_solve_chain", Kind: "solve", Question: "Solve node_0, then node_1, then node_2.", InputSchema: map[string]any{"problems": "node_0 to node_2", "dependencies": "sequential chain"}}
	if plan, ok := buildAdaptiveBraidSplitPlan(rangeNode, "answer does not contain structured node values"); !ok || len(plan.Targets) != 3 {
		t.Fatalf("adaptive range plan ok=%v targets=%#v", ok, plan.Targets)
	}
	namedTargetsNode := BraidNode{ID: "n_solve_named", Kind: "solve", InputSchema: map[string]any{"nodes_to_solve": "node_2, node_4, node_7"}}
	if got := adaptiveBraidSplitTargetIDs(namedTargetsNode); !reflect.DeepEqual(got, []string{"node_2", "node_4", "node_7"}) {
		t.Fatalf("named target extraction=%v", got)
	}
	bareTargetsNode := BraidNode{ID: "n_solve_bare", Kind: "solve", Question: "Solve nodes 0, 1, 2, 3 sequentially."}
	if got := adaptiveBraidSplitTargetIDs(bareTargetsNode); !reflect.DeepEqual(got, []string{"node_0", "node_1", "node_2", "node_3"}) {
		t.Fatalf("bare target extraction=%v", got)
	}

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{
				ID:            "n_solve_chain",
				Kind:          "solve",
				Question:      "Solve node_0, then node_1, then node_2.",
				DependsOn:     []string{"n_extract"},
				HelperPolicy:  BraidNodeHelperPolicyPreferred,
				Archetype:     BraidScaffoldClassExplicitDAG,
				ScaffoldClass: BraidScaffoldClassExplicitDAG,
				ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"problems":     "node_0 to node_2",
					"dependencies": "sequential chain",
				},
			},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve_chain"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract":     "status: solved",
		"n_solve_chain": "status: blocked summary: answer: solution = 4 checks: ephemeral_helper_solve produced an unusable candidate. detail: answer does not contain structured node values required by expected_output",
	}
	executed := map[string]struct{}{"n_extract": {}, "n_solve_chain": {}}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 2},
		graph,
		BraidNode{ID: "n_solve_chain", Kind: "solve"},
		summaries["n_solve_chain"],
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_00_node_0"); !ok {
		t.Fatalf("node_0 split missing: %#v", graph.Nodes)
	}
	third, ok := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_02_node_2")
	if !ok {
		t.Fatalf("node_2 split missing: %#v", graph.Nodes)
	}
	if got := fmt.Sprintf("%v", third.InputSchema["target_node"]); got != "node_2" {
		t.Fatalf("target_node=%q want node_2", got)
	}
	if got := fmt.Sprintf("%v", third.InputSchema["expected_output"]); !strings.Contains(got, `"node_2"`) {
		t.Fatalf("target expected_output=%q want node_2-specific output", got)
	}
}

func TestBraidPrepareRepairDoesNotAdaptivelySplitSyntaxFailure(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract"},
			{
				ID:           "n_solve",
				Kind:         "solve",
				DependsOn:    []string{"n_extract"},
				HelperPolicy: BraidNodeHelperPolicyPreferred,
				InputSchema: map[string]any{
					"solve_targets": []any{"node_0", "node_1"},
				},
			},
			{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve"}},
		},
		FinalNode: "n_reduce",
	}
	summaries := map[string]string{
		"n_extract": "status: solved",
		"n_solve":   "status: blocked checks: python skill validation/run failed: SyntaxError",
	}
	executed := map[string]struct{}{
		"n_extract": {},
		"n_solve":   {},
	}
	feedback := map[string]string{}
	attempts := 0

	ok := prepareBraidRepair(
		REPLRunnerPhase{BraidRepairAttempts: 1},
		graph,
		BraidNode{ID: "n_solve", Kind: "solve"},
		summaries["n_solve"],
		summaries,
		executed,
		feedback,
		&attempts,
	)
	if !ok {
		t.Fatal("prepareBraidRepair() returned false")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve"); !ok {
		t.Fatal("syntax failure should retry original node, not split it")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve__adaptive_merge"); ok {
		t.Fatal("syntax failure should not create adaptive merge")
	}
	if !strings.Contains(feedback["n_solve"], "Revise this node") {
		t.Fatalf("feedback=%q, want normal solve repair feedback", feedback["n_solve"])
	}
}

func TestBraidHandoffHelperInputIncludesTargetLocalContext(t *testing.T) {
	t.Parallel()

	handoff := BraidNodeHandoff{
		Node: BraidNode{
			ID:             "n_solve_0",
			Kind:           "solve",
			Question:       "Solve only node_0.",
			ExpectedOutput: "ratio and numerator",
		},
		OfficialRootTask: "Problem node_0: ...",
		Facts: map[string]any{
			"prompt": "original problem and extracted dependencies",
		},
		TaskType:      BraidScaffoldClassExplicitDAG,
		ScaffoldClass: BraidScaffoldClassExplicitDAG,
		ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
	}

	input := BraidHandoffHelperInput(handoff)
	if input["node_id"] != "n_solve_0" {
		t.Fatalf("node_id=%v want n_solve_0", input["node_id"])
	}
	if input["work_item_question"] != "Solve only node_0." {
		t.Fatalf("work_item_question=%v", input["work_item_question"])
	}
	if input["expected_output"] != "ratio and numerator" {
		t.Fatalf("expected_output=%v", input["expected_output"])
	}
	if input["root_task"] != "Problem node_0: ..." {
		t.Fatalf("root_task=%v", input["root_task"])
	}
	if input["prompt"] != "Problem node_0: ..." {
		t.Fatalf("prompt=%v, want concrete root task replacing placeholder", input["prompt"])
	}
}

func TestBraidCycleSolveTypedHandoffIncludesInputSchema(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:            "n_cycle",
		Kind:          "cycle_solve",
		Question:      "Solve the fixed-point cluster.",
		Archetype:     BraidScaffoldClassConstraintSolver,
		ScaffoldClass: BraidScaffoldClassConstraintSolver,
		ScaffoldID:    BraidScaffoldIDFiniteDomainV1,
		InputSchema: map[string]any{
			"target_nodes":   []any{"node_2", "node_5"},
			"cycle_clusters": []any{[]any{"node_2", "node_5"}},
			"variables": map[string]any{
				"node_2": []any{float64(1), float64(2)},
				"node_5": []any{float64(3), float64(4)},
			},
			"constraints": []any{"node_5 depends on node_2"},
		},
	}
	handoff := BuildBraidNodeHandoff(node, "root problem", nil, "")
	input := BraidHandoffHelperInput(handoff)
	if _, ok := input["cycle_clusters"]; !ok {
		t.Fatalf("cycle_clusters missing from helper input: %#v", input)
	}
	if input["scaffold_class"] != BraidScaffoldClassConstraintSolver {
		t.Fatalf("scaffold_class=%v", input["scaffold_class"])
	}
	if input["task_type"] != BraidScaffoldClassConstraintSolver {
		t.Fatalf("task_type=%v", input["task_type"])
	}
}

func TestBraidParseGraphTextRejectsMarkdownFence(t *testing.T) {
	t.Parallel()

	raw := "```json\n{\"version\":1,\"nodes\":[{\"id\":\"n1\",\"kind\":\"extract\",\"question\":\"q\"}],\"final_node\":\"n1\"}\n```"
	_, err := ParseBraidGraphText(raw)
	if err == nil {
		t.Fatal("ParseBraidGraphText() succeeded for fenced JSON")
	}
}

func TestBraidParseGraphTextAcceptsUnknownFields(t *testing.T) {
	t.Parallel()

	raw := `{"version":1,"nodes":[{"id":"n1","kind":"extract","question":"q","extra":"x","python_repl":"print(1)"}],"final_node":"n1"}`
	g, err := ParseBraidGraphText(raw)
	if err != nil {
		t.Fatalf("ParseBraidGraphText() failed with unknown fields: %v", err)
	}
	if len(g.Nodes) != 1 || g.Nodes[0].ID != "n1" {
		t.Fatalf("ParseBraidGraphText() graph = %+v, want one node n1", g)
	}
}

func TestParseBraidGraphTextClampsOversizedFields(t *testing.T) {
	t.Parallel()

	longQuestion := strings.Repeat("x", maxBraidNodeQuestionChars+50)
	longExpected := strings.Repeat("y", maxBraidNodeExpectedChars+30)

	raw := fmt.Sprintf(`{"version":1,"nodes":[{"id":"n1","kind":"extract","question":%q,"expected_output":%q}],"final_node":"n1"}`, longQuestion, longExpected)
	g, err := ParseBraidGraphText(raw)
	if err != nil {
		t.Fatalf("ParseBraidGraphText() failed: %v", err)
	}
	if len(g.Nodes[0].Question) != maxBraidNodeQuestionChars {
		t.Fatalf("question length = %d, want clamped to %d", len(g.Nodes[0].Question), maxBraidNodeQuestionChars)
	}
	if len(g.Nodes[0].ExpectedOutput) != maxBraidNodeExpectedChars {
		t.Fatalf("expected_output length = %d, want clamped to %d", len(g.Nodes[0].ExpectedOutput), maxBraidNodeExpectedChars)
	}
}

func TestParseBraidGraphTextDefaultsMissingOptionalFields(t *testing.T) {
	t.Parallel()

	raw := `{"version":1,"nodes":[{"id":"n1","kind":"extract","question":"q"}],"final_node":"n1"}`
	g, err := ParseBraidGraphText(raw)
	if err != nil {
		t.Fatalf("ParseBraidGraphText() failed: %v", err)
	}
	node := g.Nodes[0]
	if node.DependsOn == nil {
		t.Fatal("depends_on should default to empty slice, not nil")
	}
	if len(node.DependsOn) != 0 {
		t.Fatalf("depends_on = %v, want empty", node.DependsOn)
	}
	if node.HelperPolicy != "" {
		t.Fatalf("helper_policy = %q, want empty", node.HelperPolicy)
	}
}

func TestParseBraidGraphTextNegativeMaxSummaryCharsResetToZero(t *testing.T) {
	t.Parallel()

	raw := `{"version":1,"nodes":[{"id":"n1","kind":"solve","question":"q","max_summary_chars":-5}],"final_node":"n1"}`
	g, err := ParseBraidGraphText(raw)
	if err != nil {
		t.Fatalf("ParseBraidGraphText() failed: %v", err)
	}
	if g.Nodes[0].MaxSummaryChars != 0 {
		t.Fatalf("max_summary_chars = %d, want 0", g.Nodes[0].MaxSummaryChars)
	}
}

func TestParseBraidGraphTextCombinedNormalization(t *testing.T) {
	t.Parallel()

	longQ := strings.Repeat("a", maxBraidNodeQuestionChars+20)
	raw := fmt.Sprintf(`{"version":1,"nodes":[{"id":"n1","kind":"EXTRACT","question":%q,"extra_field":true,"depends_on":["","  ","n0"]}],"final_node":" n1 ","extra_top":42}`, longQ)
	g, err := ParseBraidGraphText(raw)
	if err != nil {
		t.Fatalf("ParseBraidGraphText() failed: %v", err)
	}
	node := g.Nodes[0]
	if node.Kind != "extract" {
		t.Fatalf("kind = %q, want lowercase 'extract'", node.Kind)
	}
	if len(node.Question) != maxBraidNodeQuestionChars {
		t.Fatalf("question length = %d, want %d (clamped)", len(node.Question), maxBraidNodeQuestionChars)
	}
	if len(node.DependsOn) != 1 || node.DependsOn[0] != "n0" {
		t.Fatalf("depends_on = %v, want [n0] (empty strings stripped)", node.DependsOn)
	}
	if g.FinalNode != "n1" {
		t.Fatalf("final_node = %q, want 'n1' (trimmed)", g.FinalNode)
	}
}

func TestRenderBraidNodeChildPromptIncludesRootTaskForExtract(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:              "n1",
		Kind:            "extract",
		Question:        "Extract facts.",
		DependsOn:       []string{"n0"},
		ExpectedOutput:  "constraint packet",
		MaxSummaryChars: 300,
	}, "Official problem text", map[string]string{"n0": "status: solved answer: 42"})

	for _, want := range []string{
		"BRAID node n1 (extract)",
		"Official root task:",
		"Official problem text",
		"Dependency summaries:",
		"status: solved answer: 42",
		"Task:",
		"Extract facts.",
		"Expected output:",
		"constraint packet",
		"under 300 characters",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidNodeHandoffCapsDependencyAndRepairContext(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:              "n_solve",
		Kind:            "solve",
		Question:        "Solve bounded branch.",
		DependsOn:       []string{"n_extract"},
		MaxSummaryChars: 400,
	}
	longDep := strings.Repeat("d", maxBraidHandoffDepChars+200)
	longRepair := strings.Repeat("r", maxBraidHandoffRepairChars+200)
	handoff := BuildBraidNodeHandoff(node, "root task", map[string]string{"n_extract": longDep}, longRepair)
	if got := len(handoff.DependencySummaries["n_extract"]); got > maxBraidHandoffDepChars {
		t.Fatalf("dependency summary len=%d exceeds cap %d", got, maxBraidHandoffDepChars)
	}
	if got := len(handoff.RepairFeedback); got > maxBraidHandoffRepairChars {
		t.Fatalf("repair feedback len=%d exceeds cap %d", got, maxBraidHandoffRepairChars)
	}
	if handoff.Budget.MaxSummaryChars != 400 {
		t.Fatalf("max summary chars=%d, want 400", handoff.Budget.MaxSummaryChars)
	}
	prompt := RenderBraidNodeHandoffPrompt(handoff)
	if !strings.Contains(prompt, "BRAID node n_solve") || !strings.Contains(prompt, "Dependency summaries:") {
		t.Fatalf("rendered prompt missing handoff sections:\n%s", prompt)
	}
}

func TestBraidHelperInputMergesRootInstanceWithDependencies(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Choose one supplier and minimize total waste.",
		"",
		"Puzzle instance:",
		"Number of packages: 3",
		"Number of suppliers: 2",
		"Packages: [2, 3, 5]",
		"Suppliers: [[4, 8], [2, 8]]",
		"",
		"Find the minimum total wasted space (mod 1000000007).",
	}, "\n")
	deps := map[string]string{"n_extract": "requested_outputs: [minimum_total_waste_mod_1000000007]"}
	input := braidHelperInput(rootPrompt, deps)

	if _, ok := input["packages"]; !ok {
		t.Fatalf("helper input missing root packages: %#v", input)
	}
	if _, ok := input["suppliers"]; !ok {
		t.Fatalf("helper input missing root suppliers: %#v", input)
	}
	if input["n_extract"] == "" {
		t.Fatalf("helper input missing dependency summary: %#v", input)
	}
}

func TestPackageWasteAnswerVerifierUsesReusableBoxSizes(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"packages":  []any{float64(2), float64(3), float64(5)},
		"suppliers": []any{[]any{float64(4), float64(8)}, []any{float64(2), float64(8)}},
	}
	diag, applicable := packageWasteAnswerVerifier("solution = 6", input)
	if !applicable || !diag.Pass {
		t.Fatalf("expected verifier pass, applicable=%v diag=%#v", applicable, diag)
	}
	diag, applicable = packageWasteAnswerVerifier("solution = -1", input)
	if !applicable || diag.Pass {
		t.Fatalf("expected verifier failure, applicable=%v diag=%#v", applicable, diag)
	}
	if diag.Expected != 6 {
		t.Fatalf("expected diagnostic expected=6, got %#v", diag.Expected)
	}
}

func TestBraidNodeHandoffTransformsStateTransitionTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle instance:",
		"Initial state: [[0], [1, 2], []]",
		"Goal state: [[], [1], [2, 0]]",
		"Number of blocks: 3",
		"Number of stacks: 3",
		"",
		"Format your solution as:",
		"solution = [move0, move1, ..., movek].",
	}, "\n")
	node := BraidNode{
		ID:        "n_solve",
		Kind:      "solve",
		Question:  "Construct a complete candidate action sequence.",
		DependsOn: []string{"n_extract"},
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "facts"}, "")
	if handoff.TaskType != BraidScaffoldClassFiniteStateTransition {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassFiniteStateTransition)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassFiniteStateTransition || handoff.ScaffoldID != BraidScaffoldIDStackRelocationV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	if handoff.OfficialRootTask != "" {
		t.Fatalf("solve handoff retained root prompt: %q", handoff.OfficialRootTask)
	}
	if handoff.AnswerFormat != "solution = [[block, from_stack, to_stack], ...]" {
		t.Fatalf("answer format=%q", handoff.AnswerFormat)
	}
	if _, ok := handoff.Facts["initial_state"]; !ok {
		t.Fatalf("handoff facts missing initial_state: %#v", handoff.Facts)
	}
	if len(handoff.DependencySummaries) != 0 {
		t.Fatalf("typed solve handoff should omit dependency summaries already represented as facts: %#v", handoff.DependencySummaries)
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassFiniteStateTransition || input["scaffold_id"] != BraidScaffoldIDStackRelocationV1 || input["answer_format"] == "" {
		t.Fatalf("helper input missing typed contract: %#v", input)
	}
	if _, ok := input["n_extract"]; ok {
		t.Fatalf("typed solve helper input retained extract summary: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	if strings.Contains(prompt, "Puzzle instance:") {
		t.Fatalf("helper prompt retained raw official prompt:\n%s", prompt)
	}
	for _, want := range []string{"Task type: finite_state_transition", "Facts available in Solve(input):", "Return answer exactly as", "State-transition output contract", "ok:false with first_failure"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestDeclaredStateTransitionHandoffUsesParsedInstanceValues(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle instance:",
		"Initial state: [[0], [1, 2], []]",
		"Goal state: [[], [1], [2, 0]]",
		"Number of blocks: 3",
		"Number of stacks: 3",
	}, "\n")
	node := BraidNode{
		ID:            "n_solve_state",
		Kind:          "solve",
		Question:      "Construct a complete candidate move sequence.",
		DependsOn:     []string{"n_extract"},
		ScaffoldClass: BraidScaffoldClassStateTransition,
		ScaffoldID:    BraidScaffoldIDStateReplayV1,
		InputSchema: map[string]any{
			"initial_state": "extracted initial state",
			"goal_state":    "extracted goal state",
			"moves":         "candidate move sequence to verify",
		},
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "facts"}, "")
	if handoff.ScaffoldClass != BraidScaffoldClassFiniteStateTransition || handoff.ScaffoldID != BraidScaffoldIDStackRelocationV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	input := BraidHandoffHelperInput(handoff)
	if _, ok := stackStateFromAny(input["initial_state"]); !ok {
		t.Fatalf("initial_state was not a parsed stack state: %#v", input["initial_state"])
	}
	if _, ok := stackStateFromAny(input["goal_state"]); !ok {
		t.Fatalf("goal_state was not a parsed stack state: %#v", input["goal_state"])
	}
	ok, detail, applicable := verifyStackMoveCandidateFromInput("solution = []", input)
	if !applicable || ok || !strings.Contains(detail, "does not match goal") {
		t.Fatalf("empty candidate ok=%v applicable=%v detail=%q input=%#v", ok, applicable, detail, input)
	}
}

func TestDeclaredExplicitDAGHandoffUsesTypedStackTransitionEvidence(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle instance:",
		"Initial state: [[0], [1, 2], []]",
		"Goal state: [[], [1], [2, 0]]",
		"Number of blocks: 3",
		"Number of stacks: 3",
	}, "\n")
	node := BraidNode{
		ID:            "n_solve_state",
		Kind:          "solve",
		Question:      "Construct a complete candidate move sequence.",
		DependsOn:     []string{"n_extract"},
		ScaffoldClass: BraidScaffoldClassExplicitDAG,
		ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
		InputSchema: map[string]any{
			"source_ref": "official_prompt",
			"prompt":     "use the official prompt state",
		},
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "facts"}, "")
	if handoff.ScaffoldClass != BraidScaffoldClassFiniteStateTransition || handoff.ScaffoldID != BraidScaffoldIDStackRelocationV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	input := BraidHandoffHelperInput(handoff)
	scaffold, ok := resolveBraidRuntimeScaffold(node, handoff, input)
	if !ok {
		t.Fatal("resolveBraidRuntimeScaffold returned false for typed stack evidence")
	}
	if scaffold.PresetName != BraidScaffoldClassFiniteStateTransition+"/"+BraidScaffoldIDStackRelocationV1 {
		t.Fatalf("preset=%q", scaffold.PresetName)
	}
	if scaffold.PresetSource == "" || scaffold.Verifier == nil {
		t.Fatalf("scaffold missing preset/verifier: %+v", scaffold)
	}
}

func TestBraidNodeHandoffTransformsGridResourceGraphSearchTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Find the minimum initial resource needed to move through a weighted grid.",
		"",
		"Puzzle instance:",
		"Grid size: 2x3",
		"Grid layout: [[0, -2, -3], [-1, -5, 4]]",
		"Starting position: (0, 0)",
		"Goal position: (1, 2)",
		"",
		"Format your solution as:",
		"solution = <integer>",
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Compute the minimum initial resource.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "facts"}, "")
	if handoff.TaskType != BraidScaffoldClassGraphSearch {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassGraphSearch)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassGraphSearch || handoff.ScaffoldID != BraidScaffoldIDResourcePathMinInitialV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	if _, ok := handoff.Facts["grid_layout"]; !ok {
		t.Fatalf("handoff facts missing grid_layout: %#v", handoff.Facts)
	}
	if handoff.Facts["graph_model"] != "grid_dag" {
		t.Fatalf("graph model=%#v", handoff.Facts["graph_model"])
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassGraphSearch || input["scaffold_id"] != BraidScaffoldIDResourcePathMinInitialV1 {
		t.Fatalf("helper input missing graph scaffold contract: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: graph_search", "Graph-search output contract", "solution = <integer>", "minimum initial resource"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidNodeHandoffTransformsExplicitShortestPathGraphSearchTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Find the shortest directed path length in the provided graph.",
		"",
		"Puzzle instance:",
		"Nodes: [\"A\", \"B\", \"C\", \"D\", \"E\"]",
		"Edges: [[\"A\", \"B\"], [\"B\", \"D\"], [\"A\", \"C\"], [\"C\", \"D\"]]",
		"Start node: \"A\"",
		"Goal node: \"D\"",
		"",
		"Format your solution as:",
		"solution = <integer>",
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Compute the shortest path length.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "graph facts"}, "")
	if handoff.TaskType != BraidScaffoldClassGraphSearch {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassGraphSearch)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassGraphSearch || handoff.ScaffoldID != BraidScaffoldIDExplicitShortestPathV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	if handoff.Facts["graph_model"] != "explicit_directed_graph" || handoff.Facts["objective"] != "shortest_path_length" {
		t.Fatalf("graph facts=%#v", handoff.Facts)
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassGraphSearch || input["scaffold_id"] != BraidScaffoldIDExplicitShortestPathV1 {
		t.Fatalf("helper input missing explicit graph scaffold contract: %#v", input)
	}
	if _, ok := explicitGraphFromInput(input); !ok {
		t.Fatalf("helper input did not parse as explicit graph: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: graph_search", "Graph-search output contract", "fewest directed edges", "BFS", "returning -1 if unreachable"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidNodeHandoffTransformsFiniteDomainConstraintTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Solve a typed finite-domain constraint instance.",
		"",
		"Puzzle instance:",
		`Variables: [{"name":"x","min":0,"max":5},{"name":"y","min":0,"max":5}]`,
		`Known values: {"target":5}`,
		`Constraints: [{"name":"sum","op":"eq","left":{"op":"add","args":[{"var":"x"},{"var":"y"}]},"right":{"known":"target"}},{"name":"x_fixed","op":"eq","left":{"var":"x"},"right":{"const":2}}]`,
		`Requested outputs: ["x","y"]`,
		"",
		"Format your solution as:",
		`solution = {"x": <integer>, "y": <integer>}`,
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Find a satisfying assignment.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "typed constraints"}, "")
	if handoff.TaskType != BraidScaffoldClassConstraintSolver {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassConstraintSolver)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassConstraintSolver || handoff.ScaffoldID != BraidScaffoldIDFiniteDomainV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	if _, ok := handoff.Facts["variables"]; !ok {
		t.Fatalf("handoff facts missing variables: %#v", handoff.Facts)
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassConstraintSolver || input["scaffold_id"] != BraidScaffoldIDFiniteDomainV1 {
		t.Fatalf("helper input missing constraint scaffold contract: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: constraint_solver", "Constraint-solver output contract", "finite integer domains", `solution = {"variable": integer, ...}`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidNodeHandoffTransformsNumericDPTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Evaluate the typed integer recurrence table.",
		"",
		"Puzzle instance:",
		"Scaffold class: numeric_dp",
		"Scaffold id: recurrence_table_v1",
		"Objective: min",
		"DP dimensions: [5]",
		"Target: [4]",
		"Base cases: [{\"index\":[0],\"value\":0}]",
		"Transitions: [{\"offset\":[-1],\"weight\":2},{\"offset\":[-2],\"weight\":3}]",
		"",
		"Format your solution as:",
		"solution = <integer>",
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Compute the target table value.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "typed recurrence facts"}, "")
	if handoff.TaskType != BraidScaffoldClassNumericDP {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassNumericDP)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassNumericDP || handoff.ScaffoldID != BraidScaffoldIDRecurrenceTableV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	if _, ok := handoff.Facts["dp_dimensions"]; !ok {
		t.Fatalf("handoff facts missing dp_dimensions: %#v", handoff.Facts)
	}
	if handoff.OfficialRootTask != "" {
		t.Fatalf("solve handoff retained root prompt: %q", handoff.OfficialRootTask)
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassNumericDP || input["scaffold_id"] != BraidScaffoldIDRecurrenceTableV1 {
		t.Fatalf("helper input missing numeric DP scaffold contract: %#v", input)
	}
	if !braidHelperInputLooksLikeNumericDP(input) {
		t.Fatalf("helper input did not satisfy numeric DP contract: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: numeric_dp", "Numeric-DP output contract", "recurrence table", "solution = <integer>"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidNodeHandoffTransformsSequenceSimulationTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Simulate a typed JSON state trace.",
		"",
		"Puzzle instance:",
		"Sequence model: json_patch_v1",
		"Initial state: {\"count\":0,\"log\":[]}",
		"Events: [{\"op\":\"inc\",\"path\":[\"count\"],\"delta\":2},{\"op\":\"append\",\"path\":[\"log\"],\"value\":\"tick\"}]",
		"Invariants: [{\"path\":[\"count\"],\"min\":0}]",
		"Goal state: {\"count\":2,\"log\":[\"tick\"]}",
		"",
		"Format your solution as:",
		"solution = <JSON final_state>",
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Simulate the sequence.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "facts"}, "")
	if handoff.TaskType != BraidScaffoldClassSequenceSimulation {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassSequenceSimulation)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassSequenceSimulation || handoff.ScaffoldID != BraidScaffoldIDJSONPatchSequenceV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassSequenceSimulation || input["scaffold_id"] != BraidScaffoldIDJSONPatchSequenceV1 {
		t.Fatalf("helper input missing sequence scaffold contract: %#v", input)
	}
	if _, ok := sequenceSimulationSpecFromInput(input); !ok {
		t.Fatalf("helper input did not parse as sequence simulation: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: sequence_simulation", "Sequence-simulation output contract", "json_patch_v1", "solution = <JSON final_state>"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidNodeHandoffTransformsLargeGridResourceGraphSearchTask(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("Puzzle description:\nFind the minimum initial health required in a large weighted grid.\n\n")
	b.WriteString("Puzzle instance:\n\n")
	b.WriteString("Grid size: 55×55\n")
	b.WriteString("Grid layout: [")
	for i := 0; i < 55; i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("[")
		for j := 0; j < 55; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			switch {
			case i == 0 && j == 0:
				b.WriteString("0")
			case (i+j)%7 == 0:
				b.WriteString("4")
			default:
				b.WriteString("-6")
			}
		}
		b.WriteString("]")
	}
	b.WriteString("]\n")
	b.WriteString("Starting position: (0, 0)\n")
	b.WriteString("Goal position: (54, 54)\n")
	rootPrompt := b.String()

	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Compute the minimum initial health.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "grid facts"}, "")
	input := BraidHandoffHelperInput(handoff)
	if handoff.ScaffoldID != BraidScaffoldIDResourcePathMinInitialV1 {
		t.Fatalf("scaffold id=%q, facts=%v input keys=%v", handoff.ScaffoldID, sortedHelperFactoryMapKeys(handoff.Facts), sortedHelperFactoryMapKeys(input))
	}
	grid, ok := intGridFromAny(input["grid_layout"])
	if !ok {
		t.Fatalf("helper input missing parseable grid_layout; keys=%v", sortedHelperFactoryMapKeys(input))
	}
	if len(grid) != 55 || len(grid[0]) != 55 {
		t.Fatalf("grid dimensions=%dx%d, want 55x55", len(grid), len(grid[0]))
	}
}

func TestRenderBraidNodeChildPromptIncludesRootTaskForSolve(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:             "n_solve",
		Kind:           "solve",
		Question:       "Solve node_4.",
		DependsOn:      []string{"n_extract"},
		ExpectedOutput: "node_4 value",
	}, "Official problem text", map[string]string{"n_extract": "requested_outputs: node_4; known_values: node_2=7"})

	if !strings.Contains(prompt, "Official root task:") || !strings.Contains(prompt, "Official problem text") {
		t.Fatalf("solve prompt should include full root task:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Dependency summaries:") || !strings.Contains(prompt, "requested_outputs: node_4") {
		t.Fatalf("solve prompt missing condensed dependency context:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Internal scaffold note") || !strings.Contains(prompt, "may still use its provided scratch/runtime phases") {
		t.Fatalf("solve prompt missing internal scaffold note:\n%s", prompt)
	}
}

func TestRenderBraidNodeChildPromptAddsExtractContract(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:             "n_extract",
		Kind:           "extract",
		Question:       "Extract facts.",
		ExpectedOutput: "facts",
	}, "Official problem text", nil)

	for _, want := range []string{
		"Extract-node contract:",
		"return facts only",
		"Do not solve, verify, reduce, declare blocked",
		"requested_outputs",
		"cycle_clusters",
		"candidate_bounds",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestRenderBraidNodeChildPromptAddsCycleSolveContract(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:             "n_cycle",
		Kind:           "cycle_solve",
		Question:       "Solve mutual constraints.",
		DependsOn:      []string{"n_extract"},
		ExpectedOutput: "candidate values plus checks",
	}, "Official problem text", map[string]string{"n_extract": "cycle_clusters: [[node_2,node_5,node_6,node_7]]; equations_or_checks: prime factor sum = 6"})

	for _, want := range []string{
		"Cycle-solve contract:",
		"candidate search",
		"fixed-point iteration",
		"constraint propagation",
		"finite candidate bounds",
		"cycle_json:",
		"Do not return `solution =`",
		`"candidates"`,
		`"checks"`,
		"full official root task is intentionally withheld",
		"cycle_clusters: [[node_2,node_5,node_6,node_7]]",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Official root task:") || strings.Contains(prompt, "Official problem text") {
		t.Fatalf("cycle_solve prompt should omit full root task:\n%s", prompt)
	}
}

func TestEffectiveBraidNodeSummaryCharsFloorsCycleSolve(t *testing.T) {
	t.Parallel()

	got := EffectiveBraidNodeSummaryChars(BraidNode{
		ID:              "n_cycle",
		Kind:            "cycle_solve",
		MaxSummaryChars: 120,
	})
	if got != minCycleSolveSummaryChars {
		t.Fatalf("EffectiveBraidNodeSummaryChars()=%d want %d", got, minCycleSolveSummaryChars)
	}
	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:              "n_cycle",
		Kind:            "cycle_solve",
		Question:        "solve cycle",
		MaxSummaryChars: 120,
	}, "root", nil)
	if !strings.Contains(prompt, "under 900 characters") {
		t.Fatalf("prompt missing cycle summary floor:\n%s", prompt)
	}
}

func TestRenderBraidNodeChildPromptKeepsRootTaskForVerify(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:        "n_verify",
		Kind:      "verify",
		Question:  "Substitute candidates into original constraints.",
		DependsOn: []string{"n_solve"},
	}, "Official problem text", map[string]string{"n_solve": "candidate values"})
	if !strings.Contains(prompt, "Official root task:") || !strings.Contains(prompt, "Official problem text") {
		t.Fatalf("verify prompt should include full root task:\n%s", prompt)
	}
	if !strings.Contains(prompt, `{"status":"pass","answer":"pass: true","pass":true`) {
		t.Fatalf("verify prompt should require pass-status NodeArtifact:\n%s", prompt)
	}
}

func TestRenderBraidNodeChildPromptIncludesRepairFeedback(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPromptWithFeedback(BraidNode{
		ID:       "n_solve",
		Kind:     "solve",
		Question: "Solve candidate.",
	}, "Official problem text", nil, "failed constraint: n4 mismatch")
	if !strings.Contains(prompt, "Repair feedback:") || !strings.Contains(prompt, "failed constraint: n4 mismatch") {
		t.Fatalf("prompt missing repair feedback:\n%s", prompt)
	}
}

func TestReplBraidGraphValidationCapUsesPhaseCap(t *testing.T) {
	t.Parallel()

	if got := replBraidGraphValidationCap(REPLRunnerPhase{MaxGraphNodes: 4}, nil); got != 4 {
		t.Fatalf("replBraidGraphValidationCap()=%d want 4", got)
	}
}

func TestStackMovesFromAnswerAcceptsObjectMoves(t *testing.T) {
	t.Parallel()

	moves, ok := stackMovesFromAnswer(`solution = [{"block": 2, "from": 1, "to": 0}, {"move": 3, "from_stack": 0, "to_stack": 2}]`)
	if !ok {
		t.Fatal("stackMovesFromAnswer rejected object move form")
	}
	want := [][3]int{{2, 1, 0}, {3, 0, 2}}
	if len(moves) != len(want) {
		t.Fatalf("moves len=%d, want %d", len(moves), len(want))
	}
	for i := range want {
		if moves[i] != want[i] {
			t.Fatalf("moves[%d]=%v, want %v", i, moves[i], want[i])
		}
	}
}

func TestBraidRuntimeShortcutPassesVerifyForRuntimeVerifiedSolve(t *testing.T) {
	t.Parallel()

	solveSummary := "status: completed summary: status: solved answer: solution = [[2,1,2],[0,0,2]] checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier."
	records := map[string]braidNodeExecutionRecord{}
	recordBraidNodeExecution(records, "n_solve", solveSummary, "helper", runtimeCertificationForNode(BraidNode{ID: "n_solve", Kind: "solve"}, "test"))
	verifySummary, _, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_verify", Kind: "verify"}, map[string]string{"n_solve": solveSummary}, nil, records)
	if !ok {
		t.Fatal("expected runtime verify shortcut")
	}
	if !braidVerificationSummaryPassed(verifySummary) {
		t.Fatalf("verify shortcut did not pass: %s", verifySummary)
	}
}

func TestBraidVerificationSummaryPassedAcceptsQuotedJSONPassField(t *testing.T) {
	t.Parallel()

	summary := `status: completed summary: {"status":"solved","answer":"true","checks":["all constraints checked"],"confidence":0.95,"pass":true}`
	if !braidVerificationSummaryPassed(summary) {
		t.Fatalf("quoted JSON pass field should count as advisory verify pass: %s", summary)
	}
}

func TestBraidRuntimeShortcutReduceForwardsVerifiedSolution(t *testing.T) {
	t.Parallel()

	solveSummary := "status: completed summary: status: solved answer: solution = [[2,1,2],[0,0,2]] checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier."
	verifySummary := "status: pass summary: answer: pass: true checks: upstream solve dependency was already verified by the runtime scaffold verifier."
	records := map[string]braidNodeExecutionRecord{}
	recordBraidNodeExecution(records, "n_solve", solveSummary, "helper", runtimeCertificationForNode(BraidNode{ID: "n_solve", Kind: "solve"}, "test"))
	recordBraidNodeExecution(records, "n_verify", verifySummary, "runtime", runtimeCertificationForNode(BraidNode{ID: "n_verify", Kind: "verify"}, "test"))
	reduceSummary, _, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_solve", "n_verify"}}, map[string]string{
		"n_solve":  solveSummary,
		"n_verify": verifySummary,
	}, map[string]BraidNode{
		"n_solve":  {ID: "n_solve", Kind: "solve"},
		"n_verify": {ID: "n_verify", Kind: "verify"},
	}, records)
	if !ok {
		t.Fatal("expected runtime reduce shortcut")
	}
	if !strings.Contains(reduceSummary, "solution = [[2,1,2],[0,0,2]]") {
		t.Fatalf("reduce shortcut lost solution: %s", reduceSummary)
	}
}

func TestBraidHelperNodeSummaryRejectsScaffoldEchoAsVerifiedSolve(t *testing.T) {
	t.Parallel()

	answer := `solution = [{"text":"{\"answer_format\":\"solution = {\\\"node_0\\\": <answer>}\",\"root_task\":\"Official task text begins...\",\"scaffold_class\":\"explicit_dag\"}"}]`
	summary := formatBraidHelperNodeSummaryVerified(BraidNode{
		ID:             "n_solve_chain",
		Kind:           "solve",
		ExpectedOutput: "final answers for node_4, node_2, node_7",
	}, answer, true)

	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if strings.Contains(summary, "verified candidate with a runtime scaffold verifier") {
		t.Fatalf("summary should not mark scaffold echo as verified: %s", summary)
	}
	if !strings.Contains(summary, "helper input packet") && !strings.Contains(summary, "placeholder") {
		t.Fatalf("summary=%q, want echo/placeholder reason", summary)
	}
}

func TestBraidHelperNodeSummaryRejectsSingleLetterFinalSolve(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummaryVerified(BraidNode{
		ID:             "n_solve_base",
		Kind:           "solve",
		ExpectedOutput: "final answers for node_4, node_2, node_7",
	}, "solution = c", true)

	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "single-letter") && !strings.Contains(summary, "too short") && !strings.Contains(summary, "structured node values") {
		t.Fatalf("summary=%q, want structured/short/single-letter reason", summary)
	}
}

func TestBraidHelperNodeSummaryRejectsUnknownSolveValue(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummaryVerified(BraidNode{
		ID:             "n_solve_0",
		Kind:           "solve",
		ExpectedOutput: "rational number representing the side ratio",
	}, `solution = {"node_0":"UNKNOWN"}`, false)

	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "unknown values") {
		t.Fatalf("summary=%q, want unknown-values reason", summary)
	}
}

func TestBraidHelperNodeSummaryRejectsNonRuntimeJSONPassVerify(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummary(BraidNode{
		ID:   "n_verify",
		Kind: "verify",
	}, `solution = {"clusters":[],"failed_reason":null,"pass":true,"verified":true}`)

	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "non-authoritative") {
		t.Fatalf("summary=%q, want non-authoritative verifier reason", summary)
	}
}

func TestBraidHelperNodeSummaryAcceptsRuntimeJSONPassVerify(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummaryVerified(BraidNode{
		ID:   "n_verify",
		Kind: "verify",
	}, `solution = {"clusters":[],"failed_reason":null,"pass":true,"verified":true}`, true)

	if !strings.Contains(summary, `"status":"pass"`) {
		t.Fatalf("summary=%q, want pass", summary)
	}
}

func TestBraidHelperNodeSummaryRejectsJSONFailedVerify(t *testing.T) {
	t.Parallel()

	summary := formatBraidHelperNodeSummary(BraidNode{
		ID:   "n_verify",
		Kind: "verify",
	}, `solution = {"failed_reason":"bad substitution","pass":false}`)

	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
}

func TestValidateAdaptiveSolveRequiresSolutionLine(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{FinalNode: "n_reduce"}
	node := BraidNode{
		ID:   "n_solve_primary__adaptive_01_node_4",
		Kind: "solve",
	}
	err := validateBraidNodeExecutionSummaryInGraph("graph_fanout", node, "status: completed summary: node_4 depends on missing values", graph.FinalNode, graph)
	if err == nil {
		t.Fatal("expected adaptive solve without solution line to fail validation")
	}
	if !strings.Contains(err.Error(), "target solution line") {
		t.Fatalf("err=%v, want target solution line", err)
	}
}

func TestValidateAdaptiveSolveAllowsSolutionLine(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{FinalNode: "n_reduce"}
	node := BraidNode{
		ID:   "n_solve_primary__adaptive_00_node_0",
		Kind: "solve",
	}
	if err := validateBraidNodeExecutionSummaryInGraph("graph_fanout", node, "status: completed summary: status: solved answer: solution = 5/2 checks: computed", graph.FinalNode, graph); err != nil {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() error = %v", err)
	}
}

func TestNormalizeBraidAdaptiveTargetSummaryWrapsScalarAnswer(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve_cluster__adaptive_01_node_7",
		Kind:           "solve",
		ExpectedOutput: "solution = <answer for node_7>",
	}
	normalized, ok := normalizeBraidAdaptiveTargetSummary(node, "status: completed summary: status: solved answer: 12 checks: computed deterministically")
	if !ok {
		t.Fatal("expected scalar adaptive answer to normalize")
	}
	if !strings.Contains(normalized, `solution = {"node_7":12}`) {
		t.Fatalf("normalized=%q", normalized)
	}
}

func TestNormalizeBraidAdaptiveTargetSummaryWrapsConcreteFormulaAnswer(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve_chain__adaptive_02_node_3",
		Kind:           "solve",
		ExpectedOutput: "solution = <answer for node_3>",
	}
	summary := `status: completed summary: status: solved answer: all pairs $(a, b)$ where $b = ka$ and $k | a^{1922}$ with $a^{1922}/k \equiv -1 \pmod{a}$ checks: verified congruence constraints`
	normalized, ok := normalizeBraidAdaptiveTargetSummary(node, summary)
	if !ok {
		t.Fatal("expected concrete formula adaptive answer to normalize")
	}
	if !strings.Contains(normalized, `"node_3":"all pairs`) {
		t.Fatalf("normalized=%q", normalized)
	}
	if !strings.Contains(normalized, `solution = {`) {
		t.Fatalf("normalized=%q", normalized)
	}
}

func TestNormalizeBraidAdaptiveTargetSummaryRejectsBlockedProse(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve_cluster__adaptive_01_node_7",
		Kind:           "solve",
		ExpectedOutput: "solution = <answer for node_7>",
	}
	if normalized, ok := normalizeBraidAdaptiveTargetSummary(node, "status: completed summary: answer: cannot solve without node_6 checks: missing dependency"); ok {
		t.Fatalf("unexpected normalization: %s", normalized)
	}
}

func TestBraidPreferredHelperFailureFallbackPolicy(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_verify", Kind: "verify"}
	if !braidNodeCanFallbackFromHelperFailure(node, BraidNodeHelperPolicyPreferred) {
		t.Fatal("preferred verify helper should fall back to child RLM after helper execution failure")
	}
	if braidNodeCanFallbackFromHelperFailure(node, BraidNodeHelperPolicyRequired) {
		t.Fatal("required verify helper should not fall back")
	}
	if !braidNodeCanFallbackFromHelperFailure(BraidNode{ID: "n_solve", Kind: "solve"}, BraidNodeHelperPolicyPreferred) {
		t.Fatal("preferred solve helper should fall back to child RLM after helper execution failure")
	}
}

func TestBraidHelperNodeSummaryRejectsScalarForMultiNodeSolve(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve",
		Kind:           "solve",
		ExpectedOutput: "Complete integer answers for nodes 0 through 7.",
		InputSchema: map[string]any{
			"variables": []any{"ans_0", "ans_1", "ans_2"},
		},
	}
	summary := formatBraidHelperNodeSummary(node, "solution = 6")
	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "structured node values") && !strings.Contains(summary, "multi-variable") {
		t.Fatalf("summary=%q, want scalar rejection", summary)
	}
}

func TestBraidHelperNodeSummaryAllowsScalarForSingleNodeOutput(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve_node0",
		Kind:           "solve",
		ExpectedOutput: "Numerator of the ratio from node_0. Show steps if needed but output the integer.",
	}
	summary := formatBraidHelperNodeSummary(node, "solution = 5")
	if strings.Contains(summary, "structured node values") {
		t.Fatalf("summary=%q should not require structured values for one scalar node output", summary)
	}
}

func TestBraidHelperNodeSummaryBlocksUnverifiedAdaptiveExplicitDAG(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve_wave__adaptive_02_node_2",
		Kind:           "solve",
		ExpectedOutput: "solution = <answer for node_2>",
		ScaffoldClass:  BraidScaffoldClassExplicitDAG,
		ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
	}
	summary := formatBraidHelperNodeSummaryVerified(node, "solution = 8", false)
	if !strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want blocked", summary)
	}
	if !strings.Contains(summary, "unverified explicit dependency candidate") {
		t.Fatalf("summary=%q, want unverified explicit-DAG reason", summary)
	}
}

func TestBraidHelperNodeSummaryAllowsVerifiedAdaptiveExplicitDAG(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_solve_wave__adaptive_02_node_2",
		Kind:           "solve",
		ExpectedOutput: "solution = <answer for node_2>",
		ScaffoldClass:  BraidScaffoldClassExplicitDAG,
		ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
	}
	summary := formatBraidHelperNodeSummaryVerified(node, "solution = 8", true)
	if strings.Contains(summary, "status: blocked") {
		t.Fatalf("summary=%q, want completed", summary)
	}
	artifact, ok := parseBraidNodeArtifact(summary)
	if !ok {
		t.Fatalf("summary=%q, want node artifact", summary)
	}
	if artifact.Provenance == nil || artifact.Provenance["runtime_certification"] == nil {
		t.Fatalf("summary=%q, want runtime certification provenance", summary)
	}
}

func TestBraidSummaryFromPythonREPLResult(t *testing.T) {
	t.Parallel()

	raw := `{"ok":true,"output":"stdout:\nstatus: completed summary: status: solved answer: solution = {\"node_2\":2692} checks: python_repl executed deterministic extraction\n","metadata":{"ok":true}}`
	summary, ok := braidSummaryFromPythonREPLResult(raw)
	if !ok {
		t.Fatal("braidSummaryFromPythonREPLResult() rejected valid stdout summary")
	}
	if !strings.Contains(summary, `"node_2":2692`) {
		t.Fatalf("summary=%q", summary)
	}
}

func TestBraidPythonREPLSummarySoftMiss(t *testing.T) {
	t.Parallel()

	summary := "status: blocked summary: answer: checks: python_repl executed deterministic extraction but target was unresolved. detail: target value is not present"
	if !braidPythonREPLSummaryIsSoftMiss(summary) {
		t.Fatal("expected unresolved deterministic extraction to be a soft miss")
	}
	if braidPythonREPLSummaryIsSoftMiss("status: blocked summary: answer: checks: python_repl syntax error") {
		t.Fatal("syntax/runtime failures should not be soft misses")
	}
}

func TestBraidNodeUsesChildREPLInsteadOfHelperWithoutRuntimeVerifier(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:            "n_solve",
		Kind:          "solve",
		Question:      "Solve numeric prose problem.",
		HelperPolicy:  BraidNodeHelperPolicyPreferred,
		ScaffoldClass: BraidScaffoldClassNumericDP,
		ScaffoldID:    BraidScaffoldIDRecurrenceTableV1,
		InputSchema:   map[string]any{"prompt": "plain prose without recurrence table"},
	}
	if !braidNodeShouldUseChildREPLInsteadOfHelper(node, "root task", nil, "") {
		t.Fatal("prose-only solve node should fall through to child RLM with python_repl")
	}
	node.InputSchema = map[string]any{
		"objective":     "count",
		"dp_dimensions": []any{3},
		"target":        []any{2},
		"base_cases":    []any{map[string]any{"index": []any{0}, "value": 1}},
		"transitions":   []any{map[string]any{"offset": []any{-1}, "weight": 1}},
	}
	if braidNodeShouldUseChildREPLInsteadOfHelper(node, "root task", nil, "") {
		t.Fatal("typed verifier-backed node should keep helper-first")
	}

	explicit := BraidNode{
		ID:            "n_parent__adaptive_00_node_0",
		Kind:          "solve",
		Question:      "Solve one explicit dependency item.",
		HelperPolicy:  BraidNodeHelperPolicyPreferred,
		ScaffoldClass: BraidScaffoldClassExplicitDAG,
		ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
		InputSchema:   map[string]any{"target_node": "node_0"},
	}
	if !braidNodeShouldUseChildREPLInsteadOfHelper(explicit, "root task", nil, "") {
		t.Fatal("explicit DAG without expected answer should fall through to child RLM")
	}
	explicit.InputSchema = map[string]any{"target_nodes": []any{"node_4", "node_2", "node_7"}}
	if braidNodeShouldUseChildREPLInsteadOfHelper(explicit, "root task", nil, "") {
		t.Fatal("explicit DAG with typed target_nodes should keep helper-first")
	}
	explicit.InputSchema["answer"] = "42"
	if braidNodeShouldUseChildREPLInsteadOfHelper(explicit, "root task", nil, "") {
		t.Fatal("explicit DAG with expected answer should keep helper-first verifier path")
	}

	explicit.InputSchema = map[string]any{
		"source_ref": "official_prompt",
		"prompt":     "use the official prompt state",
	}
	rootPrompt := strings.Join([]string{
		"Puzzle instance:",
		"Initial state: [[0], [1, 2], []]",
		"Goal state: [[], [1], [2, 0]]",
		"Number of blocks: 3",
		"Number of stacks: 3",
	}, "\n")
	if braidNodeShouldUseChildREPLInsteadOfHelper(explicit, rootPrompt, nil, "") {
		t.Fatal("explicit DAG with typed stack transition evidence should keep helper-first")
	}
}

func TestBraidSolveNodeRequiresRuntimeVerificationAllowsAdaptiveSingleTargetLeaf(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:            "n_solve__adaptive_00_node_0",
		Kind:          "solve",
		ScaffoldClass: BraidScaffoldClassExplicitDAG,
		ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
		InputSchema: map[string]any{
			"target_node":  "node_0",
			"target_nodes": []any{"node_0"},
		},
	}
	if braidSolveNodeRequiresRuntimeVerification(node) {
		t.Fatal("single-target adaptive leaf should not require runtime verification")
	}

	node.InputSchema = map[string]any{"target_nodes": []any{"node_0", "node_1"}}
	if !braidSolveNodeRequiresRuntimeVerification(node) {
		t.Fatal("multi-target adaptive node should require runtime verification")
	}
}

func TestBuildBraidPythonREPLSolveCodeExtractsDependencyValue(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_parent__adaptive_02_node_2",
		Kind:           "solve",
		Question:       "Solve only node_2.",
		ExpectedOutput: "solution = <answer for node_2>",
		ScaffoldClass:  BraidScaffoldClassExplicitDAG,
		ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
	}
	code, err := buildBraidPythonREPLSolveCode(node, map[string]any{
		"target_node": "node_2",
		"dependency_summaries": map[string]any{
			"n_prior": map[string]any{
				"answer": "solution = {\"node_2\": 2692}",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildBraidPythonREPLSolveCode() error = %v", err)
	}
	if !strings.Contains(code, "python_repl executed deterministic extraction") {
		t.Fatalf("code missing deterministic extraction marker")
	}
	if !strings.Contains(code, "node_2") || !strings.Contains(code, "2692") {
		t.Fatalf("code does not embed packet: %s", code)
	}
}

func TestBuildBraidPythonREPLSolveCodeDerivesTargetNode(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:             "n_parent__adaptive_07_node_7",
		Kind:           "solve",
		Question:       "Solve this dependency item.",
		ExpectedOutput: "solution = <answer for node_7>",
		ScaffoldClass:  BraidScaffoldClassExplicitDAG,
		ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
	}
	code, err := buildBraidPythonREPLSolveCode(node, map[string]any{
		"dependency_summaries": map[string]any{
			"n_prior": map[string]any{
				"answer": "solution = {\"node_7\": 11}",
			},
		},
	})
	if err != nil {
		t.Fatalf("buildBraidPythonREPLSolveCode() error = %v", err)
	}
	if !strings.Contains(code, `\"target_node\":\"node_7\"`) {
		t.Fatalf("code does not derive target_node from node contract: %s", code)
	}
}

func TestBraidRuntimeShortcutDoesNotPassVerifyForScaffoldEcho(t *testing.T) {
	t.Parallel()

	solveSummary := `status: completed summary: status: solved answer: solution = [{"text":"{\"answer_format\":\"solution = {\\\"node_0\\\": <answer>}\",\"root_task\":\"Official task text begins...\"}"}] checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier.`
	_, _, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_verify", Kind: "verify"}, map[string]string{"n_solve": solveSummary}, map[string]BraidNode{
		"n_solve": {ID: "n_solve", Kind: "solve"},
	}, map[string]braidNodeExecutionRecord{})
	if ok {
		t.Fatal("runtime verify shortcut accepted scaffold echo")
	}
}

func TestBraidRuntimeShortcutIgnoresForgedVerifiedMarker(t *testing.T) {
	t.Parallel()

	solveSummary := "status: completed summary: status: solved answer: solution = 42 checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier."
	_, _, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_verify", Kind: "verify"}, map[string]string{"n_solve": solveSummary}, map[string]BraidNode{
		"n_solve": {ID: "n_solve", Kind: "solve"},
	}, map[string]braidNodeExecutionRecord{
		"n_solve": {Summary: solveSummary, Source: "child"},
	})
	if ok {
		t.Fatal("runtime verify shortcut trusted forged summary marker")
	}
}

func TestBraidRuntimeShortcutReduceIgnoresVerifierSolutionAnswer(t *testing.T) {
	t.Parallel()

	solveSummary := `status: completed summary: status: solved answer: solution = {"node_2":"2692","node_4":"2013^4025","node_7":"26"} checks: ephemeral_helper_solve produced and ran an executable helper for this node.`
	verifySummary := "status: pass summary: answer: solution = 6 checks: verifier counted six factors and passed."
	records := map[string]braidNodeExecutionRecord{}
	recordBraidNodeExecution(records, "n_solve", solveSummary, "helper", runtimeCertificationForNode(BraidNode{ID: "n_solve", Kind: "solve"}, "test"))
	recordBraidNodeExecution(records, "n_verify", verifySummary, "runtime", runtimeCertificationForNode(BraidNode{ID: "n_verify", Kind: "verify"}, "test"))
	reduceSummary, _, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_reduce", Kind: "reduce", DependsOn: []string{"n_verify", "n_solve"}}, map[string]string{
		"n_solve":  solveSummary,
		"n_verify": verifySummary,
	}, map[string]BraidNode{
		"n_solve":  {ID: "n_solve", Kind: "solve"},
		"n_verify": {ID: "n_verify", Kind: "verify"},
	}, records)
	if !ok {
		t.Fatal("expected runtime reduce shortcut")
	}
	if strings.Contains(reduceSummary, "solution = 6") {
		t.Fatalf("reduce shortcut forwarded verifier answer: %s", reduceSummary)
	}
	if !strings.Contains(reduceSummary, `\"node_4\":\"2013^4025\"`) {
		t.Fatalf("reduce shortcut did not forward solve answer: %s", reduceSummary)
	}
}

func TestBraidRuntimeShortcutSkipsSplitMergeNode(t *testing.T) {
	t.Parallel()

	solveSummary := `{"status":"solved","answer":"solution = chunk_0","checks":["runtime scaffold verifier passed"],"confidence":1}`
	records := map[string]braidNodeExecutionRecord{}
	recordBraidNodeExecution(records, "n_parent__solve_00", solveSummary, "helper", runtimeCertificationForNode(BraidNode{ID: "n_parent__solve_00", Kind: "solve"}, "test"))

	merge := BraidNode{
		ID:        "n_parent__merge",
		Kind:      "reduce",
		DependsOn: []string{"n_parent__solve_00"},
		InputSchema: map[string]any{
			"split_role": "merge",
			"solve_ids":  []any{"n_parent__solve_00"},
		},
	}
	if summary, _, ok := runBraidRuntimeNodeShortcut(merge, map[string]string{"n_parent__solve_00": solveSummary}, map[string]BraidNode{
		"n_parent__solve_00": {ID: "n_parent__solve_00", Kind: "solve"},
	}, records); ok {
		t.Fatalf("split merge node used generic reduce shortcut: %s", summary)
	}
}

func TestHelperFailureSummaryIncludesLatestCounterexample(t *testing.T) {
	t.Parallel()

	raw := `{
		"ok": false,
		"error": "helper factory failed after 2 attempts",
		"repair_harness": {
			"kind": "counterexample_repair",
			"latest_counterexample": {
				"failure_kind": "constraint_mismatch",
				"first_failure": "node_2 violates dependency equation",
				"failed_node": "node_2",
				"observed": 52,
				"expected": 2692,
				"repair_hint": "repair the failed node before reducing"
			}
		}
	}`
	summary, ok := helperFailureSummaryFromToolResult(BraidNode{ID: "n_verify", Kind: "verify"}, raw)
	if !ok {
		t.Fatal("expected helper failure summary")
	}
	for _, want := range []string{"counterexample:", "constraint_mismatch", "node_2", "2692"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestHelperFailureSummaryIncludesContractCounterexample(t *testing.T) {
	t.Parallel()

	raw := `{
		"ok": false,
		"error": "helper factory failed after 5 attempts: decode repair JSON: no valid draft JSON object found",
		"repair_harness": {
			"kind": "counterexample_repair",
			"latest_counterexample": {
				"failure_kind": "helper_contract_failure",
				"stage": "draft",
				"first_failure": "decode repair JSON: no valid draft JSON object found",
				"observed": "markdown response",
				"expected": "valid helper draft JSON object",
				"repair_hint": "return exactly one JSON object"
			}
		}
	}`
	summary, ok := helperFailureSummaryFromToolResult(BraidNode{ID: "n_solve", Kind: "solve"}, raw)
	if !ok {
		t.Fatal("expected helper failure summary")
	}
	for _, want := range []string{"counterexample:", "helper_contract_failure", "valid helper draft JSON object", "return exactly one JSON object"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestHelperVerifyFailureSummaryFromOutputSummary(t *testing.T) {
	t.Parallel()

	raw := `{
		"ok": true,
		"answer": "solution = [1, 3, 2]",
		"output_summary": {
			"ok": true,
			"pass": false,
			"answer": "solution = [1, 3, 2]",
			"first_failure": "node_2 does not satisfy dependency",
			"observed": 3,
			"expected": 2692,
			"repair_hint": "repair node_2 before reducing"
		}
	}`
	summary, ok := helperVerifyFailureSummaryFromToolResult(raw)
	if !ok {
		t.Fatal("expected verify failure summary")
	}
	for _, want := range []string{"pass: false", "node_2 does not satisfy dependency", "repair node_2", "solution = [1, 3, 2]"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestEnrichBraidHelperInputWithStructuredTargetProblemText(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"target_node": "node_0",
		"prompt":      "original problem",
		"problems": []any{
			map[string]any{"id": "node_0", "text": "Compute the first value."},
			map[string]any{"id": "node_1", "text": "Use node_0."},
		},
	}
	enriched := enrichBraidHelperInputWithStructuredTargets(input, "Problem node_0: should not be parsed")
	if enriched["target_problem_text"] != "Compute the first value." {
		t.Fatalf("target_problem_text=%q", enriched["target_problem_text"])
	}
	if enriched["prompt"] != "Compute the first value." {
		t.Fatalf("prompt=%q", enriched["prompt"])
	}
}

func TestEnrichBraidHelperInputWithStructuredTargetProblemTexts(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"target_nodes": []any{"node_2", "node_5", "node_6"},
		"problem_texts": map[string]any{
			"node_2": "Solve the fixed point.",
			"node_5": "Depends on node_2.",
			"node_6": map[string]any{"question": "Depends on node_5."},
		},
	}
	enriched := enrichBraidHelperInputWithStructuredTargets(input, "Problem node_2: should not be parsed")
	texts, _ := enriched["target_problem_texts"].(map[string]any)
	if len(texts) != 3 {
		t.Fatalf("target_problem_texts=%#v", texts)
	}
	if texts["node_2"] != "Solve the fixed point." {
		t.Fatalf("node_2 text=%q", texts["node_2"])
	}
	if texts["node_6"] != "Depends on node_5." {
		t.Fatalf("node_6 text=%q", texts["node_6"])
	}
}

func TestEnrichBraidHelperInputDoesNotParseRootPromptTargets(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"target_node": "node_0",
	}
	enriched := enrichBraidHelperInputWithStructuredTargets(input, "Problem node_0: should not be parsed")
	if _, ok := enriched["target_problem_text"]; ok {
		t.Fatalf("unexpected target_problem_text from root prompt: %#v", enriched["target_problem_text"])
	}
}

func TestBraidVerifyHelperAnswerContractRejectsSolutionLine(t *testing.T) {
	t.Parallel()

	diag, applicable := braidVerifyHelperAnswerContract("solution = true", nil)
	if !applicable {
		t.Fatal("verify answer contract should apply")
	}
	if diag.Pass {
		t.Fatal("solution line should not satisfy verify answer contract")
	}
	if !strings.Contains(diag.FirstFailure, "explicit pass true/false") {
		t.Fatalf("diag=%+v", diag)
	}

	diag, applicable = braidVerifyHelperAnswerContract("pass: false first_failure: node_2 mismatch", nil)
	if !applicable || !diag.Pass {
		t.Fatalf("explicit pass false should satisfy verifier output contract: applicable=%v diag=%+v", applicable, diag)
	}

	diag, applicable = braidVerifyHelperAnswerContract(`cycle_json: {"pass": true, "candidates": [], "checks": []}`, nil)
	if !applicable {
		t.Fatal("verify answer contract should apply to JSON pass payload")
	}
	if diag.Pass {
		t.Fatal("empty pass:true JSON should not satisfy verify answer contract")
	}
	if !strings.Contains(diag.FirstFailure, "no concrete checks") {
		t.Fatalf("diag=%+v", diag)
	}

	diag, applicable = braidVerifyHelperAnswerContract(`cycle_json: {"pass": true, "candidates": {"node_2": 2692}, "checks": [{"name":"substitution","ok":true}]}`, nil)
	if !applicable || !diag.Pass {
		t.Fatalf("non-empty pass:true JSON should satisfy verifier output contract: applicable=%v diag=%+v", applicable, diag)
	}
}

func TestBraidVerifyAnswerJSONPassAcceptsPrefixedJSON(t *testing.T) {
	t.Parallel()

	pass, ok := braidVerifyAnswerJSONPass(`cycle_json: {"pass": true, "candidates": [1,3,27], "checks": ["checked"]}`)
	if !ok || !pass {
		t.Fatalf("braidVerifyAnswerJSONPass()=(%v,%v), want true,true", pass, ok)
	}
}

func TestBraidNodeHelperBudgetRemainingSubTimeout(t *testing.T) {
	t.Parallel()

	budget := &braidNodeHelperBudget{}

	rem := budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != braidNodeHelperDefaultBudget {
		t.Fatalf("fresh budget remaining = %v, want %v", rem, braidNodeHelperDefaultBudget)
	}

	budget.CumulativeDuration = braidNodeHelperDefaultBudget - 30*time.Second
	rem = budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != 30*time.Second {
		t.Fatalf("after 11m30s used, remaining = %v, want 30s", rem)
	}

	budget.CumulativeDuration = braidNodeHelperDefaultBudget - braidNodeHelperMinSubTimeout - time.Second
	rem = budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != braidNodeHelperMinSubTimeout+time.Second {
		t.Fatalf("just above floor remaining = %v, want %v", rem, braidNodeHelperMinSubTimeout+time.Second)
	}

	budget.CumulativeDuration = braidNodeHelperDefaultBudget - braidNodeHelperMinSubTimeout
	rem = budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != braidNodeHelperMinSubTimeout {
		t.Fatalf("exactly at floor remaining = %v, want %v", rem, braidNodeHelperMinSubTimeout)
	}

	budget.CumulativeDuration = braidNodeHelperDefaultBudget - braidNodeHelperMinSubTimeout + time.Second
	rem = budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != 0 {
		t.Fatalf("below floor remaining = %v, want 0", rem)
	}

	budget.CumulativeDuration = braidNodeHelperDefaultBudget + time.Minute
	rem = budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != 0 {
		t.Fatalf("over-budget remaining = %v, want 0", rem)
	}

	budget.CumulativeDuration = braidNodeHelperDefaultBudget + time.Minute
	rem = budget.remainingSubTimeout(braidNodeHelperDefaultBudget)
	if rem != 0 {
		t.Fatalf("over-budget remaining = %v, want 0", rem)
	}
}

func TestBraidNodeHelperBudgetTracking(t *testing.T) {
	t.Parallel()

	budgets := make(helperBudgetByNode)
	b1 := budgets.get("n1")
	b2 := budgets.get("n2")

	if b1 == b2 {
		t.Fatal("different nodes should have different budget entries")
	}
	if b1.Attempts != 0 || b1.CumulativeDuration != 0 {
		t.Fatal("fresh budget should be zero-valued")
	}

	b1.Attempts++
	b1.CumulativeDuration += 5 * time.Minute

	refetched := budgets.get("n1")
	if refetched.Attempts != 1 || refetched.CumulativeDuration != 5*time.Minute {
		t.Fatal("budget get should return the same pointer")
	}
}

func TestFormatDeadlineExhausted(t *testing.T) {
	t.Parallel()

	budget := &braidNodeHelperBudget{
		CumulativeDuration: 12 * time.Minute,
		Attempts:           3,
	}
	msg := formatDeadlineExhausted("n_solve", budget)
	if !strings.Contains(msg, braidDeadlineExhaustedPrefix) {
		t.Fatalf("message should contain %q: %s", braidDeadlineExhaustedPrefix, msg)
	}
	if !strings.Contains(msg, "n_solve") {
		t.Fatalf("message should contain node id: %s", msg)
	}
	if !strings.Contains(msg, "cumulative_ms=") {
		t.Fatalf("message should contain cumulative_ms: %s", msg)
	}
	if !strings.Contains(msg, "attempts=3") {
		t.Fatalf("message should contain attempts=3: %s", msg)
	}
}

func TestCapBraidRepairFeedback(t *testing.T) {
	t.Parallel()

	short := "short feedback"
	if got := capBraidRepairFeedback(short); got != short {
		t.Fatalf("short feedback should pass through: %q", got)
	}

	long := strings.Repeat("x", braidRepairFeedbackCap+500)
	capped := capBraidRepairFeedback(long)
	if len(capped) > braidRepairFeedbackCap {
		t.Fatalf("capped length %d exceeds cap %d", len(capped), braidRepairFeedbackCap)
	}
	if !strings.HasSuffix(capped, "...[truncated]") {
		t.Fatalf("capped feedback should end with truncation marker: %q", capped[len(capped)-30:])
	}

	exact := strings.Repeat("y", braidRepairFeedbackCap)
	if got := capBraidRepairFeedback(exact); len(got) != braidRepairFeedbackCap {
		t.Fatalf("exact-length feedback should not be truncated: got %d want %d", len(got), braidRepairFeedbackCap)
	}
}

func TestExtractBraidHelperFailedStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		summary string
		want    braidHelperStage
	}{
		{"stage=draft something went wrong", braidHelperStageDraft},
		{"draft error: could not generate", braidHelperStageDraft},
		{"stage=parse decode failed", braidHelperStageParse},
		{"SyntaxError: unexpected token", braidHelperStageParse},
		{"stage=validate source too long", braidHelperStageValidate},
		{"stage=run timeout exceeded", braidHelperStageRun},
		{"stage=verify candidate failed", braidHelperStageVerify},
		{"verifier rejected the candidate", braidHelperStageVerify},
		{"helper compilation error", braidHelperStageValidate},
		{"unknown error occurred", braidHelperStageRun},
		{"", braidHelperStageRun},
	}

	for idx, tt := range tests {
		got := extractBraidHelperFailedStage(tt.summary)
		if got != tt.want {
			t.Errorf("test %d: extractBraidHelperFailedStage(%q) = %q, want %q", idx, tt.summary, got, tt.want)
		}
	}
}

func TestBraidNodeHandoffTransformsSymbolicTraceTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Type inference puzzle.",
		"",
		"You must execute deterministic Algorithm W on a program.",
		"",
		"Puzzle instance:",
		`program: let id = (\a. a) in let t0 = 0 in let t1 = true in let t2 = (t0, t1) in let t3 = snd t2 in t3`,
		`queries: [{"kind": "type", "target": "q1"}, {"kind": "count", "target": "q2"}]`,
		"trace_kind: HM-TRACE",
		"",
		"Format your solution as:",
		`solution = {"q1": "...", "q2": ...}`,
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Infer types.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "typed facts"}, "")

	if handoff.TaskType != BraidScaffoldClassSymbolicTrace {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassSymbolicTrace)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassSymbolicTrace || handoff.ScaffoldID != BraidScaffoldIDTypeInferenceV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	prog, ok := handoff.Facts["program"].(string)
	if !ok || !strings.Contains(prog, "let id") {
		t.Fatalf("handoff facts missing program text: %#v", handoff.Facts)
	}
	queriesRaw, ok := handoff.Facts["queries"]
	if !ok {
		t.Fatalf("handoff facts missing queries: %#v", handoff.Facts)
	}
	queries, ok := queriesRaw.([]any)
	if !ok || len(queries) < 2 {
		t.Fatalf("handoff facts queries not []any or too short: %T %#v", queriesRaw, queriesRaw)
	}
	firstQuery, ok := queries[0].(map[string]any)
	if !ok || firstQuery["target"] != "q1" {
		t.Fatalf("first query target=%v, want q1", queries[0])
	}
	if handoff.OfficialRootTask != "" {
		t.Fatalf("solve handoff retained root prompt: %q", handoff.OfficialRootTask)
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassSymbolicTrace || input["scaffold_id"] != BraidScaffoldIDTypeInferenceV1 {
		t.Fatalf("helper input missing symbolic trace scaffold contract: %#v", input)
	}
	if !braidHelperInputLooksLikeSymbolicTrace(input) {
		t.Fatalf("helper input did not satisfy symbolic trace contract: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: symbolic_trace", "Symbolic-trace output contract", "Algorithm W", "let-binding"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestBraidHelperInputLooksLikeSymbolicTrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input map[string]any
		want  bool
	}{
		{
			name:  "empty input",
			input: map[string]any{},
			want:  false,
		},
		{
			name: "has program and queries",
			input: map[string]any{
				"scaffold_class": BraidScaffoldClassSymbolicTrace,
				"scaffold_id":    BraidScaffoldIDTypeInferenceV1,
				"program":        "let x = 0 in x",
				"queries":        []map[string]any{{"kind": "query", "target": "q1"}},
			},
			want: true,
		},
		{
			name: "has program only",
			input: map[string]any{
				"scaffold_class": BraidScaffoldClassSymbolicTrace,
				"program":        "let x = 0 in x",
			},
			want: true,
		},
		{
			name: "wrong scaffold_class",
			input: map[string]any{
				"scaffold_class": BraidScaffoldClassConstraintSolver,
				"program":        "let x = 0 in x",
			},
			want: false,
		},
		{
			name: "wrong scaffold_id",
			input: map[string]any{
				"scaffold_class": BraidScaffoldClassSymbolicTrace,
				"scaffold_id":    "wrong_id",
				"program":        "let x = 0 in x",
			},
			want: false,
		},
		{
			name: "no program or queries",
			input: map[string]any{
				"scaffold_class": BraidScaffoldClassSymbolicTrace,
				"scaffold_id":    BraidScaffoldIDTypeInferenceV1,
				"trace_kind":     "HM-TRACE",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := braidHelperInputLooksLikeSymbolicTrace(tt.input)
			if got != tt.want {
				t.Errorf("braidHelperInputLooksLikeSymbolicTrace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveBraidRuntimeScaffoldSymbolicTrace(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{
		Node:          node,
		ScaffoldClass: BraidScaffoldClassSymbolicTrace,
		ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
		Facts: map[string]any{
			"scaffold_class": BraidScaffoldClassSymbolicTrace,
			"scaffold_id":    BraidScaffoldIDTypeInferenceV1,
			"program":        "let x = 0 in x",
			"queries":        []map[string]any{{"kind": "query", "target": "q1"}},
		},
	}
	input := BraidHandoffHelperInput(handoff)
	scaffold, ok := resolveBraidRuntimeScaffold(node, handoff, input)
	if !ok {
		t.Fatal("resolveBraidRuntimeScaffold returned false for symbolic trace")
	}
	if scaffold.Class != BraidScaffoldClassSymbolicTrace {
		t.Fatalf("scaffold class=%q, want %s", scaffold.Class, BraidScaffoldClassSymbolicTrace)
	}
	if scaffold.ID != BraidScaffoldIDTypeInferenceV1 {
		t.Fatalf("scaffold id=%q, want %s", scaffold.ID, BraidScaffoldIDTypeInferenceV1)
	}
	if scaffold.PresetSource == "" {
		t.Fatal("scaffold preset source is empty")
	}
	if scaffold.Language != HelperLanguagePython {
		t.Fatalf("scaffold language=%q, want %q", scaffold.Language, HelperLanguagePython)
	}
	if scaffold.Verifier == nil {
		t.Fatal("scaffold verifier is nil")
	}
	if scaffold.MaxSourceLines != 380 {
		t.Fatalf("MaxSourceLines=%d, want 380", scaffold.MaxSourceLines)
	}
	cfg := applyBraidRuntimeScaffoldToHelperConfig(HelperFactoryConfig{Language: HelperLanguageGo}, scaffold)
	if cfg.Language != HelperLanguagePython {
		t.Fatalf("helper config language=%q, want %q", cfg.Language, HelperLanguagePython)
	}
}

func TestTypeInferenceAnswerVerifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		answer   string
		input    map[string]any
		wantOK   bool
		wantPass bool
	}{
		{
			name:   "empty input returns not ok",
			answer: `solution = {"q1": "Nat"}`,
			input:  map[string]any{},
			wantOK: false,
		},
		{
			name: "query keys without ground truth fail closed",
			answer: `ok: true
solution = {"q1": "(Nat × Bool)", "q2": 5}`,
			input: map[string]any{
				"queries": []any{
					map[string]any{"kind": "query", "target": "q1"},
					map[string]any{"kind": "query", "target": "q2"},
				},
			},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:   "missing query key",
			answer: `solution = {"q1": "Nat"}`,
			input: map[string]any{
				"queries": []any{
					map[string]any{"kind": "query", "target": "q1"},
					map[string]any{"kind": "query", "target": "q2"},
				},
			},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:   "no solution marker",
			answer: "the answer is 42",
			input: map[string]any{
				"queries": []any{},
			},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:   "matching ground truth",
			answer: `solution = {"q1": "Nat"}`,
			input: map[string]any{
				"queries": []any{
					map[string]any{"kind": "query", "target": "q1"},
				},
				"answer": map[string]any{
					"q1": "Nat",
				},
			},
			wantOK:   true,
			wantPass: true,
		},
		{
			name:   "wrong ground truth",
			answer: `solution = {"q1": "Nat"}`,
			input: map[string]any{
				"queries": []any{
					map[string]any{"kind": "query", "target": "q1"},
				},
				"answer": map[string]any{
					"q1": "Bool",
				},
			},
			wantOK:   true,
			wantPass: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag, ok := typeInferenceAnswerVerifier(tt.answer, tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok=%v, want %v; diag=%+v", ok, tt.wantOK, diag)
			}
			if ok && tt.wantOK && diag.Pass != tt.wantPass {
				t.Errorf("pass=%v, want %v; first_failure=%q", diag.Pass, tt.wantPass, diag.FirstFailure)
			}
		})
	}
}

func TestSymbolicTraceScaffoldPresetSource(t *testing.T) {
	t.Parallel()

	src := typeInferencePresetSource()
	if !strings.Contains(src, "def Solve(input)") {
		t.Fatal("preset source missing Solve function")
	}
	if !strings.Contains(src, "unify") {
		t.Fatal("preset source missing unify function")
	}
	if !strings.Contains(src, "occurs") {
		t.Fatal("preset source missing occurs check")
	}
	if !strings.Contains(src, "Algorithm W") || !strings.Contains(src, "let-binding") {
		t.Fatal("preset source missing Algorithm W or let-binding reference")
	}
}

func TestBraidNodeHandoffTransformsCandidateVerifyTask(t *testing.T) {
	t.Parallel()

	rootPrompt := strings.Join([]string{
		"Puzzle description:",
		"Pick the item with the most specific property.",
		"",
		"Puzzle instance:",
		`candidates: ["CCO", "C=O", "CC=O"]`,
		`predicates: [{"name": "p1", "check_type": "contains", "expected": "O"}]`,
		"selection_rule: best",
		"",
		"Format your solution as:",
		"solution = <letter>",
	}, "\n")
	node := BraidNode{ID: "n_solve", Kind: "solve", Question: "Pick the correct item.", DependsOn: []string{"n_extract"}}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, map[string]string{"n_extract": "candidates extracted"}, "")

	if handoff.TaskType != BraidScaffoldClassCandidateVerify {
		t.Fatalf("task type=%q, want %s", handoff.TaskType, BraidScaffoldClassCandidateVerify)
	}
	if handoff.ScaffoldClass != BraidScaffoldClassCandidateVerify || handoff.ScaffoldID != BraidScaffoldIDPropertyCheckV1 {
		t.Fatalf("scaffold class/id=%q/%q", handoff.ScaffoldClass, handoff.ScaffoldID)
	}
	_, ok := handoff.Facts["candidates"]
	if !ok {
		t.Fatalf("handoff facts missing candidates: %#v", handoff.Facts)
	}
	if handoff.OfficialRootTask != "" {
		t.Fatalf("solve handoff retained root prompt")
	}
	input := BraidHandoffHelperInput(handoff)
	if input["task_type"] != BraidScaffoldClassCandidateVerify || input["scaffold_id"] != BraidScaffoldIDPropertyCheckV1 {
		t.Fatalf("helper input missing candidate verify scaffold contract: %#v", input)
	}
	if !braidHelperInputLooksLikeCandidateVerify(input) {
		t.Fatalf("helper input did not satisfy candidate verify contract: %#v", input)
	}
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, want := range []string{"Task type: candidate_verify", "Candidate-verify output contract", "predicate"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("helper prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestHelperPreflightCheck(t *testing.T) {
	t.Parallel()

	// Normal input should pass
	input := map[string]any{"program": "let x = 0 in x"}
	handoff := BraidNodeHandoff{ScaffoldClass: BraidScaffoldClassSymbolicTrace}
	if err := helperPreflightCheck(input, handoff); err != nil {
		t.Fatalf("normal input should pass preflight: %v", err)
	}

	// Empty input should pass
	if err := helperPreflightCheck(map[string]any{}, handoff); err != nil {
		t.Fatalf("empty input should pass preflight: %v", err)
	}

	// Oversized input should fail
	bigInput := map[string]any{"data": strings.Repeat("x", 60000)}
	err := helperPreflightCheck(bigInput, handoff)
	if err == nil {
		t.Fatal("oversized input should fail preflight")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected 'too large' error, got: %v", err)
	}
}

func TestCandidateVerifyPresetSource(t *testing.T) {
	t.Parallel()

	src := candidateVerifyPresetSource()
	if !strings.Contains(src, "def Solve(input)") {
		t.Fatal("preset source missing Solve function")
	}
	if !strings.Contains(src, "candidates") {
		t.Fatal("preset source missing candidates")
	}
	if !strings.Contains(src, "predicates") {
		t.Fatal("preset source missing predicates")
	}
}

func TestCandidateVerifyAnswerVerifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		answer   string
		input    map[string]any
		wantOK   bool
		wantPass bool
	}{
		{
			name:   "empty input returns not ok",
			answer: `solution = A`,
			input:  map[string]any{},
			wantOK: false,
		},
		{
			name:     "valid solution line",
			answer:   `solution = B`,
			input:    map[string]any{"candidates": []any{"A", "B", "C"}, "answer": "B"},
			wantOK:   true,
			wantPass: true,
		},
		{
			name:     "wrong answer",
			answer:   `solution = A`,
			input:    map[string]any{"candidates": []any{"A", "B", "C"}, "answer": "B"},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:     "missing library early return",
			answer:   `ok: false\nmissing_library: rdkit`,
			input:    map[string]any{"candidates": []any{"CCO"}},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:     "no ground truth fails closed with solution",
			answer:   `solution = 3`,
			input:    map[string]any{"candidates": []any{"A", "B", "C"}},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:     "placeholder candidate answers rejected",
			answer:   `solution = {"value":"candidate answers"}`,
			input:    map[string]any{"candidates": "candidate answers", "predicates": "verification predicates"},
			wantOK:   true,
			wantPass: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag, ok := candidateVerifyAnswerVerifier(tt.answer, tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok=%v, want %v; diag=%+v", ok, tt.wantOK, diag)
			}
			if ok && tt.wantOK && diag.Pass != tt.wantPass {
				t.Errorf("pass=%v, want %v; first_failure=%q", diag.Pass, tt.wantPass, diag.FirstFailure)
			}
		})
	}
}

func TestHelperSubTimeoutIsEnforced(t *testing.T) {
	t.Parallel()

	// Verify that the budget computation yields a non-zero sub-timeout
	// and that the context.WithTimeout call is reached.
	budget := &braidNodeHelperBudget{
		CumulativeDuration: 2 * time.Minute,
		Attempts:           1,
	}
	remaining := budget.remainingSubTimeout(12 * time.Minute)
	if remaining <= 0 {
		t.Fatalf("expected remaining sub-timeout > 0 with 2min used of 12min budget, got %v", remaining)
	}
	// The remaining should be close to 10 minutes
	if remaining < 9*time.Minute {
		t.Fatalf("expected remaining >= 9min, got %v", remaining)
	}

	// Verify that zero remaining is returned when budget is exhausted
	exhausted := &braidNodeHelperBudget{
		CumulativeDuration: 12 * time.Minute,
		Attempts:           5,
	}
	remainingExhausted := exhausted.remainingSubTimeout(12 * time.Minute)
	if remainingExhausted != 0 {
		t.Fatalf("expected 0 remaining when budget fully consumed, got %v", remainingExhausted)
	}
}

func TestCommitSolverArtifactAfterValidation(t *testing.T) {
	t.Parallel()

	state := generalsolver.NewSolverState()

	// Seed the node so commitSolverArtifact can find it
	_ = generalsolver.AddWorkItem(state, generalsolver.WorkItem{
		ID:        "node1",
		Goal:      "test",
		Archetype: generalsolver.ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    generalsolver.StatusSolving,
		Priority:  1.0,
		Risk:      0.5,
	})

	// A solved summary should produce an artifact
	commitSolverArtifact(state, "node1", "status: solved answer: solution = 42 checks: verified")
	art1, ok1 := state.Artifacts["node1"]
	if !ok1 {
		t.Fatal("expected artifact for node1 after commit")
	}
	if art1.Status != generalsolver.ArtifactStatusSolved {
		t.Fatalf("expected status solved, got %s", art1.Status)
	}

	// A blocked summary should not overwrite a solved artifact
	// (commitSolverArtifact skips if item.Status == Solved)
	commitSolverArtifact(state, "node1", "status: blocked answer: checks: helper failed")
	art2 := state.Artifacts["node1"]
	if art2.Status != generalsolver.ArtifactStatusSolved {
		t.Fatalf("artifact status should remain solved, got %s", art2.Status)
	}
}

func TestDeclaredArchetypeRoutesToCorrectHandoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		archetype  string
		rootPrompt string
		wantClass  string
		wantID     string
	}{
		{
			"symbolic_trace",
			"Puzzle instance:\nprogram: let x = 0 in x\nqueries: []\n",
			BraidScaffoldClassSymbolicTrace,
			BraidScaffoldIDTypeInferenceV1,
		},
		{
			"candidate_verify",
			"Puzzle instance:\ncandidates: [a, b]\npredicates: [{\"name\": \"p1\"}]\n",
			BraidScaffoldClassCandidateVerify,
			BraidScaffoldIDPropertyCheckV1,
		},
		{
			"finite_state_transition",
			"Puzzle instance:\nInitial state: [[1],[2]]\nGoal state: [[2],[1]]\n",
			BraidScaffoldClassFiniteStateTransition,
			BraidScaffoldIDStackRelocationV1,
		},
		{"unknown_archetype", "Puzzle instance:\ndata: test\n", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.archetype, func(t *testing.T) {
			node := BraidNode{
				ID:        "n_solve",
				Kind:      "solve",
				Archetype: tt.archetype,
				Question:  "Solve the problem.",
			}
			handoff := BuildBraidNodeHandoff(node, tt.rootPrompt, nil, "")
			if tt.wantClass == "" {
				if handoff.ScaffoldClass != "" {
					t.Errorf("expected no scaffold for unknown archetype, got %q", handoff.ScaffoldClass)
				}
				return
			}
			if handoff.ScaffoldClass != tt.wantClass {
				t.Errorf("scaffold class=%q, want %q", handoff.ScaffoldClass, tt.wantClass)
			}
			if tt.wantID != "" && handoff.ScaffoldID != tt.wantID {
				t.Errorf("scaffold id=%q, want %q", handoff.ScaffoldID, tt.wantID)
			}
		})
	}
}

func TestNormalizeBraidNodeArchetype(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"symbolic_trace", "symbolic_trace"},
		{"SYMBOLIC_TRACE", "symbolic_trace"},
		{" candidate_verify ", "candidate_verify"},
		{"explicit_dag", "explicit_dag"},
		{"table_recurrence", "table_recurrence"},
		{"constraint_solve", "constraint_solve"},
		{"constraint_solver", "constraint_solve"},
		{"mixed", "mixed"},
		{"", ""},
		{"bogus_archetype", ""},
		{"type_inference", ""},
	}
	for _, tt := range tests {
		got := normalizeBraidNodeArchetype(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBraidNodeArchetype(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestApplyGraphLevelSplits(t *testing.T) {
	t.Parallel()

	// Create a graph with a large-payload node.
	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract data."},
			{ID: "n2", Kind: "solve", Question: "Solve queries.", DependsOn: []string{"n1"}},
			{ID: "n3", Kind: "reduce", Question: "Combine.", DependsOn: []string{"n2"}, ExpectedOutput: "final"},
		},
		FinalNode: "n3",
	}

	state := generalsolver.NewSolverState()
	_ = generalsolver.AddWorkItem(state, generalsolver.WorkItem{
		ID:        "n1",
		Goal:      "Extract data.",
		Archetype: generalsolver.ArchetypeExplicitDAG,
		DependsOn: []string{},
		Status:    generalsolver.StatusReady,
		Priority:  3.0,
	})
	// Manually seed n2 with a large payload.
	queries := make([]map[string]any, 10)
	for i := range queries {
		queries[i] = map[string]any{
			"id":     fmt.Sprintf("q%d", i),
			"prompt": string(make([]byte, 6000)),
		}
	}
	_ = generalsolver.AddWorkItem(state, generalsolver.WorkItem{
		ID:        "n2",
		Goal:      "Solve queries.",
		Archetype: generalsolver.ArchetypeExplicitDAG,
		DependsOn: []string{"n1"},
		Status:    generalsolver.StatusPending,
		Priority:  2.0,
		Payload:   map[string]any{"queries": queries},
	})
	_ = generalsolver.AddWorkItem(state, generalsolver.WorkItem{
		ID:        "n3",
		Goal:      "Combine.",
		Archetype: generalsolver.ArchetypeExplicitDAG,
		DependsOn: []string{"n2"},
		Status:    generalsolver.StatusPending,
		Priority:  1.0,
	})

	applyGraphLevelSplits(state, graph)

	// n2 should be replaced by parse + solve items + merge.
	if _, exists := state.Items["n2"]; exists {
		t.Fatal("n2 should be removed after split")
	}
	parseID := "n2__parse"
	mergeID := "n2__merge"
	if _, exists := state.Items[parseID]; !exists {
		t.Fatal("parse sub-item should exist")
	}
	if _, exists := state.Items[mergeID]; !exists {
		t.Fatal("merge sub-item should exist")
	}

	// n3 should now depend on merge instead of n2.
	n3 := state.Items["n3"]
	found := false
	for _, dep := range n3.DependsOn {
		if dep == mergeID {
			found = true
		}
	}
	if !found {
		t.Fatalf("n3 should depend on %s after rewire, got %v", mergeID, n3.DependsOn)
	}
}

func TestApplyGraphLevelSplitsRecordsSplitFailure(t *testing.T) {
	t.Parallel()

	state := generalsolver.NewSolverState()
	_ = generalsolver.AddWorkItem(state, generalsolver.WorkItem{
		ID:        "n2",
		Goal:      "Solve queries.",
		Archetype: generalsolver.ArchetypeExplicitDAG,
		Status:    generalsolver.StatusReady,
		Payload: map[string]any{
			"queries": []map[string]any{
				{"prompt": "first"},
				{"prompt": "second"},
			},
		},
	})
	_ = generalsolver.AddWorkItem(state, generalsolver.WorkItem{
		ID:        "n2__parse",
		Goal:      "Existing parse item.",
		Archetype: generalsolver.ArchetypeExplicitDAG,
		Status:    generalsolver.StatusPending,
	})

	applyGraphLevelSplits(state, nil)

	if _, exists := state.Items["n2"]; !exists {
		t.Fatal("n2 should remain after split failure")
	}
	if len(state.FailureLog) != 1 {
		t.Fatalf("failure log entries=%d want 1", len(state.FailureLog))
	}
	entry := state.FailureLog[0]
	if entry.WorkItemID != "n2" || !strings.Contains(entry.Reason, "graph_level_split") {
		t.Fatalf("failure entry=%+v, want graph_level_split for n2", entry)
	}
	if stage, _ := entry.Feedback["stage"].(string); stage != "graph_level_split" {
		t.Fatalf("failure feedback stage=%q want graph_level_split", stage)
	}
}

func TestSeedSolverStateFromBraidGraph_WithSmallPayload(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract data."},
			{ID: "n2", Kind: "solve", Question: "Solve.", DependsOn: []string{"n1"}},
		},
		FinalNode: "n2",
	}

	state := seedSolverStateFromBraidGraph(graph)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	// Small payloads don't get split.
	if _, exists := state.Items["n1"]; !exists {
		t.Fatal("n1 should exist")
	}
	if _, exists := state.Items["n2"]; !exists {
		t.Fatal("n2 should exist")
	}
}

func TestAttemptSplitHelperExecution_TooSmallToSplit(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n1", Kind: "solve", Question: "Small task."}
	input := map[string]any{"query": "what is 2+2"}
	handoff := BraidNodeHandoff{Node: node}

	// No toolExec -> returns false.
	result, ok, err := attemptSplitHelperExecution(context.TODO(), "phase", node, input, handoff, "", nil, nil)
	if err != nil {
		t.Fatalf("attemptSplitHelperExecution() error = %v", err)
	}
	if ok {
		t.Fatalf("expected false for nil toolExec, got result=%q", result)
	}
}

func TestAttemptSplitHelperExecutionReturnsErrorForChunkFailures(t *testing.T) {
	t.Parallel()

	helper := &HelperFactoryTools{Config: HelperFactoryConfig{
		Language: HelperLanguageGo,
		Attempts: 1,
		PresetSource: `func Solve(input map[string]any) map[string]any {
	return map[string]any{"ok": false}
}`,
	}}
	toolExec := &replToolExecutor{
		recorder:      NewRecorder(),
		helperFactory: helper,
	}
	node := BraidNode{ID: "n_split", Kind: "solve", Question: "Solve split task."}
	input := map[string]any{
		"queries": []map[string]any{
			{"prompt": "chunk 1"},
			{"prompt": "chunk 2"},
		},
	}
	handoff := BraidNodeHandoff{Node: node}
	var output engine.EngineOutput

	result, ok, err := attemptSplitHelperExecution(context.Background(), "phase", node, input, handoff, "", toolExec, &output)
	if !ok {
		t.Fatal("attemptSplitHelperExecution() did not attempt a query split")
	}
	if err == nil {
		t.Fatalf("attemptSplitHelperExecution() error = nil, result=%q", result)
	}
	if !strings.Contains(err.Error(), "split execution failed for 2/2 chunks") {
		t.Fatalf("error=%v, want split failure detail", err)
	}
	if !strings.Contains(result, "status: blocked") {
		t.Fatalf("result=%q, want blocked summary", result)
	}
	if len(output.ToolCalls) != 2 {
		t.Fatalf("tool calls=%d want 2", len(output.ToolCalls))
	}
	var sawSplitFailed bool
	for _, event := range toolExec.recorder.Events() {
		if event.Braid != nil && event.Braid.Status == "split_failed" {
			sawSplitFailed = true
		}
	}
	if !sawSplitFailed {
		t.Fatal("missing split_failed braid event")
	}
}

func TestHelperPreflightSplitErrorMessage(t *testing.T) {
	t.Parallel()

	// Verify the preflight error message mentions splitting.
	bigInput := map[string]any{
		"data": string(make([]byte, 60000)),
	}
	handoff := BraidNodeHandoff{Node: BraidNode{ID: "n1"}}
	err := helperPreflightCheck(bigInput, handoff)
	if err == nil {
		t.Fatal("expected error for oversized input")
	}
	if !strings.Contains(err.Error(), "split") {
		t.Fatalf("error should mention split: %q", err.Error())
	}
}

func TestApplyBraidGraphSplits(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract data."},
			{
				ID:            "n2",
				Kind:          "solve",
				Question:      "Solve HM type inference.",
				DependsOn:     []string{"n1"},
				HelperPolicy:  "preferred",
				Archetype:     BraidScaffoldClassSymbolicTrace,
				ScaffoldClass: BraidScaffoldClassSymbolicTrace,
				ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
				InputSchema: map[string]any{
					"program":  "let ...",
					"bindings": testBraidSplitBindings(24),
				},
			},
			{ID: "n3", Kind: "reduce", Question: "Combine.", DependsOn: []string{"n2"}},
		},
		FinalNode: "n3",
	}

	applyBraidGraphSplits(graph, nil, "test")

	// n2 should be removed.
	for _, node := range graph.Nodes {
		if node.ID == "n2" {
			t.Fatal("n2 should be removed after split")
		}
	}

	// Sub-nodes should exist.
	parseID := "n2__parse"
	mergeID := "n2__merge"
	foundParse, foundMerge := false, false
	var mergeNode BraidNode
	for _, node := range graph.Nodes {
		if node.ID == parseID {
			foundParse = true
			if node.Kind != "extract" {
				t.Fatalf("parse should be extract kind, got %q", node.Kind)
			}
			if node.HelperPolicy != BraidNodeHelperPolicyNever {
				t.Fatalf("parse should have helper_policy=never, got %q", node.HelperPolicy)
			}
		}
		if node.ID == mergeID {
			foundMerge = true
			mergeNode = node
			if node.Kind != "reduce" {
				t.Fatalf("merge should be reduce kind, got %q", node.Kind)
			}
		}
	}
	if !foundParse {
		t.Fatal("parse sub-node should exist")
	}
	if !foundMerge {
		t.Fatal("merge sub-node should exist")
	}

	// n3 should now depend on merge instead of n2.
	for _, node := range graph.Nodes {
		if node.ID == "n3" {
			found := false
			for _, dep := range node.DependsOn {
				if dep == mergeID {
					found = true
				}
				if dep == "n2" {
					t.Fatal("n3 should not still depend on n2")
				}
			}
			if !found {
				t.Fatalf("n3 should depend on %s, got %v", mergeID, node.DependsOn)
			}
		}
	}

	// Merge should depend on solve nodes.
	if len(mergeNode.DependsOn) == 0 {
		t.Fatal("merge should have solve dependencies")
	}

	// Graph should be valid (no dangling deps).
	if err := ValidateBraidGraph(*graph, 32); err != nil {
		t.Fatalf("graph validation failed after split: %v", err)
	}
}

func testBraidSplitBindings(n int) []any {
	bindings := make([]any, 0, n)
	for i := 0; i < n; i++ {
		bindings = append(bindings, map[string]any{
			"name": fmt.Sprintf("v%d", i),
			"expr": fmt.Sprintf("x%d", i),
		})
	}
	return bindings
}

func TestApplyBraidGraphSplitsUsesChunkSpecificPayloads(t *testing.T) {
	t.Parallel()

	bindings := make([]any, 0, 24)
	for i := 0; i < 24; i++ {
		bindings = append(bindings, map[string]any{"name": fmt.Sprintf("v%d", i), "expr": fmt.Sprintf("x%d", i)})
	}
	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{
				ID:            "hm",
				Kind:          "solve",
				Question:      "Infer types.",
				HelperPolicy:  BraidNodeHelperPolicyPreferred,
				Archetype:     BraidScaffoldClassSymbolicTrace,
				ScaffoldClass: BraidScaffoldClassSymbolicTrace,
				ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
				InputSchema: map[string]any{
					"program":  "let ...",
					"bindings": bindings,
				},
			},
			{ID: "final", Kind: "reduce", DependsOn: []string{"hm"}},
		},
		FinalNode: "final",
	}

	applyBraidGraphSplits(graph, nil, "test")

	solve0, ok := braidGraphNodeByID(*graph, "hm__solve_00")
	if !ok {
		t.Fatal("first split solve node missing")
	}
	solve1, ok := braidGraphNodeByID(*graph, "hm__solve_01")
	if !ok {
		t.Fatal("second split solve node missing")
	}
	if got := solve0.InputSchema["split_mode"]; got != "sequential" {
		t.Fatalf("split_mode=%v want sequential", got)
	}
	if _, exists := solve0.InputSchema["bindings"]; exists {
		t.Fatal("split solve payload should not duplicate full bindings array")
	}
	if _, exists := solve0.InputSchema["chunk"]; !exists {
		t.Fatal("split solve payload should include concrete chunk")
	}
	if !reflect.DeepEqual(solve1.DependsOn, []string{"hm__parse", "hm__solve_00"}) {
		t.Fatalf("second sequential solve deps=%v want parse plus previous solve", solve1.DependsOn)
	}
	merge, ok := braidGraphNodeByID(*graph, "hm__merge")
	if !ok {
		t.Fatal("merge node missing")
	}
	if got := merge.InputSchema["block_on_missing_artifact"]; got != true {
		t.Fatalf("merge block_on_missing_artifact=%v want true", got)
	}
}

func TestApplyBraidGraphSplitsTreatsMergeAsSolveLikeForVerificationPolicy(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract program."},
			{
				ID:            "n_solve_hm",
				Kind:          "solve",
				Question:      "Infer types.",
				DependsOn:     []string{"n_extract"},
				HelperPolicy:  BraidNodeHelperPolicyRequired,
				Archetype:     BraidScaffoldClassSymbolicTrace,
				ScaffoldClass: BraidScaffoldClassSymbolicTrace,
				ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
				InputSchema: map[string]any{
					"program":  "let ...",
					"bindings": testBraidSplitBindings(24),
				},
			},
			{
				ID:             "n_verify",
				Kind:           "verify",
				Question:       "Verify candidate against original constraints.",
				ExpectedOutput: "pass only if original constraints are satisfied",
				DependsOn:      []string{"n_solve_hm"},
			},
			{ID: "n_reduce", Kind: "reduce", Question: "Return final.", DependsOn: []string{"n_solve_hm", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	applyBraidGraphSplits(graph, nil, "test")

	verify, ok := braidGraphNodeByID(*graph, "n_verify")
	if !ok {
		t.Fatal("verify node missing")
	}
	if !reflect.DeepEqual(verify.DependsOn, []string{"n_solve_hm__merge"}) {
		t.Fatalf("verify deps=%v want split merge", verify.DependsOn)
	}
	if err := ValidateBraidGraphPolicy(*graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("split graph should satisfy LongCoT policy: %v", err)
	}
}

func TestBraidRuntimeMergeBlockReasonRequiresSolvedArtifacts(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:        "hm__merge",
		Kind:      "reduce",
		DependsOn: []string{"hm__solve_00", "hm__solve_01"},
		InputSchema: map[string]any{
			"block_on_missing_artifact": true,
			"solve_ids":                 []any{"hm__solve_00", "hm__solve_01"},
			"required_artifact_status":  []any{"solved", "pass"},
		},
	}

	if reason := braidRuntimeMergeBlockReason(node, map[string]string{
		"hm__solve_00": `{"status":"solved","answer":"a"}`,
		"hm__solve_01": `{"status":"partial","answer":"b"}`,
	}); reason == "" {
		t.Fatal("expected partial split artifact to block merge")
	}

	if reason := braidRuntimeMergeBlockReason(node, map[string]string{
		"hm__solve_00": `{"status":"solved","answer":"a"}`,
		"hm__solve_01": `{"status":"pass","answer":"b"}`,
	}); reason != "" {
		t.Fatalf("merge block reason=%q, want none", reason)
	}
}

func TestValidateBraidNodeExecutionSummaryAcceptsRawNodeArtifact(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n1", Kind: "solve", Question: "Solve."}
	summary := `{"status":"solved","answer":"solution = 42","checks":["verified"],"confidence":1}`
	if err := validateBraidNodeExecutionSummaryInGraph("test", node, summary, "n1", nil); err != nil {
		t.Fatalf("validateBraidNodeExecutionSummaryInGraph() error = %v", err)
	}
}

func TestApplyBraidGraphSplitsDoesNotRouterSplitBroadExplicitDAG(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract data."},
			{
				ID:             "n_solve_chain",
				Kind:           "solve",
				Question:       "Solve nodes 0, 1, 2, 3 sequentially.",
				DependsOn:      []string{"n_extract"},
				ExpectedOutput: "answers for nodes 0, 1, 2, and 3",
				HelperPolicy:   BraidNodeHelperPolicyPreferred,
				Archetype:      BraidScaffoldClassExplicitDAG,
				ScaffoldClass:  BraidScaffoldClassExplicitDAG,
				ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"prompt":        "Solve nodes 0, 1, 2, 3.",
					"solve_targets": []any{"node_0", "node_1", "node_2", "node_3"},
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify original constraints.", ExpectedOutput: "checks original constraints pass", DependsOn: []string{"n_solve_chain"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Return final.", DependsOn: []string{"n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	applyBraidGraphSplits(graph, nil, "test")

	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain"); !ok {
		t.Fatal("explicit_dag broad solve should remain atomic without registered split policy")
	}
	mergeID := "n_solve_chain__adaptive_merge"
	if _, ok := braidGraphNodeByID(*graph, mergeID); ok {
		t.Fatalf("explicit_dag broad solve should not create router merge node %q", mergeID)
	}
	verify, ok := braidGraphNodeByID(*graph, "n_verify")
	if !ok {
		t.Fatal("verify node missing")
	}
	if !reflect.DeepEqual(verify.DependsOn, []string{"n_solve_chain"}) {
		t.Fatalf("verify deps=%v want [n_solve_chain]", verify.DependsOn)
	}
	if err := ValidateBraidGraphPolicy(*graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("longcot controller policy should accept verify depending on adaptive merge: %v", err)
	}
}

func TestApplyAdaptiveRouterSummaryDependenciesRewiresTargetDeps(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{
				ID:   "n_solve_chain__adaptive_router",
				Kind: "extract",
			},
			{
				ID:          "n_solve_chain__adaptive_00_node_5",
				Kind:        "solve",
				DependsOn:   []string{"n_solve_chain__adaptive_router"},
				InputSchema: map[string]any{"target_node": "node_5", "target_nodes": []any{"node_5"}},
			},
			{
				ID:          "n_solve_chain__adaptive_01_node_2",
				Kind:        "solve",
				DependsOn:   []string{"n_solve_chain__adaptive_router", "n_solve_chain__adaptive_00_node_5"},
				InputSchema: map[string]any{"target_node": "node_2", "target_nodes": []any{"node_2"}},
			},
			{
				ID:          "n_solve_chain__adaptive_02_node_4",
				Kind:        "solve",
				DependsOn:   []string{"n_solve_chain__adaptive_router", "n_solve_chain__adaptive_01_node_2"},
				InputSchema: map[string]any{"target_node": "node_4", "target_nodes": []any{"node_4"}},
			},
			{
				ID:          "n_solve_chain__adaptive_03_node_3",
				Kind:        "solve",
				DependsOn:   []string{"n_solve_chain__adaptive_router", "n_solve_chain__adaptive_00_node_5"},
				InputSchema: map[string]any{"target_node": "node_3", "target_nodes": []any{"node_3"}},
			},
		},
		FinalNode: "n_solve_chain__adaptive_02_node_4",
	}
	summaries := map[string]string{
		"n_solve_chain__adaptive_router": `routing_packet: {"dependency_edges":[{"from":"node_2","to":"node_5"},{"from":"node_2","to":"node_4"},{"from":"node_2","to":"node_3"},{"from":"node_3","to":"node_4"}]}`,
	}

	rewired := applyAdaptiveRouterSummaryDependencies(graph, summaries, nil)

	if !reflect.DeepEqual(rewired, []string{"n_solve_chain__adaptive_00_node_5", "n_solve_chain__adaptive_01_node_2", "n_solve_chain__adaptive_02_node_4", "n_solve_chain__adaptive_03_node_3"}) {
		t.Fatalf("rewired=%v", rewired)
	}
	node5, _ := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_00_node_5")
	if !reflect.DeepEqual(node5.DependsOn, []string{"n_solve_chain__adaptive_router", "n_solve_chain__adaptive_01_node_2"}) {
		t.Fatalf("node5 deps=%v", node5.DependsOn)
	}
	node2, _ := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_01_node_2")
	if !reflect.DeepEqual(node2.DependsOn, []string{"n_solve_chain__adaptive_router"}) {
		t.Fatalf("node2 deps=%v", node2.DependsOn)
	}
	node4, _ := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_02_node_4")
	if !reflect.DeepEqual(node4.DependsOn, []string{"n_solve_chain__adaptive_router", "n_solve_chain__adaptive_01_node_2", "n_solve_chain__adaptive_03_node_3"}) {
		t.Fatalf("node4 deps=%v", node4.DependsOn)
	}
	node3, _ := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_03_node_3")
	if !reflect.DeepEqual(node3.DependsOn, []string{"n_solve_chain__adaptive_router", "n_solve_chain__adaptive_01_node_2"}) {
		t.Fatalf("node3 deps=%v", node3.DependsOn)
	}
}

func TestExtractBraidRouterPacketFromStructuredAdjacency(t *testing.T) {
	t.Parallel()

	packet, ok := extractBraidRouterPacketFromSummary(`routing_packet: {"dependency_edges":{"node_0":[],"node_1":["node_0"],"node_4":["node_2","node_3"]}}`)
	if !ok {
		t.Fatal("expected router packet")
	}
	got := make([]string, 0, len(packet.DependencyEdges))
	for _, edge := range packet.DependencyEdges {
		got = append(got, edge.From+"->"+edge.To)
	}
	sort.Strings(got)
	want := []string{"node_0->node_1", "node_2->node_4", "node_3->node_4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edges=%v want %v", got, want)
	}
}

func TestExtractBraidRouterPacketNormalizesNAliases(t *testing.T) {
	t.Parallel()

	packet, ok := extractBraidRouterPacketFromSummary(`routing_packet: {"dependencies":{"n0":[],"n1":["node_0"],"n2":["node_1","n5","n6","node_7"]}}`)
	if !ok {
		t.Fatal("expected router packet")
	}
	got := make([]string, 0, len(packet.DependencyEdges))
	for _, edge := range packet.DependencyEdges {
		got = append(got, edge.From+"->"+edge.To)
	}
	sort.Strings(got)
	want := []string{
		"node_0->node_1",
		"node_1->node_2",
		"node_5->node_2",
		"node_6->node_2",
		"node_7->node_2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edges=%v want %v", got, want)
	}
}

func TestExtractBraidRouterPacketFromPairList(t *testing.T) {
	t.Parallel()

	packet, ok := extractBraidRouterPacketFromSummary(`routing_packet: {"dependency_edges":[["node_2","node_0"],["node_2","node_1"],["node_4","node_3"],["node_4","node_2"]]}`)
	if !ok {
		t.Fatal("expected router packet")
	}
	got := make([]string, 0, len(packet.DependencyEdges))
	for _, edge := range packet.DependencyEdges {
		got = append(got, edge.From+"->"+edge.To)
	}
	want := []string{"node_0->node_2", "node_1->node_2", "node_3->node_4", "node_2->node_4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edges=%v want %v", got, want)
	}
}

func TestApplyAdaptiveRouterSummaryDependenciesAddsMissingTargets(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "solve_wave__adaptive_router", Kind: "extract"},
			{
				ID:              "solve_wave__adaptive_00_node_4",
				Kind:            "solve",
				DependsOn:       []string{"solve_wave__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_4", "target_nodes": []any{"node_4"}},
			},
			{
				ID:              "solve_wave__adaptive_merge",
				Kind:            "reduce",
				DependsOn:       []string{"solve_wave__adaptive_00_node_4"},
				ExpectedOutput:  "solution = [node_4, node_2]",
				MaxSummaryChars: 800,
			},
		},
		FinalNode: "solve_wave__adaptive_merge",
	}
	summaries := map[string]string{
		"solve_wave__adaptive_router": `routing_packet: {"target_ids":["node_0","node_1","node_2","node_4"],"dependency_edges":[{"from":"node_0","to":"node_1"},{"from":"node_1","to":"node_2"},{"from":"node_2","to":"node_4"}]}`,
	}

	rewired := applyAdaptiveRouterSummaryDependencies(graph, summaries, nil)

	for _, id := range []string{"node_0", "node_1", "node_2", "node_4"} {
		if _, ok := braidGraphNodeByID(*graph, "solve_wave__adaptive_extra_"+id); ok {
			t.Fatalf("unexpected literal id naming for %s", id)
		}
	}
	node4, _ := braidGraphNodeByID(*graph, "solve_wave__adaptive_00_node_4")
	var node2 BraidNode
	for _, node := range graph.Nodes {
		if braidAdaptiveNodeSingleTargetID(node) == "node_2" {
			node2 = node
		}
	}
	if node2.ID == "" {
		t.Fatalf("missing node_2 target; rewired=%v nodes=%#v", rewired, graph.Nodes)
	}
	if !reflect.DeepEqual(node4.DependsOn, []string{"solve_wave__adaptive_router", node2.ID}) {
		t.Fatalf("node4 deps=%v want router + %s", node4.DependsOn, node2.ID)
	}
	merge, _ := braidGraphNodeByID(*graph, "solve_wave__adaptive_merge")
	foundDep := false
	for _, depID := range merge.DependsOn {
		if depID == node2.ID {
			foundDep = true
		}
	}
	if !foundDep {
		t.Fatalf("merge deps=%v missing added node2 id %s", merge.DependsOn, node2.ID)
	}
}

func TestApplyAdaptiveRouterSummaryDependenciesCollapsesCycles(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "solve_core__adaptive_router", Kind: "extract"},
			{
				ID:              "solve_core__adaptive_00_node_0",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_0", "target_nodes": []any{"node_0"}},
			},
			{
				ID:              "solve_core__adaptive_01_node_1",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router", "solve_core__adaptive_00_node_0"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_1", "target_nodes": []any{"node_1"}},
			},
			{
				ID:              "solve_core__adaptive_02_node_5",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_5", "target_nodes": []any{"node_5"}},
			},
			{
				ID:              "solve_core__adaptive_03_node_6",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_6", "target_nodes": []any{"node_6"}},
			},
			{
				ID:              "solve_core__adaptive_04_node_2",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_2", "target_nodes": []any{"node_2"}},
			},
			{
				ID:              "solve_core__adaptive_05_node_3",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_3", "target_nodes": []any{"node_3"}},
			},
			{
				ID:              "solve_core__adaptive_06_node_4",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_4", "target_nodes": []any{"node_4"}},
			},
			{
				ID:              "solve_core__adaptive_07_node_7",
				Kind:            "solve",
				DependsOn:       []string{"solve_core__adaptive_router"},
				HelperPolicy:    BraidNodeHelperPolicyPreferred,
				ScaffoldClass:   BraidScaffoldClassExplicitDAG,
				ScaffoldID:      BraidScaffoldIDSearchBacktrackV1,
				MaxSummaryChars: 800,
				InputSchema:     map[string]any{"target_node": "node_7", "target_nodes": []any{"node_7"}},
			},
			{
				ID:        "solve_core__adaptive_merge",
				Kind:      "reduce",
				DependsOn: []string{"solve_core__adaptive_00_node_0", "solve_core__adaptive_01_node_1", "solve_core__adaptive_02_node_5", "solve_core__adaptive_03_node_6", "solve_core__adaptive_04_node_2", "solve_core__adaptive_05_node_3", "solve_core__adaptive_06_node_4", "solve_core__adaptive_07_node_7"},
			},
		},
		FinalNode: "solve_core__adaptive_merge",
	}
	summaries := map[string]string{
		"solve_core__adaptive_router": `routing_packet: {"dependency_edges":{"node_0":[],"node_1":["node_0"],"node_5":["node_2"],"node_6":["node_5"],"node_7":["node_6"],"node_2":["node_1","node_5","node_6","node_7"],"node_3":["node_2"],"node_4":["node_2","node_3"]}}`,
	}

	rewired := applyAdaptiveRouterSummaryDependencies(graph, summaries, nil)

	cycleID := "solve_core__adaptive_cycle_node_2_node_5_node_6_node_7"
	cycle, ok := braidGraphNodeByID(*graph, cycleID)
	if !ok {
		t.Fatalf("cycle node missing; rewired=%v nodes=%#v", rewired, graph.Nodes)
	}
	if cycle.Kind != "cycle_solve" {
		t.Fatalf("cycle kind=%q want cycle_solve", cycle.Kind)
	}
	if got := extractBraidCycleClustersFromAny(cycle.InputSchema["cycle_clusters"]); !reflect.DeepEqual(got, [][]string{{"node_2", "node_5", "node_6", "node_7"}}) {
		t.Fatalf("cycle_clusters=%v", got)
	}
	node3, _ := braidGraphNodeByID(*graph, "solve_core__adaptive_05_node_3")
	if !reflect.DeepEqual(node3.DependsOn, []string{"solve_core__adaptive_router", cycleID}) {
		t.Fatalf("node3 deps=%v want router + cycle", node3.DependsOn)
	}
	node4, _ := braidGraphNodeByID(*graph, "solve_core__adaptive_06_node_4")
	if !reflect.DeepEqual(node4.DependsOn, []string{"solve_core__adaptive_router", cycleID, "solve_core__adaptive_05_node_3"}) {
		t.Fatalf("node4 deps=%v want router + cycle + node3", node4.DependsOn)
	}
	for _, removed := range []string{"solve_core__adaptive_02_node_5", "solve_core__adaptive_03_node_6", "solve_core__adaptive_04_node_2", "solve_core__adaptive_07_node_7"} {
		if _, ok := braidGraphNodeByID(*graph, removed); ok {
			t.Fatalf("cycle member node %q should be collapsed", removed)
		}
	}
}

func TestApplyBraidGraphSplitsDoesNotRouterSplitPromptOnlyTargets(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract data."},
			{
				ID:             "n_solve_node2",
				Kind:           "solve",
				Question:       "Solve node_2 using node_5, node_6, and node_7 as dependency constraints.",
				DependsOn:      []string{"n_extract"},
				ExpectedOutput: "value of node_2",
				HelperPolicy:   BraidNodeHelperPolicyPreferred,
				Archetype:      BraidScaffoldClassExplicitDAG,
				ScaffoldClass:  BraidScaffoldClassExplicitDAG,
				ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"prompt": "node_2 depends on node_5, node_6, and node_7.",
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify.", DependsOn: []string{"n_solve_node2"}},
		},
		FinalNode: "n_verify",
	}

	applyBraidGraphSplits(graph, nil, "test")

	if _, ok := braidGraphNodeByID(*graph, "n_solve_node2"); !ok {
		t.Fatal("prompt-only node references should not trigger router split")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve_node2__adaptive_merge"); ok {
		t.Fatal("router split should require explicit typed split targets")
	}
}

func TestApplyBraidGraphSplitsDoesNotPreSplitFinalTargetNodes(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract data."},
			{
				ID:             "n_solve_chain",
				Kind:           "solve",
				Question:       "Solve the dependency chain.",
				DependsOn:      []string{"n_extract"},
				ExpectedOutput: "complete answers for node_4, node_2, and node_7",
				HelperPolicy:   BraidNodeHelperPolicyPreferred,
				Archetype:      BraidScaffoldClassExplicitDAG,
				ScaffoldClass:  BraidScaffoldClassExplicitDAG,
				ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"prompt": "problem chain with circular dependencies",
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify.", DependsOn: []string{"n_solve_chain"}},
		},
		FinalNode: "n_verify",
	}

	applyBraidGraphSplits(graph, nil, "test")

	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain"); !ok {
		t.Fatal("final target mentions should not be treated as independent split targets")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_merge"); ok {
		t.Fatal("router split should require solve_targets or cycle_clusters")
	}
}

func TestApplyBraidGraphSplitsDoesNotUseQuestionFallbackWhenInputSchemaExists(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract data."},
			{
				ID:             "n_solve_chain",
				Kind:           "solve",
				Question:       "Solve nodes node_0 through node_7.",
				DependsOn:      []string{"n_extract"},
				ExpectedOutput: "requested outputs",
				HelperPolicy:   BraidNodeHelperPolicyPreferred,
				Archetype:      BraidScaffoldClassExplicitDAG,
				ScaffoldClass:  BraidScaffoldClassExplicitDAG,
				ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"target_nodes": []any{"node_4", "node_2", "node_7"},
					"prompt":       "solve requested outputs",
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify.", DependsOn: []string{"n_solve_chain"}},
		},
		FinalNode: "n_verify",
	}

	applyBraidGraphSplits(graph, nil, "test")

	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain"); !ok {
		t.Fatal("structured node without solve_targets/cycle_clusters should remain atomic")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain__adaptive_merge"); ok {
		t.Fatal("question text should not trigger split when input_schema is present")
	}
}

func TestApplyBraidGraphSplitsDoesNotRouterSplitDeclaredCycleClusters(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract data."},
			{
				ID:             "n_solve_chain",
				Kind:           "solve",
				Question:       "Solve the dependency chain with one coupled cluster.",
				DependsOn:      []string{"n_extract"},
				ExpectedOutput: "complete answers for node_4, node_2, and node_7",
				HelperPolicy:   BraidNodeHelperPolicyPreferred,
				Archetype:      BraidScaffoldClassExplicitDAG,
				ScaffoldClass:  BraidScaffoldClassExplicitDAG,
				ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"target_nodes": []any{"node_4", "node_2", "node_7"},
					"cycle_clusters": []any{
						[]any{"node_2", "node_5", "node_6", "node_7"},
					},
					"prompt": "problem chain with one fixed-point cluster",
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify.", DependsOn: []string{"n_solve_chain"}},
		},
		FinalNode: "n_verify",
	}

	applyBraidGraphSplits(graph, nil, "test")

	if _, ok := braidGraphNodeByID(*graph, "n_solve_chain"); !ok {
		t.Fatalf("explicit_dag cycle cluster should remain atomic without registered split policy: %#v", graph.Nodes)
	}
	for _, id := range []string{
		"n_solve_chain__adaptive_00_cycle_node_2_node_5_node_6_node_7",
		"n_solve_chain__adaptive_01_requested_outputs",
	} {
		if _, ok := braidGraphNodeByID(*graph, id); ok {
			t.Fatalf("unexpected router split node %q", id)
		}
	}
}

func TestApplyBraidGraphSplitsDoesNotRouterSplitCycleSolve(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract data."},
			{
				ID:             "n_solve_cycle",
				Kind:           "cycle_solve",
				Question:       "Solve coupled nodes 2, 5, 6, and 7 as one fixed-point cluster.",
				DependsOn:      []string{"n_extract"},
				ExpectedOutput: "answers for nodes 2, 5, 6, and 7",
				HelperPolicy:   BraidNodeHelperPolicyPreferred,
				Archetype:      BraidScaffoldClassExplicitDAG,
				ScaffoldClass:  BraidScaffoldClassExplicitDAG,
				ScaffoldID:     BraidScaffoldIDSearchBacktrackV1,
				InputSchema: map[string]any{
					"prompt": "Solve nodes 2, 5, 6, and 7 as a coupled fixed-point cluster.",
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Verify.", DependsOn: []string{"n_solve_cycle"}},
		},
		FinalNode: "n_verify",
	}

	applyBraidGraphSplits(graph, nil, "test")

	if _, ok := braidGraphNodeByID(*graph, "n_solve_cycle"); !ok {
		t.Fatal("cycle_solve node should remain atomic")
	}
	if _, ok := braidGraphNodeByID(*graph, "n_solve_cycle__adaptive_merge"); ok {
		t.Fatal("cycle_solve node should not be router split")
	}
	verify, ok := braidGraphNodeByID(*graph, "n_verify")
	if !ok {
		t.Fatal("verify node missing")
	}
	if !reflect.DeepEqual(verify.DependsOn, []string{"n_solve_cycle"}) {
		t.Fatalf("verify deps=%v want [n_solve_cycle]", verify.DependsOn)
	}
}

func TestApplyBraidGraphSplits_FinalNodeRewire(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract data."},
			{
				ID:            "n2",
				Kind:          "solve",
				Question:      "Solve HM type inference.",
				DependsOn:     []string{"n1"},
				HelperPolicy:  "preferred",
				Archetype:     BraidScaffoldClassSymbolicTrace,
				ScaffoldClass: BraidScaffoldClassSymbolicTrace,
				ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
				InputSchema: map[string]any{
					"program":  "let ...",
					"bindings": testBraidSplitBindings(24),
				},
			},
		},
		FinalNode: "n2",
	}

	applyBraidGraphSplits(graph, nil, "test")

	// FinalNode should be rewired to merge.
	if graph.FinalNode != "n2__merge" {
		t.Fatalf("FinalNode should be n2__merge, got %q", graph.FinalNode)
	}
}

func TestApplyBraidGraphSplits_SmallGraph(t *testing.T) {
	t.Parallel()

	graph := &BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract data."},
			{ID: "n2", Kind: "solve", Question: "Solve simple.", DependsOn: []string{"n1"}},
		},
		FinalNode: "n2",
	}

	applyBraidGraphSplits(graph, nil, "test")

	// No split — no archetype, no helper policy.
	if len(graph.Nodes) != 2 {
		t.Fatalf("expected 2 nodes (no split), got %d", len(graph.Nodes))
	}
}

// --- Domain-agnostic tests proving that archetype routing uses typed packets,
// not domain names or keyword heuristics. ---

func TestStateTransitionHandoffIsDomainAgnostic(t *testing.T) {
	t.Parallel()

	// A state_transition handoff that uses generic field names
	// (move_sequence, initial_state, actions) but NO chess/UCI/FEN keywords.
	// The Puzzle instance section must contain parseable fields for the
	// instance extractor.
	rootPrompt := strings.Join([]string{
		"Puzzle instance:",
		"move_sequence: a2b1 b1c2",
		"actions: [a2b1, b1c2]",
	}, "\n")
	node := BraidNode{
		ID:        "n_solve",
		Kind:      "solve",
		Archetype: "state_transition",
		Question:  "Apply moves and return the final state.",
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, nil, "")

	// The handoff should route to state_transition/state_replay_v1.
	if handoff.ScaffoldClass != BraidScaffoldClassStateTransition {
		t.Fatalf("expected scaffold class %q, got %q", BraidScaffoldClassStateTransition, handoff.ScaffoldClass)
	}
	if handoff.ScaffoldID != BraidScaffoldIDStateReplayV1 {
		t.Fatalf("expected scaffold id %q, got %q", BraidScaffoldIDStateReplayV1, handoff.ScaffoldID)
	}

	// The prompt should contain no chess/UCI/FEN domain keywords in the Go routing layer.
	prompt := RenderBraidHelperHandoffPrompt(handoff)
	for _, forbidden := range []string{"chess", "Chess", "UCI", "uci_to_fen"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("helper prompt contains forbidden domain keyword %q", forbidden)
		}
	}
}

func TestExplicitDAGHandoffIsDomainAgnostic(t *testing.T) {
	t.Parallel()

	// An explicit_dag handoff with generic node/dependency fields
	// but NO math/Hindley-Milner/backtracking domain keywords.
	rootPrompt := strings.Join([]string{
		"Puzzle instance:",
		"nodes: [node_0, node_1]",
		"dependencies: [node_1 depends on node_0]",
		"problems: [node_0: compute x, node_1: compute y given x]",
	}, "\n")
	node := BraidNode{
		ID:        "n_solve",
		Kind:      "solve",
		Archetype: "explicit_dag",
		Question:  "Solve the dependency chain.",
	}
	handoff := BuildBraidNodeHandoff(node, rootPrompt, nil, "")

	if handoff.ScaffoldClass != BraidScaffoldClassExplicitDAG {
		t.Fatalf("expected scaffold class %q, got %q", BraidScaffoldClassExplicitDAG, handoff.ScaffoldClass)
	}
	if handoff.ScaffoldID != BraidScaffoldIDSearchBacktrackV1 {
		t.Fatalf("expected scaffold id %q, got %q", BraidScaffoldIDSearchBacktrackV1, handoff.ScaffoldID)
	}
}

func TestScaffoldResolutionUsesTypedFieldsNotDomainNames(t *testing.T) {
	t.Parallel()

	// Prove that resolveBraidRuntimeScaffold accepts any input with the
	// correct typed fields, even when no domain name appears anywhere.
	tests := []struct {
		name    string
		node    BraidNode
		handoff BraidNodeHandoff
		input   map[string]any
		wantOK  bool
		wantID  string
	}{
		{
			name: "state_replay with generic action sequence",
			node: BraidNode{ID: "n1", Kind: "solve"},
			handoff: BraidNodeHandoff{
				ScaffoldClass: BraidScaffoldClassStateTransition,
				ScaffoldID:    BraidScaffoldIDStateReplayV1,
			},
			input: map[string]any{
				"move_sequence": "a1b2 b2c3",
			},
			wantOK: true,
			wantID: BraidScaffoldIDStateReplayV1,
		},
		{
			name: "search_backtrack with generic dependency chain",
			node: BraidNode{ID: "n1", Kind: "solve"},
			handoff: BraidNodeHandoff{
				ScaffoldClass: BraidScaffoldClassExplicitDAG,
				ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
			},
			input: map[string]any{
				"nodes": []any{"node_0", "node_1"},
			},
			wantOK: true,
			wantID: BraidScaffoldIDSearchBacktrackV1,
		},
		{
			name: "symbolic_trace with generic program text",
			node: BraidNode{ID: "n1", Kind: "solve"},
			handoff: BraidNodeHandoff{
				ScaffoldClass: BraidScaffoldClassSymbolicTrace,
				ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
			},
			input: map[string]any{
				"program": "let x = 0 in x",
				"queries": []any{map[string]any{"kind": "type", "target": "x"}},
			},
			wantOK: true,
			wantID: BraidScaffoldIDTypeInferenceV1,
		},
		{
			name: "candidate_verify with generic items",
			node: BraidNode{ID: "n1", Kind: "solve"},
			handoff: BraidNodeHandoff{
				ScaffoldClass: BraidScaffoldClassCandidateVerify,
				ScaffoldID:    BraidScaffoldIDPropertyCheckV1,
			},
			input: map[string]any{
				"candidates": []any{"alpha", "beta", "gamma"},
				"predicates": []any{map[string]any{"name": "p1", "check_type": "equals", "expected": "beta"}},
			},
			wantOK: true,
			wantID: BraidScaffoldIDPropertyCheckV1,
		},
		{
			name: "unknown scaffold id rejected",
			node: BraidNode{ID: "n1", Kind: "solve"},
			handoff: BraidNodeHandoff{
				ScaffoldClass: BraidScaffoldClassStateTransition,
				ScaffoldID:    "unknown_v1",
			},
			input:  map[string]any{"data": "test"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scaffold, ok := resolveBraidRuntimeScaffold(tt.node, tt.handoff, tt.input)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tt.wantOK)
			}
			if ok && tt.wantID != "" && scaffold.ID != tt.wantID {
				t.Errorf("scaffold.ID=%q, want %q", scaffold.ID, tt.wantID)
			}
		})
	}
}

func TestStateReplayVerifierFailsClosedWithoutExpectedFinalState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		answer   string
		input    map[string]any
		wantPass bool
	}{
		{
			name:     "starting FEN after non-empty move list is not self-verifying",
			answer:   "solution = rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
			input:    map[string]any{"actions": []any{"e2e4"}},
			wantPass: false,
		},
		{
			name:     "expected final state exact match passes",
			answer:   "solution = rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
			input:    map[string]any{"answer": "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1", "actions": []any{"e2e4"}},
			wantPass: true,
		},
		{
			name:     "expected final state mismatch fails",
			answer:   "solution = rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1",
			input:    map[string]any{"answer": "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1", "actions": []any{"e2e4"}},
			wantPass: false,
		},
		{
			name:     "empty answer fails",
			answer:   "",
			input:    map[string]any{},
			wantPass: false,
		},
		{
			name:     "shape-only state without expected final state fails closed",
			answer:   "rnbqkbnr/pppppppp/8/8/4P3/8/PPPP1PPP/RNBQKBNR b KQkq e3 0 1",
			input:    map[string]any{},
			wantPass: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag, ok := stateReplayAnswerVerifier(tt.answer, tt.input)
			if !ok {
				t.Fatalf("state replay verifier should be applicable and fail closed")
			}
			if diag.Pass != tt.wantPass {
				t.Errorf("pass=%v, want %v; first_failure=%q", diag.Pass, tt.wantPass, diag.FirstFailure)
			}
		})
	}
}

func TestSearchBacktrackVerifierIsDomainAgnostic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		answer   string
		input    map[string]any
		wantOK   bool
		wantPass bool
	}{
		{
			name:     "structured node solution without expected value is not runtime verifiable",
			answer:   `solution = {"node_0": 42, "node_1": 13}`,
			input:    map[string]any{},
			wantOK:   false,
			wantPass: false,
		},
		{
			name:     "structured node solution with expected value passes",
			answer:   `solution = {"node_0": 42, "node_1": 13}`,
			input:    map[string]any{"answer": "42"},
			wantOK:   true,
			wantPass: true,
		},
		{
			name:     "empty answer fails",
			answer:   "",
			input:    map[string]any{},
			wantOK:   true,
			wantPass: false,
		},
		{
			name:     "structured answer without expected value is not runtime verifiable",
			answer:   "node_0 = 42\nnode_1 = 13",
			input:    map[string]any{},
			wantOK:   false,
			wantPass: false,
		},
		{
			name:     "unresolved answer fails",
			answer:   `solution = {"node_0":"UNSOLVED"}`,
			input:    map[string]any{},
			wantOK:   true,
			wantPass: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag, ok := searchBacktrackAnswerVerifier(tt.answer, tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok=%v, want %v", ok, tt.wantOK)
			}
			if ok && diag.Pass != tt.wantPass {
				t.Errorf("pass=%v, want %v; first_failure=%q", diag.Pass, tt.wantPass, diag.FirstFailure)
			}
		})
	}
}

func TestNoKeywordRoutingInDeclaredArchetypePath(t *testing.T) {
	t.Parallel()

	// Prove that the declared archetype path routes correctly based on
	// the archetype string alone, using typed instance fields.
	// The prompts include generic instance data — no domain-specific keywords.
	tests := []struct {
		arch      string
		prompt    string
		wantClass string
	}{
		{
			arch:      "symbolic_trace",
			prompt:    "Puzzle instance:\nprogram: let x = 0 in x\nqueries: []\n",
			wantClass: BraidScaffoldClassSymbolicTrace,
		},
		{
			arch:      "candidate_verify",
			prompt:    "Puzzle instance:\ncandidates: [a, b, c]\npredicates: []\n",
			wantClass: BraidScaffoldClassCandidateVerify,
		},
		{
			arch:      "explicit_dag",
			prompt:    "Puzzle instance:\nnodes: [n0, n1]\n",
			wantClass: BraidScaffoldClassExplicitDAG,
		},
		{
			arch:      "state_transition",
			prompt:    "Puzzle instance:\nmove_sequence: a1b2\nactions: [a1b2]\n",
			wantClass: BraidScaffoldClassStateTransition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			node := BraidNode{
				ID:        "n_solve",
				Kind:      "solve",
				Archetype: tt.arch,
				Question:  "Solve the problem.",
			}
			handoff := BuildBraidNodeHandoff(node, tt.prompt, nil, "")
			if handoff.ScaffoldClass != tt.wantClass {
				t.Errorf("archetype=%q: expected class %q, got %q", tt.arch, tt.wantClass, handoff.ScaffoldClass)
			}
		})
	}
}

func TestValidateBraidGraphScaffoldContractAcceptsCompleteNodes(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n1", Kind: "extract", Question: "Extract facts."},
			{
				ID: "n2", Kind: "solve", Question: "Solve.", DependsOn: []string{"n1"},
				Archetype: "state_transition", ScaffoldClass: "state_transition",
				ScaffoldID: "state_replay_v1", InputSchema: map[string]any{"actions": "e2e4 e7e5"},
			},
			{
				ID: "n_cycle", Kind: "cycle_solve", Question: "Solve fixed point.", DependsOn: []string{"n2"},
				Archetype: "explicit_dag", ScaffoldClass: "explicit_dag",
				ScaffoldID: "search_backtrack_v1", InputSchema: map[string]any{"prompt": "solve mutual constraints", "cycle_clusters": []any{[]any{"node_2", "node_5"}}},
			},
			{
				ID: "n3", Kind: "verify", Question: "Verify.", DependsOn: []string{"n_cycle"},
				Archetype: "candidate_verify", ScaffoldClass: "candidate_verify",
				ScaffoldID: "property_check_v1", InputSchema: map[string]any{"candidates": "ans"},
			},
			{ID: "n4", Kind: "reduce", Question: "Reduce.", DependsOn: []string{"n2", "n3"}},
		},
		FinalNode: "n4",
	}
	if err := ValidateBraidGraphScaffoldContract(graph); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateBraidGraphScaffoldContractRejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		node    BraidNode
		wantErr string
	}{
		{
			name:    "solve missing all scaffold fields",
			node:    BraidNode{ID: "n_solve", Kind: "solve", Question: "Solve."},
			wantErr: "missing_scaffold_contract",
		},
		{
			name:    "cycle_solve missing all scaffold fields",
			node:    BraidNode{ID: "n_cycle", Kind: "cycle_solve", Question: "Solve fixed point."},
			wantErr: "missing_scaffold_contract",
		},
		{
			name: "verify missing archetype",
			node: BraidNode{
				ID: "n_verify", Kind: "verify", Question: "Verify.",
				ScaffoldClass: "candidate_verify", ScaffoldID: "property_check_v1",
				InputSchema: map[string]any{"x": 1},
			},
			wantErr: "missing_scaffold_contract",
		},
		{
			name: "solve missing input_schema",
			node: BraidNode{
				ID: "n_solve", Kind: "solve", Question: "Solve.",
				Archetype: "state_transition", ScaffoldClass: "state_transition", ScaffoldID: "state_replay_v1",
			},
			wantErr: "missing_scaffold_contract",
		},
		{
			name:    "extract node does not require scaffold",
			node:    BraidNode{ID: "n_extract", Kind: "extract", Question: "Extract."},
			wantErr: "",
		},
		{
			name:    "reduce node does not require scaffold",
			node:    BraidNode{ID: "n_reduce", Kind: "reduce", Question: "Reduce."},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := BraidGraph{
				Version:   1,
				Nodes:     []BraidNode{tt.node},
				FinalNode: tt.node.ID,
			}
			err := ValidateBraidGraphScaffoldContract(graph)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestValidateBraidGraphScaffoldContractRejectsUnsupportedPair(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{{
			ID:            "n_solve",
			Kind:          "solve",
			Question:      "Solve.",
			Archetype:     "numeric_dp",
			ScaffoldClass: BraidScaffoldClassNumericDP,
			ScaffoldID:    BraidScaffoldIDGenericV1,
			InputSchema:   map[string]any{"nodes": "problem nodes"},
		}},
		FinalNode: "n_solve",
	}
	err := ValidateBraidGraphScaffoldContract(graph)
	if err == nil || !strings.Contains(err.Error(), "unsupported scaffold pair") {
		t.Fatalf("error=%v, want unsupported scaffold pair", err)
	}
}

func TestValidateBraidGraphScaffoldContractRejectsInvalidTypedInput(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{{
			ID:            "n_solve",
			Kind:          "solve",
			Question:      "Solve geometry.",
			Archetype:     "state_transition",
			ScaffoldClass: BraidScaffoldClassStateTransition,
			ScaffoldID:    BraidScaffoldIDStateReplayV1,
			InputSchema:   map[string]any{"prompt": "plain prose is not replayable state"},
		}},
		FinalNode: "n_solve",
	}
	err := ValidateBraidGraphScaffoldContract(graph)
	if err == nil {
		t.Fatal("ValidateBraidGraphScaffoldContract() succeeded for invalid typed scaffold input")
	}
	if _, ok := IsInvalidScaffoldInput(err); !ok {
		t.Fatalf("error=%T %v, want InvalidScaffoldInputError", err, err)
	}
	if !strings.Contains(err.Error(), "invalid_scaffold_input") {
		t.Fatalf("error=%v, want invalid_scaffold_input", err)
	}
}

func TestNormalizeBraidGraphPreservesInvalidTypedScaffoldForValidation(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{{
			ID:            "n_solve",
			Kind:          "solve",
			Question:      "Solve prose dependency chain.",
			Archetype:     "numeric_dp",
			ScaffoldClass: BraidScaffoldClassNumericDP,
			ScaffoldID:    BraidScaffoldIDRecurrenceTableV1,
			InputSchema: map[string]any{
				"tasks":      []any{"node_1", "node_2"},
				"dependency": "node_2 depends on node_1",
			},
		}},
		FinalNode: "n_solve",
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	node := normalized.Nodes[0]
	if node.ScaffoldClass != BraidScaffoldClassNumericDP || node.ScaffoldID != BraidScaffoldIDRecurrenceTableV1 {
		t.Fatalf("scaffold=%s/%s, want numeric_dp/recurrence_table_v1", node.ScaffoldClass, node.ScaffoldID)
	}
	if node.Archetype != BraidScaffoldClassNumericDP {
		t.Fatalf("archetype=%q, want numeric_dp", node.Archetype)
	}
	err := ValidateBraidGraphScaffoldContract(normalized)
	if err == nil {
		t.Fatal("ValidateBraidGraphScaffoldContract() succeeded after invalid scaffold normalization")
	}
	if _, ok := IsInvalidScaffoldInput(err); !ok {
		t.Fatalf("error=%T %v, want InvalidScaffoldInputError", err, err)
	}
}

func TestExplicitDAGUsesLLMAuthoredHelperWithVerifier(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{
		ScaffoldClass: BraidScaffoldClassExplicitDAG,
		ScaffoldID:    BraidScaffoldIDSearchBacktrackV1,
	}
	scaffold, ok := resolveBraidRuntimeScaffold(node, handoff, map[string]any{
		"prompt":             "Solve this one local work item.",
		"work_item_question": "Find x.",
		"expected_output":    "solution = <integer>",
	})
	if !ok {
		t.Fatal("explicit_dag should install answer verifier even for prompt-only work")
	}
	if scaffold.PresetSource != "" {
		t.Fatal("explicit_dag should not install a deterministic placeholder preset")
	}
	if scaffold.Verifier == nil {
		t.Fatal("explicit_dag should keep the search-backtrack verifier")
	}
	scaffold, ok = resolveBraidRuntimeScaffold(node, handoff, map[string]any{
		"nodes":        []any{"node_0"},
		"dependencies": []any{},
		"problems":     []any{map[string]any{"id": "node_0", "question": "Find x."}},
	})
	if !ok {
		t.Fatal("structured explicit_dag input should install answer verifier")
	}
	if scaffold.PresetSource != "" {
		t.Fatal("structured explicit_dag should also use an LLM-authored helper, not a placeholder preset")
	}
}

func TestNormalizeBraidGraphCanonicalizesGenericScaffoldID(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract."},
			{
				ID: "n_solve", Kind: "solve", Question: "Solve.", DependsOn: []string{"n_extract"},
				Archetype: "numeric_dp", ScaffoldClass: BraidScaffoldClassNumericDP,
				ScaffoldID: BraidScaffoldIDGenericV1,
				InputSchema: map[string]any{
					"objective":     "count",
					"dp_dimensions": []any{3},
					"target":        []any{2},
					"base_cases":    []any{map[string]any{"index": []any{0}, "value": 1}},
					"transitions":   []any{map[string]any{"offset": []any{-1}, "weight": 1}},
				},
			},
			{
				ID: "n_verify", Kind: "verify", Question: "Verify original constraints.", DependsOn: []string{"n_solve"},
				Archetype: "candidate_verify", ScaffoldClass: BraidScaffoldClassCandidateVerify,
				ScaffoldID: BraidScaffoldIDGenericV1, InputSchema: map[string]any{
					"candidates":  []any{map[string]any{"id": "answer", "value": 2}},
					"predicates":  []any{map[string]any{"name": "equals_target", "check_type": "equals", "expected": 2}},
					"answer":      map[string]any{"id": "answer", "value": 2},
					"output_kind": "verification",
				},
			},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	graph = NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 7)
	if graph.Nodes[1].ScaffoldID != BraidScaffoldIDRecurrenceTableV1 {
		t.Fatalf("solve scaffold id=%q, want %q", graph.Nodes[1].ScaffoldID, BraidScaffoldIDRecurrenceTableV1)
	}
	if graph.Nodes[2].ScaffoldID != BraidScaffoldIDPropertyCheckV1 {
		t.Fatalf("verify scaffold id=%q, want %q", graph.Nodes[2].ScaffoldID, BraidScaffoldIDPropertyCheckV1)
	}
	if err := ValidateBraidGraphScaffoldContract(graph); err != nil {
		t.Fatalf("normalized scaffold contract rejected: %v", err)
	}
}

func TestStateReplayScaffoldRequiresUCIShapedInput(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{ScaffoldClass: BraidScaffoldClassStateTransition, ScaffoldID: BraidScaffoldIDStateReplayV1}
	if _, ok := resolveBraidRuntimeScaffold(node, handoff, map[string]any{
		"actions": "dependency chain over node_0, node_1, node_2",
	}); ok {
		t.Fatal("state_replay_v1 resolved for non-UCI action text")
	}
	if _, ok := resolveBraidRuntimeScaffold(node, handoff, map[string]any{
		"move_sequence": "e2e4 e7e5 g1f3",
	}); !ok {
		t.Fatal("state_replay_v1 did not resolve for UCI move sequence")
	}
}

func TestNormalizeLongCoTControllerCapsRepairedGraphNodes(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract."},
			{ID: "n_solve_a", Kind: "solve", Question: "Solve A.", DependsOn: []string{"n_extract"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "a"}},
			{ID: "n_solve_b", Kind: "solve", Question: "Solve B.", DependsOn: []string{"n_extract"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "b"}},
			{ID: "n_solve_c", Kind: "solve", Question: "Solve C.", DependsOn: []string{"n_extract"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "c"}},
			{ID: "n_solve_d", Kind: "solve", Question: "Solve D.", DependsOn: []string{"n_extract"}, Archetype: "explicit_dag", ScaffoldClass: BraidScaffoldClassExplicitDAG, ScaffoldID: BraidScaffoldIDSearchBacktrackV1, InputSchema: map[string]any{"prompt": "d"}},
			{ID: "n_verify", Kind: "verify", Question: "Verify original constraints.", DependsOn: []string{"n_solve_d"}, Archetype: "candidate_verify", ScaffoldClass: BraidScaffoldClassCandidateVerify, ScaffoldID: BraidScaffoldIDPropertyCheckV1, InputSchema: map[string]any{"candidates": "answer"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce.", DependsOn: []string{"n_solve_d", "n_verify"}},
			{ID: "n_extra", Kind: "extract", Question: "Extra."},
		},
		FinalNode: "n_reduce",
	}
	graph = NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 7)
	if len(graph.Nodes) != 4 {
		t.Fatalf("node count=%d, want 4", len(graph.Nodes))
	}
	if err := ValidateBraidGraph(graph, 7); err != nil {
		t.Fatalf("normalized graph did not validate: %v", err)
	}
	if err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController); err != nil {
		t.Fatalf("normalized graph did not satisfy policy: %v", err)
	}
}

func TestMissingScaffoldContractErrorIdentifiesNodeAndFields(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract."},
			{ID: "n_solve", Kind: "solve", Question: "Solve.", DependsOn: []string{"n_extract"}},
		},
		FinalNode: "n_solve",
	}
	err := ValidateBraidGraphScaffoldContract(graph)
	if err == nil {
		t.Fatal("expected error for missing scaffold fields")
	}
	mse, ok := IsMissingScaffoldContract(err)
	if !ok {
		t.Fatalf("expected MissingScaffoldContractError, got %T: %v", err, err)
	}
	if mse.NodeID != "n_solve" {
		t.Errorf("NodeID=%q want %q", mse.NodeID, "n_solve")
	}
	if len(mse.Missing) != 4 {
		t.Errorf("Missing=%v want 4 fields", mse.Missing)
	}
}

func TestUnknownBraidDependencyErrorIdentifiesNodeAndDependency(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract."},
			{ID: "n_solve", Kind: "solve", Question: "Solve.", DependsOn: []string{"n_missing"}},
		},
		FinalNode: "n_solve",
	}
	err := ValidateBraidGraph(graph, 8)
	ude, ok := IsUnknownBraidDependency(err)
	if !ok {
		t.Fatalf("expected UnknownBraidDependencyError, got %T: %v", err, err)
	}
	if ude.NodeID != "n_solve" || ude.DepID != "n_missing" {
		t.Fatalf("unknown dependency=%#v", ude)
	}
	if len(ude.KnownNode) != 2 || ude.KnownNode[0] != "n_extract" || ude.KnownNode[1] != "n_solve" {
		t.Fatalf("known nodes=%v", ude.KnownNode)
	}
}

func TestRepairPromptIncludesScaffoldInstructionsOnContractError(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		Name:                    "graph_plan",
		Prompt:                  "Return JSON only.",
		BraidGraphPolicy:        BraidGraphPolicyLongCoTController,
		RequireScaffoldContract: true,
	}

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_solve", Kind: "solve", Question: "Solve."},
		},
		FinalNode: "n_solve",
	}
	validationErr := ValidateBraidGraphScaffoldContract(graph)
	if validationErr == nil {
		t.Fatal("expected scaffold contract error")
	}

	repairPrompt := buildBraidGraphRepairPrompt("task prompt", phase, engine.EngineOutput{}, "{}", validationErr, 7)

	for _, want := range []string{
		"Scaffold contract violation",
		"Repair counterexample JSON",
		"braid_graph_contract_failure",
		"missing_fields",
		"Every solve, cycle_solve, and verify node MUST include archetype, scaffold_class, scaffold_id, and input_schema",
		"explicit_dag/search_backtrack_v1",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestRepairPromptIncludesUnknownDependencyCounterexample(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		Name:             "graph_plan",
		Prompt:           "Return JSON only.",
		BraidGraphPolicy: BraidGraphPolicyLongCoTController,
	}
	validationErr := UnknownBraidDependencyError{
		NodeID:    "n_reduce",
		DepID:     "n_missing",
		KnownNode: []string{"n_extract", "n_solve", "n_reduce"},
	}
	repairPrompt := buildBraidGraphRepairPrompt("task prompt", phase, engine.EngineOutput{}, "{}", validationErr, 7)
	for _, want := range []string{
		"Repair counterexample JSON",
		"unknown_dep",
		"n_missing",
		"known_nodes",
		"every depends_on entry references an existing node id",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestRepairPromptIncludesMultiTargetSolveCounterexample(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		Name:             "graph_plan",
		Prompt:           "Return JSON only.",
		BraidGraphPolicy: BraidGraphPolicyLongCoTController,
	}
	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints."},
			{
				ID:        "n_solve",
				Kind:      "solve",
				Question:  "Solve several targets.",
				DependsOn: []string{"n_extract"},
				InputSchema: map[string]any{
					"target_nodes": []any{"node_0", "node_1"},
				},
			},
			{ID: "n_verify", Kind: "verify", Question: "Substitute candidate into original constraints.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}
	validationErr := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController)
	if validationErr == nil {
		t.Fatal("expected multi-target solve contract error")
	}
	repairPrompt := buildBraidGraphRepairPrompt("task prompt", phase, engine.EngineOutput{}, "{}", validationErr, 7)
	for _, want := range []string{
		"multi-target solve node has only target_nodes",
		"input_schema.target_nodes is for final requested outputs",
		"forbidden_input_shape",
		"minimal_valid_shape_independent",
		"input_schema.solve_targets",
		"cycle_clusters",
		"Minimal independent shape",
		"Do not leave input_schema with only target_nodes",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestRepairPromptIncludesInvalidScaffoldInputShapes(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		Name:             "graph_plan",
		Prompt:           "Return JSON only.",
		BraidGraphPolicy: BraidGraphPolicyLongCoTController,
	}
	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{{
			ID:            "n_solve",
			Kind:          "solve",
			Question:      "Solve symbolic trace.",
			Archetype:     BraidScaffoldClassSymbolicTrace,
			ScaffoldClass: BraidScaffoldClassSymbolicTrace,
			ScaffoldID:    BraidScaffoldIDTypeInferenceV1,
			InputSchema: map[string]any{
				"source_ref": "official_prompt",
				"prompt":     "infer all bindings",
			},
		}},
		FinalNode: "n_solve",
	}
	validationErr := ValidateBraidGraphScaffoldContract(graph)
	if validationErr == nil {
		t.Fatal("expected invalid scaffold input error")
	}
	repairPrompt := buildBraidGraphRepairPrompt("task prompt", phase, engine.EngineOutput{}, "{}", validationErr, 7)
	for _, want := range []string{
		"invalid scaffold input schema",
		"forbidden_input_keys_only",
		"minimal_valid_fallback",
		"minimal_valid_specialized_examples",
		"Minimal valid fallback shape",
		"Minimal symbolic_trace/type_inference_v1 shape",
		"candidate_verify/property_check_v1",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestRepairPromptIncludesOverbroadCycleCounterexample(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		Name:             "graph_plan",
		Prompt:           "Return JSON only.",
		BraidGraphPolicy: BraidGraphPolicyLongCoTController,
	}
	validationErr := fmt.Errorf("braid graph: longcot_controller cycle_solve node %q declares overbroad cycle cluster of 8 targets; split into smaller strongly connected clusters or independent solve_targets", "n_cycle")
	repairPrompt := buildBraidGraphRepairPrompt("task prompt", phase, engine.EngineOutput{}, "{}", validationErr, 7)
	for _, want := range []string{
		"Overbroad cycle_solve contract violation",
		"cycle_solve cluster is too broad",
		"not the whole dependency graph",
		"solve_targets/nodes_to_solve",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestRepairPromptIncludesMissingCycleClusterCounterexample(t *testing.T) {
	t.Parallel()

	phase := REPLRunnerPhase{
		Name:             "graph_plan",
		Prompt:           "Return JSON only.",
		BraidGraphPolicy: BraidGraphPolicyLongCoTController,
	}
	validationErr := fmt.Errorf("braid graph: longcot_controller cycle_solve node %q must declare input_schema.cycle_clusters", "n_cycle")
	repairPrompt := buildBraidGraphRepairPrompt("task prompt", phase, engine.EngineOutput{}, "{}", validationErr, 7)
	for _, want := range []string{
		"Cycle-solve missing cluster contract violation",
		"cycle_solve node is missing cycle_clusters",
		"array of arrays of explicit node ids",
		"change kind to solve and use input_schema.solve_targets",
	} {
		if !strings.Contains(repairPrompt, want) {
			t.Errorf("repair prompt missing %q", want)
		}
	}
}

func TestScaffoldDeclaredHandoffUsesNodeFields(t *testing.T) {
	t.Parallel()

	node := BraidNode{
		ID:            "n_solve",
		Kind:          "solve",
		Archetype:     "state_transition",
		ScaffoldClass: "state_transition",
		ScaffoldID:    "state_replay_v1",
		InputSchema:   map[string]any{"initial_state": "s0", "actions": "a1,a2", "goal": "g"},
	}

	handoff := BuildBraidNodeHandoff(node, "some prompt text", nil, "")
	if handoff.ScaffoldClass != "state_transition" {
		t.Errorf("ScaffoldClass=%q want %q", handoff.ScaffoldClass, "state_transition")
	}
	if handoff.ScaffoldID != "state_replay_v1" {
		t.Errorf("ScaffoldID=%q want %q", handoff.ScaffoldID, "state_replay_v1")
	}
}
