package tools

import (
	"context"
	"errors"
	"fmt"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/skillrun"
)

// registerContextGrepTool registers the context_grep tool backed by the code/context_grep skill.
func (r *Registry) registerContextGrepTool() error {
	tool := tooling.NewFuncTool(
		"context_grep",
		"Search code with context expansion (ripgrep, ast-grep, or line expansion).",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"mode": {
					Type:        "string",
					Description: "Search mode: ripgrep, ast, or line (auto-detected if omitted)",
				},
				"path": {
					Type:        "string",
					Description: "Directory or file to search (default workspace root)",
				},
				"pattern": {
					Type:        "string",
					Description: "Regex pattern for ripgrep mode",
				},
				"pattern_mode": {
					Type:        "string",
					Description: "Pattern mode: regex or literal",
				},
				"case_insensitive": {
					Type:        "boolean",
					Description: "Case-insensitive search",
				},
				"glob": {
					Type:        "array",
					Description: "Include glob patterns",
				},
				"glob_not": {
					Type:        "array",
					Description: "Exclude glob patterns",
				},
				"ast_pattern": {
					Type:        "string",
					Description: "AST-grep pattern (ast mode)",
				},
				"ast_rule": {
					Type:        "string",
					Description: "AST-grep YAML rule (ast mode)",
				},
				"language": {
					Type:        "string",
					Description: "Language for AST-grep (go, ts, python, etc.)",
				},
				"file_path": {
					Type:        "string",
					Description: "File path for line expansion mode",
				},
				"line_start": {
					Type:        "integer",
					Description: "Start line for line expansion mode",
				},
				"line_end": {
					Type:        "integer",
					Description: "End line for line expansion mode",
				},
				"expand_to": {
					Type:        "string",
					Description: "Expansion target: function, block, or class",
				},
				"max_blocks": {
					Type:        "integer",
					Description: "Maximum number of code blocks to return",
				},
				"max_block_lines": {
					Type:        "integer",
					Description: "Maximum lines per block",
				},
				"max_blocks_per_file": {
					Type:        "integer",
					Description: "Maximum blocks per file",
				},
				"max_bytes_per_file": {
					Type:        "integer",
					Description: "Maximum bytes per file",
				},
			},
		},
		r.wrapWithTelemetry("context_grep", r.contextGrep),
	)
	if err := r.tools.Register(tool); err != nil {
		return fmt.Errorf("register context_grep: %w", err)
	}
	return nil
}

func (r *Registry) contextGrep(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))

	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload map[string]any
	_, err := skillrun.RunAndDecodeInto(ctx, resolver, "code/context_grep", args, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return errorResult(fmt.Sprintf("skill code/context_grep not found: %v (ensure skill is installed)", err)), nil
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
