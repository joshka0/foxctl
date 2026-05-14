package cmd

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
	"github.com/joshka0/foxctl/internal/storage/sessions"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

func newContextTaskHistoryCommand() *cobra.Command {
	var workspacePath string
	var taskID string
	var vaultPath string
	var sessionLimit int
	var handoffLimit int
	var fileLimit int
	var gitCommitLimit int
	var anchorLimit int
	var noteLimit int
	var transcriptHistoryScope string

	cmd := &cobra.Command{
		Use:   "task-history",
		Short: "Build a deterministic continuity pack for a task",
		Long: `Build a deterministic continuity pack for one task.

Transcript history scope controls how transcript-derived continuity is filtered:
- workspace: exact checkout only
- family: any worktree in the same repo family
- auto: try workspace first, then family fallback

Use workspace when parallel worktrees are noisy. Use family when compare/research
work across sibling worktrees should share transcript continuity.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := taskhistory.ParseTranscriptHistoryScope(transcriptHistoryScope)
			if err != nil {
				return err
			}
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

			sessionDB, err := sessions.Open(ctx, cfg.Storage.Root)
			if err != nil {
				return err
			}
			defer func() { _ = sessionDB.Close() }()

			repo, err := repoindex.Open(ctx, cfg.Storage.Root, target)
			if err != nil {
				return err
			}
			defer func() { _ = repo.Close() }()

			var index obsidianindex.Store
			if strings.TrimSpace(vaultPath) != "" {
				idx, err := obsidianindex.Open(ctx, cfg.Storage.Root, vaultPath)
				if err != nil {
					return err
				}
				index = idx
				defer func() { _ = index.Close() }()
			}

			collector := taskhistory.Collector{
				WorkspaceStore:   contextplane.NewWorkspaceStore(target),
				TaskStore:        taskDB,
				SessionStore:     sessionDB,
				RepoStore:        repo,
				VaultIndex:       index,
				SemanticProvider: openObsidianSemanticProvider(cfg),
				GitRunner:        taskhistory.DefaultGitRunner{},
			}
			pack, err := collector.Collect(ctx, taskhistory.Options{
				WorkspacePath:          target,
				WorkspaceID:            workspace.CanonicalID(target),
				TaskID:                 strings.TrimSpace(taskID),
				SessionLimit:           sessionLimit,
				HandoffLimit:           handoffLimit,
				FileLimit:              fileLimit,
				GitCommitLimit:         gitCommitLimit,
				AnchorLimit:            anchorLimit,
				NoteLimit:              noteLimit,
				TranscriptHistoryScope: scope,
			})
			if err != nil {
				return err
			}
			artifact, err := taskhistory.PersistPack(ctx, cfg.Paths.CAS, pack)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/task_history", map[string]any{
				"workspace_path":                     target,
				"vault_path":                         strings.TrimSpace(vaultPath),
				"pack":                               pack,
				"artifact":                           artifact,
				"summary":                            taskhistory.RenderHookArtifactHint(pack),
				"transcript_history_scope_requested": scope,
				"transcript_history_scope_applied":   transcriptHistoryScopeApplied(pack),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Explicit task ID (default: selected/active task)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for ContextWiki durable note retrieval")
	cmd.Flags().IntVar(&sessionLimit, "session-limit", 5, "Maximum relevant sessions to include")
	cmd.Flags().IntVar(&handoffLimit, "handoff-limit", 10, "Maximum handoffs to include")
	cmd.Flags().IntVar(&fileLimit, "file-limit", 12, "Maximum touched files to retain")
	cmd.Flags().IntVar(&gitCommitLimit, "git-limit", 3, "Maximum git commits per file")
	cmd.Flags().IntVar(&anchorLimit, "anchor-limit", 8, "Maximum repo anchors to include")
	cmd.Flags().IntVar(&noteLimit, "note-limit", 5, "Maximum ContextWiki notes to include")
	cmd.Flags().StringVar(&transcriptHistoryScope, "transcript-history-scope", string(taskhistory.TranscriptHistoryScopeAuto), "Transcript history scope: auto, workspace, or family")
	return cmd
}

// newContextTaskHistorySummaryCommand is the structured Codex/agent-facing entrypoint.
// Use the hook wrapper when a prompt-ready hook payload is needed instead.
func newContextTaskHistorySummaryCommand() *cobra.Command {
	var workspacePath string
	var taskID string
	var vaultPath string
	var sessionLimit int
	var handoffLimit int
	var fileLimit int
	var gitCommitLimit int
	var anchorLimit int
	var noteLimit int
	var transcriptHistoryScope string

	cmd := &cobra.Command{
		Use:   "task-history-summary",
		Short: "Build a compact task continuity summary with artifact pointer",
		Long: `Build a compact task continuity summary with artifact pointer.

Transcript history scope controls how transcript-derived continuity is filtered:
- workspace: exact checkout only
- family: any worktree in the same repo family
- auto: try workspace first, then family fallback

Use workspace when parallel worktrees are noisy. Use family when compare/research
work across sibling worktrees should share transcript continuity.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := taskhistory.ParseTranscriptHistoryScope(transcriptHistoryScope)
			if err != nil {
				return err
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}

			pack, artifact, err := collectTaskHistoryPack(ctx, cfg, target, strings.TrimSpace(taskID), strings.TrimSpace(vaultPath), sessionLimit, handoffLimit, fileLimit, gitCommitLimit, anchorLimit, noteLimit, scope)
			if err != nil {
				return err
			}

			rendered := taskhistory.RenderHookContextWithArtifact(pack, artifact)
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/task_history_summary", map[string]any{
				"workspace_path":                     target,
				"vault_path":                         strings.TrimSpace(vaultPath),
				"summary":                            taskhistory.RenderHookArtifactHint(pack),
				"rendered":                           rendered,
				"artifact":                           artifact,
				"task_id":                            pack.Task.ID,
				"task_title":                         strings.TrimSpace(pack.Task.Title),
				"transcript_history_scope_requested": scope,
				"transcript_history_scope_applied":   transcriptHistoryScopeApplied(pack),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Explicit task ID (default: selected/active task)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for ContextWiki durable note retrieval")
	cmd.Flags().IntVar(&sessionLimit, "session-limit", 5, "Maximum relevant sessions to include")
	cmd.Flags().IntVar(&handoffLimit, "handoff-limit", 10, "Maximum handoffs to include")
	cmd.Flags().IntVar(&fileLimit, "file-limit", 12, "Maximum touched files to retain")
	cmd.Flags().IntVar(&gitCommitLimit, "git-limit", 3, "Maximum git commits per file")
	cmd.Flags().IntVar(&anchorLimit, "anchor-limit", 8, "Maximum repo anchors to include")
	cmd.Flags().IntVar(&noteLimit, "note-limit", 5, "Maximum ContextWiki notes to include")
	cmd.Flags().StringVar(&transcriptHistoryScope, "transcript-history-scope", string(taskhistory.TranscriptHistoryScopeAuto), "Transcript history scope: auto, workspace, or family")
	return cmd
}

func collectTaskHistoryPack(
	ctx context.Context,
	cfg config.Config,
	workspacePath, taskID, vaultPath string,
	sessionLimit, handoffLimit, fileLimit, gitCommitLimit, anchorLimit, noteLimit int,
	transcriptHistoryScope taskhistory.TranscriptHistoryScope,
) (taskhistory.Pack, string, error) {
	collector, cleanup, err := taskhistory.OpenCollector(ctx, cfg.Storage.Root, workspacePath, vaultPath)
	if err != nil {
		return taskhistory.Pack{}, "", err
	}
	defer cleanup()
	pack, err := collector.Collect(ctx, taskhistory.Options{
		WorkspacePath:          workspacePath,
		WorkspaceID:            workspace.CanonicalID(workspacePath),
		TaskID:                 taskID,
		SessionLimit:           sessionLimit,
		HandoffLimit:           handoffLimit,
		FileLimit:              fileLimit,
		GitCommitLimit:         gitCommitLimit,
		AnchorLimit:            anchorLimit,
		NoteLimit:              noteLimit,
		TranscriptHistoryScope: transcriptHistoryScope,
	})
	if err != nil {
		return taskhistory.Pack{}, "", err
	}
	artifact, err := taskhistory.PersistPack(ctx, cfg.Paths.CAS, pack)
	if err != nil {
		return taskhistory.Pack{}, "", err
	}
	return pack, artifact, nil
}

func transcriptHistoryScopeApplied(pack taskhistory.Pack) any {
	if pack.Transcript == nil || pack.Transcript.AppliedScope == "" {
		return nil
	}
	return pack.Transcript.AppliedScope
}
