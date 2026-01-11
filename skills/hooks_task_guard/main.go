// Package main implements the hooks/task_guard skill.
// This skill enforces the task-centric model by ensuring an active task exists
// before allowing write operations (Edit, Write, MultiEdit, NotebookEdit).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/hooks/pathutil"
	"github.com/jkatigb/agentctl/internal/hooks/toolutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// Mode controls task_guard behavior.
type Mode string

const (
	// ModeAuto auto-creates tasks when none exist.
	ModeAuto Mode = "auto"
	// ModeStrict blocks operations when no active task exists.
	ModeStrict Mode = "strict"
)

func main() {
	skillmain.Main("hooks/task_guard", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)

	// Skip non-write operations using cross-platform detection
	// Supports CC tools (Edit, Write, etc.), canonical tools (edit.*, fs.write_*), and explicit tool_kind
	if !toolutil.IsWriteOperation(in.ToolName, in.ToolCanonical, string(in.ToolKind)) {
		return emitOutput(rc, hooks.NewApprove("non-write operation", nil))
	}

	// Determine mode from environment
	mode := ModeAuto
	if m := os.Getenv("AGENTCTL_TASK_GUARD_MODE"); m == "strict" {
		mode = ModeStrict
	}

	// Resolve workspace ID from WorkspaceRoot
	workspaceID := in.WorkspaceID
	if workspaceID == "" {
		workspaceID = in.WorkspaceRoot
	}
	if workspaceID == "" {
		var wdErr error
		workspaceID, wdErr = os.Getwd()
		if wdErr != nil {
			return fmt.Errorf("failed to determine workspace directory: %w", wdErr)
		}
	}

	// Open task store
	store, err := tasks.Open(ctx, paths.StorageRoot)
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}
	defer store.Close()

	// Generate default title from tool + file path
	defaultTitle := deriveTaskTitle(in)

	// Get scope path from tool input using cross-platform path extraction
	scopePath := pathutil.ExtractPath(in.ToolInput)

	var output hooks.Output

	switch mode {
	case ModeAuto:
		// Auto-create task if none exists
		task, created, err := store.EnsureActive(ctx, workspaceID, defaultTitle, scopePath)
		if err != nil {
			return fmt.Errorf("ensure active task: %w", err)
		}

		// Check if task needs dirtying (ready_for_review or completed -> in_progress)
		dirtied := false
		if !created {
			task, dirtied, err = store.DirtyIfReviewed(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("dirty task: %w", err)
			}
		}

		reason := "task ensured"
		if created {
			reason = fmt.Sprintf("auto-created task: %s", task.Title)
		} else if dirtied {
			reason = fmt.Sprintf("task dirtied (demoted to in_progress): %s", task.Title)
		}

		// Create graph edge: task → file (modified)
		if scopePath != "" {
			createModifiedEdge(ctx, paths.StorageRoot, workspaceID, task.ID, scopePath)
		}

		output = hooks.NewApprove(reason, map[string]any{
			"task_id":      task.ID,
			"task_title":   task.Title,
			"task_status":  task.Status,
			"workspace_id": workspaceID,
			"created":      created,
			"dirtied":      dirtied,
		})

	case ModeStrict:
		// Check for existing active task
		task, found, err := store.GetActive(ctx, workspaceID)
		if err != nil {
			return fmt.Errorf("get active task: %w", err)
		}

		if !found {
			output = hooks.NewBlock(
				"No active task. Create one with: agentctl todo add --title \"<task>\" or use /start-task",
			)
		} else {
			// Check if task needs dirtying (ready_for_review or completed -> in_progress)
			task, dirtied, err := store.DirtyIfReviewed(ctx, task.ID)
			if err != nil {
				return fmt.Errorf("dirty task: %w", err)
			}

			reason := "active task exists"
			if dirtied {
				reason = fmt.Sprintf("task dirtied (demoted to in_progress): %s", task.Title)
			}

			// Create graph edge: task → file (modified)
			if scopePath != "" {
				createModifiedEdge(ctx, paths.StorageRoot, workspaceID, task.ID, scopePath)
			}

			output = hooks.NewApprove(reason, map[string]any{
				"task_id":      task.ID,
				"task_title":   task.Title,
				"task_status":  task.Status,
				"workspace_id": workspaceID,
				"dirtied":      dirtied,
			})
		}
	}

	return emitOutput(rc, output)
}

// deriveTaskTitle generates a task title from the hook input.
// Format: "<tool> <relative/path>" or "<tool> operation" if no path.
func deriveTaskTitle(in hooks.Input) string {
	filePath := pathutil.ExtractPath(in.ToolInput)
	if filePath == "" {
		toolName := in.ToolName
		if toolName == "" {
			toolName = in.ToolCanonical
		}
		if toolName == "" {
			toolName = "tool"
		}
		return fmt.Sprintf("%s operation", toolName)
	}

	// Make path relative to workspace using pathutil
	filePath = pathutil.RelativePath(filePath, in.WorkspaceRoot)

	toolName := in.ToolName
	if toolName == "" {
		toolName = in.ToolCanonical
	}
	if toolName == "" {
		toolName = "tool"
	}
	return fmt.Sprintf("%s %s", toolName, filePath)
}

func emitOutput(rc *skillmain.RunContext, output hooks.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return skillout.Emit(rc, "hooks/task_guard", data)
}

// createModifiedEdge creates a graph edge from task to file when modified.
// This enables PageRank to flow from tasks to the files they touch.
func createModifiedEdge(ctx context.Context, storagePath, workspaceID, taskID, filePath string) {
	// Early exit if no file path - avoids unnecessary graph.Open() overhead
	if filePath == "" {
		return
	}

	// Open graph store (fail silently - graph is optional)
	graphStore, err := graph.Open(ctx, storagePath)
	if err != nil {
		return
	}
	defer func() { _ = graphStore.Close() }()

	// Ensure task node exists
	taskNodeID := graph.TaskNodeID(taskID)
	taskNode := graph.Node{
		Workspace:   workspaceID,
		NodeID:      taskNodeID,
		NodeType:    graph.NodeTypeTask,
		Title:       taskID, // Will be updated by ingestion
		CurrentPath: "",
		LastSeen:    time.Now().UTC(),
	}
	_ = graphStore.UpsertNode(ctx, taskNode)

	// Ensure file node exists
	fileNodeID := graph.FileNodeID(filePath)
	fileNode := graph.Node{
		Workspace:   workspaceID,
		NodeID:      fileNodeID,
		NodeType:    graph.NodeTypeFile,
		Title:       filepath.Base(filePath),
		CurrentPath: filePath,
		LastSeen:    time.Now().UTC(),
	}
	_ = graphStore.UpsertNode(ctx, fileNode)

	// Create edge: task → file (modified)
	edge := graph.Edge{
		Workspace: workspaceID,
		FromID:    taskNodeID,
		FromType:  graph.NodeTypeTask,
		ToID:      fileNodeID,
		ToType:    graph.NodeTypeFile,
		EdgeType:  graph.EdgeTypeModified,
		Weight:    1.0,
		TTLDays:   intPtr(90), // 90 day TTL for task edges
		CreatedAt: time.Now().UTC(),
	}
	_ = graphStore.UpsertEdge(ctx, edge)
}

func intPtr(i int) *int {
	return &i
}
