// Package main implements the code/refactor_impact skill.
package main

import (
	"context"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/intelligence/refactor/impact"
)

const Command = "code/refactor_impact"

type Input = impact.Input

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	workspaceRoot, err := workspaceutil.ResolvePath(rc.Workspace, in.Workspace)
	if err != nil {
		return skillerr.WrapArg("resolve workspace", err)
	}
	in.Workspace = workspaceRoot

	structural, closeStructural := openStructuralProvider(ctx, rc, workspaceRoot)
	defer closeStructural()
	semanticProvider, closeSemantic := openSemanticProvider(ctx, rc.Config, workspaceRoot)
	defer closeSemantic()

	packet, err := impact.Analyze(ctx, in, impact.Providers{
		Diff:       gitDiffProvider{workspace: workspaceRoot},
		Structural: structural,
		Semantic:   semanticProvider,
	})
	if err != nil {
		return skillerr.WrapRuntime("analyze refactor impact", err)
	}
	return skillout.EmitWithCAS(ctx, rc, Command, packet)
}
