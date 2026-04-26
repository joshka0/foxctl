package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	braidGraphVersionV1        = 1
	maxBraidNodeIDChars        = 64
	maxBraidNodeQuestionChars  = 480
	maxBraidNodeExpectedChars  = 240
	maxBraidNodeSummaryChars   = 12000
	minCycleSolveSummaryChars  = 900
	maxBraidHandoffDepChars    = 1800
	maxBraidHandoffRepairChars = 1800
)

const (
	BraidNodeHelperPolicyAuto      = "auto"
	BraidNodeHelperPolicyPreferred = "preferred"
	BraidNodeHelperPolicyRequired  = "required"
	BraidNodeHelperPolicyNever     = "never"
)

// REPLPhaseOutputKindBraidGraph marks phase outputs that must decode to a
// bounded JSON reasoning graph.
const REPLPhaseOutputKindBraidGraph = "braid_graph"

// REPLPhaseOutputKindREPLCode marks phase outputs that are raw scratch code.
// The runtime executes the assistant text with the phase's REPL tool instead of
// asking the provider to produce a tool call.
const REPLPhaseOutputKindREPLCode = "repl_code"

// REPLPhaseOutputKindCyclePacket marks a compact JSON packet that condenses a
// cycle_solve node before scratch code generation.
const REPLPhaseOutputKindCyclePacket = "cycle_packet"

// REPLPhaseOutputKindCycleWitness marks a bounded-search witness that the
// runtime checks deterministically before emitting cycle_json.
const REPLPhaseOutputKindCycleWitness = "cycle_witness"

const BraidGraphPolicyLongCoTController = "longcot_controller"

// BraidGraph is the runtime contract emitted by the parent model.
type BraidGraph struct {
	Version   int         `json:"version"`
	Nodes     []BraidNode `json:"nodes"`
	FinalNode string      `json:"final_node"`
}

// BraidNode is one reasoning node in a bounded graph.
type BraidNode struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	Question        string   `json:"question"`
	DependsOn       []string `json:"depends_on,omitempty"`
	ExpectedOutput  string   `json:"expected_output,omitempty"`
	MaxSummaryChars int      `json:"max_summary_chars,omitempty"`
	HelperPolicy    string   `json:"helper_policy,omitempty"`
}

// BraidNodeHandoff is the bounded runtime packet passed from a parent graph
// node to a child. Child prompts are rendered from this packet so context caps
// live at the handoff boundary instead of inside the child model.
type BraidNodeHandoff struct {
	Node                BraidNode          `json:"node"`
	TaskType            string             `json:"task_type,omitempty"`
	ScaffoldClass       string             `json:"scaffold_class,omitempty"`
	ScaffoldID          string             `json:"scaffold_id,omitempty"`
	OfficialRootTask    string             `json:"official_root_task,omitempty"`
	Facts               map[string]any     `json:"facts,omitempty"`
	Rules               []string           `json:"rules,omitempty"`
	AnswerFormat        string             `json:"answer_format,omitempty"`
	Checks              []string           `json:"checks,omitempty"`
	Dependencies        []string           `json:"dependencies,omitempty"`
	DependencySummaries map[string]string  `json:"dependency_summaries,omitempty"`
	RepairFeedback      string             `json:"repair_feedback,omitempty"`
	Budget              BraidHandoffBudget `json:"budget"`
}

type BraidHandoffBudget struct {
	MaxSummaryChars           int `json:"max_summary_chars,omitempty"`
	MaxDependencySummaryChars int `json:"max_dependency_summary_chars,omitempty"`
	MaxRepairFeedbackChars    int `json:"max_repair_feedback_chars,omitempty"`
}

// ParseBraidGraphText accepts only the canonical raw JSON graph payload.
func ParseBraidGraphText(text string) (BraidGraph, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return BraidGraph{}, fmt.Errorf("braid graph: empty output")
	}

	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	decoder.DisallowUnknownFields()
	var graph BraidGraph
	if err := decoder.Decode(&graph); err != nil {
		return BraidGraph{}, fmt.Errorf("braid graph: parse JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return BraidGraph{}, fmt.Errorf("braid graph: parse JSON: %w", err)
	}
	return normalizeBraidGraph(graph), nil
}

// ValidateBraidGraph validates bounded JSON graph contract.
func ValidateBraidGraph(g BraidGraph, maxNodes int) error {
	if g.Version != braidGraphVersionV1 {
		return fmt.Errorf("braid graph: unsupported version %d", g.Version)
	}
	if maxNodes <= 0 {
		maxNodes = 64
	}
	if len(g.Nodes) == 0 {
		return fmt.Errorf("braid graph: nodes is required")
	}
	if len(g.Nodes) > maxNodes {
		return fmt.Errorf("braid graph: node count %d exceeds max %d", len(g.Nodes), maxNodes)
	}

	ids := make(map[string]int, len(g.Nodes))
	deps := make(map[string][]string, len(g.Nodes))
	for idx, node := range g.Nodes {
		if node.ID == "" {
			return fmt.Errorf("braid graph: node %d id is required", idx+1)
		}
		if !validBraidNodeID(node.ID) {
			return fmt.Errorf("braid graph: invalid node id %q", node.ID)
		}
		if prior, exists := ids[node.ID]; exists {
			return fmt.Errorf("braid graph: duplicate node id %q at %d and %d", node.ID, prior+1, idx+1)
		}
		ids[node.ID] = idx

		if !validBraidNodeKind(node.Kind) {
			return fmt.Errorf("braid graph: node %q has invalid kind %q", node.ID, node.Kind)
		}
		if node.Kind != "reduce" && strings.TrimSpace(node.Question) == "" {
			return fmt.Errorf("braid graph: node %q question is required", node.ID)
		}
		if len(node.Question) > maxBraidNodeQuestionChars {
			return fmt.Errorf("braid graph: node %q question length %d exceeds max %d", node.ID, len(node.Question), maxBraidNodeQuestionChars)
		}
		if len(node.ExpectedOutput) > maxBraidNodeExpectedChars {
			return fmt.Errorf("braid graph: node %q expected_output length %d exceeds max %d", node.ID, len(node.ExpectedOutput), maxBraidNodeExpectedChars)
		}
		if forbidden := firstForbiddenBraidNodeRuntimeToken(node.Question); forbidden != "" {
			return fmt.Errorf("braid graph: node %q question contains forbidden runtime token %q", node.ID, forbidden)
		}
		if forbidden := firstForbiddenBraidNodeRuntimeToken(node.ExpectedOutput); forbidden != "" {
			return fmt.Errorf("braid graph: node %q expected_output contains forbidden runtime token %q", node.ID, forbidden)
		}
		if node.MaxSummaryChars < 0 {
			return fmt.Errorf("braid graph: node %q max_summary_chars must be non-negative", node.ID)
		}
		if node.MaxSummaryChars > maxBraidNodeSummaryChars {
			return fmt.Errorf("braid graph: node %q max_summary_chars %d exceeds max %d", node.ID, node.MaxSummaryChars, maxBraidNodeSummaryChars)
		}
		if !validBraidNodeHelperPolicy(node.HelperPolicy) {
			return fmt.Errorf("braid graph: node %q has invalid helper_policy %q", node.ID, node.HelperPolicy)
		}
		if node.HelperPolicy == BraidNodeHelperPolicyRequired && !isBraidSolveKind(node.Kind) && node.Kind != "verify" {
			return fmt.Errorf("braid graph: node %q helper_policy required is only valid for solve-like or verify nodes", node.ID)
		}
		deps[node.ID] = append([]string(nil), node.DependsOn...)
	}

	if g.FinalNode == "" {
		return fmt.Errorf("braid graph: final_node is required")
	}
	if _, ok := ids[g.FinalNode]; !ok {
		return fmt.Errorf("braid graph: final_node %q does not exist", g.FinalNode)
	}

	for _, node := range g.Nodes {
		for _, depID := range node.DependsOn {
			if _, ok := ids[depID]; !ok {
				return fmt.Errorf("braid graph: node %q depends on unknown node %q", node.ID, depID)
			}
		}
	}
	if err := validateBraidGraphAcyclic(deps); err != nil {
		return err
	}
	return nil
}

func ValidateBraidGraphPolicy(g BraidGraph, policy string) error {
	switch strings.TrimSpace(policy) {
	case "":
		return nil
	case BraidGraphPolicyLongCoTController:
		return validateLongCoTControllerBraidGraph(g)
	default:
		return fmt.Errorf("braid graph: unsupported policy %q", policy)
	}
}

func NormalizeBraidGraphForPolicy(g BraidGraph, policy string, maxNodes int) BraidGraph {
	g = normalizeBraidGraph(g)
	for idx := range g.Nodes {
		if g.Nodes[idx].MaxSummaryChars > maxBraidNodeSummaryChars {
			g.Nodes[idx].MaxSummaryChars = maxBraidNodeSummaryChars
		}
		g.Nodes[idx].HelperPolicy = normalizeBraidNodeHelperPolicy(g.Nodes[idx].HelperPolicy)
	}
	if strings.TrimSpace(policy) != BraidGraphPolicyLongCoTController {
		return g
	}
	return normalizeLongCoTControllerBraidGraph(g, maxNodes)
}

func normalizeLongCoTControllerBraidGraph(g BraidGraph, maxNodes int) BraidGraph {
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
		if g.Nodes[idx].Kind == "verify" {
			normalizeLongCoTVerifyNode(&g.Nodes[idx])
		}
		if g.Nodes[idx].Kind == "reduce" && g.Nodes[idx].ID == g.FinalNode {
			normalizeLongCoTFinalReduceDeps(&g.Nodes[idx], byID)
		}
	}
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

func dependsOnBraidNode(node BraidNode, id string) bool {
	for _, dep := range node.DependsOn {
		if dep == id {
			return true
		}
	}
	return false
}

func countBraidSolveNodes(byKind map[string][]BraidNode) int {
	count := 0
	for kind, nodes := range byKind {
		if isBraidSolveKind(kind) {
			count += len(nodes)
		}
	}
	return count
}

func anyDepKind(depIDs []string, byID map[string]BraidNode, kind string) bool {
	for _, depID := range depIDs {
		if byID[depID].Kind == kind {
			return true
		}
	}
	return false
}

func anyDepSolveKind(depIDs []string, byID map[string]BraidNode) bool {
	for _, depID := range depIDs {
		if isBraidSolveKind(byID[depID].Kind) {
			return true
		}
	}
	return false
}

func mentionsOriginalConstraints(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"original", "constraint", "placeholder", "substitut", "fixed-point", "fixed point", "candidate", "initial state", "goal state", "simulate", "simulation", "rules", "legal"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// InitialRunnableBraidNodes returns nodes with no dependencies, preserving
// model-provided node order.
func InitialRunnableBraidNodes(g BraidGraph) []BraidNode {
	out := make([]BraidNode, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		if len(node.DependsOn) == 0 {
			out = append(out, node)
		}
	}
	return out
}

// RenderBraidNodeChildPrompt creates the runtime child prompt for one graph
// node, including the root task context the child needs to solve its branch.
func RenderBraidNodeChildPrompt(node BraidNode, rootPrompt string, dependencySummaries map[string]string) string {
	return RenderBraidNodeChildPromptWithFeedback(node, rootPrompt, dependencySummaries, "")
}

func RenderBraidNodeChildPromptWithFeedback(node BraidNode, rootPrompt string, dependencySummaries map[string]string, repairFeedback string) string {
	return RenderBraidNodeHandoffPrompt(BuildBraidNodeHandoff(node, rootPrompt, dependencySummaries, repairFeedback))
}

func BuildBraidNodeHandoff(node BraidNode, rootPrompt string, dependencySummaries map[string]string, repairFeedback string) BraidNodeHandoff {
	handoff := BraidNodeHandoff{
		Node:         node,
		Dependencies: append([]string(nil), node.DependsOn...),
		Budget: BraidHandoffBudget{
			MaxSummaryChars:           EffectiveBraidNodeSummaryChars(node),
			MaxDependencySummaryChars: maxBraidHandoffDepChars,
			MaxRepairFeedbackChars:    maxBraidHandoffRepairChars,
		},
	}
	if strings.TrimSpace(rootPrompt) != "" && braidNodeShouldIncludeRootTask(node.Kind) {
		handoff.OfficialRootTask = strings.TrimSpace(rootPrompt)
	}
	applyBraidTypedHandoff(&handoff, rootPrompt)
	if len(node.DependsOn) > 0 && len(dependencySummaries) > 0 && braidHandoffShouldIncludeDependencySummaries(handoff) {
		handoff.DependencySummaries = make(map[string]string, len(node.DependsOn))
		for _, depID := range node.DependsOn {
			if summary := strings.TrimSpace(dependencySummaries[depID]); summary != "" {
				handoff.DependencySummaries[depID] = compactBraidHandoffText(summary, maxBraidHandoffDepChars)
			}
		}
	}
	if strings.TrimSpace(repairFeedback) != "" {
		handoff.RepairFeedback = compactBraidHandoffText(repairFeedback, maxBraidHandoffRepairChars)
	}
	return handoff
}

func braidHandoffShouldIncludeDependencySummaries(handoff BraidNodeHandoff) bool {
	if handoff.ScaffoldClass == BraidScaffoldClassFiniteStateTransition && isBraidSolveKind(handoff.Node.Kind) && len(handoff.Facts) > 0 {
		return false
	}
	return true
}

func applyBraidTypedHandoff(handoff *BraidNodeHandoff, rootPrompt string) {
	if handoff == nil || !braidNodeCanUseTypedHandoff(handoff.Node.Kind) {
		return
	}
	instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
	if !ok {
		return
	}
	if braidInstanceLooksLikeConstraintSolver(instance) {
		applyBraidConstraintSolverHandoff(handoff, instance)
		return
	}
	if braidInstanceLooksLikeGraphSearch(instance) {
		applyBraidGraphSearchHandoff(handoff, instance)
		return
	}
	if braidInstanceLooksLikeNumericDP(instance) {
		applyBraidNumericDPHandoff(handoff, instance)
		return
	}
	if braidInstanceLooksLikeSequenceSimulation(instance) {
		applyBraidSequenceSimulationHandoff(handoff, instance)
		return
	}
	if !braidInstanceLooksLikeStateTransition(instance) {
		return
	}
	applyBraidStackTransitionHandoff(handoff, instance)
}

func applyBraidStackTransitionHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassFiniteStateTransition
	handoff.ScaffoldClass = BraidScaffoldClassFiniteStateTransition
	handoff.ScaffoldID = BraidScaffoldIDStackRelocationV1
	handoff.Facts = cloneMapAny(instance)
	handoff.Facts["transition_model"] = "stack_relocation"
	handoff.Rules = []string{
		"Only the top item from any stack can be moved.",
		"A moved item may be placed onto any destination stack.",
		"Each transition is [item, from_stack, to_stack].",
	}
	handoff.AnswerFormat = "solution = [[block, from_stack, to_stack], ...]"
	handoff.Checks = []string{
		"source stack index exists",
		"destination stack index exists",
		"source stack is non-empty",
		"moved block equals source stack top",
		"from_stack and to_stack are different",
		"final state exactly equals goal_state",
	}
	if initial, okInitial := stackStateFromAny(instance["initial_state"]); okInitial {
		if goal, okGoal := stackStateFromAny(instance["goal_state"]); okGoal && stackStatesStrictlyDescending(initial) && stackStatesStrictlyDescending(goal) {
			handoff.Facts["stack_order"] = "strict_descending_bottom_to_top"
			handoff.Rules = append(handoff.Rules, "Destination stack must remain strictly descending from bottom to top; a moved item must be smaller than the current destination top.")
			handoff.Checks = append(handoff.Checks, "destination order invariant remains strict descending after every move")
		}
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func stackStatesStrictlyDescending(stacks [][]int) bool {
	for _, stack := range stacks {
		for idx := 1; idx < len(stack); idx++ {
			if stack[idx-1] <= stack[idx] {
				return false
			}
		}
	}
	return true
}

func applyBraidGraphSearchHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	if braidInstanceLooksLikeExplicitShortestPath(instance) {
		applyBraidExplicitShortestPathHandoff(handoff, instance)
		return
	}
	applyBraidGridResourcePathHandoff(handoff, instance)
}

func applyBraidGridResourcePathHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassGraphSearch
	handoff.ScaffoldClass = BraidScaffoldClassGraphSearch
	handoff.ScaffoldID = BraidScaffoldIDResourcePathMinInitialV1
	handoff.Facts = cloneMapAny(instance)
	handoff.Facts["graph_model"] = "grid_dag"
	handoff.Facts["allowed_moves"] = []any{"right", "down"}
	handoff.Facts["objective"] = "minimize_initial_resource_required_to_keep_resource_positive"
	handoff.Rules = []string{
		"Treat grid cells as graph nodes.",
		"Allowed directed edges move only right or down by one cell.",
		"Entering a node adds that cell's integer resource delta.",
		"Resource must remain strictly positive at every visited node.",
	}
	handoff.AnswerFormat = "solution = <integer>"
	handoff.Checks = []string{
		"grid_layout is rectangular",
		"start and goal coordinates exist",
		"only allowed directed edges are considered",
		"computed integer is the minimum initial resource required",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func applyBraidExplicitShortestPathHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassGraphSearch
	handoff.ScaffoldClass = BraidScaffoldClassGraphSearch
	handoff.ScaffoldID = BraidScaffoldIDExplicitShortestPathV1
	handoff.Facts = cloneMapAny(instance)
	handoff.Facts["graph_model"] = "explicit_directed_graph"
	handoff.Facts["objective"] = "shortest_path_length"
	handoff.Facts["unreachable_value"] = float64(-1)
	handoff.Rules = []string{
		"nodes is the complete finite node set.",
		"edges is a list of directed ordered pairs [from, to].",
		"Each edge has unit cost unless an explicit future scaffold states otherwise.",
		"Compute the fewest directed edges from start_node to goal_node.",
		"Return -1 when goal_node is unreachable from start_node.",
	}
	handoff.AnswerFormat = "solution = <integer>"
	handoff.Checks = []string{
		"nodes contains start_node and goal_node",
		"every edge endpoint exists in nodes",
		"only directed edges from the typed edge list are traversed",
		"computed integer is the exact shortest path length, or -1 if unreachable",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func applyBraidNumericDPHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassNumericDP
	handoff.ScaffoldClass = BraidScaffoldClassNumericDP
	handoff.ScaffoldID = BraidScaffoldIDRecurrenceTableV1
	handoff.Facts = cloneMapAny(instance)
	handoff.Facts["scaffold_class"] = BraidScaffoldClassNumericDP
	handoff.Facts["scaffold_id"] = BraidScaffoldIDRecurrenceTableV1
	handoff.Rules = []string{
		"Treat the table as a bounded integer dynamic program over zero-based index tuples.",
		"Base cases provide exact table values.",
		"Each transition names a predecessor offset relative to the current table index.",
		"Use only predecessor offsets that point to earlier table cells; do not infer prose-only transitions.",
		"Combine candidates with the declared objective: min, max, or count.",
	}
	handoff.AnswerFormat = "solution = <integer>"
	handoff.Checks = []string{
		"dp_dimensions is a positive integer vector",
		"target and base case indexes are inside the table",
		"transitions are acyclic predecessor offsets",
		"computed integer satisfies the declared recurrence and objective",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func applyBraidSequenceSimulationHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassSequenceSimulation
	handoff.ScaffoldClass = BraidScaffoldClassSequenceSimulation
	handoff.ScaffoldID = BraidScaffoldIDJSONPatchSequenceV1
	handoff.Facts = cloneMapAny(instance)
	handoff.Facts["scaffold_class"] = BraidScaffoldClassSequenceSimulation
	handoff.Facts["scaffold_id"] = BraidScaffoldIDJSONPatchSequenceV1
	handoff.Facts["sequence_model"] = BraidScaffoldIDJSONPatchSequenceV1
	handoff.Rules = []string{
		"Parse initial_state as JSON-like state.",
		"Apply events in order to simulate the forward trace.",
		"For json_patch_v1, each event is an object with op and path; supported ops are set, inc, append, and delete.",
		"Check invariants before the first event and after every event.",
		"Check goal_state or goal_conditions only after all events have been applied.",
	}
	handoff.AnswerFormat = "solution = <JSON final_state>"
	handoff.Checks = []string{
		"initial_state is present",
		"events or actions is a JSON array",
		"every event has a supported op and a path array",
		"every event precondition is satisfied by the current state",
		"all invariants hold on every state prefix",
		"final state satisfies goal_state or goal_conditions when provided",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func applyBraidConstraintSolverHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassConstraintSolver
	handoff.ScaffoldClass = BraidScaffoldClassConstraintSolver
	handoff.ScaffoldID = BraidScaffoldIDFiniteDomainV1
	handoff.Facts = cloneMapAny(instance)
	handoff.Facts["version"] = float64(cycleWitnessVersionV1)
	handoff.Facts["checker_kind"] = cycleWitnessCheckerBounded
	handoff.Facts["scaffold_class"] = BraidScaffoldClassConstraintSolver
	handoff.Facts["scaffold_id"] = BraidScaffoldIDFiniteDomainV1
	handoff.Rules = []string{
		"Treat variables as finite integer domains.",
		"Evaluate only the typed constraint expressions declared in constraints.",
		"Use deterministic bounded search or equivalent constraint propagation over the declared domains.",
		"Do not infer extra constraints from prose after the typed instance has been extracted.",
	}
	handoff.AnswerFormat = `solution = {"variable": integer, ...}`
	handoff.Checks = []string{
		"variables has unique finite integer domains",
		"constraints reference only declared variables and known_values",
		"every returned assignment value is inside its declared domain",
		"all typed constraints evaluate to true for the returned assignment",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func braidNodeCanUseTypedHandoff(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "solve", "verify", "reduce":
		return true
	default:
		return false
	}
}

func braidInstanceLooksLikeStateTransition(instance map[string]any) bool {
	if len(instance) == 0 {
		return false
	}
	_, hasInitial := instance["initial_state"]
	_, hasGoal := instance["goal_state"]
	return hasInitial && hasGoal
}

func braidInstanceLooksLikeGraphSearch(instance map[string]any) bool {
	if len(instance) == 0 {
		return false
	}
	_, hasGrid := instance["grid_layout"]
	return hasGrid || braidInstanceLooksLikeExplicitShortestPath(instance)
}

func braidInstanceLooksLikeExplicitShortestPath(instance map[string]any) bool {
	if len(instance) == 0 {
		return false
	}
	_, ok := explicitGraphFromInput(instance)
	return ok
}

func braidInstanceLooksLikeNumericDP(instance map[string]any) bool {
	if len(instance) == 0 {
		return false
	}
	class, ok := instance["scaffold_class"].(string)
	if !ok || strings.TrimSpace(class) != BraidScaffoldClassNumericDP {
		return false
	}
	if id, ok := instance["scaffold_id"].(string); ok && strings.TrimSpace(id) != "" && strings.TrimSpace(id) != BraidScaffoldIDRecurrenceTableV1 {
		return false
	}
	_, ok = numericDPProblemFromInput(instance)
	return ok
}

func braidInstanceLooksLikeSequenceSimulation(instance map[string]any) bool {
	_, ok := sequenceSimulationSpecFromInput(instance)
	return ok
}

func braidInstanceLooksLikeConstraintSolver(instance map[string]any) bool {
	_, ok := finiteDomainWitnessFromInput(instance)
	return ok
}

func RenderBraidNodeHandoffPrompt(handoff BraidNodeHandoff) string {
	node := handoff.Node
	var b strings.Builder
	fmt.Fprintf(&b, "BRAID node %s (%s)\n", node.ID, node.Kind)
	b.WriteString("Leaf-node contract: solve this node directly from the provided task context and dependency summaries. Do not ask for recursive tools, subagents, or more runtime depth.\n")
	b.WriteString("Internal scaffold note: if the official task says not to use tools, code, or a solver, that restriction applies to the public benchmark answer. This internal RLM child may still use its provided scratch/runtime phases to compute or verify its bounded subproblem, then return only the requested compact child summary.\n")
	if strings.TrimSpace(handoff.TaskType) != "" {
		fmt.Fprintf(&b, "Typed task: %s\n", handoff.TaskType)
	}
	if len(handoff.Facts) > 0 {
		b.WriteString("Typed facts:\n")
		b.WriteString(renderBraidHandoffFacts(handoff.Facts))
	}
	if len(handoff.Rules) > 0 {
		b.WriteString("Typed rules:\n")
		for _, rule := range handoff.Rules {
			if strings.TrimSpace(rule) != "" {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(rule))
			}
		}
	}
	if strings.TrimSpace(handoff.AnswerFormat) != "" {
		b.WriteString("Answer format:\n")
		b.WriteString(strings.TrimSpace(handoff.AnswerFormat))
		b.WriteString("\n")
	}
	if len(handoff.Checks) > 0 {
		b.WriteString("Required checks:\n")
		for _, check := range handoff.Checks {
			if strings.TrimSpace(check) != "" {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(check))
			}
		}
	}
	if node.Kind == "extract" {
		b.WriteString("Extract-node contract: return facts only. Do not solve, verify, reduce, declare blocked, or treat circular-looking references as a blocker. Produce a compact constraint packet with fields: requested_outputs, known_values, dependency_edges, placeholders, cycle_cluster, equations_or_checks, candidate_bounds, blockers.\n")
	}
	if node.Kind == "cycle_solve" {
		b.WriteString("Cycle-solve contract: solve one mutually dependent constraint cluster as a bounded mathematical subproblem. Represent unknowns as variables and constraints, then use candidate search, fixed-point iteration, constraint propagation, or direct algebraic substitution. Do not report a runtime dependency cycle as a blocker. Block only when finite candidate bounds cannot be derived or all tested candidates fail, and include the attempted bounds/checks.\n")
		b.WriteString("Context policy: use only this node task, dependency summaries, and repair feedback. The full official root task is intentionally withheld to prevent broad narrative solving; rely on the extract constraint packet.\n")
	}
	if strings.TrimSpace(handoff.OfficialRootTask) != "" {
		b.WriteString("Official root task:\n")
		b.WriteString(strings.TrimSpace(handoff.OfficialRootTask))
		b.WriteString("\n\n")
	}
	if len(handoff.Dependencies) > 0 {
		b.WriteString("Dependencies: ")
		b.WriteString(strings.Join(handoff.Dependencies, ", "))
		b.WriteString("\n")
	}
	if len(handoff.Dependencies) > 0 && len(handoff.DependencySummaries) > 0 {
		b.WriteString("Dependency summaries:\n")
		for _, depID := range handoff.Dependencies {
			if summary := strings.TrimSpace(handoff.DependencySummaries[depID]); summary != "" {
				fmt.Fprintf(&b, "- %s: %s\n", depID, summary)
			}
		}
	}
	if strings.TrimSpace(handoff.RepairFeedback) != "" {
		b.WriteString("Repair feedback:\n")
		b.WriteString(strings.TrimSpace(handoff.RepairFeedback))
		b.WriteString("\n")
	}
	if strings.TrimSpace(node.Question) != "" {
		b.WriteString("Task:\n")
		b.WriteString(strings.TrimSpace(node.Question))
		b.WriteString("\n")
	}
	if strings.TrimSpace(node.ExpectedOutput) != "" {
		b.WriteString("Expected output:\n")
		b.WriteString(strings.TrimSpace(node.ExpectedOutput))
		b.WriteString("\n")
	}
	if maxSummaryChars := handoff.Budget.MaxSummaryChars; maxSummaryChars > 0 {
		fmt.Fprintf(&b, "Return a compact summary under %d characters.\n", maxSummaryChars)
	}
	return strings.TrimSpace(b.String())
}

func RenderBraidHelperHandoffPrompt(handoff BraidNodeHandoff) string {
	if strings.TrimSpace(handoff.TaskType) == "" {
		return RenderBraidNodeHandoffPrompt(handoff)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "BRAID helper handoff for node %s (%s)\n", handoff.Node.ID, handoff.Node.Kind)
	fmt.Fprintf(&b, "Task type: %s\n", handoff.TaskType)
	if len(handoff.Facts) > 0 {
		b.WriteString("Facts available in Solve(input):\n")
		b.WriteString(renderBraidHandoffFacts(handoff.Facts))
	}
	if len(handoff.Rules) > 0 {
		b.WriteString("Rules:\n")
		for _, rule := range handoff.Rules {
			if strings.TrimSpace(rule) != "" {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(rule))
			}
		}
	}
	if strings.TrimSpace(handoff.AnswerFormat) != "" {
		fmt.Fprintf(&b, "Return answer exactly as: %s\n", strings.TrimSpace(handoff.AnswerFormat))
	}
	if handoff.ScaffoldClass == BraidScaffoldClassFiniteStateTransition {
		b.WriteString("State-transition output contract:\n")
		b.WriteString("- Return move lists only as JSON-compatible integer triples: [[block, from_stack, to_stack], ...]. Do not return objects for moves.\n")
		b.WriteString("- Do not include no-op transitions where from_stack equals to_stack.\n")
		b.WriteString("- Your helper must verify legality and exact final state before returning ok:true or a solution = line.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_step, state_before, and repair_hint instead of returning a candidate.\n")
	}
	if handoff.ScaffoldClass == BraidScaffoldClassGraphSearch {
		b.WriteString("Graph-search output contract:\n")
		b.WriteString("- Treat the problem as a graph or dynamic-programming search over typed nodes and state variables.\n")
		b.WriteString("- For grid DAG resource paths, compute the minimum initial resource exactly; do not enumerate prose paths unless needed for verification.\n")
		b.WriteString("- For explicit unweighted graphs, compute the shortest directed path length from start_node to goal_node with BFS, returning -1 if unreachable.\n")
		b.WriteString("- Return one integer answer as `solution = <integer>` only after checking the recurrence or graph search objective.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_node, observed, expected, and repair_hint.\n")
	}
	if handoff.ScaffoldClass == BraidScaffoldClassNumericDP {
		b.WriteString("Numeric-DP output contract:\n")
		b.WriteString("- Treat the problem as a typed recurrence table, not a prose puzzle.\n")
		b.WriteString("- Fill only the finite table described by dp_dimensions, base_cases, target, objective, and transitions.\n")
		b.WriteString("- For min/max objectives, each transition contributes predecessor_value + weight. For count, transitions sum predecessor_value * multiplier.\n")
		b.WriteString("- Return one integer answer as `solution = <integer>` only after checking every referenced predecessor is earlier in table order.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_index, observed, expected, and repair_hint.\n")
	}
	if handoff.ScaffoldClass == BraidScaffoldClassSequenceSimulation {
		b.WriteString("Sequence-simulation output contract:\n")
		b.WriteString("- Treat the input as a typed state trace, not prose generation.\n")
		b.WriteString("- Parse initial_state, events or actions, invariants, and final goal fields before simulating.\n")
		b.WriteString("- Apply every json_patch_v1 event in order and reject unsupported ops, invalid paths, and failed numeric preconditions.\n")
		b.WriteString("- Return `solution = <JSON final_state>` only after all prefix invariants and final goal checks pass.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_step, state_before, observed, expected, and repair_hint.\n")
	}
	if handoff.ScaffoldClass == BraidScaffoldClassConstraintSolver {
		b.WriteString("Constraint-solver output contract:\n")
		b.WriteString("- Treat variables as finite integer domains and constraints as the complete typed checker specification.\n")
		b.WriteString("- Search or propagate deterministically over the declared domains; do not route from prose keywords.\n")
		b.WriteString("- Return `solution = {\"variable\": integer, ...}` only after evaluating every typed constraint.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_constraint, observed, expected, and repair_hint.\n")
	}
	if len(handoff.Checks) > 0 {
		b.WriteString("Verifier checks the helper must satisfy before returning an answer:\n")
		for _, check := range handoff.Checks {
			if strings.TrimSpace(check) != "" {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(check))
			}
		}
	}
	if len(handoff.DependencySummaries) > 0 {
		b.WriteString("Dependency summaries:\n")
		for _, depID := range handoff.Dependencies {
			if summary := strings.TrimSpace(handoff.DependencySummaries[depID]); summary != "" {
				fmt.Fprintf(&b, "- %s: %s\n", depID, summary)
			}
		}
	}
	if strings.TrimSpace(handoff.RepairFeedback) != "" {
		b.WriteString("Repair feedback:\n")
		b.WriteString(strings.TrimSpace(handoff.RepairFeedback))
		b.WriteString("\n")
	}
	if strings.TrimSpace(handoff.Node.Question) != "" {
		b.WriteString("Node task:\n")
		b.WriteString(strings.TrimSpace(handoff.Node.Question))
		b.WriteString("\n")
	}
	if strings.TrimSpace(handoff.Node.ExpectedOutput) != "" {
		b.WriteString("Expected node output:\n")
		b.WriteString(strings.TrimSpace(handoff.Node.ExpectedOutput))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func renderBraidHandoffFacts(facts map[string]any) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, key := range sortedHelperFactoryMapKeys(facts) {
		fmt.Fprintf(&b, "- %s: %s\n", key, helperFactoryValueSummary(facts[key], 0))
	}
	return b.String()
}

func BraidHandoffHelperInput(handoff BraidNodeHandoff) map[string]any {
	out := map[string]any{}
	for key, value := range handoff.Facts {
		out[key] = cloneAny(value)
	}
	if strings.TrimSpace(handoff.TaskType) != "" {
		out["task_type"] = strings.TrimSpace(handoff.TaskType)
	}
	if strings.TrimSpace(handoff.ScaffoldClass) != "" {
		out["scaffold_class"] = strings.TrimSpace(handoff.ScaffoldClass)
	}
	if strings.TrimSpace(handoff.ScaffoldID) != "" {
		out["scaffold_id"] = strings.TrimSpace(handoff.ScaffoldID)
	}
	if len(handoff.Rules) > 0 {
		out["rules"] = append([]string(nil), handoff.Rules...)
	}
	if strings.TrimSpace(handoff.AnswerFormat) != "" {
		out["answer_format"] = strings.TrimSpace(handoff.AnswerFormat)
	}
	if len(handoff.Checks) > 0 {
		out["checks"] = append([]string(nil), handoff.Checks...)
	}
	if len(handoff.DependencySummaries) > 0 {
		deps := map[string]any{}
		for _, depID := range handoff.Dependencies {
			if summary := strings.TrimSpace(handoff.DependencySummaries[depID]); summary != "" {
				deps[depID] = summary
				out[depID] = summary
			}
		}
		if len(deps) > 0 {
			out["dependency_summaries"] = deps
		}
	}
	return out
}

func compactBraidHandoffText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit < 32 {
		return value[:limit]
	}
	return value[:limit-15] + "...[truncated]"
}

func EffectiveBraidNodeSummaryChars(node BraidNode) int {
	maxSummaryChars := node.MaxSummaryChars
	if node.Kind == "cycle_solve" && (maxSummaryChars == 0 || maxSummaryChars < minCycleSolveSummaryChars) {
		maxSummaryChars = minCycleSolveSummaryChars
	}
	if maxSummaryChars > maxBraidNodeSummaryChars {
		return maxBraidNodeSummaryChars
	}
	return maxSummaryChars
}

func braidNodeShouldIncludeRootTask(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "extract", "solve", "verify":
		return true
	default:
		return false
	}
}

func normalizeBraidGraph(g BraidGraph) BraidGraph {
	g.FinalNode = strings.TrimSpace(g.FinalNode)
	for idx := range g.Nodes {
		g.Nodes[idx].ID = strings.TrimSpace(g.Nodes[idx].ID)
		g.Nodes[idx].Kind = strings.ToLower(strings.TrimSpace(g.Nodes[idx].Kind))
		g.Nodes[idx].Question = strings.TrimSpace(g.Nodes[idx].Question)
		g.Nodes[idx].ExpectedOutput = strings.TrimSpace(g.Nodes[idx].ExpectedOutput)
		g.Nodes[idx].HelperPolicy = normalizeBraidNodeHelperPolicy(g.Nodes[idx].HelperPolicy)
		trimmedDeps := make([]string, 0, len(g.Nodes[idx].DependsOn))
		for _, dep := range g.Nodes[idx].DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			trimmedDeps = append(trimmedDeps, dep)
		}
		g.Nodes[idx].DependsOn = trimmedDeps
	}
	return g
}

func normalizeBraidNodeHelperPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "":
		return ""
	case BraidNodeHelperPolicyAuto, BraidNodeHelperPolicyPreferred, BraidNodeHelperPolicyRequired, BraidNodeHelperPolicyNever:
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return strings.ToLower(strings.TrimSpace(policy))
	}
}

func validBraidNodeHelperPolicy(policy string) bool {
	switch normalizeBraidNodeHelperPolicy(policy) {
	case "", BraidNodeHelperPolicyAuto, BraidNodeHelperPolicyPreferred, BraidNodeHelperPolicyRequired, BraidNodeHelperPolicyNever:
		return true
	default:
		return false
	}
}

func validBraidNodeKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "extract", "solve", "cycle_solve", "verify", "reduce":
		return true
	default:
		return false
	}
}

func isBraidSolveKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "solve", "cycle_solve":
		return true
	default:
		return false
	}
}

func validBraidNodeID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxBraidNodeIDChars {
		return false
	}
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '.', ch == '-', ch == '_':
		default:
			return false
		}
	}
	return true
}

func firstForbiddenBraidNodeRuntimeToken(text string) string {
	lower := strings.ToLower(text)
	for _, token := range []string{
		"rlm_query",
		"rlm_wait",
		"rlm_result",
		"subagent",
		"sub-agent",
		"recursive tool",
		"recursive call",
		"remaining depth",
		"runtime depth",
		"subcall budget",
	} {
		if strings.Contains(lower, token) {
			return token
		}
	}
	return ""
}

func validateBraidGraphAcyclic(deps map[string][]string) error {
	visitState := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		switch visitState[id] {
		case 1:
			return fmt.Errorf("braid graph: cycle detected at node %q", id)
		case 2:
			return nil
		}
		visitState[id] = 1
		for _, depID := range deps[id] {
			if depID == id {
				return fmt.Errorf("braid graph: cycle detected at node %q", id)
			}
			if err := visit(depID); err != nil {
				return err
			}
		}
		visitState[id] = 2
		return nil
	}

	for id := range deps {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
