// Package view builds deterministic read models for the evolve CLI.
package view

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
)

var nodeStatusOrder = []model.NodeStatus{
	model.NodeStatusRoot,
	model.NodeStatusPending,
	model.NodeStatusActive,
	model.NodeStatusCommitted,
	model.NodeStatusEvaluated,
	model.NodeStatusFailed,
	model.NodeStatusDiscarded,
	model.NodeStatusPruned,
}

// NodeStatusCount captures one status bucket in deterministic order.
type NodeStatusCount struct {
	Status model.NodeStatus `json:"status"`
	Count  int              `json:"count"`
}

// StatusSummary is the compact run summary returned by evolve status.
type StatusSummary struct {
	RunID            string                `json:"run_id"`
	WorkspacePath    string                `json:"workspace_path"`
	TargetPath       string                `json:"target_path"`
	BenchmarkCommand string                `json:"benchmark_command"`
	Metric           model.MetricDirection `json:"metric"`
	Status           model.RunStatus       `json:"status"`
	TotalNodes       int                   `json:"total_nodes"`
	FrontierCount    int                   `json:"frontier_count"`
	FrontierNodeIDs  []string              `json:"frontier_node_ids"`
	NodeCounts       []NodeStatusCount     `json:"node_counts"`
}

// TreeNode is one node in a deterministic nested run tree.
type TreeNode struct {
	ID                string           `json:"id"`
	ParentID          string           `json:"parent_id,omitempty"`
	Status            model.NodeStatus `json:"status"`
	Hypothesis        string           `json:"hypothesis,omitempty"`
	Score             *float64         `json:"score,omitempty"`
	EvalEpoch         int              `json:"eval_epoch"`
	Branch            string           `json:"branch,omitempty"`
	CurrentAttempt    int              `json:"current_attempt"`
	EvaluatedAttempts int              `json:"evaluated_attempts"`
	Children          []*TreeNode      `json:"children,omitempty"`
}

// TreeView is the run tree read model plus an ASCII rendering.
type TreeView struct {
	RunID         string      `json:"run_id"`
	NodeCount     int         `json:"node_count"`
	FrontierCount int         `json:"frontier_count"`
	Roots         []*TreeNode `json:"roots"`
	Rendered      string      `json:"rendered"`
}

// BuildStatusSummary compacts run/node state for a status response.
func BuildStatusSummary(run model.Run, nodes []model.Node, frontier []model.Node) StatusSummary {
	counts := make(map[model.NodeStatus]int, len(nodeStatusOrder))
	for _, node := range nodes {
		counts[node.Status]++
	}

	statusCounts := make([]NodeStatusCount, 0, len(nodeStatusOrder))
	for _, status := range nodeStatusOrder {
		statusCounts = append(statusCounts, NodeStatusCount{
			Status: status,
			Count:  counts[status],
		})
	}

	frontierCopy := append([]model.Node(nil), frontier...)
	sortNodes(frontierCopy)
	frontierIDs := make([]string, 0, len(frontierCopy))
	for _, node := range frontierCopy {
		frontierIDs = append(frontierIDs, node.ID)
	}

	return StatusSummary{
		RunID:            run.ID,
		WorkspacePath:    run.WorkspacePath,
		TargetPath:       run.TargetPath,
		BenchmarkCommand: run.BenchmarkCommand,
		Metric:           run.Metric,
		Status:           run.Status,
		TotalNodes:       len(nodes),
		FrontierCount:    len(frontierCopy),
		FrontierNodeIDs:  frontierIDs,
		NodeCounts:       statusCounts,
	}
}

// BuildTreeView creates a deterministic nested tree and ASCII view.
func BuildTreeView(runID string, nodes []model.Node, frontier []model.Node) TreeView {
	nodeCopy := append([]model.Node(nil), nodes...)
	sortNodes(nodeCopy)

	lookup := make(map[string]*TreeNode, len(nodeCopy))
	for _, node := range nodeCopy {
		n := node
		lookup[node.ID] = &TreeNode{
			ID:                n.ID,
			ParentID:          n.ParentID,
			Status:            n.Status,
			Hypothesis:        n.Hypothesis,
			Score:             n.Score,
			EvalEpoch:         n.EvalEpoch,
			Branch:            n.Branch,
			CurrentAttempt:    n.CurrentAttempt,
			EvaluatedAttempts: n.EvaluatedAttempts,
		}
	}

	roots := make([]*TreeNode, 0, len(nodeCopy))
	for _, node := range nodeCopy {
		current := lookup[node.ID]
		if node.ParentID == "" {
			roots = append(roots, current)
			continue
		}
		parent, ok := lookup[node.ParentID]
		if !ok {
			roots = append(roots, current)
			continue
		}
		parent.Children = append(parent.Children, current)
	}

	rendered := renderTree(roots)
	return TreeView{
		RunID:         runID,
		NodeCount:     len(nodeCopy),
		FrontierCount: len(frontier),
		Roots:         roots,
		Rendered:      rendered,
	}
}

func sortNodes(nodes []model.Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt.Equal(nodes[j].CreatedAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].CreatedAt.Before(nodes[j].CreatedAt)
	})
}

func renderTree(roots []*TreeNode) string {
	if len(roots) == 0 {
		return ""
	}

	lines := make([]string, 0, len(roots))
	for _, root := range roots {
		renderNode(root, 0, &lines)
	}
	return strings.Join(lines, "\n")
}

func renderNode(node *TreeNode, depth int, lines *[]string) {
	if node == nil {
		return
	}
	indent := strings.Repeat("  ", depth)
	label := fmt.Sprintf("%s%s [%s]", indent, node.ID, node.Status)
	if node.Score != nil {
		label = fmt.Sprintf("%s score=%.4f", label, *node.Score)
	}
	*lines = append(*lines, label)
	for _, child := range node.Children {
		renderNode(child, depth+1, lines)
	}
}
