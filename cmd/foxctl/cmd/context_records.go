package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/spf13/cobra"
)

func newCaptureCommand() *cobra.Command {
	var workspacePath string
	var taskID string
	var phase string
	var outcome string
	var summary string
	var evidenceRefs []string
	var filesTouched []string
	var observations []string
	var tensions []string
	var nextActions []string
	var promotionCandidates []string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Persist a structured handoff into the workspace control plane",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(taskID) == "" {
				return fmt.Errorf("--task-id is required")
			}
			if strings.TrimSpace(summary) == "" {
				return fmt.Errorf("--summary is required")
			}

			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			record := contextplane.Handoff{
				TaskID:              strings.TrimSpace(taskID),
				Phase:               defaultString(phase, "work"),
				Outcome:             defaultString(outcome, "partial"),
				Summary:             strings.TrimSpace(summary),
				EvidenceRefs:        parseEvidenceRefs(evidenceRefs),
				FileRefs:            parseEvidenceRefs(filesTouched),
				Observations:        observations,
				Tensions:            tensions,
				NextActions:         nextActions,
				PromotionCandidates: promotionCandidates,
			}
			path := ""
			summaryText := fmt.Sprintf("Captured handoff for %s.", record.TaskID)
			if dryRun {
				summaryText = fmt.Sprintf("Dry run: handoff for %s not saved.", record.TaskID)
			} else {
				var err error
				path, err = store.SaveHandoff(record)
				if err != nil {
					return fmt.Errorf("save handoff: %w", err)
				}
			}

			env := envelope.OK("context/capture", map[string]any{
				"workspace_path": target,
				"handoff":        record,
				"path":           path,
				"summary":        summaryText,
				"dry_run":        dryRun,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Task identifier")
	cmd.Flags().StringVar(&phase, "phase", "work", "Phase name")
	cmd.Flags().StringVar(&outcome, "outcome", "partial", "Outcome: partial, complete, blocked")
	cmd.Flags().StringVar(&summary, "summary", "", "Compact handoff summary")
	cmd.Flags().StringSliceVar(&evidenceRefs, "evidence-ref", nil, "Evidence ref (repeatable)")
	cmd.Flags().StringSliceVar(&filesTouched, "file-touched", nil, "Touched file path (repeatable)")
	cmd.Flags().StringSliceVar(&observations, "observation", nil, "Observation text (repeatable)")
	cmd.Flags().StringSliceVar(&tensions, "tension", nil, "Tension text (repeatable)")
	cmd.Flags().StringSliceVar(&nextActions, "next-action", nil, "Next action (repeatable)")
	cmd.Flags().StringSliceVar(&promotionCandidates, "promotion-candidate", nil, "Promotion candidate ref (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the handoff without persisting it")
	return cmd
}

func newObserveCommand() *cobra.Command {
	var workspacePath string
	var statement string
	var confidence float64
	var count int
	var project string
	var area string
	var evidenceRefs []string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "observe",
		Short: "Append an observation record into the workspace control plane",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(statement) == "" {
				return fmt.Errorf("--statement is required")
			}
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			record := contextplane.Observation{
				Statement:    strings.TrimSpace(statement),
				Confidence:   confidence,
				Count:        count,
				Project:      strings.TrimSpace(project),
				Area:         strings.TrimSpace(area),
				EvidenceRefs: parseEvidenceRefs(evidenceRefs),
			}
			path := ""
			summaryText := "Recorded observation."
			if dryRun {
				summaryText = "Dry run: observation not recorded."
			} else {
				var err error
				path, err = store.AppendObservation(record)
				if err != nil {
					return fmt.Errorf("append observation: %w", err)
				}
			}

			env := envelope.OK("context/observe", map[string]any{
				"workspace_path": target,
				"observation":    record,
				"path":           path,
				"summary":        summaryText,
				"dry_run":        dryRun,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&statement, "statement", "", "Observation statement")
	cmd.Flags().Float64Var(&confidence, "confidence", 0.5, "Confidence from 0.0 to 1.0")
	cmd.Flags().IntVar(&count, "count", 1, "Observed count")
	cmd.Flags().StringVar(&project, "project", "", "Project name")
	cmd.Flags().StringVar(&area, "area", "", "Subsystem or area")
	cmd.Flags().StringSliceVar(&evidenceRefs, "evidence-ref", nil, "Evidence ref (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the observation without persisting it")
	return cmd
}

func newTensionCommand() *cobra.Command {
	var workspacePath string
	var kind string
	var statement string
	var impact string
	var status string
	var relatedRefs []string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "tension",
		Short: "Append a tension record into the workspace control plane",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(statement) == "" {
				return fmt.Errorf("--statement is required")
			}
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			record := contextplane.Tension{
				Kind:        defaultString(kind, "contradiction"),
				Statement:   strings.TrimSpace(statement),
				Impact:      defaultString(impact, "medium"),
				Status:      defaultString(status, "open"),
				RelatedRefs: parseEvidenceRefs(relatedRefs),
			}
			path := ""
			summaryText := "Recorded tension."
			if dryRun {
				summaryText = "Dry run: tension not recorded."
			} else {
				var err error
				path, err = store.AppendTension(record)
				if err != nil {
					return fmt.Errorf("append tension: %w", err)
				}
			}

			env := envelope.OK("context/tension", map[string]any{
				"workspace_path": target,
				"tension":        record,
				"path":           path,
				"summary":        summaryText,
				"dry_run":        dryRun,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&kind, "kind", "contradiction", "Tension kind")
	cmd.Flags().StringVar(&statement, "statement", "", "Tension statement")
	cmd.Flags().StringVar(&impact, "impact", "medium", "Impact: low, medium, high")
	cmd.Flags().StringVar(&status, "status", "open", "Status")
	cmd.Flags().StringSliceVar(&relatedRefs, "related-ref", nil, "Related ref (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the tension without persisting it")
	return cmd
}

func newRetrievalFeedbackCommand() *cobra.Command {
	var workspacePath string
	var feedbackID string
	var episodeID string
	var kind string
	var query string
	var usedRefs []string
	var gapStmt string
	var correctionStmt string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "retrieval-feedback",
		Short: "Record retrieval feedback and apply claim lifecycle effects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			var store contextengine.RetrievalFeedbackEffectStore
			if !dryRun {
				cfg, err := loadConfig(cmd.Context())
				if err != nil {
					return err
				}
				opened, err := contextstore.Open(cmd.Context(), cfg.Storage.Root)
				if err != nil {
					return fmt.Errorf("open contextengine store: %w", err)
				}
				defer opened.Close()
				store = opened
			}
			record, err := recordRetrievalFeedbackCLI(cmd.Context(), store, retrievalFeedbackCLIInput{
				WorkspacePath:  workspacePath,
				ID:             feedbackID,
				EpisodeID:      episodeID,
				Kind:           kind,
				Query:          query,
				UsedRefs:       usedRefs,
				UsedRefFlag:    "used-ref",
				GapStmt:        gapStmt,
				CorrectionStmt: correctionStmt,
				DryRun:         dryRun,
			})
			if err != nil {
				return fmt.Errorf("record retrieval feedback: %w", err)
			}

			env := envelope.OK("context/retrieval_feedback", map[string]any{
				"workspace_path": record.WorkspacePath,
				"workspace_id":   record.WorkspaceID,
				"feedback":       record.Feedback,
				"applied":        record.Applied,
				"dry_run":        record.DryRun,
			}, envelope.WithMeta(envelope.Meta{Source: "cli"}))
			return envelope.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&feedbackID, "id", "", "Feedback ID (default: deterministic from feedback content)")
	cmd.Flags().StringVar(&episodeID, "episode-id", "", "Retrieval episode ID")
	cmd.Flags().StringVar(&kind, "kind", string(contextengine.RetrievalFeedbackKindEvidenceUsed), "Feedback kind")
	cmd.Flags().StringVar(&query, "query", "", "Original retrieval query")
	cmd.Flags().StringSliceVar(&usedRefs, "used-ref", nil, "Evidence ref used in the answer (repeatable)")
	cmd.Flags().StringVar(&gapStmt, "gap", "", "Gap statement for gap_created feedback")
	cmd.Flags().StringVar(&correctionStmt, "correction", "", "Correction statement for answer_corrected feedback")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview feedback without persisting it")
	return cmd
}

func resolveContextWorkspace(workspacePath string) string {
	target := strings.TrimSpace(workspacePath)
	if target == "" {
		target = ws.Detect("")
	} else {
		target = ws.Normalize(target)
	}
	return ws.Normalize(target)
}

func deterministicRetrievalFeedbackID(feedback contextengine.RetrievalFeedback) string {
	refs := make([]string, 0, len(feedback.UsedRefs))
	for _, ref := range feedback.UsedRefs {
		formatted := strings.TrimSpace(contextengine.FormatEvidenceRef(ref))
		if formatted != "" {
			refs = append(refs, formatted)
		}
	}
	sort.Strings(refs)
	hash := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(feedback.WorkspaceID),
		strings.TrimSpace(feedback.EpisodeID),
		string(feedback.Kind),
		strings.TrimSpace(feedback.Query),
		strings.Join(refs, "\x1f"),
		strings.TrimSpace(feedback.GapStmt),
		strings.TrimSpace(feedback.CorrectionStmt),
	}, "\x00")))
	return "retrieval-feedback-" + hex.EncodeToString(hash[:])
}

func sortEvidenceRefsByIdentity(refs []contextengine.EvidenceRef) []contextengine.EvidenceRef {
	if len(refs) == 0 {
		return nil
	}
	out := append([]contextengine.EvidenceRef(nil), refs...)
	sort.SliceStable(out, func(i, j int) bool {
		return contextengine.FormatEvidenceRef(out[i]) < contextengine.FormatEvidenceRef(out[j])
	})
	return out
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func parseEvidenceRefs(raw []string) []contextengine.EvidenceRef {
	if len(raw) == 0 {
		return nil
	}
	out := make([]contextengine.EvidenceRef, 0, len(raw))
	for _, s := range raw {
		ref, err := contextengine.ParseEvidenceRef(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		out = append(out, ref)
	}
	return out
}

func parseEvidenceRefsStrict(flagName string, raw []string) ([]contextengine.EvidenceRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]contextengine.EvidenceRef, 0, len(raw))
	for i, s := range raw {
		ref, err := contextengine.ParseEvidenceRef(strings.TrimSpace(s))
		if err != nil {
			return nil, fmt.Errorf("--%s[%d]: %w", flagName, i, err)
		}
		out = append(out, ref)
	}
	return out, nil
}

func init() {
	rootCmd.AddCommand(newCaptureCommand())
	rootCmd.AddCommand(newObserveCommand())
	rootCmd.AddCommand(newTensionCommand())
	rootCmd.AddCommand(newRetrievalFeedbackCommand())
}
