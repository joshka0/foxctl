package rlm

import (
	"context"
	"encoding/json"
	"fmt"
)

type allowlistedToolExecutor struct {
	next  ToolExecutor
	tools map[string]Tool
}

func newAllowlistedToolExecutor(next ToolExecutor, tools []Tool) ToolExecutor {
	allowed := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		allowed[tool.Name] = tool
	}
	return allowlistedToolExecutor{
		next:  next,
		tools: allowed,
	}
}

func (e allowlistedToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error) {
	if e.next == nil {
		return nil, fmt.Errorf("rlm: tool executor is not configured")
	}
	tool, ok := e.tools[name]
	if !ok {
		return nil, fmt.Errorf("rlm: tool %q is not declared in environment", name)
	}
	if !tool.ReadOnly {
		return nil, fmt.Errorf("rlm: tool %q is not read-only", name)
	}
	return e.next.Execute(ctx, name, args)
}
