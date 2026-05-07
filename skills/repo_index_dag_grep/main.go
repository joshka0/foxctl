package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/repoquery"
	"github.com/joshka0/foxctl/internal/platform/errors"
)

const Command = "repo/index_dag_grep"

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
	// IncludeOwnerContainers is the explicit replacement for legacy IncludeAnchors.
	IncludeOwnerContainers *bool `json:"include_owner_containers,omitempty"`
	// Semantic anchor traversal is explicit; legacy IncludeAnchors never enables it.
	IncludeSemanticAnchors bool   `json:"include_semantic_anchors,omitempty"`
	Render                 string `json:"render,omitempty"`
}

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Query) == "" {
		return skillerr.Arg("query is required", skillerr.WithHint("provide a non-empty query string, for example \"buildEvidencePack\" or \"repo index dag grep\""))
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
	req, err := repoquery.NewDAGGrepRequest(in.Query, in.Mode, in.K, in.NodeKinds, in.EdgeSets, in.EdgeTypes, in.Direction, in.Depth, in.Budget, in.PerNodeCap, in.IncludeAnchors, in.Render)
	if err != nil {
		return skillerr.Arg(err.Error())
	}
	if in.IncludeOwnerContainers != nil {
		req.IncludeOwnerContainers = *in.IncludeOwnerContainers
	}
	req.IncludeSemanticAnchors = in.IncludeSemanticAnchors

	result, err := service.DAGGrepWithProjection(ctx, req)
	if err != nil {
		return skillerr.WrapIO("repo index dag_grep", err)
	}

	return skillout.Emit(rc, Command, map[string]any{
		"result":    result.Result,
		"anchors":   result.Anchors,
		"rendered":  result.Rendered,
		"workspace": workspaceRoot,
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
