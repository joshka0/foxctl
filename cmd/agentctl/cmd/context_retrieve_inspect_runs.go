package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

func newContextRetrieveInspectRunsCommand() *cobra.Command {
	var workspacePath string
	var limit int
	var runID string

	cmd := &cobra.Command{
		Use:   "retrieve-inspect-runs",
		Short: "List persisted ACA retrieval correction runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := resolveContextWorkspace(workspacePath)
			store := contextplane.NewWorkspaceStore(target)
			if strings.TrimSpace(runID) != "" {
				run, err := store.GetRetrievalCorrectionRun(strings.TrimSpace(runID))
				if err != nil {
					return err
				}
				if run == nil {
					return fmt.Errorf("no retrieval correction run found for %s", strings.TrimSpace(runID))
				}
				return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/retrieve_inspect_run", map[string]any{
					"workspace_path": target,
					"run":            run,
				}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
			}
			runs, err := store.ListRetrievalCorrectionRuns(limit)
			if err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/retrieve_inspect_runs", map[string]any{
				"workspace_path": target,
				"runs":           runs,
				"count":          len(runs),
			}, envelope.WithMeta(envelope.Meta{Source: "cli"})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum correction runs to list")
	cmd.Flags().StringVar(&runID, "id", "", "Optional correction run ID to fetch")
	return cmd
}

func newContextRetrieveInspectArtifactCommand() *cobra.Command {
	var artifact string

	cmd := &cobra.Command{
		Use:   "retrieve-inspect-artifact",
		Short: "Read a persisted ACA retrieval inspection artifact from CAS",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(artifact) == "" {
				return fmt.Errorf("--artifact is required")
			}
			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return err
			}
			body, err := contextplane.ReadInspectionArtifact(ctx, cfg.Paths.CAS, strings.TrimSpace(artifact))
			if err != nil {
				return err
			}
			var payload any
			if err := json.Unmarshal(body, &payload); err != nil {
				return fmt.Errorf("decode inspection artifact: %w", err)
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/retrieve_inspect_artifact", map[string]any{
				"artifact": artifact,
				"report":   payload,
			}, envelope.WithMeta(envelope.Meta{Source: "cli", CASDigest: strings.TrimSpace(artifact)})))
		},
	}

	cmd.Flags().StringVar(&artifact, "artifact", "", "CAS digest for the retrieval inspection report")
	return cmd
}
