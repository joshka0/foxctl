package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

func newTodoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "todo",
		Short: "Manage TODOs via the todo/manage skill",
	}
	cmd.AddCommand(
		newTodoAddCommand(),
		newTodoCompleteCommand(),
		newTodoListCommand(),
		newTodoActiveCommand(),
		newTodoInsightsCommand(),
		newTodoRecommendCommand(),
		newTodoPlanCommand(),
		newTodoSearchCommand(),
	)
	return cmd
}

func newTodoAddCommand() *cobra.Command {
	var workspaceID string
	var title string
	var desc string
	var parentID string
	var depends []string
	var scopePath string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a new task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectBackticks("title", title); err != nil {
				return err
			}
			if err := rejectBackticks("description", desc); err != nil {
				return err
			}
			payload := map[string]any{
				"operation": "add",
				"add": map[string]any{
					"title":       title,
					"description": desc,
					"parent_id":   parentID,
					"depends_on":  depends,
					"scope_path":  scopePath,
				},
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Task title (required)")
	cmd.Flags().StringVar(&desc, "description", "", "Task description")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent task ID")
	cmd.Flags().StringSliceVar(&depends, "depends-on", nil, "Dependency task IDs")
	cmd.Flags().StringVar(&scopePath, "scope", "", "Scope path for the task")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	if err := cmd.MarkFlagRequired("title"); err != nil {
		panic(err)
	}
	return cmd
}

func newTodoCompleteCommand() *cobra.Command {
	var workspaceID string
	var taskID string
	var notes string
	var gotchas string

	cmd := &cobra.Command{
		Use:   "complete",
		Short: "Complete a task with notes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectBackticks("notes", notes); err != nil {
				return err
			}
			if err := rejectBackticks("gotchas", gotchas); err != nil {
				return err
			}
			payload := map[string]any{
				"operation": "complete",
				"complete": map[string]any{
					"id":      taskID,
					"notes":   notes,
					"gotchas": gotchas,
				},
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&taskID, "id", "", "Task ID to complete (required)")
	cmd.Flags().StringVar(&notes, "notes", "", "Completion notes")
	cmd.Flags().StringVar(&gotchas, "gotchas", "", "Gotchas to remember")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
	return cmd
}

func newTodoListCommand() *cobra.Command {
	var workspaceID string
	var ranked bool
	var status string
	var sortBy string
	var includeMetrics bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks with optional PageRank sorting",
		Long: `List tasks from the store with optional PageRank prioritization.

Examples:
  # Simple list
  agentctl todo list

  # Ranked by PageRank (most depended-upon first)
  agentctl todo list --ranked

  # Pending tasks sorted by critical path
  agentctl todo list --status pending --sort-by critical_path

  # Full metrics for all tasks
  agentctl todo list --ranked --include-metrics`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			listOpts := map[string]any{}
			if ranked {
				listOpts["ranked"] = true
			}
			if status != "" {
				listOpts["status"] = status
			}
			if sortBy != "" {
				listOpts["sort_by"] = sortBy
			}
			if includeMetrics {
				listOpts["include_metrics"] = true
			}

			payload := map[string]any{
				"operation": "list",
			}
			if len(listOpts) > 0 {
				payload["list"] = listOpts
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	cmd.Flags().BoolVar(&ranked, "ranked", false, "Include PageRank scores and sort by priority")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status: pending, in_progress, completed, blocked")
	cmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by: created_at, pagerank, critical_path")
	cmd.Flags().BoolVar(&includeMetrics, "include-metrics", false, "Include full graph metrics (degrees, critical path)")
	return cmd
}

func newTodoActiveCommand() *cobra.Command {
	var workspaceID string
	cmd := &cobra.Command{
		Use:   "active",
		Short: "Show the active task for the current workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"operation": "get_active",
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	return cmd
}

func newTodoInsightsCommand() *cobra.Command {
	var workspaceID string
	var includeCompleted bool
	var limit int

	cmd := &cobra.Command{
		Use:   "insights",
		Short: "Show task graph insights (PageRank, critical path, cycles)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"operation": "graph_insights",
				"graph_insights": map[string]any{
					"include_completed": includeCompleted,
					"limit":             limit,
				},
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	cmd.Flags().BoolVar(&includeCompleted, "include-completed", false, "Include completed tasks in analysis")
	cmd.Flags().IntVar(&limit, "limit", 0, "Max nodes to return (0 = all)")
	return cmd
}

func newTodoRecommendCommand() *cobra.Command {
	var workspaceID string
	var limit int

	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Recommend next tasks based on graph metrics and mailbox state",
		Long: `Recommend next tasks based on overseer scoring formula:
  - Critical path score (30%)
  - PageRank (20%)
  - Unread admin messages (25%)
  - Unread overseer messages (15%)
  - Recency factor (10%)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"operation": "recommend",
				"recommend": map[string]any{
					"limit": limit,
				},
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max recommendations to return")
	return cmd
}

func newTodoPlanCommand() *cobra.Command {
	var workspaceID string
	var goal string
	var desc string
	var scopePaths []string
	var attachToTaskID string
	var apply bool
	var maxTasks int
	var strategy string

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Create or refine a task plan (epic decomposition)",
		Long: `Create or refine a task plan from a goal.

In draft mode (default), shows proposed tasks without creating them.
In apply mode (--apply), creates the tasks and emits plan.created events.

Examples:
  # Draft a plan
  agentctl todo plan --goal "Implement user authentication"

  # Apply a plan with scope paths
  agentctl todo plan --goal "Add OAuth2 support" --scope cmd/auth --scope internal/auth --apply

  # Refine an existing epic
  agentctl todo plan --goal "Add Google provider" --attach-to <epic-id> --apply`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectBackticks("goal", goal); err != nil {
				return err
			}
			if err := rejectBackticks("description", desc); err != nil {
				return err
			}
			mode := "draft"
			if apply {
				mode = "apply"
			}
			payload := map[string]any{
				"operation": "plan",
				"plan": map[string]any{
					"goal":              goal,
					"description":       desc,
					"scope_paths":       scopePaths,
					"attach_to_task_id": attachToTaskID,
					"mode":              mode,
					"max_tasks":         maxTasks,
					"strategy":          strategy,
				},
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&goal, "goal", "", "One-sentence goal for the plan (required)")
	cmd.Flags().StringVar(&desc, "description", "", "Detailed description/context for planning")
	cmd.Flags().StringSliceVar(&scopePaths, "scope", nil, "Directories/files likely to be touched")
	cmd.Flags().StringVar(&attachToTaskID, "attach-to", "", "Attach subtasks to an existing epic task ID")
	cmd.Flags().BoolVar(&apply, "apply", false, "Apply the plan (create tasks), default is draft mode")
	cmd.Flags().IntVar(&maxTasks, "max-tasks", 20, "Max tasks to create")
	cmd.Flags().StringVar(&strategy, "strategy", "auto", "Planning strategy: auto, epic, or flat")
	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	if err := cmd.MarkFlagRequired("goal"); err != nil {
		panic(err)
	}
	return cmd
}

func newTodoSearchCommand() *cobra.Command {
	var workspaceID string
	var limit int
	var minSimilarity float64

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Semantic search over tasks using embeddings",
		Long: `Search tasks using semantic similarity.

Requires task embeddings to be generated first using:
  agentctl run embedding/tasks --input '{"scope": "all"}'

Examples:
  # Search for tasks related to authentication
  agentctl todo search "user authentication"

  # Search with a higher result limit
  agentctl todo search "database optimization" --limit 20

  # Search with a higher similarity threshold
  agentctl todo search "API endpoints" --min-similarity 0.5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			payload := map[string]any{
				"operation": "search",
				"search": map[string]any{
					"query":          query,
					"limit":          limit,
					"min_similarity": minSimilarity,
				},
			}
			if workspaceID != "" {
				payload["workspace_id"] = workspaceID
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: current working directory)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results to return")
	cmd.Flags().Float64Var(&minSimilarity, "min-similarity", 0.3, "Minimum similarity threshold (0.0-1.0)")
	return cmd
}

func runTodoSkill(cmd *cobra.Command, payload map[string]any) error {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return err
	}
	_, err = findSkill(cfg, "todo/manage")
	if err != nil {
		return fmt.Errorf("todo/manage skill not found (run make skills-build or agentctl skills install): %w", err)
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if payload != nil {
		if _, ok := payload["correlation_id"]; !ok {
			payload["correlation_id"] = ulid.Make().String()
		}
		if _, ok := payload["cli_command"]; !ok {
			payload["cli_command"] = cmd.CommandPath()
		}
		input, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	runCmd.SetOut(cmd.OutOrStdout())
	runCmd.SetErr(cmd.ErrOrStderr())
	runCmd.SetArgs([]string{"--input", string(input), "todo/manage"})
	return runCmd.Execute()
}

func rejectBackticks(field, value string) error {
	if strings.ContainsRune(value, '`') {
		return fmt.Errorf("%s cannot contain backticks (`)", field)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(newTodoCommand())
}
