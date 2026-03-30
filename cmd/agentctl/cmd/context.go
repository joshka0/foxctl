package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

func newContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Inspect and promote ACA workspace control-plane state",
	}
	cmd.AddCommand(
		newContextShowCommand(),
		newContextReportCommand(),
		newContextRetrieveCommand(),
		newContextRetrieveInspectCommand(),
		newContextRetrieveInspectSuiteCommand(),
		newContextRetrieveInspectRunsCommand(),
		newContextRetrieveInspectArtifactCommand(),
		newContextRepoIndexSearchInspectSuiteCommand(),
		newContextRepoIndexDAGInspectSuiteCommand(),
		newContextSemanticSearchInspectSuiteCommand(),
		newContextTaskHistoryCommand(),
		newContextTaskHistorySummaryCommand(),
		newContextFamilyHistorySummaryCommand(),
		newContextCoChangeCommand(),
		newContextMotifsCommand(),
		newContextNextCommand(),
		newContextNextProposalMergeCommand(),
		newContextDispatchCommand(),
		newContextContradictionsCommand(),
		newContextRethinkCommand(),
		newContextHandoffsCommand(),
		newContextObservationsCommand(),
		newContextTensionsCommand(),
		newContextProposalsCommand(),
		newContextProposalCommand(),
		newContextImportEvidenceCommand(),
		newContextInferCommand(),
		newContextPromoteCommand(),
		newContextMergePromotionCommand(),
		newContextHooksCommand(),
	)
	return cmd
}

func newContextShowCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the current top-of-mind bundle",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			top, err := store.LoadTopOfMind()
			if err != nil {
				return fmt.Errorf("load top_of_mind: %w", err)
			}
			env := envelope.OK("context/show", map[string]any{
				"workspace_path": target,
				"top_of_mind":    top,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newContextReportCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Build a synthesized ACA current-state report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			report, err := store.BuildReport()
			if err != nil {
				return fmt.Errorf("build report: %w", err)
			}
			env := envelope.OK("context/report", map[string]any{
				"workspace_path": target,
				"report":         report,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newContextRetrieveCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var query string
	var limit int

	cmd := &cobra.Command{
		Use:   "retrieve",
		Short: "Blend ACA state with ranked vault hits",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("--query is required")
			}
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store := contextplane.NewWorkspaceStore(target)
			index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = index.Close() }()
			memStore, err := memorystore.OpenWithConfig(ctx, cfg)
			if err != nil {
				return err
			}
			defer func() { _ = memStore.Close() }()
			repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
			if err != nil {
				return err
			}
			defer func() { _ = repo.Close() }()
			result, err := store.RetrieveWithOptionsAndMemory(ctx, index, repo, openObsidianSemanticProvider(cfg), memStore, query, limit, store.CurrentRetrievalOptions())
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/retrieve", map[string]any{
				"workspace_path": target,
				"vault_path":     vaultPath,
				"result":         result,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&query, "query", "", "Retrieval query")
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum ranked vault hits")
	return cmd
}

func newContextNextCommand() *cobra.Command {
	var workspacePath string

	cmd := &cobra.Command{
		Use:   "next",
		Short: "Select the next ACA task candidate from the workspace task store",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			taskDB, err := taskstore.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return err
			}
			defer func() { _ = taskDB.Close() }()
			workspaceID := workspace.CanonicalID(target)
			task, ok, err := contextplane.SelectNextTask(ctx, taskDB, workspaceID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no eligible task found")
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/next", map[string]any{
				"workspace_path": target,
				"task":           task,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	return cmd
}

func newContextDispatchCommand() *cobra.Command {
	var workspacePath string
	var taskID string

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Build a bounded ACA task packet for the selected or next task",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			taskDB, err := taskstore.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return err
			}
			defer func() { _ = taskDB.Close() }()
			workspaceID := workspace.CanonicalID(target)
			store := contextplane.NewWorkspaceStore(target)
			packet, err := store.BuildTaskPacket(ctx, taskDB, workspaceID, strings.TrimSpace(taskID))
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/dispatch", map[string]any{
				"workspace_path": target,
				"packet":         packet,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Explicit task ID (default: use context next)")
	return cmd
}

func newContextContradictionsCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var limit int

	cmd := &cobra.Command{
		Use:   "contradictions",
		Short: "Link open tensions to relevant indexed vault notes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			store := contextplane.NewWorkspaceStore(target)
			index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
			if err != nil {
				return err
			}
			defer func() { _ = index.Close() }()
			repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
			if err != nil {
				return err
			}
			defer func() { _ = repo.Close() }()
			findings, err := store.DetectContradictions(ctx, index, repo, openObsidianSemanticProvider(cfg), limit)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/contradictions", map[string]any{
				"workspace_path": target,
				"vault_path":     vaultPath,
				"findings":       findings,
				"count":          len(findings),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum tension findings")
	return cmd
}

func newContextRethinkCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var limit int

	cmd := &cobra.Command{
		Use:   "rethink",
		Short: "Generate maintenance tasks from repeated or high-impact tensions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			store := contextplane.NewWorkspaceStore(target)
			if strings.TrimSpace(vaultPath) != "" {
				cfg, err := loadConfig(ctx)
				if err != nil {
					return err
				}
				index, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
				if err != nil {
					return err
				}
				defer func() { _ = index.Close() }()
				health, err := index.Health(ctx)
				if err != nil {
					return err
				}
				tasks, err := store.GenerateMaintenanceTasksWithHealth(cmd.Context(), limit, &health)
				if err != nil {
					return fmt.Errorf("generate maintenance tasks: %w", err)
				}
				env := envelope.OK("context/rethink", map[string]any{
					"workspace_path":    target,
					"vault_path":        vaultPath,
					"maintenance_tasks": tasks,
					"count":             len(tasks),
				}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
				return envelope.Write(cmd.OutOrStdout(), env)
			}
			tasks, err := store.GenerateMaintenanceTasks(cmd.Context(), limit)
			if err != nil {
				return fmt.Errorf("generate maintenance tasks: %w", err)
			}
			env := envelope.OK("context/rethink", map[string]any{
				"workspace_path":    target,
				"vault_path":        vaultPath,
				"maintenance_tasks": tasks,
				"count":             len(tasks),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path for health-derived maintenance tasks")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum maintenance tasks to emit")
	return cmd
}

func newContextHandoffsCommand() *cobra.Command {
	var workspacePath string
	var handoffPath string
	var limit int

	cmd := &cobra.Command{
		Use:   "handoffs",
		Short: "List handoffs or load a specific handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			if strings.TrimSpace(handoffPath) != "" {
				handoff, err := store.LoadHandoff(handoffPath)
				if err != nil {
					return fmt.Errorf("load handoff: %w", err)
				}
				env := envelope.OK("context/handoffs", map[string]any{
					"workspace_path": target,
					"handoff":        handoff,
					"path":           handoffPath,
				}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
				return envelope.Write(cmd.OutOrStdout(), env)
			}
			items, err := store.ListHandoffs(limit)
			if err != nil {
				return fmt.Errorf("list handoffs: %w", err)
			}
			env := envelope.OK("context/handoffs", map[string]any{
				"workspace_path": target,
				"handoffs":       items,
				"count":          len(items),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&handoffPath, "path", "", "Specific handoff path or filename")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum handoffs to list")
	return cmd
}

func newContextObservationsCommand() *cobra.Command {
	var workspacePath string
	var limit int

	cmd := &cobra.Command{
		Use:   "observations",
		Short: "List recorded observations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			items, err := store.ListObservations(limit)
			if err != nil {
				return fmt.Errorf("list observations: %w", err)
			}
			env := envelope.OK("context/observations", map[string]any{
				"workspace_path": target,
				"observations":   items,
				"count":          len(items),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum observations to list")
	return cmd
}

func newContextTensionsCommand() *cobra.Command {
	var workspacePath string
	var limit int

	cmd := &cobra.Command{
		Use:   "tensions",
		Short: "List recorded tensions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			items, err := store.ListTensions(limit)
			if err != nil {
				return fmt.Errorf("list tensions: %w", err)
			}
			env := envelope.OK("context/tensions", map[string]any{
				"workspace_path": target,
				"tensions":       items,
				"count":          len(items),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum tensions to list")
	return cmd
}

func newContextPromoteCommand() *cobra.Command {
	var workspacePath string
	var sourceKind string
	var sourcePath string
	var sourceID string
	var noteType string
	var title string

	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Draft a promotion note from the latest or selected handoff",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)

			sourceKind = defaultString(sourceKind, "handoff")
			switch sourceKind {
			case "handoff":
				path := strings.TrimSpace(sourcePath)
				if path == "" {
					items, err := store.ListHandoffs(1)
					if err != nil {
						return fmt.Errorf("list handoffs: %w", err)
					}
					if len(items) == 0 {
						return fmt.Errorf("no handoffs available to promote")
					}
					path = items[0].Path
				}
				result, err := store.DraftPromotionFromHandoff(path, noteType, title)
				if err != nil {
					return fmt.Errorf("draft promotion: %w", err)
				}
				env := envelope.OK("context/promote", map[string]any{
					"workspace_path": target,
					"draft":          result,
					"summary":        fmt.Sprintf("Drafted %s at %s.", result.Job.NoteType, filepath.Base(result.DraftPath)),
				}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
				return envelope.Write(cmd.OutOrStdout(), env)
			case "observation":
				result, err := store.DraftPromotionFromObservation(strings.TrimSpace(sourceID), noteType, title)
				if err != nil {
					return fmt.Errorf("draft promotion: %w", err)
				}
				env := envelope.OK("context/promote", map[string]any{
					"workspace_path": target,
					"draft":          result,
					"summary":        fmt.Sprintf("Drafted %s at %s.", result.Job.NoteType, filepath.Base(result.DraftPath)),
				}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
				return envelope.Write(cmd.OutOrStdout(), env)
			default:
				return fmt.Errorf("unsupported --source %q", sourceKind)
			}
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&sourceKind, "source", "handoff", "Promotion source: handoff or observation")
	cmd.Flags().StringVar(&sourcePath, "path", "", "Handoff path or filename (default: latest handoff)")
	cmd.Flags().StringVar(&sourceID, "id", "", "Observation ID when --source=observation")
	cmd.Flags().StringVar(&noteType, "type", "investigation", "Draft note type")
	cmd.Flags().StringVar(&title, "title", "", "Draft note title")
	return cmd
}

func newContextMergePromotionCommand() *cobra.Command {
	var workspacePath string
	var vaultName string
	var vaultPath string
	var draftPath string
	var targetPath string
	var heading string

	cmd := &cobra.Command{
		Use:   "merge-promotion",
		Short: "Explicitly review and merge a drafted promotion into a canonical vault note",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			if strings.TrimSpace(targetPath) == "" {
				return fmt.Errorf("--target-path is required")
			}
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			result, err := store.MergePromotionDraft(cmd.Context(), vaultName, vaultPath, draftPath, targetPath, heading)
			if err != nil {
				return fmt.Errorf("merge promotion: %w", err)
			}
			env := envelope.OK("context/merge_promotion", map[string]any{
				"workspace_path": target,
				"vault_name":     vaultName,
				"vault_path":     vaultPath,
				"merge":          result,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultName, "vault-name", "", "Vault name")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&draftPath, "draft-path", "", "Promotion draft path (default: latest drafted job)")
	cmd.Flags().StringVar(&targetPath, "target-path", "", "Canonical target note path inside the vault")
	cmd.Flags().StringVar(&heading, "heading", "", "Bounded review heading for appending into an existing canonical note")
	return cmd
}

func newContextInferCommand() *cobra.Command {
	var workspacePath string
	var summary string
	var project string
	var area string
	var evidenceRefs []string
	var apply bool

	cmd := &cobra.Command{
		Use:   "infer",
		Short: "Infer ACA observations and tensions from a compact summary",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(summary) == "" {
				return fmt.Errorf("--summary is required")
			}
			target := resolveContextWorkspace(workspacePath)
			if strings.TrimSpace(project) == "" {
				project = filepath.Base(target)
			}
			if strings.TrimSpace(area) == "" {
				area = "aca"
			}
			inference := contextplane.InferInsights(summary, project, area, evidenceRefs)
			createdObs := 0
			createdTensions := 0
			if apply {
				store := contextplane.NewWorkspaceStore(target)
				for _, obs := range inference.Observations {
					if _, err := store.AppendObservation(obs); err != nil {
						return fmt.Errorf("append observation: %w", err)
					}
					createdObs++
				}
				for _, tension := range inference.Tensions {
					if _, err := store.AppendTension(tension); err != nil {
						return fmt.Errorf("append tension: %w", err)
					}
					createdTensions++
				}
			}
			env := envelope.OK("context/infer", map[string]any{
				"workspace_path":       target,
				"inference":            inference,
				"applied":              apply,
				"created_observations": createdObs,
				"created_tensions":     createdTensions,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&summary, "summary", "", "Compact summary text to analyze")
	cmd.Flags().StringVar(&project, "project", "", "Project name override")
	cmd.Flags().StringVar(&area, "area", "", "Area name override")
	cmd.Flags().StringSliceVar(&evidenceRefs, "evidence-ref", nil, "Evidence ref (repeatable)")
	cmd.Flags().BoolVar(&apply, "apply", false, "Persist inferred observations and tensions")
	return cmd
}

func init() {
	rootCmd.AddCommand(newContextCommand())
}
