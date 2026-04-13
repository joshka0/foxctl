package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/context/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/spf13/cobra"
)

var buildSemanticSearchInspectionReportHook = buildSemanticSearchInspectionReport

func newContextSemanticSearchInspectSuiteCommand() *cobra.Command {
	var workspacePath string
	var vaultPath string
	var suiteRef string
	var limit int

	cmd := &cobra.Command{
		Use:   "semantic-search-inspect-suite",
		Short: "Inspect semantic/code retrieval misses across a suite and persist a correction report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(suiteRef) == "" {
				return fmt.Errorf("--suite is required")
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

			inspections := make([]graphInspection, 0, len(suite.Queries))
			for _, item := range suite.Queries {
				inspection, err := buildSemanticSearchInspectionReportHook(ctx, target, vaultPath, item.Query, item.ExpectedAnyOf, limit)
				if err != nil {
					return err
				}
				inspections = append(inspections, inspection)
			}

			report := graphInspectionSuiteReport{
				Method:        "semantic_search",
				Suite:         suite.Name,
				WorkspacePath: target,
				GeneratedAt:   time.Now().UTC(),
				Inspections:   inspections,
			}
			artifact, err := contextplane.PersistRetrievalInspectionReport(ctx, cfg.Paths.CAS, report)
			if err != nil {
				return err
			}
			summary := summarizeGraphInspection(inspections)
			runID := fmt.Sprintf("G-%s", time.Now().UTC().Format("20060102T150405.000000000"))
			run := contextplane.GraphCorrectionRun{
				ID:             runID,
				Method:         "semantic_search",
				Suite:          suite.Name,
				ArtifactDigest: artifact,
				Queries:        len(inspections),
				Matched:        summary.matched,
				Misses:         summary.misses,
				Classification: summary.classification,
				RecommendedFix: summary.fix,
				CreatedAt:      time.Now().UTC(),
			}
			if err := contextplane.NewWorkspaceStore(target).RecordGraphCorrectionRun(run); err != nil {
				return err
			}

			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/semantic_search_inspect_suite", map[string]any{
				"workspace_path":  target,
				"vault_path":      vaultPath,
				"suite":           suite.Name,
				"run_id":          runID,
				"artifact":        artifact,
				"matched":         summary.matched,
				"misses":          summary.misses,
				"classification":  summary.classification,
				"recommended_fix": summary.fix,
			}, envelope.WithMeta(envelope.Meta{Source: "cli", CASDigest: artifact})))
		},
	}

	cmd.Flags().StringVar(&workspacePath, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for context-enabled semantic search")
	cmd.Flags().StringVar(&suiteRef, "suite", "", "Retrieval suite name or path")
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum semantic search paths to retain")
	return cmd
}

func buildSemanticSearchInspectionReport(ctx context.Context, workspacePath, vaultPath, query string, expectedAnyOf []string, limit int) (graphInspection, error) {
	paths, err := runSemanticSearchEvalMode(ctx, workspacePath, vaultPath, query, limit, []string{"symbols", "sessions", "memories", "tasks", "codemaps"})
	inspection := graphInspection{
		Query:         query,
		ExpectedPaths: append([]string(nil), expectedAnyOf...),
		Anchors:       paths,
	}
	if err != nil {
		inspection.Classification = "engine_error"
		inspection.RecommendedFix = "stabilize code/semantic_search execution or scope defaults"
		return inspection, nil
	}
	if len(paths) == 0 {
		inspection.Classification = "no_anchors"
		inspection.RecommendedFix = "improve semantic search query shaping or fallback scopes"
		return inspection, nil
	}
	if graphPathsMatchExpected(paths, expectedAnyOf) {
		inspection.Matched = true
		inspection.Classification = "matched"
		return inspection, nil
	}
	inspection.Classification = "anchor_mismatch"
	inspection.RecommendedFix = "improve semantic search ranking, fallback ordering, or scope defaults"
	return inspection, nil
}
