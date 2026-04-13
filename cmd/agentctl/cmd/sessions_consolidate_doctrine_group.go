package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/sessionscmd"
	"github.com/jkatigb/agentctl/internal/context/transcriptpipeline"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newSessionsConsolidateDoctrineGroupCommand() *cobra.Command {
	var sourceFiles []string
	var actorID string
	var workspace string
	var frameLimit int
	var blobSummaryMode string
	var blobSummaryModel string
	var blobSummaryTimeout time.Duration
	var persistMemory bool
	var objectiveAlign bool

	cmd := &cobra.Command{
		Use:   "consolidate-doctrine-group",
		Short: "Build grouped doctrine-oriented memory from cached transcript bridge seeds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(sourceFiles) == 0 {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.consolidate_doctrine_group", "--source-file is required", "Pass one or more --source-file values.")
			}
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				runtime := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(blobSummaryMode), strings.TrimSpace(blobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), blobSummaryTimeout)
				result, err := transcriptpipeline.RunGroupedDoctrine(ctx, transcriptpipeline.GroupRunOptions{
					StorageRoot:   cfg.Storage.Root,
					CASPath:       cfg.Paths.CAS,
					SourceFiles:   sourceFiles,
					ActorID:       strings.TrimSpace(actorID),
					Workspace:     strings.TrimSpace(workspace),
					FrameLimit:    frameLimit,
					Runtime:       runtime,
					PersistMemory: persistMemory,
					AlignDoctrine: objectiveAlign,
				})
				if err != nil {
					return err
				}
				payload := map[string]any{
					"group_count":           len(result.Groups),
					"groups":                result.Groups,
					"transcript_cache_root": result.TranscriptCacheRoot,
					"transcript_cache_path": result.TranscriptCachePath,
					"persist_memory":        persistMemory,
				}
				return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.consolidate_doctrine_group", payload)
			})
		},
	}

	cmd.Flags().StringSliceVar(&sourceFiles, "source-file", nil, "Path(s) to source session JSONL")
	cmd.Flags().StringVar(&actorID, "actor-id", "actor:system:source-import", "Actor ID for synthesized turns")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path hint")
	cmd.Flags().IntVar(&frameLimit, "frame-limit", 0, "Maximum anchored frames per grouped conversation (0 = all)")
	cmd.Flags().StringVar(&blobSummaryMode, "blob-summary-mode", "auto", "Reference/tool blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&blobSummaryModel, "blob-summary-model", "nvidia/nemotron-3-nano-4b", "Model to use for one-shot LMStudio summaries")
	cmd.Flags().DurationVar(&blobSummaryTimeout, "blob-summary-timeout", 45*time.Second, "Timeout for one-shot summaries")
	cmd.Flags().BoolVar(&persistMemory, "persist-memory", false, "Persist doctrine-oriented transcript memories into named_memory")
	cmd.Flags().BoolVar(&objectiveAlign, "objective-align", false, "Apply objective alignment on doctrine claims before persistence")
	return cmd
}
