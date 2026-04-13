package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/tooling/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/spf13/cobra"
)

type retrievalInspectionSuiteReport struct {
	Suite         string                                 `json:"suite"`
	ControlSuite  string                                 `json:"control_suite,omitempty"`
	WorkspacePath string                                 `json:"workspace_path"`
	VaultPath     string                                 `json:"vault_path"`
	GeneratedAt   time.Time                              `json:"generated_at"`
	Baseline      retrievalInspectionSuiteSection        `json:"baseline"`
	PolicyPatch   retrievalInspectionPolicyPatchResult   `json:"policy_patch"`
	Control       *retrievalInspectionSuiteSection       `json:"control,omitempty"`
	ControlPatch  *retrievalInspectionControlPatchResult `json:"control_patch,omitempty"`
}

type retrievalInspectionSuiteSection struct {
	Inspections      []contextplane.RetrievalInspection           `json:"inspections"`
	Summary          contextplane.RetrievalInspectionBatchSummary `json:"summary"`
	ObservationPaths []string                                     `json:"observation_paths,omitempty"`
	ProposalIDs      []string                                     `json:"proposal_ids,omitempty"`
	Drafts           []contextplane.PromotionDraftResult          `json:"drafts,omitempty"`
}

type retrievalInspectionPolicyPatchResult struct {
	Candidate  bool                             `json:"candidate"`
	Applied    bool                             `json:"applied"`
	Accepted   bool                             `json:"accepted"`
	Reverted   bool                             `json:"reverted"`
	PolicyPath string                           `json:"policy_path,omitempty"`
	Reason     string                           `json:"reason,omitempty"`
	After      *retrievalInspectionSuiteSection `json:"after,omitempty"`
}

type retrievalInspectionControlPatchResult struct {
	Accepted bool                             `json:"accepted"`
	Reason   string                           `json:"reason,omitempty"`
	After    *retrievalInspectionSuiteSection `json:"after,omitempty"`
}

func newContextRetrieveInspectSuiteCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var suiteRef string
	var controlSuiteRef string
	var limit int
	var apply bool
	var applyPolicyPatch bool
	var draftWhenPromotable bool

	cmd := &cobra.Command{
		Use:   "retrieve-inspect-suite",
		Short: "Inspect ACA retrieval misses across a suite and summarize deterministic corrections",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required")
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
			suitePath, err := resolveEvalSuitePath(suiteRef)
			if err != nil {
				return err
			}
			suite, err := retrievaleval.LoadSuite(suitePath)
			if err != nil {
				return err
			}

			var controlSuite retrievaleval.Suite
			if strings.TrimSpace(controlSuiteRef) != "" {
				controlSuitePath, err := resolveEvalSuitePath(controlSuiteRef)
				if err != nil {
					return err
				}
				controlSuite, err = retrievaleval.LoadSuite(controlSuitePath)
				if err != nil {
					return err
				}
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

			opts := store.CurrentRetrievalOptions()
			baseline, err := collectInspectionSuiteSection(ctx, store, index, vaultPath, repo, openObsidianSemanticProvider(cfg), suite, opts, limit, apply, draftWhenPromotable)
			if err != nil {
				return err
			}

			report := retrievalInspectionSuiteReport{
				Suite:         suite.Name,
				ControlSuite:  controlSuite.Name,
				WorkspacePath: target,
				VaultPath:     vaultPath,
				GeneratedAt:   time.Now().UTC(),
				Baseline:      baseline,
				PolicyPatch: retrievalInspectionPolicyPatchResult{
					Candidate: baseline.Summary.PolicyPatchCandidate,
				},
			}

			var originalPolicy []byte
			if applyPolicyPatch && baseline.Summary.PolicyPatchCandidate {
				originalPolicy, err = store.ReadRetrievalPolicy()
				if err != nil {
					return err
				}
				policyPath, err := store.SetRetrievalPackageNoteFallback(true)
				if err != nil {
					return fmt.Errorf("apply retrieval policy patch: %w", err)
				}
				report.PolicyPatch.Applied = true
				report.PolicyPatch.PolicyPath = policyPath

				updatedOpts := store.CurrentRetrievalOptions()
				targetAfter, err := collectInspectionSuiteSection(ctx, store, index, vaultPath, repo, openObsidianSemanticProvider(cfg), suite, updatedOpts, limit, false, false)
				if err != nil {
					return err
				}
				report.PolicyPatch.After = &targetAfter

				targetAccepted, reason := evaluateInspectionPatchOutcome(baseline.Summary, targetAfter.Summary)
				report.PolicyPatch.Accepted = targetAccepted
				report.PolicyPatch.Reason = reason

				if strings.TrimSpace(controlSuite.Name) != "" {
					controlBefore, err := collectInspectionSuiteSection(ctx, store, index, vaultPath, repo, openObsidianSemanticProvider(cfg), controlSuite, opts, limit, false, false)
					if err != nil {
						return err
					}
					report.Control = &controlBefore

					controlAfter, err := collectInspectionSuiteSection(ctx, store, index, vaultPath, repo, openObsidianSemanticProvider(cfg), controlSuite, updatedOpts, limit, false, false)
					if err != nil {
						return err
					}
					controlAccepted, controlReason := evaluateControlInspectionOutcome(controlBefore.Summary, controlAfter.Summary)
					report.ControlPatch = &retrievalInspectionControlPatchResult{
						Accepted: controlAccepted,
						Reason:   controlReason,
						After:    &controlAfter,
					}
					if !controlAccepted {
						report.PolicyPatch.Accepted = false
						report.PolicyPatch.Reason = combinePatchReasons(report.PolicyPatch.Reason, controlReason)
					}
				}

				if !report.PolicyPatch.Accepted {
					if _, err := store.WriteRetrievalPolicy(originalPolicy); err != nil {
						return err
					}
					report.PolicyPatch.Reverted = true
				}
			}

			artifact, err := contextplane.PersistRetrievalInspectionReport(ctx, cfg.Paths.CAS, report)
			if err != nil {
				return err
			}

			summary := report.Baseline.Summary
			if report.PolicyPatch.After != nil && report.PolicyPatch.Accepted {
				summary = report.PolicyPatch.After.Summary
			}
			runID := fmt.Sprintf("R-%s", time.Now().UTC().Format("20060102T150405.000000000"))
			run := contextplane.RetrievalCorrectionRun{
				ID:              runID,
				Suite:           suite.Name,
				ControlSuite:    controlSuite.Name,
				ArtifactDigest:  artifact,
				Summary:         summary,
				PolicyCandidate: report.PolicyPatch.Candidate,
				PolicyApplied:   report.PolicyPatch.Applied,
				PolicyAccepted:  report.PolicyPatch.Accepted,
				PolicyReverted:  report.PolicyPatch.Reverted,
				DraftCount:      len(report.Baseline.Drafts),
				CreatedAt:       time.Now().UTC(),
			}
			if err := store.RecordRetrievalCorrectionRun(run); err != nil {
				return err
			}

			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/retrieve_inspect_suite", map[string]any{
				"workspace_path":      target,
				"vault_path":          vaultPath,
				"suite":               suite.Name,
				"control_suite":       controlSuite.Name,
				"run_id":              run.ID,
				"summary":             summary,
				"recommended_actions": report.Baseline.Summary.RecommendedActions,
				"proposal_ids":        report.Baseline.ProposalIDs,
				"drafts":              report.Baseline.Drafts,
				"policy_patch":        report.PolicyPatch,
				"control_patch":       report.ControlPatch,
				"artifact":            artifact,
			}, envelope.WithMeta(envelope.Meta{Source: "cli", CASDigest: artifact})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&suiteRef, "suite", "", "Retrieval suite name or path")
	cmd.Flags().StringVar(&controlSuiteRef, "control-suite", "", "Optional control retrieval suite name or path")
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum ranked vault hits")
	cmd.Flags().BoolVar(&apply, "apply", false, "Persist generated ACA observations for misses")
	cmd.Flags().BoolVar(&applyPolicyPatch, "apply-policy-patch", false, "Apply the safe retrieval policy patch only if the suite evaluation accepts it")
	cmd.Flags().BoolVar(&draftWhenPromotable, "draft-when-promotable", false, "Draft promotion notes for repeated missing_package_note observations")
	return cmd
}

func collectInspectionSuiteSection(
	ctx context.Context,
	store *contextplane.WorkspaceStore,
	index obsidianindex.Store,
	vaultPath string,
	repo *repoindex.Store,
	semanticProvider semantic.EmbeddingProvider,
	suite retrievaleval.Suite,
	opts contextplane.RetrievalOptions,
	limit int,
	apply bool,
	draftWhenPromotable bool,
) (retrievalInspectionSuiteSection, error) {
	inspections := make([]contextplane.RetrievalInspection, 0, len(suite.Queries))
	observationPaths := []string{}
	proposalIDs := []string{}
	drafts := []contextplane.PromotionDraftResult{}
	for _, query := range suite.Queries {
		result, err := store.RetrieveWithOptions(ctx, index, repo, semanticProvider, query.Query, limit, opts)
		if err != nil {
			return retrievalInspectionSuiteSection{}, err
		}
		inspection, err := store.InspectRetrieval(ctx, index, vaultPath, query.Query, query.ExpectedAnyOf, result, opts, limit)
		if err != nil {
			return retrievalInspectionSuiteSection{}, err
		}
		inspections = append(inspections, inspection)
		if apply && !inspection.Matched {
			path, err := store.AppendObservation(inspection.Observation)
			if err != nil {
				return retrievalInspectionSuiteSection{}, fmt.Errorf("append observation for %s: %w", query.ID, err)
			}
			observationPaths = append(observationPaths, path)
			if inspection.Proposal.Kind != "none" {
				proposal, err := store.RecordRetrievalProposal(ctx, inspection)
				if err != nil {
					return retrievalInspectionSuiteSection{}, fmt.Errorf("record proposal for %s: %w", query.ID, err)
				}
				proposalIDs = append(proposalIDs, proposal.ID)
			}
			if draftWhenPromotable && inspection.Classification == "missing_package_note" && inspection.Proposal.Kind == "draft_package_note" {
				if promoted := findObservationByStatement(store, inspection.Observation.Statement); promoted != nil && promoted.Count >= 2 && !hasPromotionJobForObservation(store, promoted.ID) {
					draft, err := store.DraftPromotionFromObservation(promoted.ID, inspection.Proposal.NoteType, inspection.Proposal.NoteTitle)
					if err != nil {
						return retrievalInspectionSuiteSection{}, fmt.Errorf("draft promotion for %s: %w", query.ID, err)
					}
					drafts = append(drafts, draft)
				}
			}
		}
	}
	return retrievalInspectionSuiteSection{
		Inspections:      inspections,
		Summary:          contextplane.SummarizeRetrievalInspections(inspections),
		ObservationPaths: observationPaths,
		ProposalIDs:      proposalIDs,
		Drafts:           drafts,
	}, nil
}

func evaluateInspectionPatchOutcome(before, after contextplane.RetrievalInspectionBatchSummary) (bool, string) {
	if after.Misses < before.Misses || after.Matched > before.Matched {
		return true, fmt.Sprintf("accepted: target suite improved from %d misses/%d matches to %d misses/%d matches", before.Misses, before.Matched, after.Misses, after.Matched)
	}
	beforeFallback := before.Classifications["package_note_fallback_disabled"]
	afterFallback := after.Classifications["package_note_fallback_disabled"]
	if beforeFallback > 0 && afterFallback < beforeFallback {
		return true, fmt.Sprintf("accepted: removed %d package_note_fallback_disabled misses (now %d)", beforeFallback-afterFallback, afterFallback)
	}
	return false, fmt.Sprintf("reverted: target suite did not improve (%d misses/%d matches -> %d misses/%d matches)", before.Misses, before.Matched, after.Misses, after.Matched)
}

func evaluateControlInspectionOutcome(before, after contextplane.RetrievalInspectionBatchSummary) (bool, string) {
	if after.Misses > before.Misses || after.Matched < before.Matched {
		return false, fmt.Sprintf("reverted: control suite regressed (%d misses/%d matches -> %d misses/%d matches)", before.Misses, before.Matched, after.Misses, after.Matched)
	}
	return true, fmt.Sprintf("accepted: control suite held (%d misses/%d matches -> %d misses/%d matches)", before.Misses, before.Matched, after.Misses, after.Matched)
}

func combinePatchReasons(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + "; " + right
	}
}
