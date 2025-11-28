package tools

import (
	"context"
	"fmt"

	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
)

// registerTodoTools registers task/todo management tools.
func (r *Registry) registerTodoTools() error {
	// todo.query - query tasks
	queryTool := dstools.NewFuncTool(
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
					Description: "Filter by tags",
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
	insightsTool := dstools.NewFuncTool(
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
	addTool := dstools.NewFuncTool(
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
					Description: "Tags for the task",
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
	completeTool := dstools.NewFuncTool(
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

	return nil
}

// todoQuery implements the todo.query tool.
// Note: This is a stub - in production it would call the agentctl task store.
func (r *Registry) todoQuery(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	// Extract filter parameters
	status := "all"
	if s, ok := args["status"].(string); ok {
		status = s
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	// Stub implementation - returns placeholder data
	// In production, this would call the agentctl task storage
	tasks := []map[string]any{
		{
			"id":          "task_001",
			"title":       "Example task",
			"status":      "pending",
			"description": "This is a placeholder task",
		},
	}

	_ = status // Will be used when connected to real store
	_ = limit

	return successResult(map[string]any{
		"tasks": tasks,
		"count": len(tasks),
		"note":  "Stub implementation - connect to agentctl task store for real data",
	}), nil
}

// todoGraphInsights implements the todo.graph_insights tool.
// Note: This is a stub - in production it would use gonum graph analysis.
func (r *Registry) todoGraphInsights(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	insightType := "all"
	if t, ok := args["insight_type"].(string); ok {
		insightType = t
	}

	// Stub implementation
	insights := map[string]any{
		"insight_type": insightType,
		"critical_path": []string{
			"task_001 -> task_002 -> task_003",
		},
		"blockers": []map[string]any{
			{
				"task_id":        "task_002",
				"blocked_count":  3,
				"recommendation": "Prioritize this task to unblock others",
			},
		},
		"note": "Stub implementation - connect to gonum graph analysis for real insights",
	}

	return successResult(insights), nil
}

// todoAdd implements the todo.add tool.
// Note: This is a stub - in production it would call the agentctl task store.
func (r *Registry) todoAdd(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	title, ok := args["title"].(string)
	if !ok || title == "" {
		return errorResult("title is required"), nil
	}

	description := ""
	if d, ok := args["description"].(string); ok {
		description = d
	}

	// Stub implementation - would create task in agentctl store
	newTask := map[string]any{
		"id":          "task_new_001",
		"title":       title,
		"description": description,
		"status":      "pending",
		"created_at":  "2024-01-01T00:00:00Z",
	}

	return successResult(map[string]any{
		"task":    newTask,
		"success": true,
		"note":    "Stub implementation - connect to agentctl task store for real creation",
	}), nil
}

// todoComplete implements the todo.complete tool.
// Note: This is a stub - in production it would call the agentctl task store.
func (r *Registry) todoComplete(_ context.Context, args map[string]any) (*models.CallToolResult, error) {
	id, ok := args["id"].(string)
	if !ok || id == "" {
		return errorResult("id is required"), nil
	}

	summary := ""
	if s, ok := args["summary"].(string); ok {
		summary = s
	}

	// Stub implementation
	_ = summary

	return successResult(map[string]any{
		"id":      id,
		"status":  "completed",
		"success": true,
		"note":    "Stub implementation - connect to agentctl task store for real completion",
	}), nil
}
