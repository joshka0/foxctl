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

const Command = "repo/index_build"

type Input struct {
	Workspace              string   `json:"workspace,omitempty"`
	GoPattern              []string `json:"go_pattern,omitempty"`
	IncludeGo              *bool    `json:"include_go,omitempty"`
	IncludePython          bool     `json:"include_python,omitempty"`
	IncludeRust            bool     `json:"include_rust,omitempty"`
	IncludeCSharp          bool     `json:"include_csharp,omitempty"`
	IncludeTypescript      *bool    `json:"include_typescript,omitempty"`
	IncludeElixir          bool     `json:"include_elixir,omitempty"`
	IncludeTerraform       bool     `json:"include_terraform,omitempty"`
	IncludeKubernetes      bool     `json:"include_kubernetes,omitempty"`
	IncludeShell           bool     `json:"include_shell,omitempty"`
	IncludeTests           bool     `json:"include_tests,omitempty"`
	IncludeSemanticAnchors bool     `json:"include_semantic_anchors,omitempty"`
	IncludeCoChange        bool     `json:"include_cochange,omitempty"`
	DryRun                 bool     `json:"dry_run,omitempty"`
	Progress               *bool    `json:"progress,omitempty"`
	Incremental            *bool    `json:"incremental,omitempty"`
}

func main() {
	skillmain.Main(Command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	workspaceRoot, err := resolveWorkspace(rc.Workspace, in.Workspace)
	if err != nil {
		return skillerr.WrapIO("resolve workspace", err)
	}

	result := executil.Run(ctx, workspaceRoot, executil.FoxctlBin(), buildRepoIndexArgs(workspaceRoot, in)...)
	if strings.TrimSpace(string(result.Stdout)) != "" {
		data, decodeErr := protocol.DecodeEnvelopeData(result.Stdout)
		if decodeErr != nil {
			return skillerr.WrapRuntime("repo index build", decodeErr)
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
		return skillerr.WrapRuntime("repo index build", fmt.Errorf("%w: %s", result.Err, strings.TrimSpace(string(result.Stderr))))
	}
	return skillerr.WrapRuntime("repo index build", fmt.Errorf("foxctl index repo build produced no envelope"))
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

func buildRepoIndexArgs(workspaceRoot string, in Input) []string {
	patterns := in.GoPattern
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	args := []string{"index", "repo", "build", "--workspace", workspaceRoot}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			args = append(args, "--go-pattern", pattern)
		}
	}

	args = append(args,
		fmt.Sprintf("--go=%t", boolDefault(in.IncludeGo, true)),
		fmt.Sprintf("--python=%t", in.IncludePython),
		fmt.Sprintf("--rust=%t", in.IncludeRust),
		fmt.Sprintf("--csharp=%t", in.IncludeCSharp),
		fmt.Sprintf("--typescript=%t", boolDefault(in.IncludeTypescript, true)),
		fmt.Sprintf("--elixir=%t", in.IncludeElixir),
		fmt.Sprintf("--terraform=%t", in.IncludeTerraform),
		fmt.Sprintf("--kubernetes=%t", in.IncludeKubernetes),
		fmt.Sprintf("--shell=%t", in.IncludeShell),
		fmt.Sprintf("--include-tests=%t", in.IncludeTests),
		fmt.Sprintf("--semantic-anchors=%t", in.IncludeSemanticAnchors),
		fmt.Sprintf("--cochange=%t", in.IncludeCoChange),
		fmt.Sprintf("--dry-run=%t", in.DryRun),
		fmt.Sprintf("--progress=%t", boolDefault(in.Progress, false)),
		fmt.Sprintf("--incremental=%t", boolDefault(in.Incremental, true)),
	)
	return args
}

func boolDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
