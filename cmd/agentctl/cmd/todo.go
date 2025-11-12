package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/config"
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
	)
	return cmd
}

func newTodoAddCommand() *cobra.Command {
	var storePath string
	var title string
	var desc string
	var parentID string
	var depends []string

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
				},
			}
			if storePath != "" {
				payload["store_path"] = storePath
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Task title (required)")
	cmd.Flags().StringVar(&desc, "description", "", "Task description")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent task ID")
	cmd.Flags().StringSliceVar(&depends, "depends-on", nil, "Dependency task IDs")
	cmd.Flags().StringVar(&storePath, "store", "", "Path to task store (default: ~/.agentctl/todo/tasks.json)")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newTodoCompleteCommand() *cobra.Command {
	var storePath string
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
			if storePath != "" {
				payload["store_path"] = storePath
			}
			return runTodoSkill(cmd, payload)
		},
	}

	cmd.Flags().StringVar(&taskID, "id", "", "Task ID to complete (required)")
	cmd.Flags().StringVar(&notes, "notes", "", "Completion notes")
	cmd.Flags().StringVar(&gotchas, "gotchas", "", "Gotchas to remember")
	cmd.Flags().StringVar(&storePath, "store", "", "Path to task store (default: ~/.agentctl/todo/tasks.json)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newTodoListCommand() *cobra.Command {
	var storePath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks from the store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload := map[string]any{
				"operation": "list",
			}
			if storePath != "" {
				payload["store_path"] = storePath
			}
			return runTodoSkill(cmd, payload)
		},
	}
	cmd.Flags().StringVar(&storePath, "store", "", "Path to task store (default: ~/.agentctl/todo/tasks.json)")
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
