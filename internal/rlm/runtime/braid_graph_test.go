package runtime

import (
	"strings"
	"testing"
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
			{ID: "n_cycle", Kind: "cycle_solve", Question: "Solve mutual constraints.", DependsOn: []string{"n_extract"}},
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

func TestBraidValidateLongCoTControllerRejectsSingleSolve(t *testing.T) {
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
	err := ValidateBraidGraphPolicy(graph, BraidGraphPolicyLongCoTController)
	if err == nil || !strings.Contains(err.Error(), "at least two solve-like nodes") {
		t.Fatalf("ValidateBraidGraphPolicy() err=%v, want at least two solve-like nodes", err)
	}
}

func TestNormalizeBraidGraphForPolicySplitsSingleSolveAndClampsSummary(t *testing.T) {
	t.Parallel()

	graph := BraidGraph{
		Version: 1,
		Nodes: []BraidNode{
			{ID: "n_extract", Kind: "extract", Question: "Extract constraints.", MaxSummaryChars: 2000},
			{ID: "n_solve", Kind: "solve", Question: "Solve candidate.", DependsOn: []string{"n_extract"}, MaxSummaryChars: 2000},
			{ID: "n_verify", Kind: "verify", Question: "Simulate candidate on initial state and check final state matches goal state.", DependsOn: []string{"n_solve"}},
			{ID: "n_reduce", Kind: "reduce", Question: "Reduce final.", DependsOn: []string{"n_solve", "n_verify"}},
		},
		FinalNode: "n_reduce",
	}

	normalized := NormalizeBraidGraphForPolicy(graph, BraidGraphPolicyLongCoTController, 8)
	if len(normalized.Nodes) != 5 {
		t.Fatalf("normalized node count = %d, want 5", len(normalized.Nodes))
	}
	if normalized.Nodes[0].MaxSummaryChars != maxBraidNodeSummaryChars {
		t.Fatalf("summary cap was not clamped: %d", normalized.Nodes[0].MaxSummaryChars)
	}
	if normalized.Nodes[2].ID != "n_solve_refine" || normalized.Nodes[2].Kind != "solve" {
		t.Fatalf("missing inserted refine solve node: %#v", normalized.Nodes[2])
	}
	for _, idx := range []int{1, 2, 3} {
		if normalized.Nodes[idx].HelperPolicy != BraidNodeHelperPolicyPreferred {
			t.Fatalf("node %s helper_policy=%q want preferred", normalized.Nodes[idx].ID, normalized.Nodes[idx].HelperPolicy)
		}
	}
	if !dependsOnBraidNode(normalized.Nodes[3], "n_solve_refine") {
		t.Fatalf("verify deps were not updated: %#v", normalized.Nodes[3].DependsOn)
	}
	if !dependsOnBraidNode(normalized.Nodes[4], "n_solve_refine") {
		t.Fatalf("reduce deps were not updated: %#v", normalized.Nodes[4].DependsOn)
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

func TestBraidParseGraphTextRejectsMarkdownFence(t *testing.T) {
	t.Parallel()

	raw := "```json\n{\"version\":1,\"nodes\":[{\"id\":\"n1\",\"kind\":\"extract\",\"question\":\"q\"}],\"final_node\":\"n1\"}\n```"
	_, err := ParseBraidGraphText(raw)
	if err == nil {
		t.Fatal("ParseBraidGraphText() succeeded for fenced JSON")
	}
}

func TestBraidParseGraphTextRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	raw := `{"version":1,"nodes":[{"id":"n1","kind":"extract","question":"q","extra":"x"}],"final_node":"n1"}`
	_, err := ParseBraidGraphText(raw)
	if err == nil {
		t.Fatal("ParseBraidGraphText() succeeded with unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ParseBraidGraphText() err = %v, want unknown field", err)
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
		"cycle_cluster",
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
	}, "Official problem text", map[string]string{"n_extract": "cycle_cluster: node_2,node_5,node_6,node_7; equations_or_checks: prime factor sum = 6"})

	for _, want := range []string{
		"Cycle-solve contract:",
		"candidate search",
		"fixed-point iteration",
		"constraint propagation",
		"finite candidate bounds",
		"full official root task is intentionally withheld",
		"cycle_cluster: node_2,node_5,node_6,node_7",
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
