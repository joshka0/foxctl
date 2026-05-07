package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/protocol"
)

const Command = "repo/index_enrich_summaries"

type Input struct {
	Workspace string `json:"workspace,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	workspaceRoot, err := resolveWorkspace(rc.Workspace, in.Workspace)
	if err != nil {
		return skillerr.WrapIO("resolve workspace", err)
	}

	result := executil.Run(ctx, workspaceRoot, executil.FoxctlBin(), buildEnrichSummariesArgs(workspaceRoot, in)...)
	if strings.TrimSpace(string(result.Stdout)) != "" {
		data, decodeErr := protocol.DecodeEnvelopeData(result.Stdout)
		if decodeErr != nil {
			return skillerr.WrapRuntime("repo index enrich summaries", decodeErr)
		}
		if result.Err != nil {
			data["subprocess_error"] = result.Err.Error()
		}
		if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
			data["stderr"] = stderr
		}
		return skillout.Emit(rc, Command, data)
	}
	if result.Err != nil {
		return skillerr.WrapRuntime("repo index enrich summaries", fmt.Errorf("%w: %s", result.Err, strings.TrimSpace(string(result.Stderr))))
	}
	return skillerr.WrapRuntime("repo index enrich summaries", fmt.Errorf("foxctl index repo enrich summaries produced no envelope"))
}

func resolveWorkspace(base, override string) (string, error) {
	workspace := strings.TrimSpace(override)
	if workspace == "" {
		workspace = base
	}
	if workspace == "" {
		workspace = "."
	}
	if !filepath.IsAbs(workspace) && base != "" {
		workspace = filepath.Join(base, workspace)
	}
	return filepath.Abs(workspace)
}

func buildEnrichSummariesArgs(workspaceRoot string, in Input) []string {
	return []string{
		"index",
		"repo",
		"enrich",
		"summaries",
		"--workspace",
		workspaceRoot,
		fmt.Sprintf("--dry-run=%t", in.DryRun),
	}
}
