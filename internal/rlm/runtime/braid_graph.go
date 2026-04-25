package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	braidGraphVersionV1       = 1
	maxBraidNodeIDChars       = 64
	maxBraidNodeQuestionChars = 480
	maxBraidNodeExpectedChars = 240
	maxBraidNodeSummaryChars  = 1200
	minCycleSolveSummaryChars = 900
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
	if maxNodes <= 0 {
		maxNodes = 64
	}
	if len(g.Nodes) >= maxNodes {
		return g
	}
	solveIndexes := make([]int, 0, 2)
	for idx, node := range g.Nodes {
		if isBraidSolveKind(node.Kind) {
			solveIndexes = append(solveIndexes, idx)
		}
	}
	if len(solveIndexes) != 1 {
		return g
	}

	sourceIdx := solveIndexes[0]
	source := g.Nodes[sourceIdx]
	refineID := uniqueBraidNodeID(g, source.ID+"_refine")
	refine := BraidNode{
		ID:              refineID,
		Kind:            "solve",
		Question:        "Refine or repair the candidate by checking it against the original task constraints.",
		DependsOn:       []string{source.ID},
		ExpectedOutput:  "Corrected candidate or confirmation.",
		MaxSummaryChars: maxBraidNodeSummaryChars,
		HelperPolicy:    BraidNodeHelperPolicyPreferred,
	}

	nodes := make([]BraidNode, 0, len(g.Nodes)+1)
	nodes = append(nodes, g.Nodes[:sourceIdx+1]...)
	nodes = append(nodes, refine)
	nodes = append(nodes, g.Nodes[sourceIdx+1:]...)
	g.Nodes = nodes

	for idx := range g.Nodes {
		if isBraidSolveKind(g.Nodes[idx].Kind) || g.Nodes[idx].Kind == "verify" {
			if g.Nodes[idx].HelperPolicy == "" || g.Nodes[idx].HelperPolicy == BraidNodeHelperPolicyAuto {
				g.Nodes[idx].HelperPolicy = BraidNodeHelperPolicyPreferred
			}
		}
		if g.Nodes[idx].ID == refineID {
			continue
		}
		switch g.Nodes[idx].Kind {
		case "verify":
			if dependsOnBraidNode(g.Nodes[idx], source.ID) && !dependsOnBraidNode(g.Nodes[idx], refineID) {
				g.Nodes[idx].DependsOn = append(g.Nodes[idx].DependsOn, refineID)
			}
		case "reduce":
			if dependsOnBraidNode(g.Nodes[idx], source.ID) && !dependsOnBraidNode(g.Nodes[idx], refineID) {
				g.Nodes[idx].DependsOn = append(g.Nodes[idx].DependsOn, refineID)
			}
		}
	}
	return g
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
	if countBraidSolveNodes(byKind) < 2 {
		return fmt.Errorf("braid graph: longcot_controller requires at least two solve-like nodes")
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

func uniqueBraidNodeID(g BraidGraph, base string) string {
	base = strings.Trim(strings.TrimSpace(base), "_-.")
	if base == "" {
		base = "n_solve_refine"
	}
	if len(base) > maxBraidNodeIDChars-4 {
		base = base[:maxBraidNodeIDChars-4]
	}
	used := make(map[string]struct{}, len(g.Nodes))
	for _, node := range g.Nodes {
		used[node.ID] = struct{}{}
	}
	if _, ok := used[base]; !ok && validBraidNodeID(base) {
		return base
	}
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s_%d", base, idx)
		if len(candidate) > maxBraidNodeIDChars {
			candidate = fmt.Sprintf("%s_%d", base[:maxBraidNodeIDChars-4], idx)
		}
		if _, ok := used[candidate]; !ok && validBraidNodeID(candidate) {
			return candidate
		}
	}
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
	var b strings.Builder
	fmt.Fprintf(&b, "BRAID node %s (%s)\n", node.ID, node.Kind)
	b.WriteString("Leaf-node contract: solve this node directly from the provided task context and dependency summaries. Do not ask for recursive tools, subagents, or more runtime depth.\n")
	b.WriteString("Internal scaffold note: if the official task says not to use tools, code, or a solver, that restriction applies to the public benchmark answer. This internal RLM child may still use its provided scratch/runtime phases to compute or verify its bounded subproblem, then return only the requested compact child summary.\n")
	if node.Kind == "extract" {
		b.WriteString("Extract-node contract: return facts only. Do not solve, verify, reduce, declare blocked, or treat circular-looking references as a blocker. Produce a compact constraint packet with fields: requested_outputs, known_values, dependency_edges, placeholders, cycle_cluster, equations_or_checks, candidate_bounds, blockers.\n")
	}
	if node.Kind == "cycle_solve" {
		b.WriteString("Cycle-solve contract: solve one mutually dependent constraint cluster as a bounded mathematical subproblem. Represent unknowns as variables and constraints, then use candidate search, fixed-point iteration, constraint propagation, or direct algebraic substitution. Do not report a runtime dependency cycle as a blocker. Block only when finite candidate bounds cannot be derived or all tested candidates fail, and include the attempted bounds/checks.\n")
		b.WriteString("Context policy: use only this node task, dependency summaries, and repair feedback. The full official root task is intentionally withheld to prevent broad narrative solving; rely on the extract constraint packet.\n")
	}
	if strings.TrimSpace(rootPrompt) != "" && braidNodeShouldIncludeRootTask(node.Kind) {
		b.WriteString("Official root task:\n")
		b.WriteString(strings.TrimSpace(rootPrompt))
		b.WriteString("\n\n")
	}
	if len(node.DependsOn) > 0 {
		b.WriteString("Dependencies: ")
		b.WriteString(strings.Join(node.DependsOn, ", "))
		b.WriteString("\n")
	}
	if len(node.DependsOn) > 0 && len(dependencySummaries) > 0 {
		b.WriteString("Dependency summaries:\n")
		for _, depID := range node.DependsOn {
			if summary := strings.TrimSpace(dependencySummaries[depID]); summary != "" {
				fmt.Fprintf(&b, "- %s: %s\n", depID, summary)
			}
		}
	}
	if strings.TrimSpace(repairFeedback) != "" {
		b.WriteString("Repair feedback:\n")
		b.WriteString(strings.TrimSpace(repairFeedback))
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
	if maxSummaryChars := EffectiveBraidNodeSummaryChars(node); maxSummaryChars > 0 {
		fmt.Fprintf(&b, "Return a compact summary under %d characters.\n", maxSummaryChars)
	}
	return strings.TrimSpace(b.String())
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
