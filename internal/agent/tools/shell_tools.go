package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	tooling "github.com/jkatigb/agentctl/internal/tooling"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/tooling/shellreduce"
	"github.com/jkatigb/agentctl/internal/tooling/skillrun"
)

func (r *Registry) registerShellTool() error {
	tool := tooling.NewFuncTool(
		"shell",
		"Route supported shell-style commands through structured reducers. Supported families: ls, tree, find, cat/read, grep/rg, git status/diff/log, go/cargo test, pytest, npm/pnpm/yarn test, ruff check, and docker ps. This is not an arbitrary shell executor.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"command": {
					Type:        "string",
					Description: "Supported shell-style command string, e.g. 'git log --stat -5' or 'grep -rn \"spawn\" internal/agent'",
				},
				"argv": {
					Type:        "array",
					Description: "Optional argv form instead of a command string",
				},
				"measure_raw": {
					Type:        "boolean",
					Description: "Measure raw command output bytes and token estimates against the reduced summary",
				},
				"token_model": {
					Type:        "string",
					Description: "Tokenizer model or encoding for measurement (default cl100k_base)",
				},
			},
		},
		r.wrapWithTelemetry("shell", r.structuredShell),
	)
	if err := r.tools.Register(tool); err != nil {
		return fmt.Errorf("register shell: %w", err)
	}
	return nil
}

func (r *Registry) structuredShell(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	argv, command, err := shellArgs(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	route, err := shellreduce.RouteArgv(argv)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	payload, err := r.runStructuredShellRoute(ctx, route)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	result := map[string]any{
		"input": map[string]any{
			"argv":      argv,
			"command":   command,
			"workspace": r.config.WorkspaceRoot,
		},
		"route": map[string]any{
			"intent": route.Intent,
			"skill":  route.Skill,
			"native": route.Native,
			"notes":  route.Notes,
		},
		"summary": shellreduce.Summarize(route, payload),
		"result":  payload,
	}
	if measure, _ := args["measure_raw"].(bool); measure {
		result["measure"] = shellreduce.Measure(ctx, r.config.WorkspaceRoot, argv, stringValueFromResult(result["summary"]), shellreduce.MeasureOptions{
			TokenModel: strings.TrimSpace(stringArgFromResult(args["token_model"])),
		})
	}
	if artifact := shellArtifact(payload); artifact != "" {
		result["result_artifact"] = artifact
	}
	return successResult(result), nil
}

func shellArgs(args map[string]any) ([]string, string, error) {
	command, _ := args["command"].(string)
	command = strings.TrimSpace(command)
	argv := shellStringSlice(args["argv"])
	if command != "" && len(argv) > 0 {
		return nil, "", fmt.Errorf("use either command or argv, not both")
	}
	if command != "" {
		parsed, err := shellreduce.SplitCommand(command)
		if err != nil {
			return nil, "", err
		}
		return parsed, shellreduce.JoinCommand(parsed), nil
	}
	if len(argv) == 0 {
		return nil, "", fmt.Errorf("command is required")
	}
	return argv, shellreduce.JoinCommand(argv), nil
}

func shellStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func (r *Registry) runStructuredShellRoute(ctx context.Context, route shellreduce.Route) (map[string]any, error) {
	if strings.TrimSpace(route.Native) != "" {
		return shellreduce.ExecuteNative(ctx, r.config.WorkspaceRoot, route)
	}

	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))

	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload map[string]any
	_, err := skillrun.RunAndDecodeInto(ctx, resolver, route.Skill, route.Input, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return nil, fmt.Errorf("skill %s not found: %v (ensure skill artifacts are built)", route.Skill, err)
		}
		var runErr skillrun.RunError
		if errors.As(err, &runErr) {
			errMsg := fmt.Sprintf("skill %s failed: %v", route.Skill, runErr.Err)
			if len(runErr.Stderr) > 0 {
				errMsg += fmt.Sprintf(" (stderr: %s)", string(runErr.Stderr))
			}
			return nil, errors.New(errMsg)
		}
		return nil, fmt.Errorf("skill %s response error: %v", route.Skill, err)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

func shellArtifact(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if artifact, ok := payload["artifact"].(string); ok {
		return strings.TrimSpace(artifact)
	}
	return ""
}

func stringArgFromResult(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func stringValueFromResult(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
