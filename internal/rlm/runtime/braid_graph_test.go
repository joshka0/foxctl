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
			{ID: "n2", Kind: "solve", Question: "Solve from extracted constraints.", DependsOn: []string{"n1"}},
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

func TestBraidValidateLongCoTControllerPolicy(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "at least two solve nodes") {
		t.Fatalf("ValidateBraidGraphPolicy() err=%v, want at least two solve nodes", err)
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

func TestRenderBraidNodeChildPromptIncludesRootTask(t *testing.T) {
	t.Parallel()

	prompt := RenderBraidNodeChildPrompt(BraidNode{
		ID:              "n1",
		Kind:            "solve",
		Question:        "Solve node_4.",
		DependsOn:       []string{"n0"},
		ExpectedOutput:  "node_4 value",
		MaxSummaryChars: 300,
	}, "Official problem text", map[string]string{"n0": "status: solved answer: 42"})

	for _, want := range []string{
		"BRAID node n1 (solve)",
		"Official root task:",
		"Official problem text",
		"Dependency summaries:",
		"status: solved answer: 42",
		"Task:",
		"Solve node_4.",
		"Expected output:",
		"node_4 value",
		"under 300 characters",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
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
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
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
