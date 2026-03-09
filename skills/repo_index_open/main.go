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

const Command = "repo/index_open"

type Input struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace,omitempty"`
}

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.ID) == "" {
		return skillerr.Arg("id is required")
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
	req, err := repoquery.NewOpenRequest(in.ID)
	if err != nil {
		return skillerr.Arg(err.Error())
	}

	result, err := service.OpenWithProjection(ctx, req)
	if err != nil {
		return skillerr.WrapIO("repo index open", err)
	}

	return skillout.Emit(rc, Command, map[string]any{
		"node":      result.Node,
		"anchor":    result.Anchor,
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
