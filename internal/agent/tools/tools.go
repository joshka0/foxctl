// Package tools provides dspy-go tool implementations that wrap agentctl skills.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/secrets"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// Registry holds all registered agent tools.
type Registry struct {
	tools         *dstools.InMemoryToolRegistry
	recorder      TelemetryRecorder
	config        Config
	pathValidator *policy.PathValidator
	openMemory    func(context.Context) (storage.MemoryStore, error)

	trajMu sync.Mutex
	trajID string
}

// Config configures tool behavior.
type Config struct {
	// WorkspaceRoot is the root directory for file operations.
	WorkspaceRoot string

	// WorkspaceID is the workspace identifier for task/mail operations.
	WorkspaceID string

	// TaskID associates tool usage with a task.
	TaskID string

	// EpicID associates tool usage with an epic.
	EpicID string

	// AgentRole identifies the agent role making tool calls.
	AgentRole string

	// TraceID is the correlation id for a session/run.
	TraceID string

	// TrajectoryStorageRoot enables trajectory capture when set.
	TrajectoryStorageRoot string

	// ActorID is the mailbox identity for this agent.
	ActorID string

	// MaxFileSize is the maximum bytes to read from a file.
	MaxFileSize int64

	// MaxSearchResults limits code search results.
	MaxSearchResults int

	// AllowedRoots are additional directories outside workspace that paths can resolve to.
	AllowedRoots []string

	// OpenMemoryStore provides access to named memory for retrieval tools like code.symbol_search.
	// When nil, tools that require named memory should return empty results with a helpful message.
	OpenMemoryStore func(context.Context) (storage.MemoryStore, error)
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
		openMemory:    cfg.OpenMemoryStore,
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
		r.captureToolEvent(ctx, trajectory.EventKindToolCall, name, "ok", summarizeToolArgs(name, args), "")
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

		status := "ok"
		if err != nil {
			status = "error"
		} else if result != nil && result.IsError {
			status = "error"
		}
		dataInline, artifact := summarizeToolResult(name, args, result, err)
		r.captureToolEvent(ctx, trajectory.EventKindToolResult, name, status, dataInline, artifact)

		return result, err
	}
}

func (r *Registry) captureToolEvent(ctx context.Context, kind trajectory.EventKind, toolName, status string, dataInline map[string]any, artifact string) {
	if r == nil {
		return
	}
	if strings.TrimSpace(r.config.TrajectoryStorageRoot) == "" {
		return
	}
	if strings.TrimSpace(r.config.WorkspaceID) == "" {
		return
	}
	traceID := strings.TrimSpace(r.config.TraceID)
	if traceID == "" {
		return
	}

	store, err := trajectory.Open(ctx, r.config.TrajectoryStorageRoot)
	if err != nil {
		return
	}
	defer func() { errspkg.Ignore(store.Close(), "close trajectory store") }()

	trajID, err := r.ensureTrajectoryID(ctx, store)
	if err != nil {
		return
	}
	if trajID == "" {
		return
	}

	dataInline = secrets.RedactMap(dataInline)
	meta := &trajectory.EventMeta{
		TraceID:   traceID,
		JobID:     "",
		TaskID:    strings.TrimSpace(r.config.TaskID),
		EpicID:    strings.TrimSpace(r.config.EpicID),
		ActorID:   strings.TrimSpace(r.config.ActorID),
		CreatedBy: "agentctl",
		CASDigest: strings.TrimSpace(artifact),
	}

	_, err = store.InsertEvent(ctx, trajectory.Event{
		TrajectoryID: trajID,
		Kind:         kind,
		Actor:        strings.TrimSpace(r.config.ActorID),
		Command:      toolName,
		Status:       status,
		DataInline:   dataInline,
		DataArtifact: strings.TrimSpace(artifact),
		Meta:         meta,
	})
	if err != nil {
		return
	}
}

func (r *Registry) ensureTrajectoryID(ctx context.Context, store trajectory.Store) (string, error) {
	r.trajMu.Lock()
	defer r.trajMu.Unlock()

	if r.trajID != "" {
		return r.trajID, nil
	}

	trajectories, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: r.config.WorkspaceID,
		TraceID:     r.config.TraceID,
		Limit:       1,
	})
	if err != nil {
		return "", err
	}
	if len(trajectories) > 0 {
		r.trajID = trajectories[0].ID
		return r.trajID, nil
	}

	text := secrets.Redact(strings.TrimSpace(r.config.AgentRole))
	if text == "" {
		text = "agent tool capture"
	}

	ur := trajectory.UserRequestCapture{
		WorkspaceID: r.config.WorkspaceID,
		Actor:       strings.TrimSpace(r.config.ActorID),
		Source:      trajectory.SourceCLI,
		Text:        text,
		CommandContext: &trajectory.CommandContext{
			CLICommand:      "",
			ProtocolCommand: "dspy-agent",
			JobID:           "",
			TraceID:         r.config.TraceID,
		},
		TaskHints: &trajectory.TaskHints{
			TaskID: strings.TrimSpace(r.config.TaskID),
			EpicID: strings.TrimSpace(r.config.EpicID),
		},
	}
	if ur.Actor == "" {
		ur.Actor = "actor:agent:dspy"
	}
	ur, err = store.InsertUserRequest(ctx, ur)
	if err != nil {
		return "", err
	}

	taskIDs := []string{}
	if strings.TrimSpace(r.config.TaskID) != "" {
		taskIDs = append(taskIDs, strings.TrimSpace(r.config.TaskID))
	}

	traj := trajectory.Trajectory{
		WorkspaceID:   r.config.WorkspaceID,
		RootRequestID: ur.ID,
		TaskIDs:       taskIDs,
		EpicID:        strings.TrimSpace(r.config.EpicID),
		AgentRole:     strings.TrimSpace(r.config.AgentRole),
		JobID:         "",
		TraceID:       r.config.TraceID,
		Status:        trajectory.StatusPartial,
		Summary:       "",
	}
	traj, err = store.InsertTrajectory(ctx, traj)
	if err != nil {
		return "", err
	}

	r.trajID = traj.ID
	return r.trajID, nil
}

func summarizeToolArgs(toolName string, args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{"summary": "no args"}
	}

	summary := map[string]any{}
	subkind := ""
	switch toolName {
	case "code.symbol_search":
		subkind = string(trajectory.EventKindGraphSearch)
		if v, ok := args["question"].(string); ok {
			summary["question"] = v
		}
		if v, ok := args["mode"].(string); ok {
			summary["mode"] = v
		}
		if v, ok := args["symbol_hint"].(string); ok {
			summary["symbol_hint"] = v
		}
		if v, ok := args["max_results"].(int); ok {
			summary["max_results"] = v
		} else if v, ok := args["max_results"].(float64); ok {
			summary["max_results"] = int(v)
		}
	case "code.swe_grep":
		subkind = string(trajectory.EventKindSWEGrep)
		if v, ok := args["question"].(string); ok {
			summary["question"] = v
		}
		if v, ok := args["candidate_files"].([]any); ok {
			summary["candidate_count"] = len(v)
		}
	case "fs.read_file", "fs.list_dir", "code.search", "edit.apply_patch", "edit.apply_structured_diff", "tests.run":
		if v, ok := args["path"].(string); ok {
			summary["path"] = v
		}
		if v, ok := args["pattern"].(string); ok {
			summary["pattern"] = v
		}
		if toolName == "edit.apply_patch" {
			if v, ok := args["old_text"].(string); ok {
				summary["old_text_len"] = len(v)
			}
			if v, ok := args["new_text"].(string); ok {
				summary["new_text_len"] = len(v)
			}
		}
		if toolName == "edit.apply_structured_diff" {
			if v, ok := args["dry_run"].(bool); ok {
				summary["dry_run"] = v
			}
			if v, ok := args["diff_json"].(map[string]any); ok {
				if hunks, ok := v["hunks"].([]any); ok {
					summary["hunk_count"] = len(hunks)
				}
			}
		}
	default:
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		summary["args_keys"] = keys
	}

	if subkind != "" {
		summary["subkind"] = subkind
	}
	if summary["summary"] == nil {
		summary["summary"] = "tool call"
	}
	return summary
}

func summarizeToolResult(toolName string, _ map[string]any, result *models.CallToolResult, callErr error) (map[string]any, string) {
	inline := map[string]any{}
	artifact := ""

	status := "ok"
	if callErr != nil {
		status = "error"
		inline["error"] = callErr.Error()
	}
	if result != nil && result.IsError {
		status = "error"
	}
	inline["status"] = status

	if toolName == "code.symbol_search" {
		inline["subkind"] = string(trajectory.EventKindGraphSearch)
	}
	if toolName == "code.swe_grep" {
		inline["subkind"] = string(trajectory.EventKindSWEGrep)
	}

	parsed := parseCallToolResult(result)
	if parsed != nil {
		if c, ok := parsed["count"].(float64); ok {
			inline["count"] = int(c)
		}
		if c, ok := parsed["count"].(int); ok {
			inline["count"] = c
		}
		if v, ok := parsed["cas_artifact"].(string); ok {
			artifact = v
		}
		if v, ok := parsed["artifact"].(string); ok {
			artifact = v
		}
		if toolName == "code.swe_grep" {
			if c, ok := parsed["snippets_emitted"].(float64); ok {
				inline["snippets_emitted"] = int(c)
			}
			if c, ok := parsed["files_considered"].(float64); ok {
				inline["files_considered"] = int(c)
			}
		}
	}

	if inline["summary"] == nil {
		inline["summary"] = "tool result"
	}
	if artifact != "" {
		inline["cas_digest"] = artifact
	}
	return inline, artifact
}

func parseCallToolResult(result *models.CallToolResult) map[string]any {
	if result == nil {
		return nil
	}
	if len(result.Content) == 0 {
		return nil
	}
	text, ok := result.Content[0].(models.TextContent)
	if !ok {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text.Text), &parsed); err != nil {
		return nil
	}
	return parsed
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
