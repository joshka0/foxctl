package cmd

import (
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
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
				EvidenceRefs:        evidenceRefs,
				FilesTouched:        filesTouched,
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
				EvidenceRefs: evidenceRefs,
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
				RelatedRefs: relatedRefs,
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

func resolveContextWorkspace(workspacePath string) string {
	target := strings.TrimSpace(workspacePath)
	if target == "" {
		target = ws.Detect("")
	} else {
		target = ws.Detect(target)
	}
	return ws.Normalize(target)
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func init() {
	rootCmd.AddCommand(newCaptureCommand())
	rootCmd.AddCommand(newObserveCommand())
	rootCmd.AddCommand(newTensionCommand())
}
