// Package tools provides dspy-go tool implementations that wrap agentctl skills.
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/tools"
	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/agent/types"
)

// Registry holds all registered agent tools.
type Registry struct {
	tools    *dstools.InMemoryToolRegistry
	recorder TelemetryRecorder
	config   ToolsConfig
}

// ToolsConfig configures tool behavior.
type ToolsConfig struct {
	// WorkspaceRoot is the root directory for file operations.
	WorkspaceRoot string

	// WorkspaceID is the workspace identifier for task/mail operations.
	WorkspaceID string

	// ActorID is the mailbox identity for this agent.
	ActorID string

	// MaxFileSize is the maximum bytes to read from a file.
	MaxFileSize int64

	// MaxSearchResults limits code search results.
	MaxSearchResults int
}

// TelemetryRecorder records tool usage for observability.
type TelemetryRecorder interface {
	RecordToolCall(call types.ToolCall)
}

// noopRecorder is a no-op telemetry recorder.
type noopRecorder struct{}

func (noopRecorder) RecordToolCall(types.ToolCall) {}

// NewRegistry creates a new tool registry with all V1 tools.
func NewRegistry(cfg ToolsConfig, recorder TelemetryRecorder) (*Registry, error) {
	if cfg.WorkspaceRoot == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		cfg.WorkspaceRoot = wd
	}

	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = 1024 * 1024 // 1MB default
	}

	if cfg.MaxSearchResults <= 0 {
		cfg.MaxSearchResults = 50
	}

	if recorder == nil {
		recorder = noopRecorder{}
	}

	r := &Registry{
		tools:    tools.NewInMemoryToolRegistry(),
		recorder: recorder,
		config:   cfg,
	}

	// Register all V1 tools
	if err := r.registerFSTools(); err != nil {
		return nil, err
	}
	if err := r.registerCodeTools(); err != nil {
		return nil, err
	}
	if err := r.registerEditTools(); err != nil {
		return nil, err
	}
	if err := r.registerTestTools(); err != nil {
		return nil, err
	}
	if err := r.registerTodoTools(); err != nil {
		return nil, err
	}
	if err := r.registerMailTools(); err != nil {
		return nil, err
	}

	return r, nil
}

// GetRegistry returns the underlying dspy-go tool registry.
func (r *Registry) GetRegistry() *dstools.InMemoryToolRegistry {
	return r.tools
}

// List returns all registered tools.
func (r *Registry) List() []*dstools.FuncTool {
	// Return tools as FuncTool slice
	allTools := r.tools.List()
	result := make([]*dstools.FuncTool, 0, len(allTools))
	for _, t := range allTools {
		if ft, ok := t.(*dstools.FuncTool); ok {
			result = append(result, ft)
		}
	}
	return result
}

// wrapWithTelemetry wraps a tool function to record telemetry.
func (r *Registry) wrapWithTelemetry(
	name string,
	fn func(ctx context.Context, args map[string]any) (*models.CallToolResult, error),
) dstools.ToolFunc {
	return func(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
		start := time.Now()
		result, err := fn(ctx, args)
		duration := time.Since(start)

		call := types.ToolCall{
			ToolName:  name,
			Args:      args,
			Result:    result,
			Duration:  duration,
			Timestamp: start,
		}
		if err != nil {
			call.Error = err.Error()
		}
		r.recorder.RecordToolCall(call)

		return result, err
	}
}

// resolvePath resolves a relative path to an absolute path within the workspace.
func (r *Registry) resolvePath(path string) (string, error) {
	// Handle absolute paths
	if filepath.IsAbs(path) {
		// Ensure it's within workspace
		rel, err := filepath.Rel(r.config.WorkspaceRoot, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path %q is outside workspace", path)
		}
		return path, nil
	}

	// Resolve relative path
	absPath := filepath.Join(r.config.WorkspaceRoot, path)

	// Verify it's still within workspace (handles .. attempts)
	rel, err := filepath.Rel(r.config.WorkspaceRoot, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q escapes workspace", path)
	}

	return absPath, nil
}
