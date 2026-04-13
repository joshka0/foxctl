package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

func newOrientCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "orient",
		Short: "Compute top-of-mind and scaffold the workspace context runtime plane",
		Long: `Builds a bounded top-of-mind bundle from the current workspace task/session state
and persists it under .agentctl/runtime/ together with default policy files and
Obsidian vault templates.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			cfg, err := loadConfig(ctx)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			target := strings.TrimSpace(workspacePath)
			if target == "" {
				target = ws.Detect("")
			} else {
				target = ws.Detect(target)
			}
			target = ws.Normalize(target)
			if target == "" {
				return fmt.Errorf("detect workspace")
			}

			taskStore, err := tasks.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open task store: %w", err)
			}
			defer func() { _ = taskStore.Close() }()

			sessionStore, err := sessions.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return fmt.Errorf("open session store: %w", err)
			}
			defer func() { _ = sessionStore.Close() }()

			orienter := contextplane.NewOrienter(taskStore, sessionStore)
			top, err := orienter.Build(ctx, target)
			if err != nil {
				return fmt.Errorf("build orientation: %w", err)
			}

			workspaceStore := contextplane.NewWorkspaceStore(target)
			layout, err := workspaceStore.SaveTopOfMind(top)
			if err != nil {
				return fmt.Errorf("persist orientation: %w", err)
			}

			data := map[string]any{
				"workspace_path": target,
				"top_of_mind":    top,
				"paths": map[string]string{
					"runtime_root":          layout.RuntimeDir,
					"top_of_mind":           layout.TopOfMindPath,
					"orientation_export":    layout.OrientationExportPath,
					"retrieval_policy":      layout.RetrievalPolicyPath,
					"promotion_policy":      layout.PromotionPolicyPath,
					"task_types_policy":     layout.TaskTypesPolicyPath,
					"obsidian_template_dir": layout.TemplatesDir,
				},
				"summary": fmt.Sprintf(
					"Oriented %s with %d active task(s), %d blocker(s), and %d next action(s).",
					filepath.Base(target),
					len(top.ActiveTaskIDs),
					len(top.Blockers),
					len(top.NextActions),
				),
			}

			env := envelope.OK("context/orient", data, envelope.WithMeta(envelope.Meta{
				Source: "cli",
			}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path to orient (default: auto-detect from cwd)")
	return cmd
}

func init() {
	rootCmd.AddCommand(newOrientCommand())
}
