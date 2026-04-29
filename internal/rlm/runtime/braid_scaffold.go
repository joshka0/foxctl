package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	BraidScaffoldClassFiniteStateTransition = "finite_state_transition"
	BraidScaffoldClassGraphSearch           = "graph_search"
	BraidScaffoldClassNumericDP             = "numeric_dp"
	BraidScaffoldClassSequenceSimulation    = "sequence_simulation"
	BraidScaffoldClassConstraintSolver      = "constraint_solver"
	BraidScaffoldClassSymbolicTrace         = "symbolic_trace"
	BraidScaffoldClassCandidateVerify       = "candidate_verify"
	BraidScaffoldClassStateTransition       = "state_transition"
	BraidScaffoldClassExplicitDAG           = "explicit_dag"
	BraidScaffoldIDStackRelocationV1        = "stack_relocation_v1"
	BraidScaffoldIDResourcePathMinInitialV1 = "resource_path_min_initial_v1"
	BraidScaffoldIDExplicitShortestPathV1   = "explicit_shortest_path_v1"
	BraidScaffoldIDRecurrenceTableV1        = "recurrence_table_v1"
	BraidScaffoldIDJSONPatchSequenceV1      = "json_patch_v1"
	BraidScaffoldIDFiniteDomainV1           = "finite_domain_v1"
	BraidScaffoldIDTypeInferenceV1          = "type_inference_v1"
	BraidScaffoldIDPropertyCheckV1          = "property_check_v1"
	BraidScaffoldIDStateReplayV1            = "state_replay_v1"
	BraidScaffoldIDSearchBacktrackV1        = "search_backtrack_v1"
	BraidScaffoldIDGenericV1                = "generic_v1"
)

type braidRuntimeScaffold struct {
	Class          string
	ID             string
	Language       string
	PresetName     string
	PresetSource   string
	PresetInput    map[string]any
	MaxSourceLines int
	MaxSourceChars int
	Verifier       HelperAnswerVerifier
}

func resolveBraidRuntimeScaffold(node BraidNode, handoff BraidNodeHandoff, input map[string]any) (braidRuntimeScaffold, bool) {
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
			Language:       HelperLanguageGo,
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
				Language:       HelperLanguageGo,
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
				Language:       HelperLanguageGo,
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
			Language:       HelperLanguageGo,
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
			Language:       HelperLanguageGo,
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
			Language:       HelperLanguageGo,
			PresetName:     BraidScaffoldClassConstraintSolver + "/" + BraidScaffoldIDFiniteDomainV1,
			PresetSource:   finiteDomainConstraintPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 340,
			MaxSourceChars: 18000,
			Verifier:       finiteDomainAnswerVerifier,
		}, true
	case BraidScaffoldClassSymbolicTrace:
		if handoff.ScaffoldID != BraidScaffoldIDTypeInferenceV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeSymbolicTrace(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassSymbolicTrace,
			ID:             BraidScaffoldIDTypeInferenceV1,
			Language:       HelperLanguagePython,
			PresetName:     BraidScaffoldClassSymbolicTrace + "/" + BraidScaffoldIDTypeInferenceV1,
			PresetSource:   typeInferencePresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 380,
			MaxSourceChars: 18000,
			Verifier:       typeInferenceAnswerVerifier,
		}, true
	case BraidScaffoldClassCandidateVerify:
		if handoff.ScaffoldID != BraidScaffoldIDPropertyCheckV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeCandidateVerify(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassCandidateVerify,
			ID:             BraidScaffoldIDPropertyCheckV1,
			Language:       HelperLanguagePython,
			PresetName:     BraidScaffoldClassCandidateVerify + "/" + BraidScaffoldIDPropertyCheckV1,
			PresetSource:   candidateVerifyPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 300,
			MaxSourceChars: 15000,
			Verifier:       candidateVerifyAnswerVerifier,
		}, true
	case BraidScaffoldClassStateTransition:
		if handoff.ScaffoldID != BraidScaffoldIDStateReplayV1 {
			return braidRuntimeScaffold{}, false
		}
		if !braidHelperInputLooksLikeStateReplay(input) {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassStateTransition,
			ID:             BraidScaffoldIDStateReplayV1,
			Language:       HelperLanguagePython,
			PresetName:     BraidScaffoldClassStateTransition + "/" + BraidScaffoldIDStateReplayV1,
			PresetSource:   stateReplayPresetSource(),
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 200,
			MaxSourceChars: 10000,
			Verifier:       stateReplayAnswerVerifier,
		}, true
	case BraidScaffoldClassExplicitDAG:
		if handoff.ScaffoldID != BraidScaffoldIDSearchBacktrackV1 {
			return braidRuntimeScaffold{}, false
		}
		return braidRuntimeScaffold{
			Class:          BraidScaffoldClassExplicitDAG,
			ID:             BraidScaffoldIDSearchBacktrackV1,
			Language:       HelperLanguagePython,
			PresetName:     BraidScaffoldClassExplicitDAG + "/" + BraidScaffoldIDSearchBacktrackV1,
			PresetInput:    cloneMapAny(input),
			MaxSourceLines: 250,
			MaxSourceChars: 12000,
			Verifier:       searchBacktrackAnswerVerifier,
		}, true
	default:
		return braidRuntimeScaffold{}, false
	}
}

func braidHelperInputLooksLikeExplicitDAGPreset(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if _, ok := input["nodes"]; ok {
		return true
	}
	if _, ok := input["dependencies"]; ok {
		return true
	}
	if _, ok := input["problems"]; ok {
		return true
	}
	return false
}

func braidHelperInputLooksLikeStateReplay(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	for _, key := range []string{"move_sequence", "actions", "transitions"} {
		if valueLooksLikeUCIMoveSequence(input[key]) {
			return true
		}
	}
	return false
}

func valueLooksLikeUCIMoveSequence(value any) bool {
	switch typed := value.(type) {
	case string:
		return stringLooksLikeUCIMoveSequence(typed)
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok && looksLikeUCIMoveToken(strings.TrimSpace(text)) {
				return true
			}
		}
	}
	return false
}

func stringLooksLikeUCIMoveSequence(value string) bool {
	for _, token := range strings.Fields(strings.TrimSpace(value)) {
		token = strings.Trim(token, "[](),.;:'\"")
		if looksLikeUCIMoveToken(token) {
			return true
		}
	}
	return false
}

func looksLikeUCIMoveToken(token string) bool {
	if len(token) != 4 && len(token) != 5 {
		return false
	}
	if token[0] < 'a' || token[0] > 'h' || token[2] < 'a' || token[2] > 'h' {
		return false
	}
	if token[1] < '1' || token[1] > '8' || token[3] < '1' || token[3] > '8' {
		return false
	}
	if len(token) == 5 {
		switch token[4] {
		case 'q', 'r', 'b', 'n':
			return true
		default:
			return false
		}
	}
	return true
}

func applyBraidRuntimeScaffoldToHelperConfig(cfg HelperFactoryConfig, scaffold braidRuntimeScaffold) HelperFactoryConfig {
	cfg.PresetName = scaffold.PresetName
	cfg.PresetSource = scaffold.PresetSource
	cfg.PresetInput = cloneMapAny(scaffold.PresetInput)
	cfg.AnswerVerifier = scaffold.Verifier
	if strings.TrimSpace(scaffold.Language) != "" {
		cfg.Language = strings.TrimSpace(scaffold.Language)
	}
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

func braidHelperInputLooksLikeSymbolicTrace(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if class, ok := input["scaffold_class"].(string); ok && strings.TrimSpace(class) != "" && strings.TrimSpace(class) != BraidScaffoldClassSymbolicTrace {
		return false
	}
	if id, ok := input["scaffold_id"].(string); ok && strings.TrimSpace(id) != "" && strings.TrimSpace(id) != BraidScaffoldIDTypeInferenceV1 {
		return false
	}
	_, hasProgram := input["program"]
	_, hasQueries := input["queries"]
	return hasProgram || hasQueries
}

func typeInferencePresetSource() string {
	return `def Solve(input):
    """Type inference via Algorithm W.

    Input fields:
      program: str  -- the let-binding program text
      queries: list -- query descriptors [{kind, target}, ...]
      trace_kind: str -- e.g. "HM-TRACE"

    Output: solution = {"q1": "...", "q2": ..., ...}

    Algorithm:
    1. Parse the program into a list of let-bindings.
    2. Maintain a type environment (var -> type scheme) and a substitution.
    3. For each let-binding, infer the type using Algorithm W:
       a. Look up free variables in the environment.
       b. For lambda abstractions, introduce fresh type variables.
       c. For applications, unify the function and argument types.
       d. For let-bindings, generalize the bound type over free variables.
       e. For pairs, unify both component types.
       f. For projections (fst/snd), unify with a product type.
       g. For conditionals, unify the condition with Bool and both branches.
       h. For succ/isZero, unify with Nat.
    4. Maintain a global binding trace recording each binding step.
    5. Answer the queries by looking up type schemes in the final environment.

    Type syntax:
      Bool, Nat, (t1 -> t2), (t1 x t2), a_k (type variables)

    Unification:
      - occurs check before binding a variable
      - apply substitution compositionally
    """
    program = input.get("program", "")
    queries = input.get("queries", [])
    trace_kind = input.get("trace_kind", "")

    # Parse program
    lines = []
    for line in program.split("\\n"):
        line = line.strip()
        if line.startswith("let "):
            lines.append(line)
    if not lines:
        return {"ok": False, "error": "no let-bindings found in program"}

    # Fresh type variable counter
    var_counter = [0]
    def fresh():
        var_counter[0] += 1
        return ("var", var_counter[0])

    # Substitution: dict mapping var index -> type
    subst = {}

    def apply_subst(t):
        if t[0] == "var":
            idx = t[1]
            while idx in subst:
                t = subst[idx]
                if t[0] != "var":
                    break
                idx = t[1]
            return t
        if t[0] == "arrow":
            return ("arrow", apply_subst(t[1]), apply_subst(t[2]))
        if t[0] == "prod":
            return ("prod", apply_subst(t[1]), apply_subst(t[2]))
        return t

    def occurs(idx, t):
        t = apply_subst(t)
        if t[0] == "var":
            return t[1] == idx
        if t[0] == "arrow":
            return occurs(idx, t[1]) or occurs(idx, t[2])
        if t[0] == "prod":
            return occurs(idx, t[1]) or occurs(idx, t[2])
        return False

    def unify(t1, t2):
        t1, t2 = apply_subst(t1), apply_subst(t2)
        if t1 == t2:
            return True
        if t1[0] == "var":
            if occurs(t1[1], t2):
                return False
            subst[t1[1]] = t2
            return True
        if t2[0] == "var":
            if occurs(t2[1], t1):
                return False
            subst[t2[1]] = t1
            return True
        if t1[0] == "arrow" and t2[0] == "arrow":
            return unify(t1[1], t2[1]) and unify(t1[2], t2[2])
        if t1[0] == "prod" and t2[0] == "prod":
            return unify(t1[1], t2[1]) and unify(t1[2], t2[2])
        return False

    # Type environment: name -> type scheme (vars, type)
    env = {}
    binding_trace = []

    # Infer type for a parsed term
    def infer(term_text):
        term_text = term_text.strip()
        # Boolean literal
        if term_text == "true" or term_text == "false":
            return ("bool",)
        # Zero
        if term_text == "0":
            return ("nat",)
        # Variable
        if not any(c in term_text for c in " \\(,)"):
            if term_text in env:
                scheme = env[term_text]
                vs, t = scheme
                mapping = {}
                for v in vs:
                    nv = fresh()
                    mapping[v] = nv
                def inst(t, m):
                    if t[0] == "var" and t[1] in m:
                        return m[t[1]]
                    if t[0] == "arrow":
                        return ("arrow", inst(t[1], m), inst(t[2], m))
                    if t[0] == "prod":
                        return ("prod", inst(t[1], m), inst(t[2], m))
                    return t
                return inst(t, mapping)
            return fresh()
        # Add more pattern matching as needed for full Algorithm W
        return fresh()

    # Process each let-binding
    for line in lines:
        rest = line[4:]  # skip "let "
        eq_pos = rest.find(" = ")
        if eq_pos < 0:
            continue
        name = rest[:eq_pos].strip()
        body = rest[eq_pos+3:]
        # Remove trailing " in"
        if body.endswith(" in"):
            body = body[:-3]
        body_type = infer(body)
        # Generalize
        free_in_env = set()
        for v in env.values():
            pass  # simplified: generalize over all vars not in env
        env[name] = ([], body_type)
        binding_trace.append({"name": name, "type": body_type})

    # Answer queries (simplified placeholder)
    answers = {}
    for i, q in enumerate(queries):
        qk = f"q{i+1}"
        kind = q.get("kind", "")
        target = q.get("target", "")
        if kind == "type_scheme" and target in env:
            answers[qk] = str(env[target][1])
        else:
            answers[qk] = "unknown"

    return {"ok": True, "solution": answers}
`
}

func typeInferenceAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailureKind: "type_inference"}
	if len(input) == 0 {
		return base, false
	}
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		base.FirstFailure = "empty answer"
		return base, true
	}
	// Check for solution= line
	if !strings.Contains(trimmed, "solution") {
		base.FirstFailure = "answer does not contain solution"
		return base, true
	}
	// Parse the solution JSON if present
	solStart := strings.Index(trimmed, "{")
	solEnd := strings.LastIndex(trimmed, "}")
	if solStart < 0 || solEnd < 0 || solEnd <= solStart {
		base.FirstFailure = "answer does not contain JSON solution object"
		return base, true
	}
	var sol map[string]any
	if err := json.Unmarshal([]byte(trimmed[solStart:solEnd+1]), &sol); err != nil {
		base.FirstFailure = "solution JSON parse error: " + err.Error()
		return base, true
	}
	// Check that expected query keys are present
	if queries, ok := input["queries"].([]any); ok && len(queries) > 0 {
		for i := range queries {
			key := fmt.Sprintf("q%d", i+1)
			if _, found := sol[key]; !found {
				base.FirstFailure = fmt.Sprintf("missing query key %s", key)
				return base, true
			}
		}
	}
	// If we have ground truth answers, check them
	if expected, ok := input["answer"].(map[string]any); ok && len(expected) > 0 {
		allMatch := true
		for k, ev := range expected {
			got, found := sol[k]
			if !found {
				allMatch = false
				base.FirstFailure = fmt.Sprintf("missing expected key %s", k)
				break
			}
			gotStr := strings.TrimSpace(strings.ReplaceAll(
				strings.ReplaceAll(fmt.Sprintf("%v", got), " ", ""), "\\t", ""))
			expStr := strings.TrimSpace(strings.ReplaceAll(
				strings.ReplaceAll(fmt.Sprintf("%v", ev), " ", ""), "\\t", ""))
			if gotStr != expStr {
				allMatch = false
				base.FirstFailure = fmt.Sprintf("key %s: got %v, want %v", k, got, ev)
				break
			}
		}
		if allMatch {
			base.Pass = true
			return base, true
		}
		return base, true
	}
	// No ground truth: pass if all queries answered
	base.Pass = true
	return base, true
}

func braidHelperInputLooksLikeCandidateVerify(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	if class, ok := input["scaffold_class"].(string); ok && strings.TrimSpace(class) != "" && strings.TrimSpace(class) != BraidScaffoldClassCandidateVerify {
		return false
	}
	if id, ok := input["scaffold_id"].(string); ok && strings.TrimSpace(id) != "" && strings.TrimSpace(id) != BraidScaffoldIDPropertyCheckV1 {
		return false
	}
	_, hasCandidates := input["candidates"]
	_, hasPredicates := input["predicates"]
	return hasCandidates || hasPredicates
}

func candidateVerifyPresetSource() string {
	return `def Solve(input):
    """Candidate verification and property checking.

    Input fields:
      candidates: list -- items to evaluate (strings, dicts, etc.)
      predicates: list -- named property checks [{name, check_type, params}]
      selection_rule: str -- "best", "all_matching", "nth", "count_matching"
      output_schema: dict -- expected answer shape

    Output: solution = <answer matching output_schema>

    Algorithm:
    1. Parse candidates list.
    2. For each candidate, evaluate every predicate.
       IMPORTANT: You MUST implement actual predicate logic for each check_type.
       Do NOT default all predicates to True. If you cannot verify a predicate,
       mark it as False with a reason.
    3. Apply selection_rule to filtered results.
    4. Return answer in output_schema format.

    If a required library is unavailable, return:
      ok: false
      error: "missing_library: <name>"
      repair_hint: "install <name> or provide alternative"
    """
    import sys
    candidates = input.get("candidates", [])
    predicates = input.get("predicates", [])
    selection_rule = input.get("selection_rule", "best")
    output_schema = input.get("output_schema", {})

    if not candidates:
        return {"ok": False, "error": "no candidates provided"}

    # Evaluate each predicate against each candidate.
    # YOU MUST replace this with actual domain-specific predicate evaluation.
    # The scaffold cannot evaluate predicates generically -- each check_type
    # requires domain knowledge that the LLM must provide.
    #
    # Pattern: for each predicate, implement the check based on check_type.
    # Common check_types:
    #   "equals"     -> candidate == expected_value
    #   "contains"   -> expected_value in str(candidate)
    #   "matches"    -> regex or pattern match
    #   "satisfies"  -> custom condition (YOU implement)
    #   "property"   -> domain property check (YOU implement)
    #
    # If you cannot determine whether a predicate is satisfied, set it to
    # False (never default to True without evidence).

    results = []
    for i, cand in enumerate(candidates):
        cand_result = {"index": i, "candidate": cand, "matches": {}, "reasons": {}}
        all_match = True
        for pred in predicates:
            name = pred.get("name", f"pred_{i}")
            check_type = pred.get("check_type", "equals")
            params = pred.get("params", {})
            expected_val = pred.get("expected", params.get("expected"))

            matched = False
            reason = ""

            if check_type == "equals":
                matched = (cand == expected_val)
                reason = f"equals check: {cand!r} == {expected_val!r} -> {matched}"
            elif check_type == "contains":
                matched = str(expected_val) in str(cand)
                reason = f"contains check: {expected_val!r} in {cand!r} -> {matched}"
            elif check_type == "not_contains":
                matched = str(expected_val) not in str(cand)
                reason = f"not_contains: {expected_val!r} not in {cand!r} -> {matched}"
            elif check_type == "greater_than":
                try:
                    matched = float(cand) > float(expected_val)
                except (ValueError, TypeError):
                    matched = False
                    reason = f"cannot compare {cand!r} > {expected_val!r}"
            elif check_type == "less_than":
                try:
                    matched = float(cand) < float(expected_val)
                except (ValueError, TypeError):
                    matched = False
                    reason = f"cannot compare {cand!r} < {expected_val!r}"
            else:
                # Unknown/custom predicate type: LLM MUST implement.
                # Default to False (fail-closed) rather than True.
                matched = False
                reason = f"unimplemented check_type: {check_type} -- implement domain logic"

            if not reason:
                reason = f"{check_type}: {matched}"
            cand_result["matches"][name] = matched
            cand_result["reasons"][name] = reason
            if not matched:
                all_match = False

        cand_result["all_match"] = all_match
        results.append(cand_result)

    # Apply selection rule
    if selection_rule == "count_matching":
        matching = [r for r in results if r["all_match"]]
        return {"ok": True, "solution": len(matching)}
    elif selection_rule == "all_matching":
        matching = [r for r in results if r["all_match"]]
        return {"ok": True, "solution": [r["candidate"] for r in matching]}
    elif selection_rule == "nth":
        n = input.get("n", 0)
        if 0 <= n < len(results):
            return {"ok": True, "solution": results[n]["candidate"]}
        return {"ok": False, "error": f"n={n} out of range"}
    else:
        # "best" or default: return first matching
        for r in results:
            if r["all_match"]:
                return {"ok": True, "solution": r["candidate"]}
        return {"ok": False, "error": "no candidate matched all predicates", "results": results}
`
}

func candidateVerifyAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailureKind: "candidate_verify"}
	if len(input) == 0 {
		return base, false
	}
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		base.FirstFailure = "empty answer"
		return base, true
	}
	// Check for missing library early return
	if strings.Contains(trimmed, "missing_library") || strings.Contains(trimmed, "ok: false") {
		base.FirstFailure = "helper reported missing library or failure"
		// Extract the library name for structured reporting
		if idx := strings.Index(trimmed, "missing_library:"); idx >= 0 {
			libName := strings.TrimSpace(trimmed[idx+len("missing_library:"):])
			if end := strings.Index(libName, "\n"); end >= 0 {
				libName = libName[:end]
			}
			if end := strings.Index(libName, "\""); end >= 0 {
				libName = libName[:end]
			}
			base.Observed = "missing_library: " + libName
		}
		return base, true
	}
	// Check for solution marker
	if !strings.Contains(strings.ToLower(trimmed), "solution") {
		base.FirstFailure = "answer does not contain solution"
		return base, true
	}
	// If ground truth answer is provided, check it
	if expected, ok := input["answer"]; ok {
		expectedStr := strings.TrimSpace(fmt.Sprintf("%v", expected))
		// Try to find solution value in answer
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "solution =") {
				got := strings.TrimSpace(strings.TrimPrefix(line, "solution ="))
				if got == expectedStr {
					base.Pass = true
					return base, true
				}
				base.FirstFailure = fmt.Sprintf("got %q, want %q", got, expectedStr)
				return base, true
			}
		}
		// Check JSON solution
		solStart := strings.Index(trimmed, "{")
		solEnd := strings.LastIndex(trimmed, "}")
		if solStart >= 0 && solEnd > solStart {
			var sol map[string]any
			if err := json.Unmarshal([]byte(trimmed[solStart:solEnd+1]), &sol); err == nil {
				if solVal, found := sol["solution"]; found {
					gotStr := strings.TrimSpace(fmt.Sprintf("%v", solVal))
					if gotStr == expectedStr {
						base.Pass = true
						return base, true
					}
					base.FirstFailure = fmt.Sprintf("solution=%v, want %v", solVal, expected)
					return base, true
				}
			}
		}
	}
	// No ground truth: check that the solution looks like a real answer, not
	// a placeholder or instruction text.
	solutionVal := extractSolutionValue(trimmed)
	if solutionVal == "" {
		base.FirstFailure = "answer does not contain a concrete solution value"
		return base, true
	}
	// Reject placeholder patterns like "Consider the..." or "selected mol as..."
	lower := strings.ToLower(solutionVal)
	if isSchemaPlaceholderAnswer(lower) {
		base.FirstFailure = fmt.Sprintf("solution looks like a placeholder: %q", solutionVal)
		return base, true
	}
	base.Pass = true
	return base, true
}

func isSchemaPlaceholderAnswer(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return true
	}
	lower = strings.Trim(lower, `"'`)
	if strings.HasPrefix(lower, "{") {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			for _, key := range []string{"value", "answer", "solution"} {
				if isSchemaPlaceholderAnswer(fmt.Sprintf("%v", decoded[key])) {
					return true
				}
			}
		}
	}
	if strings.HasPrefix(lower, "[") {
		var decoded []any
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			for _, item := range decoded {
				if isSchemaPlaceholderAnswer(fmt.Sprintf("%v", item)) {
					return true
				}
			}
		}
	}
	placeholders := []string{
		"<answer>",
		"answer",
		"answers",
		"candidate",
		"candidates",
		"candidate answer",
		"candidate answers",
		"candidate values",
		"numerical answer",
		"numerical answers",
		"output value",
		"output values",
		"verification predicates",
		"predicate checks",
		"constraint checks",
		"problem constraint checks",
		"consider ",
		"selected ",
		"insert ",
		"replace ",
		"fill in",
		"todo",
		"tbd",
		"placeholder",
	}
	for _, ph := range placeholders {
		if lower == ph || strings.HasPrefix(lower, ph) {
			return true
		}
	}
	return false
}

// --- State replay scaffold (generic state_transition/state_replay_v1) ---

// extractSolutionValue extracts the concrete solution value from an answer
// string, looking for "solution = <value>" or JSON solution fields.
func extractSolutionValue(answer string) string {
	// Try "solution = <value>" pattern.
	lower := strings.ToLower(answer)
	idx := strings.Index(lower, "solution")
	if idx < 0 {
		return ""
	}
	after := answer[idx:]
	// Skip "solution" keyword and whitespace/equals.
	after = strings.TrimLeft(after[len("solution"):], " \t=")
	// Take until newline or end.
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		after = after[:nl]
	}
	return strings.TrimSpace(after)
}

func stateReplayPresetSource() string {
	return `def Solve(input):
    """Apply a sequence of UCI chess moves from the starting position and
    return the resulting FEN string.

    Input fields:
      move_sequence: str -- space-separated UCI moves (e.g. "e2e4 e7e5 g1f3")
      OR the prompt may contain the move sequence directly.

    Output: solution = "<FEN string>"
    FEN format: piece_placement active_color castling en_passant halfmove fullmove

    Algorithm:
    1. Parse the UCI move sequence from input.
    2. Start from the standard chess starting position.
    3. Apply each move, updating the board state.
    4. Return the final position as a FEN string.

    IMPORTANT: Implement the move application correctly:
    - Normal moves: piece moves from source square to target square
    - Captures: replace target square piece
    - Pawn promotion: e.g., e7e8q means pawn promotes to queen
    - Castling: e1g1 (kingside), e1c1 (queenside) for white
    - En passant: if pawn moves two squares, set en passant square
    - Update castling rights when king or rook moves
    """
    # Parse moves from input
    moves_str = input.get("move_sequence", "")
    if not moves_str:
        prompt = input.get("prompt", "")
        # Extract moves from prompt text
        import re
        # Look for a long sequence of lowercase move patterns
        match = re.findall(r'\b([a-h][1-8][a-h][1-8][qrbn]?)\b', prompt)
        if match:
            moves_str = " ".join(match)

    moves = moves_str.split() if moves_str else []

    # Starting position board (8=ranks, 8=files)
    # Board representation: list of 8 strings (ranks 8 to 1)
    board = [
        ['r','n','b','q','k','b','n','r'],  # rank 8
        ['p','p','p','p','p','p','p','p'],  # rank 7
        ['.','.','.','.','.','.','.','.'],  # rank 6
        ['.','.','.','.','.','.','.','.'],  # rank 5
        ['.','.','.','.','.','.','.','.'],  # rank 4
        ['.','.','.','.','.','.','.','.'],  # rank 3
        ['P','P','P','P','P','P','P','P'],  # rank 2
        ['R','N','B','Q','K','B','N','R'],  # rank 1
    ]

    active = 'w'
    castling = 'KQkq'
    en_passant = '-'
    halfmove = 0
    fullmove = 1

    def sq2rc(sq):
        """Convert algebraic square to (rank_idx, file_idx)."""
        f = ord(sq[0]) - ord('a')
        r = 8 - int(sq[1])
        return r, f

    for move in moves:
        if len(move) < 4:
            continue
        fr, ff = sq2rc(move[0:2])
        tr, tf = sq2rc(move[2:4])
        promo = move[4] if len(move) > 4 else None

        piece = board[fr][ff]
        captured = board[tr][tf]

        # Move piece
        board[tr][tf] = piece
        board[fr][ff] = '.'

        # Pawn promotion
        if promo and piece in ('P', 'p'):
            if piece == 'P':
                board[tr][tf] = promo.upper()
            else:
                board[tr][tf] = promo.lower()

        # En passant capture
        if piece in ('P', 'p') and tf != ff and captured == '.':
            board[fr][tf] = '.'

        # Update castling rights
        if piece == 'K':
            castling = castling.replace('K', '').replace('Q', '')
        elif piece == 'k':
            castling = castling.replace('k', '').replace('q', '')
        if (fr, ff) == (7, 0) or (tr, tf) == (7, 0):
            castling = castling.replace('Q', '')
        if (fr, ff) == (7, 7) or (tr, tf) == (7, 7):
            castling = castling.replace('K', '')
        if (fr, ff) == (0, 0) or (tr, tf) == (0, 0):
            castling = castling.replace('q', '')
        if (fr, ff) == (0, 7) or (tr, tf) == (0, 7):
            castling = castling.replace('k', '')

        # En passant square
        if piece in ('P', 'p') and abs(fr - tr) == 2:
            en_passant = move[2] + str((int(move[1]) + int(move[3])) // 2)
        else:
            en_passant = '-'

        # Halfmove clock
        if piece in ('P', 'p') or captured != '.':
            halfmove = 0
        else:
            halfmove += 1

        # Fullmove number
        if active == 'b':
            fullmove += 1

        active = 'b' if active == 'w' else 'w'

    if not castling:
        castling = '-'

    # Build FEN
    ranks = []
    for rank in board:
        fen_rank = ''
        empty = 0
        for sq in rank:
            if sq == '.':
                empty += 1
            else:
                if empty > 0:
                    fen_rank += str(empty)
                    empty = 0
                fen_rank += sq
        if empty > 0:
            fen_rank += str(empty)
        ranks.append(fen_rank)

    fen = ' '.join(['/'.join(ranks), active, castling, en_passant, str(halfmove), str(fullmove)])
    return {"ok": True, "solution": fen}
`
}

func stateReplayAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailureKind: "state_replay"}
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		base.FirstFailure = "empty answer"
		return base, true
	}
	// Check for FEN-like pattern (at least 8 slashes for 8 ranks)
	if strings.Count(trimmed, "/") < 7 {
		// Try to extract FEN from answer text
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if strings.Count(line, "/") >= 7 {
				trimmed = line
				break
			}
		}
	}
	// Check ground truth if available
	if expected, ok := input["answer"]; ok {
		expectedStr := strings.TrimSpace(fmt.Sprintf("%v", expected))
		// Normalize FEN for comparison (collapse whitespace)
		normalized := strings.Join(strings.Fields(trimmed), " ")
		expectedNorm := strings.Join(strings.Fields(expectedStr), " ")
		if strings.HasPrefix(normalized, expectedNorm) || strings.Contains(normalized, expectedNorm) {
			base.Pass = true
			return base, true
		}
		base.FirstFailure = fmt.Sprintf("FEN mismatch: got %q, want %q", safeTelemetryExcerpt(normalized, 100), expectedStr)
		return base, true
	}
	// No ground truth: pass if FEN has valid structure
	if strings.Count(trimmed, "/") >= 7 {
		base.Pass = true
		return base, true
	}
	base.FirstFailure = "answer does not contain a valid FEN"
	return base, true
}

// --- Math backtracking chain explicit DAG scaffold ---

func searchBacktrackAnswerVerifier(answer string, input map[string]any) (HelperVerifierDiagnostic, bool) {
	base := HelperVerifierDiagnostic{Pass: false, FailureKind: "search_backtrack"}
	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		base.FirstFailure = "empty answer"
		return base, true
	}
	if strings.Contains(strings.ToUpper(trimmed), "UNSOLVED") || strings.Contains(strings.ToLower(trimmed), "null") {
		base.FirstFailure = "answer contains unresolved values"
		base.RepairHint = "return only fully solved node values or ok:false with first_failure"
		return base, true
	}
	if !strings.Contains(trimmed, "node_") && !strings.Contains(trimmed, "solution") {
		base.FirstFailure = "answer does not contain structured solution"
		return base, true
	}
	if expected, ok := input["answer"]; ok {
		expectedStr := strings.TrimSpace(fmt.Sprintf("%v", expected))
		if expectedStr != "" && strings.Contains(trimmed, expectedStr) {
			base.Pass = true
			return base, true
		}
		base.FirstFailure = "answer did not contain expected value"
		base.RepairHint = "substitute the candidate into the explicit dependency graph and return the requested output values"
		return base, true
	}
	// A structured explicit-DAG answer is a candidate, not a runtime-verified
	// solution, unless the input provides machine-checkable expected values.
	return HelperVerifierDiagnostic{}, false
}
