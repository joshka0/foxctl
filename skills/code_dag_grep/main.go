// Package main implements the code/dag_grep skill.
// It runs a scored repo index search and expands into a compact explanation DAG.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/repoquery"
)

// Command is the skill name used in envelopes.
const Command = "code/dag_grep"

// Input configures DAG_grep behavior.
type Input struct {
	Query          string   `json:"query"`
	Workspace      string   `json:"workspace,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	K              int      `json:"k,omitempty"`
	NodeKinds      []string `json:"node_kinds,omitempty"`
	EdgeSets       []string `json:"edge_sets,omitempty"`
	EdgeTypes      []string `json:"edge_types,omitempty"`
	Direction      string   `json:"direction,omitempty"`
	Depth          int      `json:"depth,omitempty"`
	Budget         int      `json:"budget,omitempty"`
	PerNodeCap     int      `json:"per_node_cap,omitempty"`
	IncludeAnchors *bool    `json:"include_anchors,omitempty"`
	Render         string   `json:"render,omitempty"`
}

// Output is the response for code/dag_grep.
type Output struct {
	Result   repoindex.DAGGrepResult `json:"result"`
	Rendered string                  `json:"rendered,omitempty"`
}

const (
	defaultPreviewNodes = 40
	defaultPreviewEdges = 80
	previewDocLimit     = 240
	previewSummaryLimit = 180
)

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Query) == "" {
		return skillerr.Arg("query is required")
	}

	workspaceRoot, err := resolveWorkspace(rc.Workspace, in.Workspace)
	if err != nil {
		return skillerr.WrapIO("resolve workspace", err)
	}

	store, err := repoindex.Open(ctx, rc.Config.Storage.Root, workspaceRoot)
	if err != nil {
		return skillerr.WrapIO("open repoindex", err)
	}
	defer func() { errors.Ignore(store.Close(), "close repoindex store") }()

	engine := repoindex.NewQueryEngine(store)

	req, err := repoquery.NewDAGGrepRequest(
		in.Query,
		in.Mode,
		in.K,
		in.NodeKinds,
		in.EdgeSets,
		in.EdgeTypes,
		in.Direction,
		in.Depth,
		in.Budget,
		in.PerNodeCap,
		in.IncludeAnchors,
		in.Render,
	)
	if err != nil {
		return skillerr.Arg(err.Error())
	}

	result, err := engine.DAGGrep(ctx, repoindex.DAGGrepRequest{
		Query:          req.Query,
		Mode:           req.Mode,
		K:              req.K,
		NodeKinds:      req.NodeKinds,
		EdgeTypes:      req.EdgeTypes,
		Direction:      req.Direction,
		Depth:          req.Depth,
		Budget:         req.Budget,
		PerNodeCap:     req.PerNodeCap,
		IncludeAnchors: req.IncludeAnchors,
	})
	if err != nil {
		return skillerr.WrapIO("dag_grep", err)
	}

	output := Output{Result: result}
	if rendered := renderDAG(result, in.Render); rendered != "" {
		output.Rendered = rendered
	}

	payload, err := json.Marshal(output)
	if err != nil {
		return skillerr.WrapIO("marshal output", err)
	}

	inlineLimit := rc.InlineLimit()
	if inlineLimit <= 0 || inlineLimit > 64*1024 {
		inlineLimit = 64 * 1024
	}
	if len(payload) <= inlineLimit {
		return skillout.Emit(rc, Command, output)
	}

	artifact, err := skillmain.PersistJSON(ctx, rc, output, Command)
	if err != nil {
		return skillerr.WrapIO("persist output", err)
	}
	casResult := skillout.BuildCASResult(artifact, rc.ExposePolicy())

	preview := trimDAGResult(result, defaultPreviewNodes, defaultPreviewEdges)
	previewOutput := Output{Result: preview}
	if rendered := renderDAG(preview, in.Render); rendered != "" {
		previewOutput.Rendered = rendered
	}

	envelope := map[string]any{
		"result":    previewOutput.Result,
		"rendered":  previewOutput.Rendered,
		"truncated": true,
	}
	for key, value := range casResult {
		envelope[key] = value
	}

	return skillout.Emit(rc, Command, envelope)
}

func resolveWorkspace(base, override string) (string, error) {
	workspace := strings.TrimSpace(override)
	if workspace == "" {
		workspace = base
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if !filepath.IsAbs(workspace) {
		if base != "" {
			workspace = filepath.Join(base, workspace)
		}
	}
	return filepath.Abs(workspace)
}

func renderDAG(result repoindex.DAGGrepResult, mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "tree":
		return renderTree(result)
	case "mermaid":
		return renderMermaid(result)
	default:
		return ""
	}
}

func renderTree(result repoindex.DAGGrepResult) string {
	if len(result.Graph.Nodes) == 0 {
		return ""
	}
	labels := make(map[string]string, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		labels[node.ID] = nodeLabel(node)
	}
	layerBuckets := make(map[int][]string)
	maxLayer := 0
	for id, layer := range result.DAG.Layers {
		layerBuckets[layer] = append(layerBuckets[layer], id)
		if layer > maxLayer {
			maxLayer = layer
		}
	}
	var b strings.Builder
	for layer := 0; layer <= maxLayer; layer++ {
		ids := layerBuckets[layer]
		if len(ids) == 0 {
			continue
		}
		sort.Strings(ids)
		b.WriteString(fmt.Sprintf("Layer %d:\n", layer))
		for _, id := range ids {
			label := labels[id]
			if label == "" {
				label = id
			}
			b.WriteString("  - ")
			b.WriteString(label)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func renderMermaid(result repoindex.DAGGrepResult) string {
	if len(result.DAG.Edges) == 0 {
		return ""
	}
	labels := make(map[string]string, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		labels[node.ID] = nodeLabel(node)
	}
	var b strings.Builder
	b.WriteString("graph TD\n")
	for _, edge := range result.DAG.Edges {
		srcLabel := labels[edge.Src]
		if srcLabel == "" {
			srcLabel = edge.Src
		}
		dstLabel := labels[edge.Dst]
		if dstLabel == "" {
			dstLabel = edge.Dst
		}
		b.WriteString(fmt.Sprintf("  \"%s\"[\"%s\"] --> \"%s\"[\"%s\"]\n",
			escapeMermaid(edge.Src), escapeMermaidLabel(srcLabel),
			escapeMermaid(edge.Dst), escapeMermaidLabel(dstLabel),
		))
	}
	return strings.TrimSpace(b.String())
}

func nodeLabel(node repoindex.Node) string {
	switch node.Kind {
	case repoindex.NodeSymbol:
		if node.Name != "" && node.File != "" {
			return fmt.Sprintf("%s (%s)", node.Name, node.File)
		}
		if node.Name != "" {
			return node.Name
		}
	case repoindex.NodeFile:
		if node.File != "" {
			return node.File
		}
	case repoindex.NodePackage:
		if node.Pkg != "" {
			return node.Pkg
		}
	case repoindex.NodeConcept:
		if node.Name != "" {
			return node.Name
		}
	}
	if node.ID != "" {
		return node.ID
	}
	return "unknown"
}

func escapeMermaid(value string) string {
	return strings.ReplaceAll(value, "\"", "'")
}

func escapeMermaidLabel(value string) string {
	value = strings.ReplaceAll(value, "\"", "'")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}

func trimDAGResult(result repoindex.DAGGrepResult, maxNodes, maxEdges int) repoindex.DAGGrepResult {
	if maxNodes <= 0 {
		return result
	}
	nodeByID := make(map[string]repoindex.Node, len(result.Graph.Nodes))
	for _, node := range result.Graph.Nodes {
		nodeByID[node.ID] = compactNode(node)
	}

	type layerItem struct {
		id    string
		layer int
	}
	items := make([]layerItem, 0, len(result.DAG.Layers))
	for id, layer := range result.DAG.Layers {
		items = append(items, layerItem{id: id, layer: layer})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].layer == items[j].layer {
			return items[i].id < items[j].id
		}
		return items[i].layer < items[j].layer
	})

	keep := make(map[string]struct{}, maxNodes)
	for _, item := range items {
		if len(keep) >= maxNodes {
			break
		}
		keep[item.id] = struct{}{}
	}

	nodes := make([]repoindex.Node, 0, len(keep))
	trimmedLayers := make(map[string]int, len(keep))
	for _, item := range items {
		if _, ok := keep[item.id]; !ok {
			continue
		}
		if node, ok := nodeByID[item.id]; ok {
			nodes = append(nodes, node)
			trimmedLayers[item.id] = item.layer
		}
	}

	edges := make([]repoindex.Edge, 0, len(result.Graph.Edges))
	for _, edge := range result.Graph.Edges {
		if _, ok := keep[edge.Src]; !ok {
			continue
		}
		if _, ok := keep[edge.Dst]; !ok {
			continue
		}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Src != edges[j].Src {
			return edges[i].Src < edges[j].Src
		}
		if edges[i].Dst != edges[j].Dst {
			return edges[i].Dst < edges[j].Dst
		}
		return string(edges[i].Type) < string(edges[j].Type)
	})
	if maxEdges > 0 && len(edges) > maxEdges {
		edges = edges[:maxEdges]
	}

	dagEdges := make([]repoindex.Edge, 0, len(result.DAG.Edges))
	for _, edge := range result.DAG.Edges {
		if _, ok := keep[edge.Src]; !ok {
			continue
		}
		if _, ok := keep[edge.Dst]; !ok {
			continue
		}
		dagEdges = append(dagEdges, edge)
	}
	sort.Slice(dagEdges, func(i, j int) bool {
		if dagEdges[i].Src != dagEdges[j].Src {
			return dagEdges[i].Src < dagEdges[j].Src
		}
		if dagEdges[i].Dst != dagEdges[j].Dst {
			return dagEdges[i].Dst < dagEdges[j].Dst
		}
		return string(dagEdges[i].Type) < string(dagEdges[j].Type)
	})

	backEdges := make([]repoindex.Edge, 0, len(result.DAG.BackEdges))
	for _, edge := range result.DAG.BackEdges {
		if _, ok := keep[edge.Src]; !ok {
			continue
		}
		if _, ok := keep[edge.Dst]; !ok {
			continue
		}
		backEdges = append(backEdges, edge)
	}
	sort.Slice(backEdges, func(i, j int) bool {
		if backEdges[i].Src != backEdges[j].Src {
			return backEdges[i].Src < backEdges[j].Src
		}
		if backEdges[i].Dst != backEdges[j].Dst {
			return backEdges[i].Dst < backEdges[j].Dst
		}
		return string(backEdges[i].Type) < string(backEdges[j].Type)
	})

	result.Graph.Nodes = nodes
	result.Graph.Edges = edges
	result.DAG.Layers = trimmedLayers
	result.DAG.Edges = dagEdges
	result.DAG.BackEdges = backEdges
	result.Stats.NodeCount = len(nodes)
	result.Stats.EdgeCount = len(edges)
	return result
}

func compactNode(node repoindex.Node) repoindex.Node {
	if node.Doc != "" {
		node.Doc = truncateText(node.Doc, previewDocLimit)
	}
	if node.Summary != "" {
		node.Summary = truncateText(node.Summary, previewSummaryLimit)
	}
	node.Meta = nil
	return node
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
}
