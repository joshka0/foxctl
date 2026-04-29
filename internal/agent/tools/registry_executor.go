package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpmodels "github.com/XiaoConstantine/mcp-go/pkg/model"

	"github.com/joshka0/foxctl/internal/agent/toolnames"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/runtime/engine"
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

	_, coreTool, err := r.resolveTool(name)
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

func (r *RegistryToolExecutor) resolveTool(name string) (string, Tool, error) {
	candidates := []string{name}
	if canonical, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeRuntime, name); ok {
		candidates = append(candidates, canonical)
		candidates = append(candidates, runtimeToNamespaceDottedToolName(canonical))
		candidates = append(candidates, runtimeToLegacyToolName(canonical))
	}
	if canonical, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeLegacy, name); ok {
		candidates = append(candidates, canonical)
		candidates = append(candidates, legacyRepoIndexToolName(canonical))
	}

	var lastErr error
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		coreTool, err := r.registry.Get(candidate)
		if err == nil {
			return candidate, coreTool, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("tool name is required")
	}
	return "", nil, lastErr
}

func runtimeToLegacyToolName(name string) string {
	if canonical, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeLegacy, name); ok {
		return canonical
	}
	return ""
}

func runtimeToNamespaceDottedToolName(name string) string {
	if n := strings.IndexByte(name, '_'); n >= 0 {
		return name[:n] + "." + name[n+1:]
	}
	return ""
}

func legacyRepoIndexToolName(name string) string {
	switch name {
	case repoindex.ToolSearchLegacy:
		return repoindex.ToolSearch
	case repoindex.ToolExpandLegacy:
		return repoindex.ToolExpand
	case repoindex.ToolOpenLegacy:
		return repoindex.ToolOpen
	case repoindex.ToolDAGGrepLegacy:
		return repoindex.ToolDAGGrep
	default:
		return ""
	}
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
