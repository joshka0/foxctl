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

func newSessionsConsolidateDoctrineCommand() *cobra.Command {
	var provider string
	var sourceFile string
	var sessionID string
	var workspace string
	var actorID string
	var frameLimit int
	var blobSummaryMode string
	var blobSummaryModel string
	var blobSummaryTimeout time.Duration
	var persistMemory bool
	var objectiveAlign bool

	cmd := &cobra.Command{
		Use:   "consolidate-doctrine",
		Short: "Build doctrine-oriented memory from cached transcript bridge seeds",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return sessionscmd.WithConfig(cmd, func(ctx context.Context, cfg config.Config) error {
				runtime := transcriptpipeline.NewLocalModelRuntime(strings.TrimSpace(blobSummaryMode), strings.TrimSpace(blobSummaryModel), cfg.LLM.ResolveBaseURL("lmstudio"), blobSummaryTimeout)
				result, err := transcriptpipeline.RunSingleDoctrine(ctx, transcriptpipeline.SingleRunOptions{
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
				})
				if err != nil {
					return err
				}
				payload := map[string]any{
					"provider":               string(result.Parsed.Provider),
					"session_id":             result.Parsed.SessionID,
					"source_path":            result.Parsed.SourcePath,
					"workspace_path":         result.Parsed.WorkspacePath,
					"conversation_id":        result.ConversationID,
					"objective":              result.Objective,
					"doctrine_seed_claims":   result.DoctrineSeedClaims,
					"doctrine_claims":        result.DoctrineClaims,
					"aligned_claims":         result.AlignedClaims,
					"doctrine_seed_artifact": result.DoctrineSeedArtifact,
					"doctrine_artifact":      result.DoctrineArtifact,
					"alignment_artifact":     result.AlignmentArtifact,
					"persisted_memory":       result.PersistedMemory,
					"persist_memory":         persistMemory,
					"transcript_cache_path":  result.TranscriptCachePath,
					"transcript_cache_root":  result.TranscriptCacheRoot,
				}
				return sessionscmd.WriteOK(cmd.OutOrStdout(), "agentctl.sessions.consolidate_doctrine", payload)
			})
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "auto", "Source provider: auto, claude, codex")
	cmd.Flags().StringVar(&sourceFile, "source-file", "", "Path to source session JSONL")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Source session ID (for auto-locate)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path hint")
	cmd.Flags().StringVar(&actorID, "actor-id", "actor:system:source-import", "Actor ID for synthesized turns")
	cmd.Flags().IntVar(&frameLimit, "frame-limit", 0, "Maximum anchored frames to consider (0 = all)")
	cmd.Flags().StringVar(&blobSummaryMode, "blob-summary-mode", "auto", "Reference blob summary mode: auto, deterministic, or lmstudio")
	cmd.Flags().StringVar(&blobSummaryModel, "blob-summary-model", "nvidia/nemotron-3-nano-4b", "Model to use for one-shot LMStudio reference blob summaries")
	cmd.Flags().DurationVar(&blobSummaryTimeout, "blob-summary-timeout", 45*time.Second, "Timeout for one-shot reference blob summaries")
	cmd.Flags().BoolVar(&persistMemory, "persist-memory", false, "Persist doctrine-oriented transcript memories into named_memory")
	cmd.Flags().BoolVar(&objectiveAlign, "objective-align", false, "Apply objective alignment on doctrine claims before persistence")
	return cmd
}
