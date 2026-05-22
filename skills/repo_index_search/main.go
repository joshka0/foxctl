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

const Command = "repo/index_search"

type Input struct {
	Query      string `json:"query"`
	Workspace  string `json:"workspace,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	InlineMode string `json:"inline_mode,omitempty"`
}

type Output struct {
	Count        int                `json:"count"`
	Results      []repoindex.Node   `json:"results"`
	Anchors      []repoquery.Anchor `json:"anchors,omitempty"`
	Workspace    string             `json:"workspace,omitempty"`
	InlineMode   string             `json:"inline_mode,omitempty"`
	ResultsTotal int                `json:"results_total,omitempty"`
	AnchorsTotal int                `json:"anchors_total,omitempty"`
	Truncated    bool               `json:"truncated,omitempty"`
	Artifact     string             `json:"artifact,omitempty"`
}

type InlineMode = inlineutil.Mode

const (
	InlineModeAuto         = inlineutil.ModeAuto
	InlineModeFull         = inlineutil.ModeFull
	InlineModePreview      = inlineutil.ModePreview
	InlineModeArtifactOnly = inlineutil.ModeArtifactOnly
	defaultPreviewResults  = 20
	defaultPreviewAnchors  = 12
	previewDocLimit        = 240
	previewSummaryLimit    = 180
)

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Query) == "" {
		return skillerr.Arg("query is required", skillerr.WithHint("provide a non-empty query string, for example \"auth\" or \"repo index dag grep\""))
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
	req, err := repoquery.NewSearchRequest(in.Query, in.Limit)
	if err != nil {
		return skillerr.Arg(err.Error())
	}

	result, err := service.SearchWithProjection(ctx, req)
	if err != nil {
		return skillerr.WrapIO("repo index search", err)
	}

	return emitSearchOutput(ctx, rc, in, Output{
		Count:     len(result.Nodes),
		Results:   result.Nodes,
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

func compactSearchNode(node repoindex.Node) repoindex.Node {
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

func estimateSearchOutputSize(out Output) int {
	payload, err := json.Marshal(out)
	if err != nil {
		return 0
	}
	return len(payload)
}

func trimSearchOutput(out Output) Output {
	preview := out
	if len(preview.Results) > defaultPreviewResults {
		preview.Results = append([]repoindex.Node(nil), preview.Results[:defaultPreviewResults]...)
	}
	for i := range preview.Results {
		preview.Results[i] = compactSearchNode(preview.Results[i])
	}
	if len(preview.Anchors) > defaultPreviewAnchors {
		preview.Anchors = append([]repoquery.Anchor(nil), preview.Anchors[:defaultPreviewAnchors]...)
	}
	for i := range preview.Anchors {
		preview.Anchors[i] = compactAnchor(preview.Anchors[i])
	}
	preview.InlineMode = string(InlineModePreview)
	preview.ResultsTotal = out.Count
	preview.AnchorsTotal = len(out.Anchors)
	preview.Truncated = len(preview.Results) < out.Count || len(preview.Anchors) < len(out.Anchors)
	return preview
}

func shouldPreviewSearchOutput(rc *skillmain.RunContext, out Output) bool {
	if len(out.Results) > defaultPreviewResults || len(out.Anchors) > defaultPreviewAnchors {
		return true
	}
	return rc != nil && rc.ShouldTruncate(estimateSearchOutputSize(out))
}

func emitSearchOutput(ctx context.Context, rc *skillmain.RunContext, in Input, out Output) error {
	mode, err := parseInlineMode(in.InlineMode)
	if err != nil {
		return err
	}
	out.ResultsTotal = out.Count
	out.AnchorsTotal = len(out.Anchors)

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
				Count:        out.Count,
				Workspace:    out.Workspace,
				InlineMode:   string(InlineModeArtifactOnly),
				ResultsTotal: out.ResultsTotal,
				AnchorsTotal: out.AnchorsTotal,
				Truncated:    true,
				Artifact:     artifact.Digest,
			})
		}
		preview := trimSearchOutput(out)
		preview.Artifact = artifact.Digest
		return skillout.Emit(rc, Command, preview)
	default:
		if !shouldPreviewSearchOutput(rc, out) {
			out.InlineMode = string(InlineModeFull)
			return skillout.Emit(rc, Command, out)
		}
		artifact, err := skillmain.PersistJSON(ctx, rc, out, Command)
		if err != nil {
			return skillerr.WrapIO("persist output", err)
		}
		preview := trimSearchOutput(out)
		preview.Artifact = artifact.Digest
		return skillout.Emit(rc, Command, preview)
	}
}
