package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/config"
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
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks from the store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"operation": "list",
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
