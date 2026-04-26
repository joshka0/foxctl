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
	deps, ok := input["dependency_summaries"].(map[string]any)
	if !ok || deps["n_solve"] == "" {
		t.Fatalf("input missing dependency summaries: %#v", input)
	}
	if _, ok := input["initial_state"]; !ok {
		t.Fatalf("input missing parsed official fields: %#v", input)
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
	verifySummary, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_verify", Kind: "verify"}, map[string]string{"n_solve": solveSummary})
	if !ok {
		t.Fatal("expected runtime verify shortcut")
	}
	if !braidVerificationSummaryPassed(verifySummary) {
		t.Fatalf("verify shortcut did not pass: %s", verifySummary)
	}
}

func TestBraidRuntimeShortcutReduceForwardsVerifiedSolution(t *testing.T) {
	t.Parallel()

	solveSummary := "status: completed summary: status: solved answer: solution = [[2,1,2],[0,0,2]] checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier."
	verifySummary := "status: pass summary: answer: pass: true checks: upstream solve dependency was already verified by the runtime scaffold verifier."
	reduceSummary, ok := runBraidRuntimeNodeShortcut(BraidNode{ID: "n_reduce", Kind: "reduce"}, map[string]string{
		"n_solve":  solveSummary,
		"n_verify": verifySummary,
	})
	if !ok {
		t.Fatal("expected runtime reduce shortcut")
	}
	if !strings.Contains(reduceSummary, "solution = [[2,1,2],[0,0,2]]") {
		t.Fatalf("reduce shortcut lost solution: %s", reduceSummary)
	}
}
