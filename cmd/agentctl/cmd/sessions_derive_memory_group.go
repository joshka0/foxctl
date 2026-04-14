package cmd

import (
	"context"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/cmd/agentctl/cmd/sessionscmd"
	"github.com/jkatigb/agentctl/internal/context/transcriptpipeline"
	"github.com/jkatigb/agentctl/internal/platform/config"
	memorystore "github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/spf13/cobra"
)

func newSessionsDeriveMemoryGroupCommand() *cobra.Command {
	var sourceFiles []string
	var actorID string
	var workspace string
	var frameLimit int
	var memoryLane string
	var objectiveAlign bool
	var blobSummaryMode string
	var blobSummaryModel string
	var blobSummaryTimeout time.Duration
	var persistMemory bool
	var persistHistory bool

	cmd := &cobra.Command{
		Use:   "derive-memory-group",
		Short: "Group multiple transcript files into conversation lineages and derive decision-useful signals through the selected lane",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(sourceFiles) == 0 {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory_group", "--source-file is required", "Pass one or more --source-file values.")
			}
			lane, err := parseTranscriptMemoryLane(memoryLane)
			if err != nil {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory_group", "--memory-lane must be one of: insight, doctrine, mixed", "Use --memory-lane insight for decision support, doctrine for doctrine-first extraction, or mixed for the legacy full pipeline.")
			}
			if lane == transcriptMemoryLaneInsight && persistMemory {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory_group", "--persist-memory is not supported with --memory-lane insight", "Use the insight lane for decision support output only, or switch to doctrine/mixed if you want persistence.")
			}
			if lane != transcriptMemoryLaneInsight && persistHistory {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory_group", "--persist-history is only supported with --memory-lane insight", "Use --memory-lane insight when persisting history-oriented retrieval records.")
			}
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				runtime := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(blobSummaryMode), strings.TrimSpace(blobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), blobSummaryTimeout)
				runOpts := transcriptpipeline.GroupRunOptions{
					StorageRoot:   cfg.Storage.Root,
					CASPath:       cfg.Paths.CAS,
					SourceFiles:   sourceFiles,
					ActorID:       strings.TrimSpace(actorID),
					Workspace:     strings.TrimSpace(workspace),
					FrameLimit:    frameLimit,
					Runtime:       runtime,
					PersistMemory: persistMemory,
					AlignDoctrine: objectiveAlign,
				}
				var result transcriptpipeline.GroupRunResult
				switch lane {
				case transcriptMemoryLaneInsight:
					result, err = transcriptpipeline.RunGroupedInsight(ctx, runOpts)
				case transcriptMemoryLaneDoctrine:
					result, err = transcriptpipeline.RunGroupedDoctrine(ctx, runOpts)
				case transcriptMemoryLaneMixed:
					runOpts.Classifier = transcriptpipeline.NewCachedClaimClassifier(runtime)
					result, err = transcriptpipeline.RunGrouped(ctx, runOpts)
				default:
					return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory_group", "--memory-lane must be one of: doctrine, mixed", "Use --memory-lane doctrine for the doctrine-first path or --memory-lane mixed for the legacy full pipeline.")
				}
				if err != nil {
					return err
				}
				if lane == transcriptMemoryLaneInsight && persistHistory {
					memStore, err := memorystore.OpenFromConfig(ctx, cfg)
					if err != nil {
						return err
					}
					defer memStore.Close()
					if err := persistGroupedHistoryRecords(ctx, memStore, &result, buildHistoryRecordEmbedder(cfg)); err != nil {
						return err
					}
				}

				payload := map[string]any{
					"group_count":           len(result.Groups),
					"groups":                result.Groups,
					"history_profile":       result.HistoryProfile,
					"memory_lane":           string(lane),
					"objective_align":       objectiveAlign,
					"transcript_cache_root": result.TranscriptCacheRoot,
					"transcript_cache_path": result.TranscriptCachePath,
					"persist_memory":        persistMemory,
					"persist_history":       persistHistory,
				}
				return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.derive_memory_group", payload)
			})
		},
	}

	cmd.Flags().StringSliceVar(&sourceFiles, "source-file", nil, "Path(s) to source session JSONL")
	cmd.Flags().StringVar(&actorID, "actor-id", "actor:system:source-import", "Actor ID for synthesized turns")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path hint (used by Claude locate)")
	cmd.Flags().IntVar(&frameLimit, "frame-limit", 20, "Maximum anchored frames per grouped conversation")
	cmd.Flags().StringVar(&memoryLane, "memory-lane", string(transcriptMemoryLaneInsight), "Memory lane: insight, doctrine, or mixed")
	cmd.Flags().BoolVar(&objectiveAlign, "objective-align", false, "Apply objective alignment before persistence when using the doctrine lane")
	cmd.Flags().StringVar(&blobSummaryMode, "blob-summary-mode", "auto", "Reference/tool blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&blobSummaryModel, "blob-summary-model", "nvidia/nemotron-3-nano-4b", "Model to use for one-shot LMStudio summaries")
	cmd.Flags().DurationVar(&blobSummaryTimeout, "blob-summary-timeout", 45*time.Second, "Timeout for one-shot summaries")
	cmd.Flags().BoolVar(&persistMemory, "persist-memory", false, "Persist durable transcript-derived memories into named_memory")
	cmd.Flags().BoolVar(&persistHistory, "persist-history", false, "Persist insight-lane history records into named_memory with best-effort embeddings")
	return cmd
}
