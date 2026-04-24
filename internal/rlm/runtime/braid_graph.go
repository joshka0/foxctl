package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	braidGraphVersionV1      = 1
	maxBraidNodeIDChars      = 64
	maxBraidNodeSummaryChars = 1200
)

// REPLPhaseOutputKindBraidGraph marks phase outputs that must decode to a
// bounded JSON reasoning graph.
const REPLPhaseOutputKindBraidGraph = "braid_graph"

// REPLPhaseOutputKindREPLCode marks phase outputs that are raw scratch code.
// The runtime executes the assistant text with the phase's REPL tool instead of
// asking the provider to produce a tool call.
const REPLPhaseOutputKindREPLCode = "repl_code"

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
	for _, kind := range []string{"extract", "solve", "verify", "reduce"} {
		if len(byKind[kind]) == 0 {
			return fmt.Errorf("braid graph: longcot_controller requires a %s node", kind)
		}
	}
	if len(byKind["solve"]) < 2 {
		return fmt.Errorf("braid graph: longcot_controller requires at least two solve nodes")
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
			return fmt.Errorf("braid graph: longcot_controller verify node %q must depend on a solve node", verify.ID)
		}
		if !anyDepKind(verify.DependsOn, byID, "solve") {
			return fmt.Errorf("braid graph: longcot_controller verify node %q must depend on a solve node", verify.ID)
		}
		if !mentionsOriginalConstraints(verify.Question + " " + verify.ExpectedOutput) {
			return fmt.Errorf("braid graph: longcot_controller verify node %q must check original constraints, not prior summary only", verify.ID)
		}
	}
	return nil
}

func anyDepKind(depIDs []string, byID map[string]BraidNode, kind string) bool {
	for _, depID := range depIDs {
		if byID[depID].Kind == kind {
			return true
		}
	}
	return false
}

func mentionsOriginalConstraints(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"original", "constraint", "placeholder", "substitut", "fixed-point", "fixed point", "candidate"} {
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
	b.WriteString("Leaf-node contract: solve this node directly from the official task and dependency summaries. Do not ask for recursive tools, subagents, or more runtime depth.\n")
	if node.Kind == "extract" {
		b.WriteString("Extract-node contract: return facts only. Do not solve, verify, reduce, declare blocked, or treat circular-looking references as a blocker. List placeholders, requested outputs, equations, and dependency constraints as data.\n")
	}
	if strings.TrimSpace(rootPrompt) != "" {
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
	if node.MaxSummaryChars > 0 {
		fmt.Fprintf(&b, "Return a compact summary under %d characters.\n", node.MaxSummaryChars)
	}
	return strings.TrimSpace(b.String())
}

func normalizeBraidGraph(g BraidGraph) BraidGraph {
	g.FinalNode = strings.TrimSpace(g.FinalNode)
	for idx := range g.Nodes {
		g.Nodes[idx].ID = strings.TrimSpace(g.Nodes[idx].ID)
		g.Nodes[idx].Kind = strings.ToLower(strings.TrimSpace(g.Nodes[idx].Kind))
		g.Nodes[idx].Question = strings.TrimSpace(g.Nodes[idx].Question)
		g.Nodes[idx].ExpectedOutput = strings.TrimSpace(g.Nodes[idx].ExpectedOutput)
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

func validBraidNodeKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "extract", "solve", "verify", "reduce":
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
