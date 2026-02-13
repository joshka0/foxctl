package tools

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/jkatigb/agentctl/internal/analysis/tasksgraph"
	errspkg "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	tooling "github.com/jkatigb/agentctl/internal/tooling"
)

// registerTodoTools registers task/todo management tools.
func (r *Registry) registerTodoTools() error {
	// todo.query - query tasks
	queryTool := tooling.NewFuncTool(
		"todo.query",
		"Query tasks in the task database. Returns matching tasks with their status and metadata.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"status": {
					Type:        "string",
					Description: "Filter by status: pending, active, completed, blocked, all",
				},
				"tags": {
					Type:        "array",
					Description: "Filter by tags (currently unused)",
				},
				"parent_id": {
					Type:        "string",
					Description: "Filter by parent task ID",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of tasks to return (default 50)",
				},
			},
		},
		r.wrapWithTelemetry("todo.query", r.todoQuery),
	)
	if err := r.tools.Register(queryTool); err != nil {
		return fmt.Errorf("register todo.query: %w", err)
	}

	// todo.graph_insights - get task graph insights
	insightsTool := tooling.NewFuncTool(
		"todo.graph_insights",
		"Get insights about the task graph including critical paths, blocking tasks, and priority recommendations.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"root_id": {
					Type:        "string",
					Description: "Root task ID to analyze from (optional, analyzes all if not specified)",
				},
				"insight_type": {
					Type:        "string",
					Description: "Type of insight: critical_path, blockers, priorities, dependencies, all",
				},
			},
		},
		r.wrapWithTelemetry("todo.graph_insights", r.todoGraphInsights),
	)
	if err := r.tools.Register(insightsTool); err != nil {
		return fmt.Errorf("register todo.graph_insights: %w", err)
	}

	// todo.add - add a new task
	addTool := tooling.NewFuncTool(
		"todo.add",
		"Add a new task to the task database.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"title": {
					Type:        "string",
					Description: "Task title",
					Required:    true,
				},
				"description": {
					Type:        "string",
					Description: "Task description",
				},
				"parent_id": {
					Type:        "string",
					Description: "Parent task ID for subtasks",
				},
				"tags": {
					Type:        "array",
					Description: "Tags for the task (unused)",
				},
				"depends_on": {
					Type:        "array",
					Description: "IDs of tasks this task depends on",
				},
			},
		},
		r.wrapWithTelemetry("todo.add", r.todoAdd),
	)
	if err := r.tools.Register(addTool); err != nil {
		return fmt.Errorf("register todo.add: %w", err)
	}

	// todo.complete - mark a task as complete
	completeTool := tooling.NewFuncTool(
		"todo.complete",
		"Mark a task as completed.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"id": {
					Type:        "string",
					Description: "Task ID to complete",
					Required:    true,
				},
				"summary": {
					Type:        "string",
					Description: "Completion summary",
				},
			},
		},
		r.wrapWithTelemetry("todo.complete", r.todoComplete),
	)
	if err := r.tools.Register(completeTool); err != nil {
		return fmt.Errorf("register todo.complete: %w", err)
	}

	// todo.set_active - set active task
	setActiveTool := tooling.NewFuncTool(
		"todo.set_active",
		"Set the active task for the workspace.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"task_id": {
					Type:        "string",
					Description: "Task ID to set as active",
					Required:    true,
				},
			},
		},
		r.wrapWithTelemetry("todo.set_active", r.todoSetActive),
	)
	if err := r.tools.Register(setActiveTool); err != nil {
		return fmt.Errorf("register todo.set_active: %w", err)
	}

	// todo.ensure_active - ensure an active task exists
	ensureActiveTool := tooling.NewFuncTool(
		"todo.ensure_active",
		"Get the active task, or create one if none exists.",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"default_title": {
					Type:        "string",
					Description: "Title to use if creating a new task",
					Required:    true,
				},
				"scope_path": {
					Type:        "string",
					Description: "Scope path for the new task (optional)",
				},
			},
		},
		r.wrapWithTelemetry("todo.ensure_active", r.todoEnsureActive),
	)
	if err := r.tools.Register(ensureActiveTool); err != nil {
		return fmt.Errorf("register todo.ensure_active: %w", err)
	}

	return nil
}

// todoQuery implements the todo.query tool.
func (r *Registry) todoQuery(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openTasksStore == nil {
		return errorResult("tasks store not configured"), nil
	}

	status := "all"
	if s, ok := args["status"].(string); ok {
		status = s
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	parentID := ""
	if p, ok := args["parent_id"].(string); ok {
		parentID = p
	}

	store, err := r.openTasksStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open tasks store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close tasks store") }()

	allTasks, err := store.ListByWorkspace(ctx, r.config.WorkspaceID)
	if err != nil {
		return errorResult(fmt.Sprintf("list tasks: %v", err)), nil
	}

	// Filter in memory
	var filtered []tasks.Task
	for _, t := range allTasks {
		if status != "all" && t.Status != status {
			continue
		}
		if parentID != "" && t.ParentID != parentID {
			continue
		}
		filtered = append(filtered, t)
	}

	// Truncate to limit
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	// Get active task
	activeTask, hasActive, err := store.GetActive(ctx, r.config.WorkspaceID)
	if err != nil {
		return errorResult(fmt.Sprintf("get active task: %v", err)), nil
	}
	var activeTaskPtr *tasks.Task
	if hasActive {
		activeTaskPtr = &activeTask
	}

	return successResult(map[string]any{
		"tasks":       filtered,
		"active_task": activeTaskPtr,
		"count":       len(filtered),
	}), nil
}

// todoGraphInsights implements the todo.graph_insights tool.
func (r *Registry) todoGraphInsights(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openTasksStore == nil {
		return errorResult("tasks store not configured"), nil
	}

	insightType := "all"
	if t, ok := args["insight_type"].(string); ok {
		insightType = t
	}

	store, err := r.openTasksStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open tasks store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close tasks store") }()

	allTasks, err := store.ListByWorkspace(ctx, r.config.WorkspaceID)
	if err != nil {
		return errorResult(fmt.Sprintf("list tasks: %v", err)), nil
	}

	analyzer := tasksgraph.NewAnalyzer()
	insights, err := analyzer.Analyze(allTasks, r.config.WorkspaceID)
	if err != nil {
		return errorResult(fmt.Sprintf("analyze graph: %v", err)), nil
	}

	result := map[string]any{
		"workspace_id":      insights.WorkspaceID,
		"generated_at":      insights.GeneratedAt,
		"insight_type":      insightType,
		"topological_order": insights.TopologicalOrder,
		"cycles":            insights.Cycles,
		"nodes":             insights.Nodes,
	}

	return successResult(result), nil
}

// todoAdd implements the todo.add tool.
func (r *Registry) todoAdd(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openTasksStore == nil {
		return errorResult("tasks store not configured"), nil
	}

	title, ok := args["title"].(string)
	if !ok || title == "" {
		return errorResult("title is required"), nil
	}

	description := ""
	if d, ok := args["description"].(string); ok {
		description = d
	}

	parentID := ""
	if p, ok := args["parent_id"].(string); ok {
		parentID = p
	}

	var dependsOn []string
	if deps, ok := args["depends_on"].([]any); ok {
		for _, d := range deps {
			if ds, ok := d.(string); ok {
				dependsOn = append(dependsOn, ds)
			}
		}
	}

	store, err := r.openTasksStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open tasks store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close tasks store") }()

	newTask := tasks.Task{
		WorkspaceID: r.config.WorkspaceID,
		Title:       title,
		Description: description,
		ParentID:    parentID,
		DependsOn:   dependsOn,
		Status:      tasks.StatusPending,
	}

	// Validate parent exists BEFORE creating the child to avoid orphaned tasks
	var parent *tasks.Task
	if parentID != "" {
		p, err := store.Get(ctx, parentID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errorResult(fmt.Sprintf("parent task %q not found (hint: use 'todo.query' to list available task IDs)", parentID)), nil
			}
			return errorResult(fmt.Sprintf("validate parent task: %v", err)), nil
		}
		parent = &p
	}

	created, err := store.Add(ctx, newTask)
	if err != nil {
		return errorResult(fmt.Sprintf("add task: %v", err)), nil
	}

	// Update parent's Children list after successful child creation
	if parent != nil {
		parent.Children = append(parent.Children, created.ID)
		if _, err := store.Update(ctx, *parent); err != nil {
			// Child was created but parent update failed - log but return the created task
			return successResult(map[string]any{
				"task":    created,
				"success": true,
				"warning": fmt.Sprintf("task created but parent update failed: %v", err),
			}), nil
		}
	}

	return successResult(map[string]any{
		"task":    created,
		"success": true,
	}), nil
}

// todoComplete implements the todo.complete tool.
func (r *Registry) todoComplete(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openTasksStore == nil {
		return errorResult("tasks store not configured"), nil
	}

	id, ok := args["id"].(string)
	if !ok || id == "" {
		return errorResult("id is required"), nil
	}

	summary := ""
	if s, ok := args["summary"].(string); ok {
		summary = s
	}

	store, err := r.openTasksStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open tasks store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close tasks store") }()

	task, err := store.Get(ctx, id)
	if err != nil {
		return errorResult(fmt.Sprintf("get task: %v", err)), nil
	}

	// Idempotency
	if task.Status == tasks.StatusCompleted {
		return successResult(map[string]any{
			"task":    task,
			"success": true,
			"note":    "task already completed",
		}), nil
	}

	now := time.Now().UTC()
	task.Status = tasks.StatusCompleted
	task.CompletedAt = &now
	if summary != "" {
		if task.Notes != "" {
			task.Notes += "\n\nCompletion summary: " + summary
		} else {
			task.Notes = "Completion summary: " + summary
		}
	}

	updated, err := store.Update(ctx, task)
	if err != nil {
		return errorResult(fmt.Sprintf("update task: %v", err)), nil
	}

	return successResult(map[string]any{
		"task":    updated,
		"success": true,
	}), nil
}

// todoSetActive implements the todo.set_active tool.
func (r *Registry) todoSetActive(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openTasksStore == nil {
		return errorResult("tasks store not configured"), nil
	}

	id, ok := args["task_id"].(string)
	if !ok || id == "" {
		return errorResult("task_id is required"), nil
	}

	store, err := r.openTasksStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open tasks store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close tasks store") }()

	task, err := store.SetActive(ctx, r.config.WorkspaceID, id)
	if err != nil {
		return errorResult(fmt.Sprintf("set active task: %v", err)), nil
	}

	return successResult(map[string]any{
		"active_task": task,
		"success":     true,
	}), nil
}

// todoEnsureActive implements the todo.ensure_active tool.
func (r *Registry) todoEnsureActive(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	if r.openTasksStore == nil {
		return errorResult("tasks store not configured"), nil
	}

	defaultTitle, ok := args["default_title"].(string)
	if !ok || defaultTitle == "" {
		return errorResult("default_title is required"), nil
	}

	scopePath := ""
	if s, ok := args["scope_path"].(string); ok {
		scopePath = s
	}

	store, err := r.openTasksStore(ctx)
	if err != nil {
		return errorResult(fmt.Sprintf("open tasks store: %v", err)), nil
	}
	defer func() { errspkg.Ignore(store.Close(), "close tasks store") }()

	task, created, err := store.EnsureActive(ctx, r.config.WorkspaceID, defaultTitle, scopePath)
	if err != nil {
		return errorResult(fmt.Sprintf("ensure active task: %v", err)), nil
	}

	return successResult(map[string]any{
		"active_task": task,
		"created":     created,
		"success":     true,
	}), nil
}
