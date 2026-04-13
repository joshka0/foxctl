package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/storage/obsidianindex"
	"github.com/spf13/cobra"
)

func newContextRetrieveInspectCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var query string
	var expectedPaths []string
	var limit int
	var apply bool
	var applyPolicyPatch bool
	var draftWhenPromotable bool

	cmd := &cobra.Command{
		Use:   "retrieve-inspect",
		Short: "Inspect one ACA retrieval miss and propose a deterministic correction",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("--query is required")
			}
			if strings.TrimSpace(vaultPath) == "" {
				return fmt.Errorf("--vault-path is required")
			}
			if len(expectedPaths) == 0 {
				return fmt.Errorf("--expected-path is required")
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

			opts := store.CurrentRetrievalOptions()
			result, err := store.RetrieveWithOptions(ctx, index, repo, openObsidianSemanticProvider(cfg), query, limit, opts)
			if err != nil {
				return err
			}

			inspection, err := store.InspectRetrieval(ctx, index, vaultPath, query, expectedPaths, result, opts, limit)
			if err != nil {
				return err
			}

			appliedObservation := false
			observationPath := ""
			var recordedProposal any
			if apply {
				observationPath, err = store.AppendObservation(inspection.Observation)
				if err != nil {
					return fmt.Errorf("append observation: %w", err)
				}
				appliedObservation = true
				if !inspection.Matched && inspection.Proposal.Kind != "none" {
					proposal, err := store.RecordRetrievalProposal(ctx, inspection)
					if err != nil {
						return fmt.Errorf("record proposal: %w", err)
					}
					recordedProposal = proposal
				}
			}

			policyPatchApplied := false
			policyPath := ""
			var rechecked map[string]any
			if applyPolicyPatch && inspection.Proposal.Kind == "policy_patch" {
				policyPath, err = store.SetRetrievalPackageNoteFallback(true)
				if err != nil {
					return fmt.Errorf("apply retrieval policy patch: %w", err)
				}
				policyPatchApplied = true
				updatedOpts := store.CurrentRetrievalOptions()
				recheckedResult, err := store.RetrieveWithOptions(ctx, index, repo, openObsidianSemanticProvider(cfg), query, limit, updatedOpts)
				if err != nil {
					return err
				}
				recheckedInspection, err := store.InspectRetrieval(ctx, index, vaultPath, query, expectedPaths, recheckedResult, updatedOpts, limit)
				if err != nil {
					return err
				}
				rechecked = map[string]any{
					"result":     recheckedResult,
					"inspection": recheckedInspection,
				}
			}

			var draft any
			if draftWhenPromotable && appliedObservation && inspection.Proposal.Kind == "draft_package_note" {
				if promoted := findObservationByStatement(store, inspection.Observation.Statement); promoted != nil && promoted.Count >= 2 {
					result, err := store.DraftPromotionFromObservation(promoted.ID, inspection.Proposal.NoteType, inspection.Proposal.NoteTitle)
					if err != nil {
						return fmt.Errorf("draft promotion from observation: %w", err)
					}
					draft = result
				}
			}

			summary := inspection.Proposal.Summary
			if summary == "" {
				summary = fmt.Sprintf("ACA retrieval inspection classified this case as %s.", inspection.Classification)
			}

			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/retrieve_inspect", map[string]any{
				"workspace_path":        target,
				"vault_path":            vaultPath,
				"result":                result,
				"inspection":            inspection,
				"applied_observation":   appliedObservation,
				"observation_path":      observationPath,
				"proposal":              recordedProposal,
				"policy_patch_applied":  policyPatchApplied,
				"policy_path":           policyPath,
				"rechecked":             rechecked,
				"draft_promotion":       draft,
				"summary":               summary,
				"expected_repo_package": filepath.Base(strings.TrimSpace(inspection.CandidateNote)),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path")
	cmd.Flags().StringVar(&query, "query", "", "Retrieval query")
	cmd.Flags().StringSliceVar(&expectedPaths, "expected-path", nil, "Expected repo or note path (repeatable)")
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum ranked vault hits")
	cmd.Flags().BoolVar(&apply, "apply", false, "Persist the generated ACA observation and proposal")
	cmd.Flags().BoolVar(&applyPolicyPatch, "apply-policy-patch", false, "Apply the suggested retrieval policy patch when safe")
	cmd.Flags().BoolVar(&draftWhenPromotable, "draft-when-promotable", false, "Draft a promotion note when the generated observation is promotable")
	return cmd
}

func findObservationByStatement(store *contextplane.WorkspaceStore, statement string) *contextplane.Observation {
	items, err := store.ListObservations(50)
	if err != nil {
		return nil
	}
	for _, item := range items {
		if strings.TrimSpace(item.Statement) == strings.TrimSpace(statement) {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func hasPromotionJobForObservation(store *contextplane.WorkspaceStore, observationID string) bool {
	jobs, err := store.ListPromotionJobs(0)
	if err != nil {
		return false
	}
	sourceRef := "observation:" + strings.TrimSpace(observationID)
	for _, job := range jobs {
		if strings.TrimSpace(job.SourceRef) == sourceRef {
			return true
		}
	}
	return false
}
