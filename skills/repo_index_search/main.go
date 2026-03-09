package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/repoquery"
)

const Command = "repo/index_search"

type Input struct {
	Query     string `json:"query"`
	Workspace string `json:"workspace,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

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

	return skillout.Emit(rc, Command, map[string]any{
		"count":     len(result.Nodes),
		"results":   result.Nodes,
		"anchors":   result.Anchors,
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
