package tasksgraph

import (
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzer_EmptyGraph(t *testing.T) {
	a := NewAnalyzer()
	insights, err := a.Analyze(nil, "test-workspace")

	require.NoError(t, err)
	assert.Equal(t, "test-workspace", insights.WorkspaceID)
	assert.Empty(t, insights.Nodes)
	assert.Empty(t, insights.TopologicalOrder)
	assert.Empty(t, insights.Cycles)
}

func TestAnalyzer_SingleNode(t *testing.T) {
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", Status: "pending"},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)
	assert.Len(t, insights.Nodes, 1)
	assert.Equal(t, "A", insights.Nodes[0].TaskID)
	assert.Equal(t, 0, insights.Nodes[0].CriticalPathScore)
	assert.Equal(t, 0, insights.Nodes[0].InDegree)
	assert.Equal(t, 0, insights.Nodes[0].OutDegree)
	assert.Empty(t, insights.Cycles)
	assert.Equal(t, []string{"A"}, insights.TopologicalOrder)
}

func TestAnalyzer_LinearChain(t *testing.T) {
	// A depends on B, B depends on C, C depends on D
	// Graph: A -> B -> C -> D
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{"B"}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{"C"}},
		{ID: "C", WorkspaceID: "ws", Title: "Task C", DependsOn: []string{"D"}},
		{ID: "D", WorkspaceID: "ws", Title: "Task D", DependsOn: []string{}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)
	assert.Len(t, insights.Nodes, 4)
	assert.Empty(t, insights.Cycles)

	// Build map for easier assertions
	nodeMap := make(map[string]NodeMetrics)
	for _, n := range insights.Nodes {
		nodeMap[n.TaskID] = n
	}

	// Critical path scores: A=3, B=2, C=1, D=0
	assert.Equal(t, 3, nodeMap["A"].CriticalPathScore)
	assert.Equal(t, 2, nodeMap["B"].CriticalPathScore)
	assert.Equal(t, 1, nodeMap["C"].CriticalPathScore)
	assert.Equal(t, 0, nodeMap["D"].CriticalPathScore)

	// In-degree (how many depend on this): D=1, C=1, B=1, A=0
	assert.Equal(t, 0, nodeMap["A"].InDegree)
	assert.Equal(t, 1, nodeMap["B"].InDegree)
	assert.Equal(t, 1, nodeMap["C"].InDegree)
	assert.Equal(t, 1, nodeMap["D"].InDegree)

	// Out-degree (how many dependencies): A=1, B=1, C=1, D=0
	assert.Equal(t, 1, nodeMap["A"].OutDegree)
	assert.Equal(t, 1, nodeMap["B"].OutDegree)
	assert.Equal(t, 1, nodeMap["C"].OutDegree)
	assert.Equal(t, 0, nodeMap["D"].OutDegree)

	// Topological order should have D before C before B before A
	assert.Len(t, insights.TopologicalOrder, 4)
	// Verify D comes before C, C before B, B before A
	posMap := make(map[string]int)
	for i, id := range insights.TopologicalOrder {
		posMap[id] = i
	}
	assert.Less(t, posMap["D"], posMap["C"])
	assert.Less(t, posMap["C"], posMap["B"])
	assert.Less(t, posMap["B"], posMap["A"])
}

func TestAnalyzer_ForkJoin(t *testing.T) {
	// D depends on B and C
	// B depends on A
	// C depends on A
	// Graph: D -> {B, C} -> A
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{"A"}},
		{ID: "C", WorkspaceID: "ws", Title: "Task C", DependsOn: []string{"A"}},
		{ID: "D", WorkspaceID: "ws", Title: "Task D", DependsOn: []string{"B", "C"}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)
	assert.Len(t, insights.Nodes, 4)
	assert.Empty(t, insights.Cycles)

	nodeMap := make(map[string]NodeMetrics)
	for _, n := range insights.Nodes {
		nodeMap[n.TaskID] = n
	}

	// In-degrees: A=2 (B and C depend on it), B=1, C=1, D=0
	assert.Equal(t, 2, nodeMap["A"].InDegree)
	assert.Equal(t, 1, nodeMap["B"].InDegree)
	assert.Equal(t, 1, nodeMap["C"].InDegree)
	assert.Equal(t, 0, nodeMap["D"].InDegree)

	// Out-degrees: D=2, B=1, C=1, A=0
	assert.Equal(t, 2, nodeMap["D"].OutDegree)
	assert.Equal(t, 1, nodeMap["B"].OutDegree)
	assert.Equal(t, 1, nodeMap["C"].OutDegree)
	assert.Equal(t, 0, nodeMap["A"].OutDegree)

	// Critical paths: D=2, B=1, C=1, A=0
	assert.Equal(t, 2, nodeMap["D"].CriticalPathScore)
	assert.Equal(t, 1, nodeMap["B"].CriticalPathScore)
	assert.Equal(t, 1, nodeMap["C"].CriticalPathScore)
	assert.Equal(t, 0, nodeMap["A"].CriticalPathScore)

	// Topological order: A must come before B and C, B and C before D
	posMap := make(map[string]int)
	for i, id := range insights.TopologicalOrder {
		posMap[id] = i
	}
	assert.Less(t, posMap["A"], posMap["B"])
	assert.Less(t, posMap["A"], posMap["C"])
	assert.Less(t, posMap["B"], posMap["D"])
	assert.Less(t, posMap["C"], posMap["D"])
}

func TestAnalyzer_SimpleCycle(t *testing.T) {
	// A -> B -> C -> A (cycle)
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{"B"}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{"C"}},
		{ID: "C", WorkspaceID: "ws", Title: "Task C", DependsOn: []string{"A"}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)
	assert.Len(t, insights.Nodes, 3)

	// Should have exactly one cycle containing A, B, C
	assert.Len(t, insights.Cycles, 1)
	cycle := insights.Cycles[0]
	assert.Len(t, cycle, 3)
	assert.Contains(t, cycle, "A")
	assert.Contains(t, cycle, "B")
	assert.Contains(t, cycle, "C")

	// Topological order should be empty when cycles exist
	assert.Empty(t, insights.TopologicalOrder)
}

func TestAnalyzer_DisconnectedSubgraphs(t *testing.T) {
	// Two independent chains: A->B and C->D
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{"B"}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{}},
		{ID: "C", WorkspaceID: "ws", Title: "Task C", DependsOn: []string{"D"}},
		{ID: "D", WorkspaceID: "ws", Title: "Task D", DependsOn: []string{}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)
	assert.Len(t, insights.Nodes, 4)
	assert.Empty(t, insights.Cycles)

	nodeMap := make(map[string]NodeMetrics)
	for _, n := range insights.Nodes {
		nodeMap[n.TaskID] = n
	}

	// Critical paths: A=1, C=1, B=0, D=0
	assert.Equal(t, 1, nodeMap["A"].CriticalPathScore)
	assert.Equal(t, 1, nodeMap["C"].CriticalPathScore)
	assert.Equal(t, 0, nodeMap["B"].CriticalPathScore)
	assert.Equal(t, 0, nodeMap["D"].CriticalPathScore)

	// Topological order exists
	assert.Len(t, insights.TopologicalOrder, 4)
}

func TestAnalyzer_OrphanDependency(t *testing.T) {
	// A depends on X which doesn't exist - should be skipped
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{"X"}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)
	assert.Len(t, insights.Nodes, 2)
	assert.Empty(t, insights.Cycles)

	nodeMap := make(map[string]NodeMetrics)
	for _, n := range insights.Nodes {
		nodeMap[n.TaskID] = n
	}

	// A should have out-degree 0 since X doesn't exist
	assert.Equal(t, 0, nodeMap["A"].OutDegree)
	assert.Equal(t, 0, nodeMap["A"].CriticalPathScore)
}

func TestAnalyzer_PageRankValues(t *testing.T) {
	// Simple graph to verify PageRank is computed
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{"B", "C"}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{}},
		{ID: "C", WorkspaceID: "ws", Title: "Task C", DependsOn: []string{}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)

	// PageRank values should be non-zero
	for _, node := range insights.Nodes {
		assert.Greater(t, node.PageRank, 0.0, "PageRank for %s should be > 0", node.TaskID)
	}
}

func TestAnalyzer_GeneratedAt(t *testing.T) {
	a := NewAnalyzer()
	before := time.Now().UTC()
	insights, err := a.Analyze([]tasks.Task{{ID: "A"}}, "ws")
	after := time.Now().UTC()

	require.NoError(t, err)
	assert.True(t, insights.GeneratedAt.After(before) || insights.GeneratedAt.Equal(before))
	assert.True(t, insights.GeneratedAt.Before(after) || insights.GeneratedAt.Equal(after))
}

func TestAnalyzer_NodesSortedByCriticalPath(t *testing.T) {
	a := NewAnalyzer()
	taskList := []tasks.Task{
		{ID: "D", WorkspaceID: "ws", Title: "Task D", DependsOn: []string{}},
		{ID: "C", WorkspaceID: "ws", Title: "Task C", DependsOn: []string{"D"}},
		{ID: "B", WorkspaceID: "ws", Title: "Task B", DependsOn: []string{"C"}},
		{ID: "A", WorkspaceID: "ws", Title: "Task A", DependsOn: []string{"B"}},
	}

	insights, err := a.Analyze(taskList, "ws")

	require.NoError(t, err)

	// Nodes should be sorted by critical path descending
	assert.Equal(t, "A", insights.Nodes[0].TaskID) // CPS=3
	assert.Equal(t, "B", insights.Nodes[1].TaskID) // CPS=2
	assert.Equal(t, "C", insights.Nodes[2].TaskID) // CPS=1
	assert.Equal(t, "D", insights.Nodes[3].TaskID) // CPS=0
}
