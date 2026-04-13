package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/tooling"

	"github.com/jkatigb/agentctl/internal/agent/toolnames"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	sysconfig "github.com/jkatigb/agentctl/internal/platform/config"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/maputil"
	"github.com/jkatigb/agentctl/internal/platform/secrets"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

// Registry holds all registered agent tools.
type Registry struct {
	tools               *tooling.InMemoryToolRegistry
	recorder            TelemetryRecorder
	config              Config
	pathValidator       *policy.PathValidator
	openMemory          func(context.Context) (storage.MemoryStore, error)
	openBoardStore      func(context.Context) (blackboard.BoardStore, error)
	openTasksStore      func(context.Context) (tasks.Store, error)
	openBlackboardStore func(context.Context) (blackboard.Store, error)
	openMailboxStore    func(context.Context) (mailbox.Store, error)
	openCASStore        func(context.Context) (storage.CASStore, error)
	openAgentsStore     func(context.Context) (agents.Store, error)
	openRepoIndexStore  func(context.Context) (*repoindex.Store, error)

	trajMu sync.Mutex
	trajID string
}

// Tool is the runtime-facing tool contract exposed by the agent tools package.
type Tool = tooling.Tool

// ToolFunc is the runtime-facing callable tool function shape.
type ToolFunc = tooling.ToolFunc

// Config configures tool behavior.
type Config struct {
	// WorkspaceRoot is the root directory for file operations.
	WorkspaceRoot string

	// WorkspaceID is the workspace identifier for task/mail operations.
	WorkspaceID string

	// SessionID is the current agent session ID for hook context.
	SessionID string

	// TaskID associates tool usage with a task.
	TaskID string

	// EpicID associates tool usage with an epic.
	EpicID string

	// Depth is the caller's depth in the agent hierarchy.
	Depth int

	// MaxDepth is the global max depth for the hierarchy.
	MaxDepth int

	// LocalMaxDepth is the caller's subtree depth limit.
	LocalMaxDepth int

	// AgentRole identifies the agent role making tool calls.
	AgentRole string

	// TraceID is the correlation id for a session/run.
	TraceID string

	// TrajectoryStorageRoot enables trajectory capture when set.
	TrajectoryStorageRoot string

	// ActorID is the mailbox identity for this agent.
	ActorID string

	// HookDispatcher dispatches PreToolUse/PostToolUse hooks.
	HookDispatcher hooks.Dispatcher

	// MaxFileSize is the maximum bytes to read from a file.
	MaxFileSize int64

	// MaxSearchResults limits code search results.
	MaxSearchResults int

	// AllowedRoots are additional directories outside workspace that paths can resolve to.
	AllowedRoots []string

	// FilesystemPolicy configures allowed roots ("workspace", "home", "tmp", "all").
	FilesystemPolicy string

	// MaxOutputSize limits tool output size in bytes. Larger outputs use CAS.
	// If 0, defaults to config.DefaultInlineOutputKB * 1024.
	MaxOutputSize int

	// Allowlist filters available tools. If non-empty, only tools in this list are registered.
	Allowlist []string

	// OpenMemoryStore provides access to named memory for retrieval tools like code.symbol_search.
	// When nil, tools that require named memory should return empty results with a helpful message.
	OpenMemoryStore func(context.Context) (storage.MemoryStore, error)

	// OpenCASStore provides access to the content-addressable store for large outputs.
	OpenCASStore func(context.Context) (storage.CASStore, error)

	// OpenBoardStore provides access to the blackboard board store.
	OpenBoardStore func(context.Context) (blackboard.BoardStore, error)

	// OpenTasksStore provides access to the tasks store.
	OpenTasksStore func(context.Context) (tasks.Store, error)

	// OpenBlackboardStore provides access to the blackboard topic store.
	OpenBlackboardStore func(context.Context) (blackboard.Store, error)

	// OpenMailboxStore provides access to the mailbox store.
	OpenMailboxStore func(context.Context) (mailbox.Store, error)

	// OpenAgentsStore provides access to the agents store for resolving agent references.
	OpenAgentsStore func(context.Context) (agents.Store, error)

	// OpenRepoIndexStore provides access to the repo index store.
	OpenRepoIndexStore func(context.Context) (*repoindex.Store, error)

	// EndTick requests that a tick-mode agent stop its long-running loop.
	// When nil, the end-tick tool is unavailable.
	EndTick func(context.Context) (bool, error)
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

	// Ensure workspace root is absolute for reliable filepath.Rel calls
	absWorkspace, err := filepath.Abs(cfg.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute workspace path: %w", err)
	}
	cfg.WorkspaceRoot = absWorkspace

	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = 1024 * 1024 // 1MB default
	}

	if cfg.MaxSearchResults <= 0 {
		cfg.MaxSearchResults = 50
	}

	if cfg.MaxOutputSize <= 0 {
		cfg.MaxOutputSize = sysconfig.DefaultInlineOutputKB * 1024
	}

	if recorder == nil {
		recorder = noopRecorder{}
	}

	// Configure allowed roots based on policy
	switch cfg.FilesystemPolicy {
	case "home":
		if home, err := os.UserHomeDir(); err == nil {
			cfg.AllowedRoots = append(cfg.AllowedRoots, home)
		}
	case "tmp":
		cfg.AllowedRoots = append(cfg.AllowedRoots, os.TempDir())
	case "all":
		if home, err := os.UserHomeDir(); err == nil {
			cfg.AllowedRoots = append(cfg.AllowedRoots, home)
		}
		cfg.AllowedRoots = append(cfg.AllowedRoots, os.TempDir())
	case "workspace", "":
		// Default: only workspace + explicitly allowed roots
	}

	// Initialize PathValidator for secure path resolution
	pathValidator, err := policy.NewPathValidator(cfg.WorkspaceRoot, cfg.AllowedRoots)
	if err != nil {
		return nil, fmt.Errorf("init path validator: %w", err)
	}

	r := &Registry{
		tools:               tooling.NewInMemoryToolRegistry(),
		recorder:            recorder,
		config:              cfg,
		pathValidator:       pathValidator,
		openMemory:          cfg.OpenMemoryStore,
		openBoardStore:      cfg.OpenBoardStore,
		openTasksStore:      cfg.OpenTasksStore,
		openBlackboardStore: cfg.OpenBlackboardStore,
		openMailboxStore:    cfg.OpenMailboxStore,
		openCASStore:        cfg.OpenCASStore,
		openAgentsStore:     cfg.OpenAgentsStore,
		openRepoIndexStore:  cfg.OpenRepoIndexStore,
	}

	// Register all V1 tools
	if err := r.registerAgentTools(); err != nil {
		return nil, err
	}
	if err := r.registerFSTools(); err != nil {
		return nil, err
	}
	if r.config.OpenRepoIndexStore != nil {
		if err := r.registerRepoIndexTools(); err != nil {
			return nil, err
		}
	}
	if err := r.registerContextGrepTool(); err != nil {
		return nil, err
	}
	if err := r.registerRefactorScoutTool(); err != nil {
		return nil, err
	}
	if err := r.registerShellTool(); err != nil {
		return nil, err
	}
	if err := r.registerCodeTools(); err != nil {
		return nil, err
	}
	if err := r.registerHeartwoodTools(); err != nil {
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
	if err := r.registerBBTools(); err != nil {
		return nil, err
	}
	if err := r.registerSessionTools(); err != nil {
		return nil, err
	}
	if err := r.registerMemoryTools(); err != nil {
		return nil, err
	}

	// Apply allowlist filtering if configured
	if len(cfg.Allowlist) > 0 {
		if err := r.filterByAllowlist(cfg.Allowlist); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Get returns one registered tool by name.
func (r *Registry) Get(name string) (Tool, error) {
	if r == nil {
		return nil, fmt.Errorf("tool registry not configured")
	}
	return r.tools.Get(name)
}

// List returns all registered tools.
func (r *Registry) List() []Tool {
	if r == nil {
		return nil
	}
	return r.tools.List()
}

// SetSessionID updates the session ID used for hook context.
func (r *Registry) SetSessionID(sessionID string) {
	r.config.SessionID = sessionID
}

type hookDispatchContextKey struct{}

// WithHookDispatch marks the context as already dispatched through hooks.
// This prevents double-dispatch when a tool runner wraps the tool call.
func WithHookDispatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, hookDispatchContextKey{}, true)
}

func hooksAlreadyDispatched(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(hookDispatchContextKey{}).(bool)
	return ok && v
}

// wrapWithTelemetry wraps a tool function to record telemetry.
func (r *Registry) wrapWithTelemetry(
	name string,
	fn func(ctx context.Context, args map[string]any) (*models.CallToolResult, error),
) ToolFunc {
	return func(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
		start := time.Now()
		argsForCall := args
		var result *models.CallToolResult
		var callErr error
		var returnErr error
		executed := false

		finalize := func() (*models.CallToolResult, error) {
			duration := time.Since(start)
			call := types.ToolCall{
				ToolName:  name,
				Args:      argsForCall,
				Result:    result,
				Duration:  duration,
				Timestamp: start,
			}
			if callErr != nil {
				call.Error = callErr.Error()
			}
			r.recorder.RecordToolCall(call)

			status := "ok"
			if callErr != nil {
				status = "error"
			} else if result != nil && result.IsError {
				status = "error"
			}

			// Check output size and use CAS if needed (only for successful results)
			if status == "ok" && result != nil && r.config.MaxOutputSize > 0 && r.config.OpenCASStore != nil {
				if err := r.checkAndOffloadToCAS(ctx, result); err != nil {
					errspkg.Ignore(err, "tool output CAS offload failed")
				}
			}

			dataInline, artifact := summarizeToolResult(name, argsForCall, result, callErr)
			r.captureToolEvent(ctx, trajectory.EventKindToolResult, name, status, dataInline, artifact)

			if executed {
				r.dispatchPostToolUse(ctx, name, argsForCall, result, callErr, duration)
			}

			return result, returnErr
		}

		preOutput, err := r.dispatchPreToolUse(ctx, name, argsForCall)
		if err != nil {
			callErr = fmt.Errorf("hook error: %w", err)
			result = errorResult(callErr.Error())
			r.captureToolEvent(ctx, trajectory.EventKindToolCall, name, "error", summarizeToolArgs(name, argsForCall), "")
			returnErr = nil
			return finalize()
		}
		if preOutput.Decision.IsBlocking() {
			callErr = fmt.Errorf("blocked by hook: %s", preOutput.Reason)
			result = errorResult(callErr.Error())
			r.captureToolEvent(ctx, trajectory.EventKindToolCall, name, "error", summarizeToolArgs(name, argsForCall), "")
			returnErr = nil
			return finalize()
		}

		if len(preOutput.UpdatedToolInput) > 0 {
			var updated map[string]any
			if err := json.Unmarshal(preOutput.UpdatedToolInput, &updated); err != nil {
				callErr = fmt.Errorf("hook updated_tool_input invalid: %w", err)
				result = errorResult(callErr.Error())
				r.captureToolEvent(ctx, trajectory.EventKindToolCall, name, "error", summarizeToolArgs(name, argsForCall), "")
				returnErr = nil
				return finalize()
			}
			argsForCall = updated
		}

		r.captureToolEvent(ctx, trajectory.EventKindToolCall, name, "ok", summarizeToolArgs(name, argsForCall), "")
		result, callErr = fn(ctx, argsForCall)
		returnErr = callErr
		executed = true

		return finalize()
	}
}

func (r *Registry) shouldDispatchHooks(ctx context.Context) bool {
	if r == nil || r.config.HookDispatcher == nil {
		return false
	}
	if hooksAlreadyDispatched(ctx) {
		return false
	}
	return true
}

func (r *Registry) dispatchPreToolUse(ctx context.Context, toolName string, args map[string]any) (hooks.Output, error) {
	if !r.shouldDispatchHooks(ctx) {
		return hooks.NewApprove("no dispatcher", nil), nil
	}

	toolInput, err := json.Marshal(args)
	if err != nil {
		return hooks.Output{}, fmt.Errorf("marshal tool input: %w", err)
	}

	input := hooks.Input{
		Event:         hooks.EventPreToolUse,
		ToolName:      toolName,
		ToolCanonical: toolName,
		ToolKind:      hooks.ClassifyToolKind(toolName, toolName),
		ToolInput:     toolInput,
		SessionID:     r.config.SessionID,
		ActorID:       r.config.ActorID,
		WorkspaceID:   r.config.WorkspaceID,
		WorkspaceRoot: r.config.WorkspaceRoot,
		TraceID:       r.config.TraceID,
	}

	result, err := r.config.HookDispatcher.Dispatch(ctx, input)
	if err != nil {
		return hooks.Output{}, err
	}
	return result.Output, nil
}

func (r *Registry) dispatchPostToolUse(
	ctx context.Context,
	toolName string,
	args map[string]any,
	result *models.CallToolResult,
	callErr error,
	duration time.Duration,
) {
	if !r.shouldDispatchHooks(ctx) {
		return
	}

	toolInput, err := json.Marshal(args)
	if err != nil {
		return
	}

	resultSummary, _ := summarizeToolResult(toolName, args, result, callErr)
	observation := map[string]any{
		"tool":   toolName,
		"result": resultSummary,
	}
	toolObservation, err := json.Marshal(observation)
	if err != nil {
		return
	}

	input := hooks.Input{
		Event:           hooks.EventPostToolUse,
		ToolName:        toolName,
		ToolCanonical:   toolName,
		ToolKind:        hooks.ClassifyToolKind(toolName, toolName),
		ToolInput:       toolInput,
		ToolObservation: toolObservation,
		ToolDurationMS:  duration.Milliseconds(),
		SessionID:       r.config.SessionID,
		ActorID:         r.config.ActorID,
		WorkspaceID:     r.config.WorkspaceID,
		WorkspaceRoot:   r.config.WorkspaceRoot,
		TraceID:         r.config.TraceID,
	}

	if callErr != nil {
		input.ToolError = callErr.Error()
	} else if result != nil && result.IsError {
		input.ToolError = extractToolError(result)
	}

	_, _ = r.config.HookDispatcher.Dispatch(ctx, input)
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
			if v, ok := maputil.AsStringMap(args["diff_json"]); ok {
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

func extractToolError(result *models.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(models.TextContent); ok {
		return tc.Text
	}
	return ""
}

// filterByAllowlist restricts registered tools to those in the allowlist.
func (r *Registry) filterByAllowlist(allowlist []string) error {
	allowed := make(map[string]bool)
	for _, name := range allowlist {
		trimmed := strings.TrimSpace(name)
		if canonical, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeLegacy, trimmed); ok {
			allowed[canonical] = true
		}
		switch trimmed {
		case repoindex.ToolSearchLegacy:
			allowed[repoindex.ToolSearch] = true
		case repoindex.ToolExpandLegacy:
			allowed[repoindex.ToolExpand] = true
		case repoindex.ToolOpenLegacy:
			allowed[repoindex.ToolOpen] = true
		case repoindex.ToolDAGGrepLegacy:
			allowed[repoindex.ToolDAGGrep] = true
		default:
			allowed[trimmed] = true
		}
	}

	filteredRegistry := tooling.NewInMemoryToolRegistry()
	for _, tool := range r.tools.List() {
		if allowed[tool.Name()] {
			if err := filteredRegistry.Register(tool); err != nil {
				return fmt.Errorf("register tool %s: %w", tool.Name(), err)
			}
		}
	}
	r.tools = filteredRegistry
	return nil
}

// checkAndOffloadToCAS checks if the result is too large and moves it to CAS.
func (r *Registry) checkAndOffloadToCAS(ctx context.Context, result *models.CallToolResult) error {
	if len(result.Content) == 0 {
		return nil
	}

	// Calculate size (only TextContent for now)
	var totalSize int
	for _, item := range result.Content {
		if tc, ok := item.(models.TextContent); ok {
			totalSize += len(tc.Text)
		}
	}

	if totalSize <= r.config.MaxOutputSize {
		return nil
	}

	casStore, err := r.config.OpenCASStore(ctx)
	if err != nil {
		return fmt.Errorf("open cas store: %w", err)
	}

	// Marshal content to JSON for storage
	data, err := json.Marshal(result.Content)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}

	// Store in CAS
	obj, err := casStore.Put(ctx, bytes.NewReader(data), "application/json", []string{"tool-output", r.config.TraceID})
	if err != nil {
		return fmt.Errorf("cas put: %w", err)
	}

	summaryMap := map[string]any{
		"summary":  fmt.Sprintf("Output too large (%d bytes). Stored as artifact.", totalSize),
		"artifact": obj.Digest,
	}
	summaryJSON, err := json.Marshal(summaryMap)
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}

	result.Content = []models.Content{
		models.TextContent{
			Type: "text",
			Text: string(summaryJSON),
		},
	}

	return nil
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
