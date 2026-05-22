package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/inlineutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/platform/errors"
)

const Command = "repo/index_expand"

type Input struct {
	Seeds      []string `json:"seeds"`
	Workspace  string   `json:"workspace,omitempty"`
	InlineMode string   `json:"inline_mode,omitempty"`
	EdgeTypes  []string `json:"edge_types,omitempty"`
	Direction  string   `json:"direction,omitempty"`
	Depth      int      `json:"depth,omitempty"`
	Budget     int      `json:"budget,omitempty"`
	PerNodeCap int      `json:"per_node_cap,omitempty"`
	// Semantic anchor traversal is explicit; nil/default edge filters remain structural-only.
	IncludeSemanticAnchors bool `json:"include_semantic_anchors,omitempty"`
}

type Output struct {
	Result           repoindex.ExpandResult `json:"result"`
	Anchors          []repoquery.Anchor     `json:"anchors,omitempty"`
	Workspace        string                 `json:"workspace,omitempty"`
	InlineMode       string                 `json:"inline_mode,omitempty"`
	NodeCountTotal   int                    `json:"node_count_total,omitempty"`
	EdgeCountTotal   int                    `json:"edge_count_total,omitempty"`
	AnchorCountTotal int                    `json:"anchor_count_total,omitempty"`
	Truncated        bool                   `json:"truncated,omitempty"`
	Artifact         string                 `json:"artifact,omitempty"`
}

type InlineMode = inlineutil.Mode

const (
	InlineModeAuto         = inlineutil.ModeAuto
	InlineModeFull         = inlineutil.ModeFull
	InlineModePreview      = inlineutil.ModePreview
	InlineModeArtifactOnly = inlineutil.ModeArtifactOnly
)

const (
	defaultPreviewNodes   = 40
	defaultPreviewEdges   = 80
	defaultPreviewAnchors = 20
	previewDocLimit       = 240
	previewSummaryLimit   = 180
	previewTrailLimit     = 20
)

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if len(in.Seeds) == 0 {
		return skillerr.Arg("seeds are required")
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

	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))
	req, err := repoquery.NewExpandRequest(in.Seeds, in.EdgeTypes, in.Direction, in.Depth, in.Budget, in.PerNodeCap)
	if err != nil {
		return skillerr.Arg(err.Error())
	}
	req.IncludeSemanticAnchors = in.IncludeSemanticAnchors

	result, err := service.ExpandWithProjection(ctx, req)
	if err != nil {
		return skillerr.WrapIO("repo index expand", err)
	}

	return emitExpandOutput(ctx, rc, in, Output{
		Result:    result.Result,
		Anchors:   result.Anchors,
		Workspace: workspaceRoot,
	})
}

func resolveWorkspace(base, override string) (string, error) {
	workspace := strings.TrimSpace(override)
	if workspace == "" {
		workspace = base
	}
	if workspace == "" {
		return "", fmt.Errorf("workspace is required")
	}
	if !filepath.IsAbs(workspace) && base != "" {
		workspace = filepath.Join(base, workspace)
	}
	return filepath.Abs(workspace)
}

func parseInlineMode(value string) (InlineMode, error) {
	if mode, ok := inlineutil.Parse(value); ok {
		return mode, nil
	}
	return InlineModeAuto, skillerr.Arg("inline_mode must be one of: " + inlineutil.ValidModes)
}

func estimateExpandOutputSize(out Output) int {
	payload, err := json.Marshal(out)
	if err != nil {
		return 0
	}
	return len(payload)
}

func compactExpandNode(node repoindex.Node) repoindex.Node {
	if node.Doc != "" {
		node.Doc = truncateText(node.Doc, previewDocLimit)
	}
	if node.Summary != "" {
		node.Summary = truncateText(node.Summary, previewSummaryLimit)
	}
	node.Meta = nil
	return node
}

func compactAnchor(anchor repoquery.Anchor) repoquery.Anchor {
	if anchor.Summary != "" {
		anchor.Summary = truncateText(anchor.Summary, previewSummaryLimit)
	}
	return anchor
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "..."
}

func trimExpandResult(result repoindex.ExpandResult, anchors []repoquery.Anchor, maxNodes, maxEdges, maxAnchors int) (repoindex.ExpandResult, []repoquery.Anchor) {
	keep := map[string]struct{}{}
	nodes := make([]repoindex.Node, 0, min(maxNodes, len(result.Nodes)))
	for _, node := range result.Nodes {
		if maxNodes > 0 && len(nodes) >= maxNodes {
			break
		}
		nodes = append(nodes, compactExpandNode(node))
		keep[node.ID] = struct{}{}
	}

	edges := make([]repoindex.Edge, 0, min(maxEdges, len(result.Edges)))
	for _, edge := range result.Edges {
		if _, ok := keep[edge.Src]; !ok {
			continue
		}
		if _, ok := keep[edge.Dst]; !ok {
			continue
		}
		edges = append(edges, edge)
		if maxEdges > 0 && len(edges) >= maxEdges {
			break
		}
	}

	trimmedTrail := result.Trail
	if maxNodes > 0 && len(trimmedTrail) > previewTrailLimit {
		trimmedTrail = append([]string(nil), trimmedTrail[:previewTrailLimit]...)
	}

	trimmedAnchors := make([]repoquery.Anchor, 0, min(maxAnchors, len(anchors)))
	for _, anchor := range anchors {
		trimmedAnchors = append(trimmedAnchors, compactAnchor(anchor))
		if maxAnchors > 0 && len(trimmedAnchors) >= maxAnchors {
			break
		}
	}

	return repoindex.ExpandResult{
		Nodes: nodes,
		Edges: edges,
		Trail: trimmedTrail,
	}, trimmedAnchors
}

func shouldPreviewExpandOutput(rc *skillmain.RunContext, out Output) bool {
	if len(out.Result.Nodes) > defaultPreviewNodes || len(out.Result.Edges) > defaultPreviewEdges || len(out.Anchors) > defaultPreviewAnchors {
		return true
	}
	return rc != nil && rc.ShouldTruncate(estimateExpandOutputSize(out))
}

func emitExpandOutput(ctx context.Context, rc *skillmain.RunContext, in Input, out Output) error {
	mode, err := parseInlineMode(in.InlineMode)
	if err != nil {
		return err
	}
	out.NodeCountTotal = len(out.Result.Nodes)
	out.EdgeCountTotal = len(out.Result.Edges)
	out.AnchorCountTotal = len(out.Anchors)

	switch mode {
	case InlineModeFull:
		out.InlineMode = string(InlineModeFull)
		return skillout.Emit(rc, Command, out)
	case InlineModePreview, InlineModeArtifactOnly:
		artifact, err := skillmain.PersistJSON(ctx, rc, out, Command)
		if err != nil {
			return skillerr.WrapIO("persist output", err)
		}
		if mode == InlineModeArtifactOnly {
			return skillout.Emit(rc, Command, Output{
				Workspace:        out.Workspace,
				InlineMode:       string(InlineModeArtifactOnly),
				NodeCountTotal:   out.NodeCountTotal,
				EdgeCountTotal:   out.EdgeCountTotal,
				AnchorCountTotal: out.AnchorCountTotal,
				Truncated:        true,
				Artifact:         artifact.Digest,
			})
		}
		previewResult, previewAnchors := trimExpandResult(out.Result, out.Anchors, defaultPreviewNodes, defaultPreviewEdges, defaultPreviewAnchors)
		return skillout.Emit(rc, Command, Output{
			Result:           previewResult,
			Anchors:          previewAnchors,
			Workspace:        out.Workspace,
			InlineMode:       string(InlineModePreview),
			NodeCountTotal:   out.NodeCountTotal,
			EdgeCountTotal:   out.EdgeCountTotal,
			AnchorCountTotal: out.AnchorCountTotal,
			Truncated:        true,
			Artifact:         artifact.Digest,
		})
	default:
		if !shouldPreviewExpandOutput(rc, out) {
			out.InlineMode = string(InlineModeFull)
			return skillout.Emit(rc, Command, out)
		}
		artifact, err := skillmain.PersistJSON(ctx, rc, out, Command)
		if err != nil {
			return skillerr.WrapIO("persist output", err)
		}
		previewResult, previewAnchors := trimExpandResult(out.Result, out.Anchors, defaultPreviewNodes, defaultPreviewEdges, defaultPreviewAnchors)
		return skillout.Emit(rc, Command, Output{
			Result:           previewResult,
			Anchors:          previewAnchors,
			Workspace:        out.Workspace,
			InlineMode:       string(InlineModePreview),
			NodeCountTotal:   out.NodeCountTotal,
			EdgeCountTotal:   out.EdgeCountTotal,
			AnchorCountTotal: out.AnchorCountTotal,
			Truncated:        true,
			Artifact:         artifact.Digest,
		})
	}
}
