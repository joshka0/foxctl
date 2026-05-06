package tasksgraph

import (
	"sort"
	"time"

	"github.com/joshka0/foxctl/internal/storage/tasks"
	"gonum.org/v1/gonum/graph/network"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// NodeMetrics holds computed metrics for a single task node.
type NodeMetrics struct {
	TaskID            string  `json:"task_id"`
	Title             string  `json:"title"`
	PageRank          float64 `json:"pagerank"`
	CriticalPathScore int     `json:"critical_path_score"`
	InDegree          int     `json:"in_degree"`
	OutDegree         int     `json:"out_degree"`
}

// Insights is the output of graph analysis for a workspace.
type Insights struct {
	WorkspaceID      string        `json:"workspace_id"`
	GeneratedAt      time.Time     `json:"generated_at"`
	Nodes            []NodeMetrics `json:"nodes"`
	TopologicalOrder []string      `json:"topological_order"`
	Cycles           [][]string    `json:"cycles"`
}

// Analyzer computes graph metrics from a task set.
type Analyzer interface {
	Analyze(taskList []tasks.Task, workspaceID string) (Insights, error)
}

type analyzer struct{}

// NewAnalyzer returns a new gonum-backed task graph analyzer.
func NewAnalyzer() Analyzer {
	return &analyzer{}
}

// Analyze computes task-graph metrics for a workspace.
// Edge direction: if task A depends on task B, edge goes A -> B (A points to B).
//
// Index:
//   Purpose: Compute graph insights for task scoring and prioritization
//   Keywords: pagerank, critical_path_score, topological_order, cycles, in_degree, out_degree
//   Related: network.PageRank, topo.Sort, detectCycles, computeCriticalPaths
//   Flow: build graph -> compute pagerank/degree -> detect cycles -> compute critical paths -> topo order -> assemble metrics
//   Resources: gonum graph library
//   Events: task-graph-analyzed
//   OutputFields: Insights
//
// [[protocol:task-graph-analysis]]
// [[invariant:empty-input-yields-empty-insights]]
func (a *analyzer) Analyze(taskList []tasks.Task, workspaceID string) (Insights, error) {
	insights := Insights{
		WorkspaceID: workspaceID,
		GeneratedAt: time.Now().UTC(),
		Nodes:       []NodeMetrics{},
		Cycles:      [][]string{},
	}

	if len(taskList) == 0 {
		return insights, nil
	}

	// Build ID -> numeric ID mapping for gonum
	idToNode := make(map[string]int64)
	nodeToID := make(map[int64]string)
	idToTitle := make(map[string]string)
	for i, t := range taskList {
		nodeID := int64(i)
		idToNode[t.ID] = nodeID
		nodeToID[nodeID] = t.ID
		idToTitle[t.ID] = t.Title
	}

	// Build directed graph
	g := simple.NewDirectedGraph()

	// Add all nodes first
	for _, t := range taskList {
		nodeID := idToNode[t.ID]
		g.AddNode(simple.Node(nodeID))
	}

	// Add edges: task A depends on B means A -> B
	for _, t := range taskList {
		fromID := idToNode[t.ID]
		for _, dep := range t.DependsOn {
			toID, exists := idToNode[dep]
			if !exists {
				// Dependency not in task list (orphan reference), skip
				continue
			}
			if fromID != toID { // No self-loops
				g.SetEdge(g.NewEdge(simple.Node(fromID), simple.Node(toID)))
			}
		}
	}

	// Compute PageRank
	pageRanks := network.PageRank(g, 0.85, 1e-6)

	// Compute in-degree and out-degree
	inDegree := make(map[int64]int)
	outDegree := make(map[int64]int)
	nodes := g.Nodes()
	for nodes.Next() {
		n := nodes.Node()
		nid := n.ID()
		inDegree[nid] = g.To(nid).Len()
		outDegree[nid] = g.From(nid).Len()
	}

	// Detect cycles using Tarjan's SCC
	cycles := detectCycles(g, nodeToID)
	insights.Cycles = cycles
	hasCycles := len(cycles) > 0

	// Compute critical path scores (longest path from each node to any sink)
	criticalPaths := computeCriticalPaths(g, nodeToID)

	// Compute topological order if no cycles
	if !hasCycles {
		sorted, err := topo.Sort(g)
		if err == nil {
			topoOrder := make([]string, len(sorted))
			// Reverse the order: dependencies (sinks) should come first
			for i, n := range sorted {
				topoOrder[len(sorted)-1-i] = nodeToID[n.ID()]
			}
			insights.TopologicalOrder = topoOrder
		}
	}

	// Build node metrics
	for _, t := range taskList {
		nodeID := idToNode[t.ID]
		metrics := NodeMetrics{
			TaskID:            t.ID,
			Title:             idToTitle[t.ID],
			PageRank:          pageRanks[nodeID],
			CriticalPathScore: criticalPaths[t.ID],
			InDegree:          inDegree[nodeID],
			OutDegree:         outDegree[nodeID],
		}
		insights.Nodes = append(insights.Nodes, metrics)
	}

	// Sort by critical path desc, then pagerank desc for stable output
	sort.Slice(insights.Nodes, func(i, j int) bool {
		if insights.Nodes[i].CriticalPathScore != insights.Nodes[j].CriticalPathScore {
			return insights.Nodes[i].CriticalPathScore > insights.Nodes[j].CriticalPathScore
		}
		return insights.Nodes[i].PageRank > insights.Nodes[j].PageRank
	})

	return insights, nil
}

// computeCriticalPaths calculates the longest path from each node to any sink.
// A sink is a node with no outgoing edges (no dependencies).
// Uses memoized DFS with cycle detection.
func computeCriticalPaths(g *simple.DirectedGraph, nodeToID map[int64]string) map[string]int {
	memo := make(map[int64]int)
	visiting := make(map[int64]bool) // Track nodes currently in DFS stack (cycle detection)
	result := make(map[string]int)

	var dfs func(n int64) int
	dfs = func(n int64) int {
		if val, ok := memo[n]; ok {
			return val
		}

		// Cycle detection: if we're revisiting a node in current path, return 0
		if visiting[n] {
			return 0
		}
		visiting[n] = true
		defer func() { visiting[n] = false }()

		// Get successors (nodes this one points to = dependencies)
		successors := g.From(n)
		if successors.Len() == 0 {
			// Sink node - no dependencies
			memo[n] = 0
			return 0
		}

		maxPath := 0
		for successors.Next() {
			succ := successors.Node()
			pathLen := dfs(succ.ID()) + 1
			if pathLen > maxPath {
				maxPath = pathLen
			}
		}

		memo[n] = maxPath
		return maxPath
	}

	nodes := g.Nodes()
	for nodes.Next() {
		n := nodes.Node()
		taskID := nodeToID[n.ID()]
		result[taskID] = dfs(n.ID())
	}

	return result
}

// detectCycles finds all cycles using Tarjan's SCC algorithm.
// Returns cycles as slices of task IDs.
func detectCycles(g *simple.DirectedGraph, nodeToID map[int64]string) [][]string {
	sccs := topo.TarjanSCC(g)
	var cycles [][]string

	for _, scc := range sccs {
		if len(scc) > 1 {
			// SCC with >1 node is a cycle
			cycle := make([]string, len(scc))
			for i, n := range scc {
				cycle[i] = nodeToID[n.ID()]
			}
			cycles = append(cycles, cycle)
		} else if len(scc) == 1 {
			// Single node - check for self-loop
			n := scc[0]
			if g.HasEdgeFromTo(n.ID(), n.ID()) {
				cycles = append(cycles, []string{nodeToID[n.ID()]})
			}
		}
	}

	return cycles
}
