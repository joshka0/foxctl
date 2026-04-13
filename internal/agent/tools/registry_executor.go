package tools

import (
	"context"
	"encoding/json"
	"fmt"

	mcpmodels "github.com/XiaoConstantine/mcp-go/pkg/model"

	"github.com/jkatigb/agentctl/internal/agent/toolnames"
	"github.com/jkatigb/agentctl/internal/runtime/engine"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
)

// RegistryToolExecutor adapts a tools.Registry to the engine.ToolExecutor interface.
type RegistryToolExecutor struct {
	registry *Registry
}

// NewRegistryToolExecutor creates a ToolExecutor backed by a tools.Registry.
func NewRegistryToolExecutor(registry *Registry) *RegistryToolExecutor {
	return &RegistryToolExecutor{registry: registry}
}

// Execute implements engine.ToolExecutor.
func (r *RegistryToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if r == nil || r.registry == nil {
		return "", fmt.Errorf("tool registry not configured")
	}

	if canonical, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeLegacy, name); ok {
		name = canonical
	}

	switch name {
	case repoindex.ToolSearchLegacy:
		name = repoindex.ToolSearch
	case repoindex.ToolExpandLegacy:
		name = repoindex.ToolExpand
	case repoindex.ToolOpenLegacy:
		name = repoindex.ToolOpen
	case repoindex.ToolDAGGrepLegacy:
		name = repoindex.ToolDAGGrep
	}

	coreTool, err := r.registry.Get(name)
	if err != nil {
		return "", fmt.Errorf("tool %q not found: %w", name, err)
	}

	var argsMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return "", fmt.Errorf("parse tool args: %w", err)
		}
	}

	ctx = WithHookDispatch(ctx)
	result, err := coreTool.Call(ctx, argsMap)
	if err != nil {
		return "", err
	}
	if result == nil || len(result.Content) == 0 {
		return "", nil
	}

	for _, content := range result.Content {
		if tc, ok := content.(mcpmodels.TextContent); ok {
			return tc.Text, nil
		}
	}

	b, err := json.Marshal(result.Content)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(b), nil
}

// List implements engine.ToolExecutor.
func (r *RegistryToolExecutor) List() []engine.ToolDef {
	if r == nil || r.registry == nil {
		return nil
	}
	tools := r.registry.List()
	defs := make([]engine.ToolDef, len(tools))
	for i, t := range tools {
		schema, _ := json.Marshal(t.InputSchema())
		name := t.Name()
		if canonical, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeRuntime, name); ok {
			name = canonical
		}
		defs[i] = engine.ToolDef{
			Name:        name,
			Description: t.Description(),
			Parameters:  schema,
		}
	}
	return defs
}
