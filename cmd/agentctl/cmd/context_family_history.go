package cmd

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/context/contextplane/taskhistory"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

func newContextFamilyHistorySummaryCommand() *cobra.Command {
	var workspacePath string
	var transcriptHistoryScope string
	var ownerLimit int
	var summaryProvider string
	var summaryModel string
	var focusQuery string
	var dateFrom string
	var dateTo string

	cmd := &cobra.Command{
		Use:   "family-history-summary",
		Short: "Build a repo-family transcript overview across recent worktrees and sessions",
		Long: `Build a repo-family transcript overview across recent worktrees and sessions.

This command summarizes what is going on overall for the repo family, what changed
recently, and the top learnings, risks, surprises, and next work items across
recent transcript owners.

Use --focus-query to bias owner selection toward one coherent transcript lane
instead of summarizing the most recent mixed-family sessions.

Use --date-from and --date-to (YYYY-MM-DD) to bound transcript history before
owner selection and summarization.

Transcript history scope controls how transcript-derived continuity is filtered:
- workspace: exact checkout only
- family: any worktree in the same repo family
- auto: try workspace first, then family fallback`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scope, err := taskhistory.ParseTranscriptHistoryScope(transcriptHistoryScope)
			if err != nil {
				return err
			}
			dateRange, err := taskhistory.ParseTranscriptHistoryDateRange(dateFrom, dateTo)
			if err != nil {
				return err
			}
			target := resolveContextWorkspace(workspacePath)
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			collector, cleanup, err := taskhistory.OpenCollector(ctx, cfg.Storage.Root, target, "")
			if err != nil {
				return err
			}
			defer cleanup()
			collector.TranscriptWorker = taskhistory.TranscriptSummaryWorkerConfig(cfg, summaryProvider, summaryModel)

			overview, err := collector.CollectTranscriptFamilyOverview(ctx, target, scope, ownerLimit, focusQuery, dateRange)
			if err != nil {
				return err
			}
			if overview == nil {
				return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/family_history_summary", map[string]any{
					"workspace_path":                     target,
					"focus_query":                        strings.TrimSpace(focusQuery),
					"date_from":                          strings.TrimSpace(dateRange.DateFrom),
					"date_to":                            strings.TrimSpace(dateRange.DateTo),
					"summary":                            "No transcript family overview available",
					"transcript_history_scope_requested": scope,
					"transcript_history_scope_applied":   nil,
				}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
			}
			artifact, err := taskhistory.PersistValue(ctx, cfg.Paths.CAS, overview, "transcript-family-overview")
			if err != nil {
				return err
			}
			rendered := taskhistory.RenderTranscriptFamilyOverview(*overview, artifact)
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/family_history_summary", map[string]any{
				"workspace_path":                     target,
				"family_path":                        strings.TrimSpace(overview.FamilyPath),
				"focus_query":                        strings.TrimSpace(overview.FocusQuery),
				"date_from":                          strings.TrimSpace(overview.DateFrom),
				"date_to":                            strings.TrimSpace(overview.DateTo),
				"overview":                           overview,
				"rendered":                           rendered,
				"artifact":                           artifact,
				"summary":                            taskhistory.RenderTranscriptFamilyOverviewHint(*overview),
				"transcript_history_scope_requested": scope,
				"transcript_history_scope_applied":   overview.AppliedScope,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&transcriptHistoryScope, "transcript-history-scope", string(taskhistory.TranscriptHistoryScopeFamily), "Transcript history scope: auto, workspace, or family")
	cmd.Flags().IntVar(&ownerLimit, "owner-limit", 6, "Maximum recent transcript owners to summarize")
	cmd.Flags().StringVar(&summaryProvider, "summary-provider", "", "Transcript family summarizer provider override (for example: openrouter or lmstudio)")
	cmd.Flags().StringVar(&summaryModel, "summary-model", "", "Transcript family summarizer model override")
	cmd.Flags().StringVar(&focusQuery, "focus-query", "", "Optional semantic focus query to summarize one coherent transcript lane instead of the most recent mixed-family work")
	cmd.Flags().StringVar(&dateFrom, "date-from", "", "Optional start date (YYYY-MM-DD) for transcript history filtering before summarization")
	cmd.Flags().StringVar(&dateTo, "date-to", "", "Optional end date (YYYY-MM-DD) for transcript history filtering before summarization")
	return cmd
}
