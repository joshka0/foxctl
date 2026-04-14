package tools

import (
	"context"
	"errors"
	"fmt"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/joshka0/foxctl/internal/tooling"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/tooling/skillrun"
)

func (r *Registry) registerRefactorScoutTool() error {
	tool := tooling.NewFuncTool(
		"refactor_scout",
		"Rank likely single-language refactor hotspots and entrypoints. Prefer this first for refactor-entrypoint questions.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "File or directory to analyze (default workspace root)",
				},
				"language": {
					Type:        "string",
					Description: "Single language to analyze: go, python, javascript, typescript, or elixir",
					Required:    true,
				},
				"min_score": {
					Type:        "integer",
					Description: "Minimum finding score (default 50)",
				},
				"max_results": {
					Type:        "integer",
					Description: "Maximum findings to return (default 100)",
				},
				"rule_set": {
					Type:        "string",
					Description: "Threshold profile: conservative, default, or aggressive",
				},
			},
		},
		r.wrapWithTelemetry("refactor_scout", r.refactorScout),
	)
	if err := r.tools.Register(tool); err != nil {
		return fmt.Errorf("register refactor_scout: %w", err)
	}
	return nil
}

func (r *Registry) refactorScout(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))

	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload map[string]any
	_, err := skillrun.RunAndDecodeInto(ctx, resolver, "code/refactor_scout", args, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return errorResult(fmt.Sprintf("skill code/refactor_scout not found: %v (ensure skill is installed)", err)), nil
		}
		var runErr skillrun.RunError
		if errors.As(err, &runErr) {
			errMsg := fmt.Sprintf("skill execution failed: %v", runErr.Err)
			if len(runErr.Stderr) > 0 {
				errMsg += fmt.Sprintf(" (stderr: %s)", string(runErr.Stderr))
			}
			return errorResult(errMsg), nil
		}
		return errorResult(fmt.Sprintf("skill response error: %v", err)), nil
	}

	if payload == nil {
		payload = map[string]any{}
	}
	return successResult(payload), nil
}
