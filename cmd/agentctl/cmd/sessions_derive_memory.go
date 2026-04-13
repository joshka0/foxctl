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

func newSessionsDeriveMemoryCommand() *cobra.Command {
	var provider string
	var sourceFile string
	var sessionID string
	var workspace string
	var actorID string
	var frameLimit int
	var memoryLane string
	var objectiveAlign bool
	var explorerTurnLimit int
	var blobSummaryMode string
	var blobSummaryModel string
	var blobSummaryTimeout time.Duration
	var persistMemory bool
	var persistHistory bool

	cmd := &cobra.Command{
		Use:   "derive-memory",
		Short: "Parse one Claude/Codex transcript and derive decision-useful transcript signals through the selected lane",
		RunE: func(cmd *cobra.Command, _ []string) error {
			lane, err := parseTranscriptMemoryLane(memoryLane)
			if err != nil {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory", "--memory-lane must be one of: insight, doctrine, mixed", "Use --memory-lane insight for decision support, doctrine for doctrine-first extraction, or mixed for the legacy full pipeline.")
			}
			if lane == transcriptMemoryLaneInsight && persistMemory {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory", "--persist-memory is not supported with --memory-lane insight", "Use the insight lane for decision support output only, or switch to doctrine/mixed if you want persistence.")
			}
			if lane != transcriptMemoryLaneInsight && persistHistory {
				return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory", "--persist-history is only supported with --memory-lane insight", "Use --memory-lane insight when persisting history-oriented retrieval records.")
			}
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				runtime := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(blobSummaryMode), strings.TrimSpace(blobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), blobSummaryTimeout)
				runOpts := transcriptpipeline.SingleRunOptions{
					StorageRoot:   cfg.Storage.Root,
					CASPath:       cfg.Paths.CAS,
					Provider:      strings.TrimSpace(provider),
					SourceFile:    strings.TrimSpace(sourceFile),
					SessionID:     strings.TrimSpace(sessionID),
					Workspace:     strings.TrimSpace(workspace),
					ActorID:       strings.TrimSpace(actorID),
					FrameLimit:    frameLimit,
					Runtime:       runtime,
					PersistMemory: persistMemory,
					AlignDoctrine: objectiveAlign,
				}

				var result transcriptpipeline.SingleRunResult
				switch lane {
				case transcriptMemoryLaneInsight:
					result, err = transcriptpipeline.RunSingleInsight(ctx, runOpts)
				case transcriptMemoryLaneDoctrine:
					result, err = transcriptpipeline.RunSingleDoctrine(ctx, runOpts)
				case transcriptMemoryLaneMixed:
					runOpts.Classifier = transcriptpipeline.NewCachedClaimClassifier(runtime)
					result, err = transcriptpipeline.RunSingle(ctx, runOpts)
				default:
					return sessionscmd.WriteArgError(cmd.OutOrStdout(), "agentctl.sessions.derive_memory", "--memory-lane must be one of: doctrine, mixed", "Use --memory-lane doctrine for the doctrine-first path or --memory-lane mixed for the legacy full pipeline.")
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
					if err := persistSingleHistoryRecords(ctx, memStore, &result, buildHistoryRecordEmbedder(cfg)); err != nil {
						return err
					}
				}

				payload := map[string]any{
					"provider":                 string(result.Parsed.Provider),
					"session_id":               result.Parsed.SessionID,
					"source_path":              result.Parsed.SourcePath,
					"workspace_path":           result.Parsed.WorkspacePath,
					"workspace_family_path":    result.WorkspaceFamilyPath,
					"conversation_id":          result.ConversationID,
					"turn_count":               len(result.Parsed.Turns),
					"frame_count":              len(result.Frames),
					"memory_lane":              string(lane),
					"insights":                 result.Insights,
					"insight_brief":            result.InsightBrief,
					"insight_timeline":         result.InsightTimeline,
					"notable_insights":         result.NotableInsights,
					"history_profile":          result.HistoryProfile,
					"history_answers":          result.HistoryAnswers,
					"history_pack":             result.HistoryPack,
					"history_records":          result.HistoryRecords,
					"persisted_history":        result.PersistedHistory,
					"removed_history":          result.RemovedHistory,
					"objective":                result.Objective,
					"frames":                   result.Frames,
					"derivations":              result.Derivations,
					"synopses":                 result.Synopses,
					"classified_claims":        result.ClassifiedClaims,
					"consolidated_claims":      result.ConsolidatedClaims,
					"reviewed_claims":          result.ReviewedClaims,
					"doctrine_seed_claims":     result.DoctrineSeedClaims,
					"doctrine_claims":          result.DoctrineClaims,
					"aligned_claims":           result.AlignedClaims,
					"classification_artifact":  result.ClassificationArtifact,
					"classification_artifacts": result.ClassificationArtifacts,
					"review_artifact":          result.ReviewArtifact,
					"doctrine_seed_artifact":   result.DoctrineSeedArtifact,
					"doctrine_artifact":        result.DoctrineArtifact,
					"alignment_artifact":       result.AlignmentArtifact,
					"prederived_artifacts":     result.PrederivedArtifacts,
					"persisted_memory":         result.PersistedMemory,
					"persist_memory":           persistMemory,
					"persist_history":          persistHistory,
					"objective_align":          objectiveAlign,
					"transcript_cache_path":    result.TranscriptCachePath,
					"transcript_cache_root":    result.TranscriptCacheRoot,
					"explorer_prompt":          transcriptpipeline.BuildExplorerPrompt(result.Parsed, result.Derivations, explorerTurnLimit),
					"comparison_hint":          "Use explorer_prompt with an explorer/reviewer agent to compare free-form memoryization against the structured anchored pipeline output.",
					"memory_strategy":          "anchor_state_t + user_t -> assistant_t -> user_t+1",
					"config_storage_root":      cfg.Storage.Root,
				}
				return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.derive_memory", payload)
			})
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "auto", "Source provider: auto, claude, codex")
	cmd.Flags().StringVar(&sourceFile, "source-file", "", "Path to source session JSONL")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Source session ID (for auto-locate)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path hint (used by Claude locate)")
	cmd.Flags().StringVar(&actorID, "actor-id", "actor:system:source-import", "Actor ID for synthesized turns")
	cmd.Flags().IntVar(&frameLimit, "frame-limit", 20, "Maximum anchored frames to return")
	cmd.Flags().StringVar(&memoryLane, "memory-lane", string(transcriptMemoryLaneInsight), "Memory lane: insight, doctrine, or mixed")
	cmd.Flags().BoolVar(&objectiveAlign, "objective-align", false, "Apply objective alignment before persistence when using the doctrine lane")
	cmd.Flags().IntVar(&explorerTurnLimit, "explorer-turn-limit", 24, "Maximum turns to include in explorer prompt transcript")
	cmd.Flags().StringVar(&blobSummaryMode, "blob-summary-mode", "auto", "Reference blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&blobSummaryModel, "blob-summary-model", "nvidia/nemotron-3-nano-4b", "Model to use for one-shot LMStudio reference blob summaries")
	cmd.Flags().DurationVar(&blobSummaryTimeout, "blob-summary-timeout", 45*time.Second, "Timeout for one-shot reference blob summaries")
	cmd.Flags().BoolVar(&persistMemory, "persist-memory", false, "Persist durable transcript-derived memories into named_memory")
	cmd.Flags().BoolVar(&persistHistory, "persist-history", false, "Persist insight-lane history records into named_memory with best-effort embeddings")
	return cmd
}
