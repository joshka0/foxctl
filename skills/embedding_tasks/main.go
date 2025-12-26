// Package main implements the embedding/tasks skill for generating task embeddings.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/rs/zerolog"
)

const (
	command         = "embedding/tasks"
	taskType        = "task_embedding"
	geminiModel     = "gemini-embedding-001"
	defaultBatchMax = 10 // Process 10 tasks at a time by default
)

// Input is the skill input schema.
type Input struct {
	// Scope determines which tasks to embed: "all", "pending", "completed", or "workspace".
	Scope string `json:"scope"`

	// WorkspaceID is required when Scope is "workspace".
	WorkspaceID string `json:"workspace_id,omitempty"`

	// TaskID is optional - if set, only embed this specific task.
	TaskID string `json:"task_id,omitempty"`

	// BatchSize limits how many tasks to process in one invocation.
	BatchSize int `json:"batch_size,omitempty"`

	// DryRun if true, lists tasks but doesn't generate embeddings.
	DryRun bool `json:"dry_run,omitempty"`
}

// Output is the skill output.
type Output struct {
	Scope        string       `json:"scope"`
	TasksFound   int          `json:"tasks_found"`
	Embedded     int          `json:"embedded"`
	Skipped      int          `json:"skipped"`
	Errors       int          `json:"errors"`
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

func main() {
	if err := run(context.Background(), os.Stdin, os.Stdout); err != nil {
		env := envelope.Error(command, "ERUNTIME", err.Error(), nil)
		_ = json.NewEncoder(os.Stdout).Encode(env)
		os.Exit(1)
	}
}

func run(ctx context.Context, r io.Reader, w io.Writer) error {
	var input Input
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return fmt.Errorf("parse input: %w", err)
	}

	// Validate input
	if input.Scope == "" && input.TaskID == "" {
		return fmt.Errorf("scope or task_id is required (scope: all, pending, completed, workspace)")
	}
	if input.Scope == "workspace" && input.WorkspaceID == "" {
		return fmt.Errorf("workspace_id is required when scope is 'workspace'")
	}
	if input.BatchSize <= 0 {
		input.BatchSize = defaultBatchMax
	}

	start := time.Now()
	output := Output{
		Scope: input.Scope,
	}

	// Check for API key
	geminiKey := os.Getenv("GEMINI_API_KEY")
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if geminiKey == "" && voyageKey == "" && !input.DryRun {
		return fmt.Errorf("no embedding API key set (GEMINI_API_KEY or VOYAGE_API_KEY)")
	}

	// Open task store
	root := getStorageRoot()
	taskStore, err := tasks.Open(ctx, root)
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}
	defer taskStore.Close() //nolint:errcheck

	// Get tasks based on scope
	var taskList []tasks.Task
	if input.TaskID != "" {
		// Single task mode
		task, err := taskStore.Get(ctx, input.TaskID)
		if err != nil {
			return fmt.Errorf("get task %s: %w", input.TaskID, err)
		}
		taskList = []tasks.Task{task}
	} else {
		// List tasks - we get all and filter by scope
		// Note: tasks.ListByWorkspace requires a workspace ID, so we iterate all workspaces
		taskList, err = listAllTasks(ctx, taskStore, input.Scope, input.WorkspaceID)
		if err != nil {
			return fmt.Errorf("list tasks: %w", err)
		}
	}

	output.TasksFound = len(taskList)

	// Apply batch limit
	if len(taskList) > input.BatchSize {
		taskList = taskList[:input.BatchSize]
	}

	// Dry run - just list tasks
	if input.DryRun {
		for _, t := range taskList {
			content := taskEmbeddingContent(t)
			output.Tasks = append(output.Tasks, TaskResult{
				TaskID:  t.ID,
				Title:   t.Title,
				Status:  "dry_run",
				Message: fmt.Sprintf("Would embed %d chars", len(content)),
			})
		}
		output.DurationMs = time.Since(start).Milliseconds()
		return json.NewEncoder(w).Encode(envelope.OK(command, output))
	}

	// Initialize embedding provider (prefer Voyage, fall back to Gemini)
	var provider interface {
		Embed(ctx context.Context, text string) ([]float32, error)
		Model() string
	}

	if voyageKey != "" {
		vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return fmt.Errorf("voyage provider: %w", err)
		}
		provider = vp
	} else {
		gp, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey:        geminiKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return fmt.Errorf("gemini provider: %w", err)
		}
		provider = gp
	}

	// Open memory store
	memStore, err := memory.Open(ctx, root, filepath.Join(root, "cas"))
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close() //nolint:errcheck

	// Resolve session ID for all entries
	sessionID := resolveSessionID()

	// Process each task
	for _, t := range taskList {
		content := taskEmbeddingContent(t)
		if content == "" {
			output.Skipped++
			output.Tasks = append(output.Tasks, TaskResult{
				TaskID:  t.ID,
				Title:   t.Title,
				Status:  "skipped",
				Message: "No content to embed",
			})
			continue
		}

		// Generate embedding
		embedding, err := provider.Embed(ctx, content)
		if err != nil {
			output.Errors++
			output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("%s: %v", t.ID, err))
			output.Tasks = append(output.Tasks, TaskResult{
				TaskID:  t.ID,
				Title:   t.Title,
				Status:  "error",
				Message: err.Error(),
			})
			continue
		}

		// Store embedding in memory.db
		// Name format: task://<task_id>
		name := fmt.Sprintf("task://%s", t.ID)
		workspace := t.WorkspaceID
		if workspace == "" {
			workspace = "default"
		}

		// Build result metadata
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
			output.Tasks = append(output.Tasks, TaskResult{
				TaskID:  t.ID,
				Title:   t.Title,
				Status:  "error",
				Message: err.Error(),
			})
			continue
		}

		// Update embedding
		if err := memStore.UpdateEmbedding(ctx, name, workspace, embedding); err != nil {
			output.Errors++
			output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("%s: update embedding failed: %v", t.ID, err))
			output.Tasks = append(output.Tasks, TaskResult{
				TaskID:  t.ID,
				Title:   t.Title,
				Status:  "error",
				Message: err.Error(),
			})
			continue
		}

		output.Embedded++
		output.Tasks = append(output.Tasks, TaskResult{
			TaskID:     t.ID,
			Title:      t.Title,
			Status:     "embedded",
			Dimensions: len(embedding),
		})
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return json.NewEncoder(w).Encode(envelope.OK(command, output))
}

// taskEmbeddingContent builds rich semantic text from task fields.
// Format: Title + Description + Notes + Gotchas for maximum semantic coverage.
func taskEmbeddingContent(t tasks.Task) string {
	var parts []string

	// Title is always included
	if t.Title != "" {
		parts = append(parts, "Task: "+t.Title)
	}

	// Description provides context
	if t.Description != "" {
		parts = append(parts, "Description: "+t.Description)
	}

	// Notes capture implementation details
	if t.Notes != "" {
		parts = append(parts, "Notes: "+t.Notes)
	}

	// Gotchas are valuable for future reference
	if t.Gotchas != "" {
		parts = append(parts, "Gotchas: "+t.Gotchas)
	}

	// Status provides context about completion
	if t.Status != "" {
		parts = append(parts, "Status: "+t.Status)
	}

	return strings.Join(parts, "\n")
}

// listAllTasks lists tasks based on scope, filtering by status as needed.
func listAllTasks(ctx context.Context, store tasks.Store, scope, workspaceID string) ([]tasks.Task, error) {
	// Get all tasks from the specified workspace (or current workspace)
	var allTasks []tasks.Task

	// Default to current working directory if no workspace specified
	if workspaceID == "" {
		cwd, err := os.Getwd()
		if err == nil {
			workspaceID = cwd
		}
	}

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

func getStorageRoot() string {
	log := zerolog.New(os.Stderr).With().Str("skill", command).Logger()

	cfg, err := config.Load(context.Background())
	if err != nil {
		log.Warn().Err(err).Msg("failed to load config, using fallbacks")
	}
	if err == nil && cfg.Storage.Root != "" {
		return cfg.Storage.Root
	}

	if root := os.Getenv("AGENTCTL_HOME"); root != "" {
		return filepath.Join(root, "storage")
	}

	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agentctl", "storage")
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// resolveSessionID returns the session ID from environment variables.
// Priority: AGENTCTL_SESSION_ID > CLAUDE_SESSION_ID > OPENCODE_SESSION_ID >
// CURSOR_SESSION_ID > TERM_SESSION_ID. Returns empty string if none set.
func resolveSessionID() string {
	if sid := os.Getenv("AGENTCTL_SESSION_ID"); sid != "" {
		return sid
	}
	if sid := os.Getenv("CLAUDE_SESSION_ID"); sid != "" {
		return sid
	}
	if sid := os.Getenv("OPENCODE_SESSION_ID"); sid != "" {
		return sid
	}
	if sid := os.Getenv("CURSOR_SESSION_ID"); sid != "" {
		return sid
	}
	if sid := os.Getenv("TERM_SESSION_ID"); sid != "" {
		return sid
	}
	return ""
}
