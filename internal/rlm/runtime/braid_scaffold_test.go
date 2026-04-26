package runtime

import (
	"strings"
	"testing"
)

func TestClassifyScaffoldDomainCS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prompt string
		want   string
	}{
		{
			prompt: "Use Algorithm W to infer types for the following let-bindings...",
			want:   "cs",
		},
		{
			prompt: "Simulate a Hindley-Milner type inference engine...",
			want:   "cs",
		},
		{
			prompt: "Simulate a distributed memory bitonic sort on 16 processors...",
			want:   "cs",
		},
		{
			prompt: "Given a Turing machine with states Q={q0,q1,q2}...",
			want:   "cs",
		},
		{
			prompt: "Compute the matrix chain multiplication order for dimensions...",
			want:   "cs",
		},
		{
			prompt: "Run the Ford-Fulkerson flow network algorithm...",
			want:   "cs",
		},
	}
	for _, tc := range tests {
		got := classifyScaffoldDomain(tc.prompt)
		if got != tc.want {
			t.Errorf("classifyScaffoldDomain(%q) = %q, want %q", tc.prompt[:40], got, tc.want)
		}
	}
}

func TestClassifyScaffoldDomainChemistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prompt string
		want   string
	}{
		{
			prompt: "Given SMILES strings for molecules A: CCO, B: CC(=O)O...",
			want:   "chemistry",
		},
		{
			prompt: "Compute the Morgan fingerprint for the molecule...",
			want:   "chemistry",
		},
		{
			prompt: "Predict the reaction product when molecule A reacts with B...",
			want:   "chemistry",
		},
		{
			prompt: "What is the topological polar surface area of...",
			want:   "chemistry",
		},
	}
	for _, tc := range tests {
		got := classifyScaffoldDomain(tc.prompt)
		if got != tc.want {
			t.Errorf("classifyScaffoldDomain(%q) = %q, want %q", tc.prompt[:40], got, tc.want)
		}
	}
}

func TestClassifyScaffoldDomainChess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prompt string
		want   string
	}{
		{
			prompt: "Alice and Bob play a minimax game. The knight is at [7,13]...",
			want:   "chess",
		},
		{
			prompt: "A chess board is 30x30 with pawns at positions...",
			want:   "chess",
		},
		{
			prompt: "Find the minimum number of knight moves to capture all pawns...",
			want:   "chess",
		},
	}
	for _, tc := range tests {
		got := classifyScaffoldDomain(tc.prompt)
		if got != tc.want {
			t.Errorf("classifyScaffoldDomain(%q) = %q, want %q", tc.prompt[:40], got, tc.want)
		}
	}
}

func TestClassifyScaffoldDomainMath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prompt string
		want   string
	}{
		{
			prompt: "Problem node_0: The top section of an 8cm rectangular sheet...",
			want:   "math",
		},
		{
			prompt: "DAG Chained Problems: A sequence of olympiad-style subproblems...",
			want:   "math",
		},
	}
	for _, tc := range tests {
		got := classifyScaffoldDomain(tc.prompt)
		if got != tc.want {
			t.Errorf("classifyScaffoldDomain(%q) = %q, want %q", tc.prompt[:40], got, tc.want)
		}
	}
}

func TestClassifyScaffoldDomainLogic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prompt string
		want   string
	}{
		{
			prompt: "Sokoban puzzle on an 11x8 grid with 5 boxes...",
			want:   "logic",
		},
		{
			prompt: "BlocksWorld: move blocks from initial state to goal state...",
			want:   "logic",
		},
		{
			prompt: "Tower of Hanoi with 5 disks and 3 pegs...",
			want:   "logic",
		},
	}
	for _, tc := range tests {
		got := classifyScaffoldDomain(tc.prompt)
		if got != tc.want {
			t.Errorf("classifyScaffoldDomain(%q) = %q, want %q", tc.prompt[:40], got, tc.want)
		}
	}
}

func TestClassifyScaffoldDomainUnknown(t *testing.T) {
	t.Parallel()

	got := classifyScaffoldDomain("What is the meaning of life?")
	if got != "" {
		t.Fatalf("expected empty domain for generic prompt, got %q", got)
	}
}

func TestClassifyScaffoldDomainPriority(t *testing.T) {
	t.Parallel()

	// When multiple markers match, the first matching domain wins.
	// CS markers are checked before chemistry, chess, math, logic.
	prompt := "Given SMILES strings, compute the distributed memory bitonic sort"
	got := classifyScaffoldDomain(prompt)
	if got != "cs" {
		t.Fatalf("expected cs (checked first), got %q", got)
	}
}

func TestResolveBraidRuntimeScaffoldByHeuristicFallback(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{Node: node}
	input := map[string]any{"prompt": "test"}
	rootPrompt := "Use Algorithm W to infer types for the following let-bindings..."

	scaffold, ok := resolveBraidRuntimeScaffoldByHeuristic(node, handoff, input, rootPrompt)
	if !ok {
		t.Fatal("expected heuristic to match CS prompt")
	}
	// Heuristic now returns domain-specific scaffold instead of general_solver
	if scaffold.Class != BraidScaffoldClassTypeInference {
		t.Fatalf("expected type_inference class for CS domain, got %q", scaffold.Class)
	}
	if scaffold.ID != BraidScaffoldIDHindleyMilnerV1 {
		t.Fatalf("expected hindley_milner_v1 id, got %q", scaffold.ID)
	}
	if strings.TrimSpace(scaffold.PresetSource) == "" {
		t.Fatal("expected non-empty preset source")
	}
	if scaffold.MaxSourceLines != 380 {
		t.Fatalf("expected 380 max lines, got %d", scaffold.MaxSourceLines)
	}
	if scaffold.Verifier == nil {
		t.Fatal("type_inference should have a verifier")
	}
}

func TestResolveBraidRuntimeScaffoldByHeuristicNoMatch(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{Node: node}
	input := map[string]any{"prompt": "test"}
	rootPrompt := "What is the capital of France?"

	_, ok := resolveBraidRuntimeScaffoldByHeuristic(node, handoff, input, rootPrompt)
	if ok {
		t.Fatal("expected no heuristic match for generic prompt")
	}
}

func TestResolveBraidRuntimeScaffoldByHeuristicRejectsNonSolve(t *testing.T) {
	t.Parallel()

	node := BraidNode{ID: "n_extract", Kind: "extract"}
	handoff := BraidNodeHandoff{Node: node}
	input := map[string]any{}
	rootPrompt := "Use Algorithm W to infer types..."

	_, ok := resolveBraidRuntimeScaffoldByHeuristic(node, handoff, input, rootPrompt)
	if ok {
		t.Fatal("expected no heuristic match for extract node")
	}
}

func TestResolveBraidRuntimeScaffoldDefaultCaseFallsBackToHeuristic(t *testing.T) {
	t.Parallel()

	// When handoff has no scaffold_class, the default case should try heuristic.
	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{Node: node} // no ScaffoldClass
	input := map[string]any{"some_key": "some_value"}
	rootPrompt := "Simulate a Hindley-Milner type inference engine with 364 let-bindings"

	scaffold, ok := ResolveBraidRuntimeScaffold(node, handoff, input, rootPrompt)
	if !ok {
		t.Fatal("expected default-case heuristic fallback to match CS prompt")
	}
	// Heuristic now returns domain-specific scaffold instead of general_solver
	if scaffold.Class != BraidScaffoldClassTypeInference {
		t.Fatalf("expected type_inference class for CS domain, got %q", scaffold.Class)
	}
}

func TestGeneralSolverPresetSourceIsValidGo(t *testing.T) {
	t.Parallel()

	source := generalSolverPresetSource()
	if !strings.Contains(source, "func Solve(input map[string]any) map[string]any") {
		t.Fatal("preset source missing Solve function")
	}
	if !strings.Contains(source, "func parseProblem(input map[string]any)") {
		t.Fatal("preset source missing parseProblem function")
	}
	if !strings.Contains(source, "func computeAnswer(parsed map[string]any)") {
		t.Fatal("preset source missing computeAnswer function")
	}
	if !strings.Contains(source, "func selfCheck(answer string, parsed map[string]any)") {
		t.Fatal("preset source missing selfCheck function")
	}
	if !strings.Contains(source, "func formatAnswer(answer string, parsed map[string]any)") {
		t.Fatal("preset source missing formatAnswer function")
	}
	if !strings.Contains(source, "TODO") {
		t.Fatal("preset source should contain TODO placeholders for LLM to fill in")
	}
}

func TestBuildGeneralSolverHelperContract(t *testing.T) {
	t.Parallel()

	contract := buildGeneralSolverHelperContract()
	for _, want := range []string{
		"Parse the problem",
		"core computation",
		"self-check",
		"ok:true",
		"ok:false",
		"first_failure",
		"repair_hint",
		"bounded",
		"step-by-step",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("general solver contract missing %q:\n%s", want, contract)
		}
	}
}

func TestResolveBraidRuntimeScaffoldTypedHandoffTakesPrecedence(t *testing.T) {
	t.Parallel()

	// When the handoff has a valid typed scaffold_class, the typed path should
	// handle it, not the heuristic fallback.
	node := BraidNode{ID: "n_solve", Kind: "solve"}
	handoff := BraidNodeHandoff{
		Node:          node,
		ScaffoldClass: BraidScaffoldClassFiniteStateTransition,
		ScaffoldID:    BraidScaffoldIDStackRelocationV1,
	}
	input := map[string]any{
		"initial_state":     []any{[]any{float64(1)}, []any{float64(2), float64(3)}, []any{}},
		"goal_state":        []any{[]any{}, []any{float64(1)}, []any{float64(3), float64(2)}},
		"transition_model":  "stack_relocation",
	}
	rootPrompt := "Use Algorithm W..." // CS prompt, but typed handoff takes precedence

	scaffold, ok := ResolveBraidRuntimeScaffold(node, handoff, input, rootPrompt)
	if !ok {
		t.Fatal("expected typed handoff to match")
	}
	if scaffold.Class != BraidScaffoldClassFiniteStateTransition {
		t.Fatalf("expected finite_state_transition class from typed handoff, got %q", scaffold.Class)
	}
}
