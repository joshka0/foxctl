package runtime

import (
	"context"
	"strings"
)

// RecursiveTrace is a compact, readable tree for async RLM recursion. It is
// safe for eval artifacts because it omits raw prompts, answers, and REPL code.
type RecursiveTrace struct {
	RunID      string               `json:"run_id,omitempty"`
	RootNodeID string               `json:"root_node_id,omitempty"`
	Children   []RecursiveTraceNode `json:"children,omitempty"`
}

// RecursiveTraceNode summarizes one async child node and any nested trace
// emitted by that child runner.
type RecursiveTraceNode struct {
	NodeID                  string               `json:"node_id,omitempty"`
	ParentNodeID            string               `json:"parent_node_id,omitempty"`
	Depth                   int                  `json:"depth,omitempty"`
	Status                  NodeStatus           `json:"status,omitempty"`
	Summary                 string               `json:"summary,omitempty"`
	SummaryChars            int                  `json:"summary_chars,omitempty"`
	SummaryTruncated        bool                 `json:"summary_truncated,omitempty"`
	SummaryCompactionMethod string               `json:"summary_compaction_method,omitempty"`
	ErrorCode               string               `json:"error_code,omitempty"`
	ErrorMessage            string               `json:"error_message,omitempty"`
	OutputNamespace         string               `json:"output_namespace,omitempty"`
	RequiredSubcalls        int                  `json:"required_subcalls,omitempty"`
	RequiredSubcallAttempts int                  `json:"required_subcall_attempts,omitempty"`
	RecursiveSubcallsUsed   int                  `json:"recursive_subcalls_used,omitempty"`
	Iterations              int                  `json:"iterations,omitempty"`
	Subcalls                int                  `json:"subcalls,omitempty"`
	ChildInputTokens        int                  `json:"child_input_tokens,omitempty"`
	ChildOutputTokens       int                  `json:"child_output_tokens,omitempty"`
	ChildTotalTokens        int                  `json:"child_total_tokens,omitempty"`
	InternalTrace           *RecursiveTrace      `json:"internal_trace,omitempty"`
	Children                []RecursiveTraceNode `json:"children,omitempty"`
}

func buildRecursiveTrace(ctx context.Context, store NodeStore, runID, rootNodeID string) (*RecursiveTrace, error) {
	if store == nil {
		return nil, nil
	}
	runID = strings.TrimSpace(runID)
	rootNodeID = strings.TrimSpace(rootNodeID)
	if runID == "" || rootNodeID == "" {
		return nil, nil
	}
	children, err := buildRecursiveTraceChildren(ctx, store, runID, rootNodeID)
	if err != nil {
		return nil, err
	}
	return &RecursiveTrace{
		RunID:      runID,
		RootNodeID: rootNodeID,
		Children:   children,
	}, nil
}

func buildRecursiveTraceChildren(ctx context.Context, store NodeStore, runID, parentNodeID string) ([]RecursiveTraceNode, error) {
	children, err := store.ListChildren(ctx, runID, parentNodeID)
	if err != nil {
		return nil, err
	}
	out := make([]RecursiveTraceNode, 0, len(children))
	for _, child := range children {
		item := recursiveTraceNodeFromNode(child)
		nested, err := buildRecursiveTraceChildren(ctx, store, runID, child.ID)
		if err != nil {
			return nil, err
		}
		item.Children = nested
		out = append(out, item)
	}
	return out, nil
}

func recursiveTraceNodeFromNode(node Node) RecursiveTraceNode {
	item := RecursiveTraceNode{
		NodeID:          node.ID,
		ParentNodeID:    node.ParentNodeID,
		Depth:           node.Depth,
		Status:          node.Status,
		OutputNamespace: stringFromMapAny(node.Metadata, "node_dir"),
	}
	if node.Result == nil {
		return item
	}

	item.Status = node.Result.Status
	item.Summary = strings.TrimSpace(node.Result.Summary)
	item.SummaryChars = intFromAny(node.Result.Metadata["summary_chars"])
	item.SummaryTruncated = boolFromAny(node.Result.Metadata["summary_truncated"])
	item.SummaryCompactionMethod = stringFromMapAny(node.Result.Metadata, "summary_compaction_method")
	item.ErrorCode = strings.TrimSpace(node.Result.ErrorCode)
	item.ErrorMessage = strings.TrimSpace(node.Result.ErrorMessage)
	item.RequiredSubcalls = intFromAny(node.Result.Metadata["required_subcalls"])
	item.RequiredSubcallAttempts = intFromAny(node.Result.Metadata["required_subcall_attempts"])
	item.RecursiveSubcallsUsed = intFromAny(node.Result.Metadata["recursive_subcalls_used"])
	item.Iterations = intFromAny(node.Result.Metadata["iterations"])
	item.Subcalls = intFromAny(node.Result.Metadata["subcalls"])
	item.ChildInputTokens = intFromAny(node.Result.Metadata["child_input_tokens"])
	item.ChildOutputTokens = intFromAny(node.Result.Metadata["child_output_tokens"])
	item.ChildTotalTokens = intFromAny(node.Result.Metadata["child_total_tokens"])
	if outputNamespace := stringFromMapAny(node.Result.Metadata, "output_namespace"); outputNamespace != "" {
		item.OutputNamespace = outputNamespace
	}
	if trace, ok := node.Result.Metadata["recursive_trace"].(*RecursiveTrace); ok {
		item.InternalTrace = trace
	}
	return item
}
