// Package tools provides dspy-go tool implementations that wrap agentctl skills.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/policy"
)

// Registry holds all registered agent tools.
type Registry struct {
	tools         *dstools.InMemoryToolRegistry
	recorder      TelemetryRecorder
	config        Config
	pathValidator *policy.PathValidator
}

// Config configures tool behavior.
type Config struct {
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

	// AllowedRoots are additional directories outside workspace that paths can resolve to.
	AllowedRoots []string
}

// TelemetryRecorder records tool usage for observability.
type TelemetryRecorder interface {
	RecordToolCall(call types.ToolCall)
}

// noopRecorder is a no-op telemetry recorder.
type noopRecorder struct{}

func (noopRecorder) RecordToolCall(types.ToolCall) {}

// NewRegistry creates a new tool registry with all V1 tools.
func NewRegistry(cfg Config, recorder TelemetryRecorder) (*Registry, error) {
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

	// Initialize PathValidator for secure path resolution
	pathValidator, err := policy.NewPathValidator(cfg.WorkspaceRoot, cfg.AllowedRoots)
	if err != nil {
		return nil, fmt.Errorf("init path validator: %w", err)
	}

	r := &Registry{
		tools:         dstools.NewInMemoryToolRegistry(),
		recorder:      recorder,
		config:        cfg,
		pathValidator: pathValidator,
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
			Duration:  types.Duration(duration),
			Timestamp: start,
		}
		if err != nil {
			call.Error = err.Error()
		}
		r.recorder.RecordToolCall(call)

		return result, err
	}
}

// resolvePath resolves a user-provided path using the PathValidator.
// It ensures paths are safe and within the workspace or allowed roots.
func (r *Registry) resolvePath(userPath string) (string, error) {
	if r.pathValidator == nil {
		return "", fmt.Errorf("path validator not configured")
	}

	abs, err := r.pathValidator.ValidatePath(userPath)
	if err != nil {
		// Map validator errors to user-friendly messages
		switch {
		case errors.Is(err, policy.ErrPathEscape):
			return "", fmt.Errorf("path %q escapes workspace boundary", userPath)
		case errors.Is(err, policy.ErrSymlinkEscape):
			return "", fmt.Errorf("path %q contains symlink pointing outside workspace", userPath)
		case errors.Is(err, policy.ErrNullByte):
			return "", fmt.Errorf("path %q contains invalid characters", userPath)
		case errors.Is(err, policy.ErrInvalidPath):
			return "", fmt.Errorf("path %q is invalid", userPath)
		default:
			return "", fmt.Errorf("invalid path %q: %w", userPath, err)
		}
	}
	return abs, nil
}
