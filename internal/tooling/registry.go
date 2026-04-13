// Package tooling provides generic callable-tool substrate shared across
// skills, codemap tooling, and other runtime-neutral workflows. It is not the
// home for runtime-facing agent tool wrappers.
package tooling

import (
	"context"
	"errors"
	"sort"
	"sync"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// Tool defines the interface for callable tools with schemas.
type Tool interface {
	Name() string
	Description() string
	InputSchema() models.InputSchema
	Call(ctx context.Context, args map[string]any) (*models.CallToolResult, error)
}

// ToolFunc is the function signature for tool implementations.
type ToolFunc func(ctx context.Context, args map[string]any) (*models.CallToolResult, error)

// FuncTool is a simple in-memory tool implementation.
type FuncTool struct {
	name        string
	description string
	schema      models.InputSchema
	fn          ToolFunc
}

// NewFuncTool creates a new functional tool.
func NewFuncTool(name, description string, schema models.InputSchema, fn ToolFunc) *FuncTool {
	return &FuncTool{
		name:        name,
		description: description,
		schema:      schema,
		fn:          fn,
	}
}

// Name returns the tool name.
func (t *FuncTool) Name() string {
	if t == nil {
		return ""
	}
	return t.name
}

// Description returns the tool description.
func (t *FuncTool) Description() string {
	if t == nil {
		return ""
	}
	return t.description
}

// InputSchema returns the tool input schema.
func (t *FuncTool) InputSchema() models.InputSchema {
	if t == nil {
		return models.InputSchema{}
	}
	return t.schema
}

// Call executes the tool function.
func (t *FuncTool) Call(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if t == nil || t.fn == nil {
		return nil, errors.New("tool function not configured")
	}
	return t.fn(ctx, args)
}

// InMemoryToolRegistry stores tools in memory with basic lookup.
type InMemoryToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewInMemoryToolRegistry creates an empty tool registry.
func NewInMemoryToolRegistry() *InMemoryToolRegistry {
	return &InMemoryToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry.
func (r *InMemoryToolRegistry) Register(tool Tool) error {
	if r == nil {
		return errors.New("tool registry not configured")
	}
	if tool == nil || tool.Name() == "" {
		return errors.New("tool name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name()]; exists {
		return errors.New("tool already registered: " + tool.Name())
	}
	r.tools[tool.Name()] = tool
	return nil
}

// Get returns a tool by name.
func (r *InMemoryToolRegistry) Get(name string) (Tool, error) {
	if r == nil {
		return nil, errors.New("tool registry not configured")
	}
	if name == "" {
		return nil, errors.New("tool name is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	if !ok {
		return nil, errors.New("tool not found: " + name)
	}
	return tool, nil
}

// List returns all tools in deterministic order.
func (r *InMemoryToolRegistry) List() []Tool {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tools) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.tools))
	for name := range r.tools {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	tools := make([]Tool, 0, len(keys))
	for _, name := range keys {
		tools = append(tools, r.tools[name])
	}
	return tools
}
