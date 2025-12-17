// Package main implements the hooks/task_guard skill.
// This skill enforces the task-centric model by ensuring an active task exists
// before allowing write operations (Edit, Write, MultiEdit, NotebookEdit).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
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
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("hooks/task_guard", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("hooks/task_guard", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in hook.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("hooks/task_guard", "EARG", fmt.Errorf("decode input: %w", err))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("hooks/task_guard", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in hook.Input) error {
	// Skip non-write operations
	if !hook.IsWriteOperation(in.ToolName) {
		return emitOutput(rc, hook.NewApprove("non-write operation", nil))
	}

	// Determine mode from environment
	mode := ModeAuto
	if m := os.Getenv("AGENTCTL_TASK_GUARD_MODE"); m == "strict" {
		mode = ModeStrict
	}

	// Resolve workspace ID from WorkspaceRoot
	workspaceID := in.WorkspaceRoot
	if workspaceID == "" {
		var wdErr error
		workspaceID, wdErr = os.Getwd()
		if wdErr != nil {
			return fmt.Errorf("failed to determine workspace directory: %w", wdErr)
		}
	}

	// Open task store
	store, err := tasks.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}
	defer func() {
		// Store cleanup in defer; error is not actionable.
		_ = store.Close() //nolint:errcheck
	}()

	// Generate default title from tool + file path
	defaultTitle := deriveTaskTitle(in)

	// Get scope path from tool input
	scopePath := extractFilePath(in.ToolInput)

	var output hook.Output

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

		output = hook.NewApprove(reason, map[string]any{
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
			output = hook.NewBlock(
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

			output = hook.NewApprove(reason, map[string]any{
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
func deriveTaskTitle(in hook.Input) string {
	filePath := extractFilePath(in.ToolInput)
	if filePath == "" {
		return fmt.Sprintf("%s operation", in.ToolName)
	}

	// Make path relative to workspace if possible
	if in.WorkspaceRoot != "" {
		if rel, err := filepath.Rel(in.WorkspaceRoot, filePath); err == nil {
			filePath = rel
		}
	}

	return fmt.Sprintf("%s %s", in.ToolName, filePath)
}

// extractFilePath extracts the file_path from tool input JSON.
func extractFilePath(toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}

	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ""
	}
	return input.FilePath
}

func emitOutput(rc *runner.RunnerContext, output hook.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return rc.Emit("hooks/task_guard", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit hook failure")
	os.Exit(1)
}
