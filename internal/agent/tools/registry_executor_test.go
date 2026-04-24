package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/tooling"
)

func TestRegistryToolExecutorCanonicalizesFullyDottedDAGGrep(t *testing.T) {
	t.Parallel()

	registry := &Registry{tools: tooling.NewInMemoryToolRegistry()}
	tool := tooling.NewFuncTool(
		repoindex.ToolDAGGrep,
		"test dag grep",
		models.InputSchema{Type: "object"},
		func(context.Context, map[string]any) (*models.CallToolResult, error) {
			return successResult(map[string]any{"ok": true}), nil
		},
	)
	if err := registry.tools.Register(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	executor := NewRegistryToolExecutor(registry)
	result, err := executor.Execute(context.Background(), "repo.index.dag.grep", json.RawMessage(`{"query":"x"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result, `"ok":true`) {
		t.Fatalf("result=%q", result)
	}
}

func TestRegistryToolExecutorFallsBackToRegisteredLegacyTool(t *testing.T) {
	t.Parallel()

	registry := &Registry{tools: tooling.NewInMemoryToolRegistry()}
	tool := tooling.NewFuncTool(
		"fs.read_file",
		"test read file",
		models.InputSchema{Type: "object"},
		func(context.Context, map[string]any) (*models.CallToolResult, error) {
			return successResult(map[string]any{"path": "ok"}), nil
		},
	)
	if err := registry.tools.Register(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	executor := NewRegistryToolExecutor(registry)
	result, err := executor.Execute(context.Background(), "fs_read_file", json.RawMessage(`{"path":"x"}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result, `"path":"ok"`) {
		t.Fatalf("result=%q", result)
	}
}
