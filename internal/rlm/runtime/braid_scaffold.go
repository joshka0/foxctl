package runtime

import "strings"

const (
	BraidScaffoldClassFiniteStateTransition = "finite_state_transition"
	BraidScaffoldClassGraphSearch           = "graph_search"
	BraidScaffoldClassNumericDP             = "numeric_dp"
	BraidScaffoldClassSequenceSimulation    = "sequence_simulation"
	BraidScaffoldClassConstraintSolver      = "constraint_solver"
	BraidScaffoldClassGeneralSolver         = "general_solver"
	BraidScaffoldIDStackRelocationV1        = "stack_relocation_v1"
	BraidScaffoldIDResourcePathMinInitialV1 = "resource_path_min_initial_v1"
	BraidScaffoldIDExplicitShortestPathV1   = "explicit_shortest_path_v1"
	BraidScaffoldIDRecurrenceTableV1        = "recurrence_table_v1"
	BraidScaffoldIDJSONPatchSequenceV1      = "json_patch_v1"
	BraidScaffoldIDFiniteDomainV1           = "finite_domain_v1"
	BraidScaffoldIDGeneralSolverV1          = "general_solver_v1"
	BraidScaffoldClassTypeInference         = "type_inference"
	BraidScaffoldIDHindleyMilnerV1          = "hindley_milner_v1"
	BraidScaffoldClassChainedDAG            = "chained_dag"
	BraidScaffoldIDBacktrackingV1           = "backtracking_dag_v1"
)

type braidRuntimeScaffold struct {
	Class          string
	ID             string
	PresetName     string
	PresetSource   string
	PresetInput    map[string]any
	MaxSourceLines int
	MaxSourceChars int
	Verifier       HelperAnswerVerifier
}

// ResolveBraidRuntimeScaffold attempts to find a runtime scaffold for the given
// node. It first tries the typed handoff path (scaffold_class/scaffold_id),
// then falls back to heuristic prompt classification via rootPrompt when the
// handoff did not produce a match.
func ResolveBraidRuntimeScaffold(node BraidNode, handoff BraidNodeHandoff, input map[string]any, rootPrompt string) (braidRuntimeScaffold, bool) {
	if !isBraidSolveKind(node.Kind) || len(input) == 0 {
		return braidRuntimeScaffold{}, false
	}
	switch handoff.ScaffoldClass {
	case BraidScaffoldClassFiniteStateTransition:
		if handoff.ScaffoldID != BraidScaffoldIDStackRelocationV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeTransitionSystem(input) || !braidHelperInputLooksLikeStackRelocation(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassFiniteStateTransition,
			ID:             BraidScaffoldIDStackRelocationV1,
			PresetName:     BraidScaffoldClassFiniteStateTransition + "/" + BraidScaffoldIDStackRelocationV1,
			PresetSource:   stackTransitionPlannerPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 380,
			MaxSourceChars: 18000,
			Verifier:       stackMoveAnswerVerifier,
		}, true
	case BraidScaffoldClassGraphSearch:
		switch handoff.ScaffoldID {
		case BraidScaffoldIDResourcePathMinInitialV1:
			if !braidHelperInputLooksLikeResourcePathMinInitial(input) {
				return braidRuntimeScaffold{}, false
			}
			return braidRuntimeScaffold{
				Class:          BraidScaffoldClassGraphSearch,
				ID:             BraidScaffoldIDResourcePathMinInitialV1,
				PresetName:     BraidScaffoldClassGraphSearch + "/" + BraidScaffoldIDResourcePathMinInitialV1,
				PresetSource:   gridResourcePathPresetSource(),
				PresetInput:    cloneMapAny(input),
				MaxSourceLines: 180,
				MaxSourceChars: 9000,
				Verifier:       gridResourcePathAnswerVerifier,
			}, true
		case BraidScaffoldIDExplicitShortestPathV1:
			if !braidHelperInputLooksLikeExplicitShortestPath(input) {
				return braidRuntimeScaffold{}, false
			}
			return braidRuntimeScaffold{
				Class:          BraidScaffoldClassGraphSearch,
				ID:             BraidScaffoldIDExplicitShortestPathV1,
				PresetName:     BraidScaffoldClassGraphSearch + "/" + BraidScaffoldIDExplicitShortestPathV1,
				PresetSource:   explicitShortestPathPresetSource(),
				PresetInput:    cloneMapAny(input),
				MaxSourceLines: 220,
				MaxSourceChars: 11000,
				Verifier:       explicitShortestPathAnswerVerifier,
			}, true
		default:
			return braidRuntimeScaffold{}, false
		}
	case BraidScaffoldClassNumericDP:
		if handoff.ScaffoldID != BraidScaffoldIDRecurrenceTableV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeNumericDP(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassNumericDP,
			ID:             BraidScaffoldIDRecurrenceTableV1,
			PresetName:     BraidScaffoldClassNumericDP + "/" + BraidScaffoldIDRecurrenceTableV1,
			PresetSource:   numericDPTablePresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 300,
			MaxSourceChars: 14000,
			Verifier:       numericDPAnswerVerifier,
		}, true
	case BraidScaffoldClassSequenceSimulation:
		if handoff.ScaffoldID != BraidScaffoldIDJSONPatchSequenceV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeSequenceSimulation(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassSequenceSimulation,
			ID:             BraidScaffoldIDJSONPatchSequenceV1,
			PresetName:     BraidScaffoldClassSequenceSimulation + "/" + BraidScaffoldIDJSONPatchSequenceV1,
			PresetSource:   jsonPatchSequenceSimulationPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 360,
			MaxSourceChars: 18000,
			Verifier:       sequenceSimulationAnswerVerifier,
		}, true
	case BraidScaffoldClassConstraintSolver:
		if handoff.ScaffoldID != BraidScaffoldIDFiniteDomainV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeFiniteDomainConstraint(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassConstraintSolver,
			ID:             BraidScaffoldIDFiniteDomainV1,
			PresetName:     BraidScaffoldClassConstraintSolver + "/" + BraidScaffoldIDFiniteDomainV1,
			PresetSource:   finiteDomainConstraintPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 340,
			MaxSourceChars: 18000,
			Verifier:       finiteDomainAnswerVerifier,
		}, true
	case BraidScaffoldClassChainedDAG:
		if handoff.ScaffoldID != BraidScaffoldIDBacktrackingV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeChainedDAG(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassChainedDAG,
			ID:             BraidScaffoldIDBacktrackingV1,
			PresetName:     BraidScaffoldClassChainedDAG + "/" + BraidScaffoldIDBacktrackingV1,
			PresetSource:   backtrackingDAGPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 380,
			MaxSourceChars: 18000,
			Verifier:       chainedDAGAnswerVerifier,
		}, true
	case BraidScaffoldClassTypeInference:
		if handoff.ScaffoldID != BraidScaffoldIDHindleyMilnerV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeTypeInference(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassTypeInference,
			ID:             BraidScaffoldIDHindleyMilnerV1,
			PresetName:     BraidScaffoldClassTypeInference + "/" + BraidScaffoldIDHindleyMilnerV1,
			PresetSource:   hindleyMilnerPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 380,
			MaxSourceChars: 18000,
			Verifier:       typeInferenceAnswerVerifier,
		}, true
	default:
		// Heuristic fallback: classify from the root prompt when no typed
		// handoff matched. The general_solver scaffold provides structure
		// (parse, compute, verify, format) without domain-specific logic.
		return resolveBraidRuntimeScaffoldByHeuristic(node, handoff, input, rootPrompt)
	}
}

// resolveBraidRuntimeScaffold is the internal dispatch. Exported as
// ResolveBraidRuntimeScaffold above.
func resolveBraidRuntimeScaffold(node BraidNode, handoff BraidNodeHandoff, input map[string]any, rootPrompt string) (braidRuntimeScaffold, bool) {
	return ResolveBraidRuntimeScaffold(node, handoff, input, rootPrompt)
}

func applyBraidRuntimeScaffoldToHelperConfig(cfg HelperFactoryConfig, scaffold braidRuntimeScaffold) HelperFactoryConfig {
	cfg.PresetName = scaffold.PresetName
	cfg.PresetSource = scaffold.PresetSource
	cfg.PresetInput = cloneMapAny(scaffold.PresetInput)
	cfg.AnswerVerifier = scaffold.Verifier
	if scaffold.MaxSourceLines > cfg.MaxSourceLines {
		cfg.MaxSourceLines = scaffold.MaxSourceLines
	}
	if scaffold.MaxSourceChars > cfg.MaxSourceChars {
		cfg.MaxSourceChars = scaffold.MaxSourceChars
	}
	return cfg
}

func braidHelperInputLooksLikeStackRelocation(input map[string]any) bool {
	initial, okInitial := stackStateFromAny(input["initial_state"])
	goal, okGoal := stackStateFromAny(input["goal_state"])
	if !okInitial || !okGoal || len(initial) == 0 || len(initial) != len(goal) {
		return false
	}
	if !sameIntMultiset(flattenIntStacks(initial), flattenIntStacks(goal)) {
		return false
	}
	if model, ok := input["transition_model"].(string); ok && strings.TrimSpace(model) != "" {
		return strings.TrimSpace(model) == "stack_relocation"
	}
	return true
}

func flattenIntStacks(stacks [][]int) []int {
	out := []int{}
	for _, stack := range stacks {
		out = append(out, stack...)
	}
	return out
}

func sameIntMultiset(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[int]int{}
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func braidHelperInputLooksLikeResourcePathMinInitial(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	grid, ok := intGridFromAny(input["grid_layout"])
	if !ok || len(grid) == 0 || len(grid[0]) == 0 {
		return false
	}
	for _, row := range grid {
		if len(row) != len(grid[0]) {
			return false
		}
	}
	return true
}

func braidHelperInputLooksLikeExplicitShortestPath(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if objective, ok := input["objective"].(string); ok && strings.TrimSpace(objective) != "" && strings.TrimSpace(objective) != "shortest_path_length" {
		return false
	}
	_, ok := explicitGraphFromInput(input)
	return ok
}

func braidHelperInputLooksLikeNumericDP(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if class, ok := input["scaffold_class"].(string); !ok || strings.TrimSpace(class) != BraidScaffoldClassNumericDP {
		return false
	}
	if id, ok := input["scaffold_id"].(string); ok && strings.TrimSpace(id) != "" && strings.TrimSpace(id) != BraidScaffoldIDRecurrenceTableV1 {
		return false
	}
	if _, ok := numericDPProblemFromInput(input); !ok {
		return false
	}
	return true
}

func braidHelperInputLooksLikeSequenceSimulation(input map[string]any) bool {
	_, ok := sequenceSimulationSpecFromInput(input)
	return ok
}

func braidHelperInputLooksLikeFiniteDomainConstraint(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if class, ok := input["scaffold_class"].(string); ok && strings.TrimSpace(class) != "" && strings.TrimSpace(class) != BraidScaffoldClassConstraintSolver {
		return false
	}
	if id, ok := input["scaffold_id"].(string); ok && strings.TrimSpace(id) != "" && strings.TrimSpace(id) != BraidScaffoldIDFiniteDomainV1 {
		return false
	}
	_, ok := finiteDomainWitnessFromInput(input)
	return ok
}

func braidHelperInputLooksLikeTypeInference(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	_, hasExpr := input["expressions"]
	_, hasBindings := input["let_bindings"]
	_, hasEnv := input["type_environment"]
	_, hasConstraints := input["unification_constraints"]
	return (hasExpr || hasBindings) && (hasEnv || hasConstraints)
}

func braidHelperInputLooksLikeChainedDAG(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	_, hasNodes := input["node_ids"]
	_, hasDeps := input["dependencies"]
	_, hasSubproblems := input["subproblems"]
	_, hasVars := input["variables"]
	_, hasConstraints := input["constraints"]
	_, hasAnswerSpec := input["answer_spec"]
	count := 0
	for _, ok := range []bool{hasNodes, hasDeps, hasSubproblems, hasVars, hasConstraints, hasAnswerSpec} {
		if ok {
			count++
		}
	}
	return count >= 2
}

// resolveBraidRuntimeScaffoldByHeuristic attempts to classify the problem shape
// from the root prompt when the parent model did not emit a typed handoff with
// a recognized scaffold_class. It returns a scaffold that provides structure
// (parse, compute, verify, format) without domain-specific logic, letting the
// LLM fill in the computation.
func resolveBraidRuntimeScaffoldByHeuristic(node BraidNode, handoff BraidNodeHandoff, input map[string]any, rootPrompt string) (braidRuntimeScaffold, bool) {
	if !isBraidSolveKind(node.Kind) {
		return braidRuntimeScaffold{}, false
	}
	detected := classifyScaffoldDomain(rootPrompt)
	if detected == "" {
		return braidRuntimeScaffold{}, false
	}
	return braidRuntimeScaffold{
		Class:          BraidScaffoldClassGeneralSolver,
		ID:             BraidScaffoldIDGeneralSolverV1,
		PresetName:     BraidScaffoldClassGeneralSolver + "/" + BraidScaffoldIDGeneralSolverV1,
		PresetSource:   generalSolverPresetSource(),
		PresetInput:    cloneMapAny(input),
		MaxSourceLines: 360,
		MaxSourceChars: 18000,
		Verifier:       nil,
	}, true
}

// classifyScaffoldDomain returns a domain label derived from prompt markers, or
// empty string when no domain is recognized. This is a lightweight heuristic
// used only when the typed handoff path did not produce a scaffold.
func classifyScaffoldDomain(prompt string) string {
	lower := strings.ToLower(prompt)
	// CS / type inference / algorithm simulation
	for _, marker := range []string{
		"algorithm w", "hindley-milner", "type inference",
		"turing machine", "flow network", "bitonic",
		"matrix chain", "register allocation", "ir optimi",
		"distributed memory", "scheduling algorithm",
		"unification", "let-binding", "let binding",
	} {
		if strings.Contains(lower, marker) {
			return "cs"
		}
	}
	// Chemistry / molecular reasoning
	for _, marker := range []string{
		"smiles", "molecule", "morgan fingerprint",
		"reaction", "substructure", "topological polar",
		"molecular", "chirality", "stereochem",
		"functional group", "bond order", "molar mass",
	} {
		if strings.Contains(lower, marker) {
			return "chemistry"
		}
	}
	// Chess / board simulation
	for _, marker := range []string{
		"knight", "bishop", "rook", "pawn",
		"minimax", "chess board", "chessboard",
		"captured via", "checkmate", "stalemate",
		"board size", "fen ",
	} {
		if strings.Contains(lower, marker) {
			return "chess"
		}
	}
	// Math / chained olympiad problems
	for _, marker := range []string{
		"node_0", "node_1", "dag chained",
		"subproblem", "olympiad", "mathemati",
		"modular inverse", "number theory", "combinator",
	} {
		if strings.Contains(lower, marker) {
			return "math"
		}
	}
	// Logic / planning (already well-covered by typed scaffolds, but
	// include for completeness when typed handoff fails)
	for _, marker := range []string{
		"sokoban", "blocksworld", "blocks world",
		"tower of hanoi", "packaging", "initial state",
		"goal state", "grid (", "dungeon",
	} {
		if strings.Contains(lower, marker) {
			return "logic"
		}
	}
	return ""
}

func generalSolverPresetSource() string {
	return strings.TrimSpace(`
func Solve(input map[string]any) map[string]any {
	parsed, parseOk := parseProblem(input)
	if !parseOk {
		return map[string]any{"ok": false, "first_failure": "parse failed", "repair_hint": "fix input parsing"}
	}
	answer, computeOk := computeAnswer(parsed)
	if !computeOk {
		return map[string]any{"ok": false, "first_failure": "compute_answer returned no result", "repair_hint": "implement the core computation"}
	}
	if diag, ok := selfCheck(answer, parsed); !ok {
		return map[string]any{"ok": false, "first_failure": diag, "repair_hint": "fix the computation"}
	}
	return map[string]any{"ok": true, "answer": formatAnswer(answer, parsed)}
}

func parseProblem(input map[string]any) (map[string]any, bool) {
	// Extract structured data from the raw input.
	// TODO: adapt for the specific domain.
	return input, true
}

func computeAnswer(parsed map[string]any) (string, bool) {
	// Run the core computation.
	// TODO: implement domain logic.
	// Return the answer string (e.g. "solution = ...").
	return "", false
}

func selfCheck(answer string, parsed map[string]any) (string, bool) {
	// Verify the answer against the problem constraints.
	// TODO: implement checks.
	if answer == "" {
		return "answer is empty", false
	}
	return "", true
}

func formatAnswer(answer string, parsed map[string]any) string {
	if len(answer) > 10 && answer[:10] == "solution =" {
		return answer
	}
	return "solution = " + answer
}
`)
}

func buildGeneralSolverHelperContract() string {
	return strings.TrimSpace(`
General solver helper contract:
- Parse the problem input into structured data (typed fields, not prose).
- Implement the core computation as a deterministic algorithm.
- Self-check the answer against the original problem constraints before returning.
- Return ok:true only with a verified answer in the requested format.
- If the computation fails or the self-check fails, return ok:false with first_failure and repair_hint.
- Do not refuse to compute. The scaffold provides structure; fill in the domain logic.
- Keep the implementation bounded. Do not enumerate all possible states when a constructive algorithm exists.
- For problems with many interdependent steps, compute step-by-step and verify each intermediate result.
`)
}
