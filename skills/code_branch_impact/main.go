// Package main implements the code/branch_impact skill.
package main

import (
	"context"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/intelligence/branchimpact"
)

const Command = "code/branch_impact"

type Input = branchimpact.Input

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	workspaceRoot, err := workspaceutil.ResolvePath(rc.Workspace, in.Workspace)
	if err != nil {
		return skillerr.WrapArg("resolve workspace", err)
	}
	in.Workspace = workspaceRoot

	diff := gitDiffProvider{workspace: workspaceRoot}
	graph, err := openGraphProvider(ctx, rc, workspaceRoot)
	if err != nil {
		return skillerr.WrapIO("open repoindex", err)
	}
	if graph != nil {
		defer graph.close()
	}
	semanticProvider, closeSemantic := openSemanticProvider(ctx, rc.Config, workspaceRoot)
	defer closeSemantic()

	out, err := branchimpact.Analyze(ctx, in, branchimpact.Providers{
		Diff:     diff,
		Graph:    graph,
		Semantic: semanticProvider,
	})
	if err != nil {
		return skillerr.WrapRuntime("analyze branch impact", err)
	}
	return skillout.Emit(rc, Command, out)
}
