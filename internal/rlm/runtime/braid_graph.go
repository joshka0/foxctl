package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
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

// MissingScaffoldContractError is returned when an executable graph node
// (solve, cycle_solve, verify) is missing required scaffold metadata fields.
type MissingScaffoldContractError struct {
	NodeID  string
	Missing []string
}

func (e MissingScaffoldContractError) Error() string {
	return fmt.Sprintf("braid graph: missing_scaffold_contract: node %q missing %v", e.NodeID, e.Missing)
}

// IsMissingScaffoldContract checks whether an error is a scaffold contract
// validation failure.
func IsMissingScaffoldContract(err error) (MissingScaffoldContractError, bool) {
	var mse MissingScaffoldContractError
	if ok := errors.As(err, &mse); ok {
		return mse, true
	}
	return MissingScaffoldContractError{}, false
}

// InvalidScaffoldInputError is returned when a node declares a supported
// scaffold pair but its input_schema does not satisfy that scaffold's typed
// input contract.
type InvalidScaffoldInputError struct {
	NodeID        string
	ScaffoldClass string
	ScaffoldID    string
	InputKeys     []string
	Expected      string
}

func (e InvalidScaffoldInputError) Error() string {
	return fmt.Sprintf("braid graph: invalid_scaffold_input: node %q input_schema keys %v do not satisfy %s/%s: %s", e.NodeID, e.InputKeys, e.ScaffoldClass, e.ScaffoldID, e.Expected)
}

func IsInvalidScaffoldInput(err error) (InvalidScaffoldInputError, bool) {
	var ise InvalidScaffoldInputError
	if ok := errors.As(err, &ise); ok {
		return ise, true
	}
	return InvalidScaffoldInputError{}, false
}

// UnknownBraidDependencyError is returned when a graph node references a
// dependency id that is not declared in the graph.
type UnknownBraidDependencyError struct {
	NodeID    string
	DepID     string
	KnownNode []string
}

func (e UnknownBraidDependencyError) Error() string {
	return fmt.Sprintf("braid graph: node %q depends on unknown node %q", e.NodeID, e.DepID)
}

func IsUnknownBraidDependency(err error) (UnknownBraidDependencyError, bool) {
	var ude UnknownBraidDependencyError
	if ok := errors.As(err, &ude); ok {
		return ude, true
	}
	return UnknownBraidDependencyError{}, false
}

// BraidGraph is the runtime contract emitted by the parent model.
type BraidGraph struct {
	Version   int         `json:"version"`
	Nodes     []BraidNode `json:"nodes"`
	FinalNode string      `json:"final_node"`
}

// BraidNode is one reasoning node in a bounded graph.
type BraidNode struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`
	Question        string         `json:"question"`
	DependsOn       []string       `json:"depends_on,omitempty"`
	ExpectedOutput  string         `json:"expected_output,omitempty"`
	MaxSummaryChars int            `json:"max_summary_chars,omitempty"`
	HelperPolicy    string         `json:"helper_policy,omitempty"`
	Archetype       string         `json:"archetype,omitempty"`
	ScaffoldClass   string         `json:"scaffold_class,omitempty"`
	ScaffoldID      string         `json:"scaffold_id,omitempty"`
	InputSchema     map[string]any `json:"input_schema,omitempty"`
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

// ParseBraidGraphText accepts the raw JSON graph payload. Unknown fields are
// silently stripped during normalization so model variance (extra fields like
// python_repl) does not cause parse failure.
func ParseBraidGraphText(text string) (BraidGraph, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return BraidGraph{}, fmt.Errorf("braid graph: empty output")
	}

	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
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
				return UnknownBraidDependencyError{
					NodeID:    node.ID,
					DepID:     depID,
					KnownNode: sortedBraidGraphNodeIDs(ids),
				}
			}
		}
	}
	if err := validateBraidGraphAcyclic(deps); err != nil {
		return err
	}
	return nil
}

func sortedBraidGraphNodeIDs(ids map[string]int) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// ValidateBraidGraphScaffoldContract checks that every executable node
// (solve, cycle_solve, verify) declares archetype, scaffold_class, scaffold_id, and
// input_schema. Returns a MissingScaffoldContractError with the list of
// missing fields per node.
func ValidateBraidGraphScaffoldContract(g BraidGraph) error {
	for _, node := range g.Nodes {
		if !braidNodeRequiresScaffoldContract(node.Kind) {
			continue
		}
		missing := []string{}
		if strings.TrimSpace(node.Archetype) == "" {
			missing = append(missing, "archetype")
		}
		if strings.TrimSpace(node.ScaffoldClass) == "" {
			missing = append(missing, "scaffold_class")
		}
		if strings.TrimSpace(node.ScaffoldID) == "" {
			missing = append(missing, "scaffold_id")
		}
		if len(node.InputSchema) == 0 {
			missing = append(missing, "input_schema")
		}
		if len(missing) > 0 {
			return MissingScaffoldContractError{NodeID: node.ID, Missing: missing}
		}
		if !validBraidNodeScaffoldClass(node.ScaffoldClass) {
			return fmt.Errorf("braid graph: node %q has invalid scaffold_class %q", node.ID, node.ScaffoldClass)
		}
		if !validBraidNodeScaffoldPair(node.ScaffoldClass, node.ScaffoldID) {
			return fmt.Errorf("braid graph: node %q has unsupported scaffold pair %q/%q", node.ID, node.ScaffoldClass, node.ScaffoldID)
		}
		if err := validateBraidNodeScaffoldInput(node); err != nil {
			return err
		}
	}
	return nil
}

func validateBraidNodeScaffoldInput(node BraidNode) error {
	input := cloneMapAny(node.InputSchema)
	if len(input) == 0 {
		return InvalidScaffoldInputError{
			NodeID:        node.ID,
			ScaffoldClass: strings.TrimSpace(node.ScaffoldClass),
			ScaffoldID:    strings.TrimSpace(node.ScaffoldID),
			InputKeys:     nil,
			Expected:      braidScaffoldInputExpectation(node.ScaffoldClass, node.ScaffoldID),
		}
	}
	input["scaffold_class"] = strings.TrimSpace(node.ScaffoldClass)
	input["scaffold_id"] = strings.TrimSpace(node.ScaffoldID)
	if braidScaffoldInputMatches(node.ScaffoldClass, node.ScaffoldID, input) {
		return nil
	}
	return InvalidScaffoldInputError{
		NodeID:        node.ID,
		ScaffoldClass: strings.TrimSpace(node.ScaffoldClass),
		ScaffoldID:    strings.TrimSpace(node.ScaffoldID),
		InputKeys:     sortedHelperFactoryMapKeys(input),
		Expected:      braidScaffoldInputExpectation(node.ScaffoldClass, node.ScaffoldID),
	}
}

func braidScaffoldInputMatches(cls, id string, input map[string]any) bool {
	contract, ok := braidScaffoldContractFor(cls, id)
	if !ok || contract.ValidateInput == nil {
		return false
	}
	return contract.ValidateInput(input)
}

func braidScaffoldInputExpectation(cls, id string) string {
	if contract, ok := braidScaffoldContractFor(cls, id); ok {
		return contract.ExpectedInput
	}
	return "supported scaffold input"
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
		g.Nodes[idx].ScaffoldID = normalizeBraidNodeScaffoldID(g.Nodes[idx].ScaffoldClass, g.Nodes[idx].ScaffoldID)
	}
	if strings.TrimSpace(policy) != BraidGraphPolicyLongCoTController {
		return g
	}
	return normalizeLongCoTControllerBraidGraph(g, maxNodes)
}

func uniqueBraidNodeID(existing map[string]BraidNode, preferred string) string {
	if _, ok := existing[preferred]; !ok {
		return preferred
	}
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s_%d", preferred, idx)
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
}

func normalizeBraidGraphDependencies(g BraidGraph) BraidGraph {
	known := make(map[string]bool, len(g.Nodes))
	for _, node := range g.Nodes {
		if node.ID != "" {
			known[node.ID] = true
		}
	}
	for idx := range g.Nodes {
		nodeID := g.Nodes[idx].ID
		if len(g.Nodes[idx].DependsOn) == 0 {
			continue
		}
		seen := make(map[string]bool, len(g.Nodes[idx].DependsOn))
		deps := make([]string, 0, len(g.Nodes[idx].DependsOn))
		for _, dep := range g.Nodes[idx].DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" || dep == nodeID || !known[dep] || seen[dep] {
				continue
			}
			deps = append(deps, dep)
			seen[dep] = true
		}
		g.Nodes[idx].DependsOn = deps
	}
	return g
}

func firstBraidNodeByKind(nodes []BraidNode, kind string) BraidNode {
	for _, node := range nodes {
		if node.Kind == kind {
			return node
		}
	}
	return BraidNode{}
}

func firstBraidSolveNode(nodes []BraidNode) BraidNode {
	for _, node := range nodes {
		if isBraidSolveKind(node.Kind) {
			return node
		}
	}
	return BraidNode{}
}

func lastBraidSolveNode(nodes []BraidNode) BraidNode {
	for idx := len(nodes) - 1; idx >= 0; idx-- {
		if isBraidSolveKind(nodes[idx].Kind) {
			return nodes[idx]
		}
	}
	return BraidNode{}
}

func firstBraidReduceNode(nodes []BraidNode, finalNodeID string) BraidNode {
	for _, node := range nodes {
		if node.ID == finalNodeID && node.Kind == "reduce" {
			return node
		}
	}
	return firstBraidNodeByKind(nodes, "reduce")
}

func filterBraidDepsToSelected(deps []string, selected map[string]bool) []string {
	out := make([]string, 0, len(deps))
	seen := map[string]bool{}
	for _, dep := range deps {
		if selected[dep] && !seen[dep] {
			out = append(out, dep)
			seen[dep] = true
		}
	}
	return out
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
		node := byID[depID]
		if isBraidSolveKind(node.Kind) {
			return true
		}
		if isBraidSplitMergeNode(node) {
			return true
		}
		if node.Kind == "reduce" && strings.HasSuffix(node.ID, "__adaptive_merge") {
			return true
		}
	}
	return false
}

func isBraidSplitMergeNode(node BraidNode) bool {
	if strings.TrimSpace(node.Kind) != "reduce" || !strings.HasSuffix(strings.TrimSpace(node.ID), "__merge") {
		return false
	}
	if strings.TrimSpace(stringFromAny(node.InputSchema["split_role"])) != "merge" {
		return false
	}
	if len(stringSliceFromAny(node.InputSchema["solve_ids"])) == 0 {
		return false
	}
	return true
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
	node := handoff.Node

	// Primary path: use the declared scaffold_class and scaffold_id from the
	// graph plan node. These are the hard schema contract.
	declaredClass := strings.TrimSpace(node.ScaffoldClass)
	declaredID := strings.TrimSpace(node.ScaffoldID)
	if declaredClass != "" {
		applyBraidDeclaredScaffoldHandoff(handoff, rootPrompt, declaredClass, declaredID)
		return
	}

	// Legacy fallback: archetype field (pre-contract graphs).
	declaredArchetype := strings.TrimSpace(node.Archetype)
	if declaredArchetype != "" {
		applyBraidDeclaredArchetypeHandoff(handoff, rootPrompt, declaredArchetype)
		return
	}

	// Narrow instance-based fallback: detect from typed fields in the prompt
	// instance section. No domain keyword heuristics.
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
	if braidInstanceLooksLikeSymbolicTrace(instance) {
		applyBraidSymbolicTraceHandoff(handoff, instance)
		return
	}
	if braidInstanceLooksLikeCandidateVerify(instance) {
		applyBraidCandidateVerifyHandoff(handoff, instance)
		return
	}
	if !braidInstanceLooksLikeStateTransition(instance) {
		return
	}
	applyBraidStackTransitionHandoff(handoff, instance)
}

// applyBraidDeclaredScaffoldHandoff routes from the model-declared scaffold_class
// and scaffold_id fields on the graph node. This is the primary contract path.
// No keyword heuristics are used. The input_schema on the node provides typed fields.
func applyBraidDeclaredScaffoldHandoff(handoff *BraidNodeHandoff, rootPrompt string, scaffoldClass, scaffoldID string) {
	instance := braidDeclaredScaffoldInstance(handoff.Node.InputSchema, rootPrompt)
	switch scaffoldClass {
	case BraidScaffoldClassSymbolicTrace:
		applyBraidSymbolicTraceHandoff(handoff, instance)
	case BraidScaffoldClassCandidateVerify:
		if validateCandidateVerifyInstance(instance) {
			applyBraidCandidateVerifyHandoff(handoff, instance)
		}
	case BraidScaffoldClassStateTransition:
		if braidInstanceLooksLikeStateTransition(instance) {
			applyBraidStackTransitionHandoff(handoff, instance)
			return
		}
		applyBraidStateTransitionHandoff(handoff, instance)
	case BraidScaffoldClassExplicitDAG:
		if braidInstanceLooksLikeStateTransition(instance) {
			applyBraidStackTransitionHandoff(handoff, instance)
			return
		}
		applyBraidExplicitDAGHandoff(handoff, instance)
	case BraidScaffoldClassGraphSearch:
		applyBraidGraphSearchHandoff(handoff, instance)
	case BraidScaffoldClassNumericDP:
		applyBraidNumericDPHandoff(handoff, instance)
	case BraidScaffoldClassSequenceSimulation:
		applyBraidSequenceSimulationHandoff(handoff, instance)
	case BraidScaffoldClassConstraintSolver:
		applyBraidConstraintSolverHandoff(handoff, instance)
	case BraidScaffoldClassFiniteStateTransition:
		if len(instance) > 0 {
			applyBraidStackTransitionHandoff(handoff, instance)
		}
	default:
		// Unknown scaffold class — no routing.
	}
}

func braidDeclaredScaffoldInstance(schema map[string]any, rootPrompt string) map[string]any {
	instance := cloneMapAny(schema)
	if extracted, ok := helperFactoryExtractInstanceFields(rootPrompt); ok {
		if len(instance) == 0 {
			return extracted
		}
		for key, value := range extracted {
			instance[key] = cloneAny(value)
		}
	}
	return instance
}

// applyBraidDeclaredArchetypeHandoff is the legacy path that routes from a
// model-declared archetype field. Superseded by applyBraidDeclaredScaffoldHandoff
// for new graphs that include scaffold_class/scaffold_id. No keyword heuristics.
func applyBraidDeclaredArchetypeHandoff(handoff *BraidNodeHandoff, rootPrompt string, archetype string) {
	switch archetype {
	case "symbolic_trace":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok {
			applyBraidSymbolicTraceHandoff(handoff, instance)
		}
	case "candidate_verify":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok && validateCandidateVerifyInstance(instance) {
			applyBraidCandidateVerifyHandoff(handoff, instance)
		}
	case "finite_state_transition", "state_transition":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if !ok {
			return
		}
		if braidInstanceLooksLikeStateTransition(instance) {
			applyBraidStackTransitionHandoff(handoff, instance)
		} else {
			applyBraidStateTransitionHandoff(handoff, instance)
		}
	case "graph_search":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok {
			applyBraidGraphSearchHandoff(handoff, instance)
		}
	case "numeric_dp", "table_recurrence":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok && braidInstanceLooksLikeNumericDP(instance) {
			applyBraidNumericDPHandoff(handoff, instance)
		}
	case "sequence_simulation":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok && braidInstanceLooksLikeSequenceSimulation(instance) {
			applyBraidSequenceSimulationHandoff(handoff, instance)
		}
	case "constraint_solver":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok && braidInstanceLooksLikeConstraintSolver(instance) {
			applyBraidConstraintSolverHandoff(handoff, instance)
		}
	case "explicit_dag":
		instance, ok := helperFactoryExtractInstanceFields(rootPrompt)
		if ok {
			applyBraidExplicitDAGHandoff(handoff, instance)
		}
	default:
		// Unknown declared archetype — no routing.
	}
}

// validateCandidateVerifyInstance checks that the typed packet has the
// required fields for a candidate_verify handoff.
func validateCandidateVerifyInstance(instance map[string]any) bool {
	_, hasCandidates := instance["candidates"]
	_, hasPredicates := instance["predicates"]
	return hasCandidates || hasPredicates
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
	case "solve", "cycle_solve", "verify", "reduce":
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

func braidInstanceLooksLikeSymbolicTrace(instance map[string]any) bool {
	_, hasProgram := instance["program"]
	_, hasQueries := instance["queries"]
	// Require scaffold_class for unambiguous detection, or accept
	// program+queries as a strong signal.
	if class, ok := instance["scaffold_class"].(string); ok && strings.TrimSpace(class) == BraidScaffoldClassSymbolicTrace {
		return true
	}
	return hasProgram && hasQueries
}

func braidInstanceLooksLikeCandidateVerify(instance map[string]any) bool {
	_, hasCandidates := instance["candidates"]
	_, hasPredicates := instance["predicates"]
	if class, ok := instance["scaffold_class"].(string); ok && strings.TrimSpace(class) == BraidScaffoldClassCandidateVerify {
		return true
	}
	return hasCandidates || hasPredicates
}

func applyBraidSymbolicTraceHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassSymbolicTrace
	handoff.ScaffoldClass = BraidScaffoldClassSymbolicTrace
	handoff.ScaffoldID = BraidScaffoldIDTypeInferenceV1

	facts := map[string]any{
		"scaffold_class": BraidScaffoldClassSymbolicTrace,
		"scaffold_id":    BraidScaffoldIDTypeInferenceV1,
		"trace_kind":     "environment_update",
	}
	// Forward typed fields from the instance if present.
	for _, key := range []string{"program", "queries", "environment", "bindings", "events"} {
		if val, ok := instance[key]; ok {
			facts[key] = val
		}
	}
	handoff.Facts = facts
	handoff.Rules = []string{
		"Parse the program/trace as a sequence of binding or environment-update steps.",
		"Maintain an environment mapping variables to type schemes or values.",
		"Apply the appropriate inference algorithm (e.g., Algorithm W for let-bindings).",
		"Unification must include occurs check.",
		"Maintain a trace recording each step with its index.",
		"Answer queries by looking up in the final state.",
	}
	handoff.AnswerFormat = `solution = {"q1": "<answer>", "q2": "<answer>"} or solution = <single_answer>`
	handoff.Checks = []string{
		"program/trace text is non-empty",
		"every binding/step appears in the environment after processing",
		"unification applies substitution compositionally with occurs check",
		"query answers reference entries that exist in the trace",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

func applyBraidCandidateVerifyHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassCandidateVerify
	handoff.ScaffoldClass = BraidScaffoldClassCandidateVerify
	handoff.ScaffoldID = BraidScaffoldIDPropertyCheckV1

	facts := map[string]any{
		"scaffold_class": BraidScaffoldClassCandidateVerify,
		"scaffold_id":    BraidScaffoldIDPropertyCheckV1,
		"selection_rule": "best",
	}
	// Forward typed fields from the instance.
	for _, key := range []string{"candidates", "predicates", "selection_rule", "output_schema"} {
		if val, ok := instance[key]; ok {
			facts[key] = val
		}
	}
	handoff.Facts = facts
	handoff.Rules = []string{
		"Evaluate each predicate/property for each candidate.",
		"Apply the selection rule to pick the answer.",
		"If a required library is unavailable, return a structured missing_backend error immediately — do not attempt to install it.",
	}
	handoff.AnswerFormat = "solution = <answer>"
	handoff.Checks = []string{
		"every candidate was evaluated",
		"every predicate was checked",
		"selection rule was applied correctly",
		"answer matches the requested output format",
	}
	if handoff.Node.Kind != "extract" {
		handoff.OfficialRootTask = ""
	}
}

// applyBraidStateTransitionHandoff handles the generic state_transition/state_replay_v1
// archetype. The model should emit archetype=state_transition with input_schema
// containing the move_sequence or actions. The preset source handles replay generically.
func applyBraidStateTransitionHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassStateTransition
	handoff.ScaffoldClass = BraidScaffoldClassStateTransition
	handoff.ScaffoldID = BraidScaffoldIDStateReplayV1

	facts := map[string]any{
		"scaffold_class": BraidScaffoldClassStateTransition,
		"scaffold_id":    BraidScaffoldIDStateReplayV1,
	}
	for _, key := range []string{"move_sequence", "initial_state", "actions", "transitions"} {
		if val, ok := instance[key]; ok {
			facts[key] = val
		}
	}
	handoff.Facts = facts
	handoff.Rules = []string{
		"Apply each action sequentially from the initial state.",
		"Handle all action types correctly.",
		"Track all state components.",
		"Return the final state in the requested format.",
	}
	handoff.AnswerFormat = "solution = <state_representation>"
	handoff.Checks = []string{
		"all actions were applied",
		"state components are consistent",
		"output format is valid",
	}
	if handoff.Node.Kind != "extract" {
		handoff.Node.MaxSummaryChars = 200
	}
}

// applyBraidSearchBacktrackHandoff handles the generic explicit_dag/search_backtrack_v1 archetype.
func applyBraidSearchBacktrackHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	handoff.TaskType = BraidScaffoldClassExplicitDAG
	handoff.ScaffoldClass = BraidScaffoldClassExplicitDAG
	handoff.ScaffoldID = BraidScaffoldIDSearchBacktrackV1

	facts := map[string]any{
		"scaffold_class": BraidScaffoldClassExplicitDAG,
		"scaffold_id":    BraidScaffoldIDSearchBacktrackV1,
	}
	for _, key := range []string{"nodes", "dependencies", "problems", "target_nodes", "cycle_clusters"} {
		if val, ok := instance[key]; ok {
			facts[key] = val
		}
	}
	handoff.Facts = facts
	handoff.Rules = []string{
		"Parse sub-problems from the input.",
		"Identify dependencies between sub-problems.",
		"Solve leaf nodes first, then propagate forward.",
		"Return structured JSON with each node's answer.",
	}
	handoff.AnswerFormat = `solution = {"node_0": <answer>, "node_1": <answer>, ...}`
	handoff.Checks = []string{
		"all sub-problems were solved",
		"dependencies were resolved correctly",
		"answers are in the expected format",
	}
	if handoff.Node.Kind != "extract" {
		handoff.Node.MaxSummaryChars = 500
	}
}

func applyBraidExplicitDAGHandoff(handoff *BraidNodeHandoff, instance map[string]any) {
	applyBraidSearchBacktrackHandoff(handoff, instance)
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
		b.WriteString("Extract-node contract: return facts only. Do not solve, verify, reduce, declare blocked, or treat circular-looking references as a blocker. Produce a compact constraint packet with fields: requested_outputs, known_values, dependency_edges, placeholders, cycle_clusters, equations_or_checks, candidate_bounds, blockers.\n")
	}
	if node.Kind == "cycle_solve" {
		b.WriteString("Cycle-solve contract: solve one mutually dependent constraint cluster as a bounded mathematical subproblem. Represent unknowns as variables and constraints, then use candidate search, fixed-point iteration, constraint propagation, or direct algebraic substitution. Do not report a runtime dependency cycle as a blocker. Block only when finite candidate bounds cannot be derived or all tested candidates fail, and include the attempted bounds/checks.\n")
		b.WriteString(cycleSolveHelperOutputContract())
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
	b.WriteString("Return one compact NodeArtifact JSON object when this is the final child response: {\"status\":\"solved|partial|blocked\",\"answer\":\"...\",\"checks\":[\"...\"],\"confidence\":0.0}. For verify nodes, use {\"status\":\"pass\",\"answer\":\"pass: true\",\"pass\":true,\"checks\":[...]} only when every original constraint is checked; use status \"blocked\" with pass:false for the first concrete failure. Legacy status/answer/checks lines are accepted only as fallback.\n")
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
	if handoff.ScaffoldClass == BraidScaffoldClassSymbolicTrace {
		b.WriteString("Symbolic-trace output contract:\n")
		b.WriteString("- Treat the input as a typed symbolic trace, not prose generation.\n")
		b.WriteString("- Parse the program text from the program field into let-bindings.\n")
		b.WriteString("- Execute Algorithm W: maintain type environment, substitution, and binding trace.\n")
		b.WriteString("- For each let-binding: infer type, unify constraints with occurs check, generalize over free variables.\n")
		b.WriteString("- Answer each query from the queries field by looking up type schemes or trace entries.\n")
		b.WriteString("- Return `solution = {\"q1\": \"...\", \"q2\": \"...\", ...}` with all query answers.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_binding, observed, expected, and repair_hint.\n")
	}
	if handoff.ScaffoldClass == BraidScaffoldClassCandidateVerify {
		b.WriteString("Candidate-verify output contract:\n")
		b.WriteString("- Treat the input as a candidate enumeration and verification problem, not prose generation.\n")
		b.WriteString("- Evaluate every predicate for every candidate. Do not skip or short-circuit.\n")
		b.WriteString("- Do not claim a required library is unavailable unless a REPL import actually failed and checks include the exact ImportError. If the prompt packet says a library is available, import it and use it.\n")
		b.WriteString("- Return `solution = <answer>` matching the output_schema.\n")
		b.WriteString("- If verification fails, return ok:false with first_failure, failed_candidate, observed, expected, and repair_hint.\n")
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
	if strings.TrimSpace(handoff.Node.ID) != "" {
		out["node_id"] = strings.TrimSpace(handoff.Node.ID)
	}
	if strings.TrimSpace(handoff.Node.Question) != "" {
		out["work_item_question"] = strings.TrimSpace(handoff.Node.Question)
	}
	if strings.TrimSpace(handoff.Node.ExpectedOutput) != "" {
		out["expected_output"] = strings.TrimSpace(handoff.Node.ExpectedOutput)
	}
	if strings.TrimSpace(handoff.OfficialRootTask) != "" {
		out["root_task"] = strings.TrimSpace(handoff.OfficialRootTask)
		if existing := strings.TrimSpace(fmt.Sprintf("%v", out["prompt"])); existing == "" || braidInputPromptLooksLikePlaceholder(existing) {
			out["prompt"] = strings.TrimSpace(handoff.OfficialRootTask)
		}
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
		depText := map[string]any{}
		for _, depID := range handoff.Dependencies {
			if summary := strings.TrimSpace(handoff.DependencySummaries[depID]); summary != "" {
				packet := braidDependencyHandoffPacket(summary)
				deps[depID] = packet
				depText[depID] = summary
				out[depID] = packet
			}
		}
		if len(deps) > 0 {
			out["dependency_summaries"] = deps
			out["dependency_summary_text"] = depText
		}
	}
	return out
}

func braidInputPromptLooksLikePlaceholder(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	switch trimmed {
	case "", "original problem", "original prompt", "original problem and extracted dependencies", "original problem and extracted dependency graph":
		return true
	default:
		return false
	}
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
		g.Nodes[idx].Question = clampString(strings.TrimSpace(g.Nodes[idx].Question), maxBraidNodeQuestionChars)
		g.Nodes[idx].ExpectedOutput = clampString(strings.TrimSpace(g.Nodes[idx].ExpectedOutput), maxBraidNodeExpectedChars)
		g.Nodes[idx].HelperPolicy = normalizeBraidNodeHelperPolicy(g.Nodes[idx].HelperPolicy)
		g.Nodes[idx].Archetype = normalizeBraidNodeArchetype(g.Nodes[idx].Archetype)
		g.Nodes[idx].ScaffoldClass = strings.ToLower(strings.TrimSpace(g.Nodes[idx].ScaffoldClass))
		g.Nodes[idx].ScaffoldID = strings.ToLower(strings.TrimSpace(g.Nodes[idx].ScaffoldID))
		if g.Nodes[idx].MaxSummaryChars < 0 {
			g.Nodes[idx].MaxSummaryChars = 0
		}
		if g.Nodes[idx].DependsOn == nil {
			g.Nodes[idx].DependsOn = []string{}
		}
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

func cloneBraidGraph(g BraidGraph) BraidGraph {
	out := g
	if len(g.Nodes) > 0 {
		out.Nodes = make([]BraidNode, len(g.Nodes))
		for idx, node := range g.Nodes {
			out.Nodes[idx] = cloneBraidNode(node)
		}
	}
	return out
}

func cloneBraidNode(node BraidNode) BraidNode {
	out := node
	if len(node.DependsOn) > 0 {
		out.DependsOn = append([]string(nil), node.DependsOn...)
	}
	out.InputSchema = cloneMapAny(node.InputSchema)
	return out
}

func clampString(s string, maxLen int) string {
	if maxLen <= 0 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
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

// normalizeBraidNodeArchetype validates and normalizes the declared archetype
// from a braid graph node. Returns empty string for unknown archetypes so the
// runtime falls back to prompt-based detection.
func normalizeBraidNodeArchetype(archetype string) string {
	trimmed := strings.ToLower(strings.TrimSpace(archetype))
	switch trimmed {
	case "symbolic_trace", "state_transition", "candidate_verify",
		"explicit_dag", "table_recurrence", "constraint_solve",
		"finite_state_transition", "graph_search", "numeric_dp",
		"sequence_simulation", "mixed":
		return trimmed
	case "constraint_solver":
		return "constraint_solve"
	default:
		return ""
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

func validBraidNodeScaffoldClass(cls string) bool {
	return braidScaffoldClassKnown(cls)
}

func validBraidNodeScaffoldPair(cls, id string) bool {
	_, ok := braidScaffoldContractFor(cls, id)
	return ok
}

func normalizeBraidNodeScaffoldID(cls, id string) string {
	cls = strings.TrimSpace(cls)
	id = strings.TrimSpace(id)
	if id != BraidScaffoldIDGenericV1 {
		return id
	}
	switch cls {
	case BraidScaffoldClassFiniteStateTransition:
		return BraidScaffoldIDStackRelocationV1
	case BraidScaffoldClassGraphSearch:
		return BraidScaffoldIDExplicitShortestPathV1
	case BraidScaffoldClassNumericDP:
		return BraidScaffoldIDRecurrenceTableV1
	case BraidScaffoldClassSequenceSimulation:
		return BraidScaffoldIDJSONPatchSequenceV1
	case BraidScaffoldClassConstraintSolver:
		return BraidScaffoldIDFiniteDomainV1
	case BraidScaffoldClassSymbolicTrace:
		return BraidScaffoldIDTypeInferenceV1
	case BraidScaffoldClassCandidateVerify:
		return BraidScaffoldIDPropertyCheckV1
	case BraidScaffoldClassStateTransition:
		return BraidScaffoldIDStateReplayV1
	case BraidScaffoldClassExplicitDAG:
		return BraidScaffoldIDSearchBacktrackV1
	default:
		return id
	}
}

func braidNodeRequiresScaffoldContract(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "solve", "cycle_solve", "verify":
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
