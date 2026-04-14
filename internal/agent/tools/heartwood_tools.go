package tools

import (
	"context"
	"errors"
	"fmt"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	tooling "github.com/jkatigb/agentctl/internal/tooling"
	"github.com/jkatigb/agentctl/internal/tooling/skillrun"
)

func (r *Registry) registerHeartwoodTools() error {
	stateTool := tooling.NewFuncTool(
		"heartwood.state",
		"Fetch compact Heartwood participant state through the generated SpacetimeDB client.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"heartwood_root":  {Type: "string", Description: "Path to the Heartwood repo"},
				"host":            {Type: "string", Description: "WebSocket host, e.g. ws://127.0.0.1:3001", Required: true},
				"db_name":         {Type: "string", Description: "Heartwood database name", Required: true},
				"token":           {Type: "string", Description: "Optional SpacetimeDB token"},
				"token_path":      {Type: "string", Description: "Optional token file path"},
				"wait_timeout_ms": {Type: "integer", Description: "Connection/subscription timeout in milliseconds"},
				"message_limit":   {Type: "integer", Description: "Recent message limit"},
			},
		},
		r.wrapWithTelemetry("heartwood.state", r.heartwoodState),
	)
	if err := r.tools.Register(stateTool); err != nil {
		return fmt.Errorf("register heartwood.state: %w", err)
	}

	actionTool := tooling.NewFuncTool(
		"heartwood.action",
		"Execute a whitelisted Heartwood participant action through the generated SpacetimeDB client.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"heartwood_root":  {Type: "string", Description: "Path to the Heartwood repo"},
				"host":            {Type: "string", Description: "WebSocket host, e.g. ws://127.0.0.1:3001", Required: true},
				"db_name":         {Type: "string", Description: "Heartwood database name", Required: true},
				"token":           {Type: "string", Description: "Optional SpacetimeDB token"},
				"token_path":      {Type: "string", Description: "Optional token file path"},
				"wait_timeout_ms": {Type: "integer", Description: "Connection timeout in milliseconds"},
				"operation":       {Type: "string", Description: "Heartwood action name", Required: true},
				"args":            {Type: "object", Description: "Action arguments"},
			},
		},
		r.wrapWithTelemetry("heartwood.action", r.heartwoodAction),
	)
	if err := r.tools.Register(actionTool); err != nil {
		return fmt.Errorf("register heartwood.action: %w", err)
	}
	return nil
}

func (r *Registry) heartwoodState(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	return r.runHeartwoodSkill(ctx, "heartwood/state", args)
}

func (r *Registry) heartwoodAction(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	return r.runHeartwoodSkill(ctx, "heartwood/action", args)
}

func (r *Registry) runHeartwoodSkill(ctx context.Context, skillName string, args map[string]any) (*models.CallToolResult, error) {
	resolver := skill.NewResolver(skill.WithSearchPaths(
		r.config.WorkspaceRoot+"/dist/skills",
		r.config.WorkspaceRoot+"/skills",
	))
	ctx = workspace.WithContext(ctx, r.config.WorkspaceRoot)

	var payload map[string]any
	_, err := skillrun.RunAndDecodeInto(ctx, resolver, skillName, args, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: r.config.WorkspaceRoot,
	}, &payload)
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return errorResult(fmt.Sprintf("skill %s not found: %v (ensure skill is installed)", skillName, err)), nil
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
