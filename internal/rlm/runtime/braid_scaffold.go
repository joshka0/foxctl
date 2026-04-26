package runtime

import "strings"

const (
	BraidScaffoldClassFiniteStateTransition = "finite_state_transition"
	BraidScaffoldClassGraphSearch           = "graph_search"
	BraidScaffoldClassNumericDP             = "numeric_dp"
	BraidScaffoldClassSequenceSimulation    = "sequence_simulation"
	BraidScaffoldClassConstraintSolver      = "constraint_solver"
	BraidScaffoldIDStackRelocationV1        = "stack_relocation_v1"
	BraidScaffoldIDResourcePathMinInitialV1 = "resource_path_min_initial_v1"
	BraidScaffoldIDExplicitShortestPathV1   = "explicit_shortest_path_v1"
	BraidScaffoldIDRecurrenceTableV1        = "recurrence_table_v1"
	BraidScaffoldIDJSONPatchSequenceV1      = "json_patch_v1"
	BraidScaffoldIDFiniteDomainV1           = "finite_domain_v1"
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
	default:
		return braidRuntimeScaffold{}, false
	}
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
