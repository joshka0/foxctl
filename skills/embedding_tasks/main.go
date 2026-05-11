// Package main implements the embedding/tasks skill for generating task embeddings.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

const (
	command         = "embedding/tasks"
	taskType        = "task_embedding"
	geminiModel     = "gemini-embedding-001"
	defaultBatchMax = 10 // Process 10 tasks at a time by default
)

// Input is the skill input schema for embedding/tasks operations.
type Input struct {
	// Scope determines which tasks to embed: "all", "pending", "completed", or "workspace".
	Scope string `json:"scope" validate:"omitempty,oneof=all pending completed workspace"`

	// WorkspaceID is required when Scope is "workspace".
	WorkspaceID string `json:"workspace_id,omitempty"`

	// TaskID is optional - if set, only embed this specific task.
	TaskID string `json:"task_id,omitempty"`

	// BatchSize limits how many tasks to process per batch.
	BatchSize int `json:"batch_size,omitempty"`

	// ProcessAll loops internally until all tasks are embedded.
	// When false, returns after one batch.
	ProcessAll bool `json:"process_all,omitempty"`

	// DryRun if true, lists tasks but doesn't generate embeddings.
	DryRun bool `json:"dry_run,omitempty"`
}

// Output is the skill output for embedding/tasks operations.
type Output struct {
	Scope        string       `json:"scope"`
	TasksFound   int          `json:"tasks_found"`
	Embedded     int          `json:"embedded"`
	Skipped      int          `json:"skipped"`
	Errors       int          `json:"errors"`
	Remaining    int          `json:"remaining,omitempty"`
	BatchCount   int          `json:"batch_count,omitempty"`
	DurationMs   int64        `json:"duration_ms"`
	Tasks        []TaskResult `json:"tasks,omitempty"`
	ErrorDetails []string     `json:"error_details,omitempty"`
}

// TaskResult captures the result of embedding a single task.
type TaskResult struct {
	TaskID     string `json:"task_id"`
	Title      string `json:"title"`
	Status     string `json:"status"` // "embedded", "skipped", "error"
	Dimensions int    `json:"dimensions,omitempty"`
	Message    string `json:"message,omitempty"`
}

// main is the skill entry point for embedding/tasks.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates task embedding generation with batch processing and scope filtering.
//
// Index:
//
//	Purpose: Generate semantic embeddings for tasks to enable task search and retrieval
//	Flow: validate input → list tasks by scope → process in batches → generate embeddings → store as memories
//	SideEffects: embedding API calls; memory store updates; task content formatting; batch processing
//	FailureModes: missing API keys, task store errors, embedding failures, memory store errors
//	Observability: emits processing statistics, task results, error details, and timing metrics
//	Related: taskEmbeddingContent, listAllTasks
//	Keywords: embedding/tasks, tasks, semantic_search, batch_processing, embeddings
//
// [[domain:work-item-embedding-generation]]
// [[protocol:work-item-memory-embedding-storage]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate input
	if in.Scope == "" && in.TaskID == "" {
		return skillerr.Arg("scope or task_id is required (scope: all, pending, completed, workspace)")
	}
	if in.Scope == "workspace" && in.WorkspaceID == "" {
		return skillerr.Arg("workspace_id is required when scope is 'workspace'")
	}
	if in.BatchSize <= 0 {
		in.BatchSize = defaultBatchMax
	}

	start := time.Now()
	output := Output{
		Scope: in.Scope,
	}

	geminiKey := os.Getenv("GEMINI_API_KEY")

	// Open task store
	taskStore, err := rc.Stores.Tasks(ctx)
	if err != nil {
		return skillerr.Runtime("open task store", skillerr.WithCause(err))
	}

	// Open memory store
	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err != nil {
		return skillerr.Runtime("open memory store", skillerr.WithCause(err))
	}
	defer memStore.Close() //nolint:errcheck

	// Get initial task list to count total
	var allTasks []tasks.Task
	if in.TaskID != "" {
		task, err := taskStore.Get(ctx, in.TaskID)
		if err != nil {
			return skillerr.Runtime(fmt.Sprintf("get task %s", in.TaskID), skillerr.WithCause(err))
		}
		allTasks = []tasks.Task{task}
	} else {
		allTasks, err = listAllTasks(ctx, taskStore, in.Scope, in.WorkspaceID)
		if err != nil {
			return skillerr.Runtime("list tasks", skillerr.WithCause(err))
		}
	}
	output.TasksFound = len(allTasks)

	// Dry run - just list tasks
	if in.DryRun {
		for _, t := range allTasks {
			content := taskEmbeddingContent(t)
			output.Tasks = append(output.Tasks, TaskResult{
				TaskID:  t.ID,
				Title:   t.Title,
				Status:  "dry_run",
				Message: fmt.Sprintf("Would embed %d chars", len(content)),
			})
		}
		output.DurationMs = time.Since(start).Milliseconds()
		return skillout.Emit(rc, command, output)
	}

	embedder, err := semantic.NewEmbedderFromConfig(
		semantic.ScopeTasks,
		rc.Config,
		semantic.WithGeminiKey(geminiKey),
		skillmain.EmbeddingGuard(rc),
	)
	if err != nil {
		return skillerr.Runtime("embedding provider", skillerr.WithCause(err))
	}

	sessionID := sessionkit.ResolveSessionID(rc.Workspace, rc.SessionID)

	// Track already embedded tasks to skip
	embeddedIDs := make(map[string]bool)

	// Process in batches
	for {
		// Get fresh task list each iteration
		var taskList []tasks.Task
		if in.TaskID != "" {
			task, err := taskStore.Get(ctx, in.TaskID)
			if err != nil {
				output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("get task %s: %v", in.TaskID, err))
				output.Errors++
				break
			}
			if !embeddedIDs[task.ID] {
				taskList = []tasks.Task{task}
			}
		} else {
			all, err := listAllTasks(ctx, taskStore, in.Scope, in.WorkspaceID)
			if err != nil {
				output.ErrorDetails = append(output.ErrorDetails, "list tasks: "+err.Error())
				output.Errors++
				break
			}
			// Filter out already embedded
			for _, t := range all {
				if !embeddedIDs[t.ID] {
					taskList = append(taskList, t)
				}
			}
		}

		// Nothing left
		if len(taskList) == 0 {
			break
		}

		// Apply batch limit
		batch := taskList
		remaining := 0
		if len(batch) > in.BatchSize {
			batch = batch[:in.BatchSize]
			remaining = len(taskList) - in.BatchSize
		}

		// Process batch
		for _, t := range batch {
			content := taskEmbeddingContent(t)
			if content == "" {
				output.Skipped++
				embeddedIDs[t.ID] = true
				continue
			}

			embeddingResult, err := embedder.Embed(ctx, content)
			if err != nil {
				output.Errors++
				output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("%s: %v", t.ID, err))
				embeddedIDs[t.ID] = true
				continue
			}

			name := fmt.Sprintf("task://%s", t.ID)
			workspace := t.WorkspaceID
			if workspace == "" {
				workspace = "default"
			}

			resultData, _ := json.Marshal(map[string]any{
				"task_id": t.ID,
				"status":  t.Status,
			})

			entry := storage.NamedEntry{
				Name:      name,
				Type:      taskType,
				Workspace: workspace,
				Summary:   content,
				Result:    resultData,
				SessionID: sessionID,
			}

			if _, err := memStore.Save(ctx, entry); err != nil {
				output.Errors++
				output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("%s: save failed: %v", t.ID, err))
				embeddedIDs[t.ID] = true
				continue
			}

			if err := memStore.UpdateEmbedding(ctx, name, workspace, embeddingResult.Vec); err != nil {
				output.Errors++
				output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("%s: update embedding failed: %v", t.ID, err))
				embeddedIDs[t.ID] = true
				continue
			}

			output.Embedded++
			embeddedIDs[t.ID] = true
		}
		output.BatchCount++

		// If not process_all, return after one batch
		if !in.ProcessAll {
			output.Remaining = remaining
			break
		}

		// Check context
		if ctx.Err() != nil {
			output.Remaining = remaining
			break
		}
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return skillout.Emit(rc, command, output)
}

// taskEmbeddingContent builds rich semantic text from task fields.
// Format: Title + Description + Notes + Gotchas for maximum semantic coverage.
// taskEmbeddingContent generates embedding text for a task.
// Format: [Jan 2026] [completed] Task: title\nDescription: ...\nDependencies: N tasks\nEpic: ...
func taskEmbeddingContent(t tasks.Task) string {
	var parts []string

	// Date prefix (use completed_at if done, else created_at)
	var dateStr string
	if t.CompletedAt != nil {
		dateStr = t.CompletedAt.Format("Jan 2006")
	} else {
		dateStr = t.CreatedAt.Format("Jan 2006")
	}

	// Status prefix
	status := t.Status
	if status == "" {
		status = "pending"
	}
	parts = append(parts, fmt.Sprintf("[%s] [%s]", dateStr, status))

	// Title is always included
	if t.Title != "" {
		parts = append(parts, "Task: "+t.Title)
	}

	// Description provides context
	if t.Description != "" {
		parts = append(parts, "Description: "+t.Description)
	}

	// Dependencies count
	if len(t.DependsOn) > 0 {
		parts = append(parts, fmt.Sprintf("Dependencies: %d tasks", len(t.DependsOn)))
	}

	// Epic association
	if t.EpicID != "" {
		parts = append(parts, "Epic: "+t.EpicID)
	}

	// Notes capture implementation details
	if t.Notes != "" {
		parts = append(parts, "Notes: "+t.Notes)
	}

	// Gotchas are valuable for future reference
	if t.Gotchas != "" {
		parts = append(parts, "Gotchas: "+t.Gotchas)
	}

	return strings.Join(parts, "\n")
}

// listAllTasks lists tasks based on scope, filtering by status as needed.
func listAllTasks(ctx context.Context, store tasks.Store, scope, workspaceID string) ([]tasks.Task, error) {
	// Get all tasks from the specified workspace (or current workspace)
	var allTasks []tasks.Task

	workspaceID = workspaceutil.ResolveID(workspaceID, "")

	// Fetch tasks for the workspace
	taskList, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	allTasks = taskList

	// Filter by scope
	switch scope {
	case "all":
		return allTasks, nil
	case "pending":
		var filtered []tasks.Task
		for _, t := range allTasks {
			if t.Status == "pending" || t.Status == "in_progress" {
				filtered = append(filtered, t)
			}
		}
		return filtered, nil
	case "completed":
		var filtered []tasks.Task
		for _, t := range allTasks {
			if t.Status == "completed" {
				filtered = append(filtered, t)
			}
		}
		return filtered, nil
	case "workspace":
		// Already filtered by workspace
		return allTasks, nil
	default:
		return allTasks, nil
	}
}

// boolPtr returns a pointer to a bool value.
