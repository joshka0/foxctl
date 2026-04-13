package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	"github.com/spf13/cobra"
)

var buildRepoIndexSearchInspectionReportHook = buildRepoIndexSearchInspectionReport

func newContextRepoIndexSearchInspectSuiteCommand() *cobra.Command {
	var workspacePath string
	var suiteRef string
	var limit int

	cmd := &cobra.Command{
		Use:   "repoindex-search-inspect-suite",
		Short: "Inspect repoindex search misses across a suite and persist a correction report",
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
			report, err := buildRepoIndexSearchInspectionReportHook(ctx, cfg.Storage.Root, target, suite, limit)
			if err != nil {
				return err
			}
			artifact, err := contextplane.PersistRetrievalInspectionReport(ctx, cfg.Paths.CAS, report)
			if err != nil {
				return err
			}
			summary := summarizeGraphInspection(report.Inspections)
			runID := fmt.Sprintf("G-%s", time.Now().UTC().Format("20060102T150405.000000000"))
			run := contextplane.GraphCorrectionRun{
				ID:             runID,
				Method:         "repoindex_search",
				Suite:          suite.Name,
				ArtifactDigest: artifact,
				Queries:        len(report.Inspections),
				Matched:        summary.matched,
				Misses:         summary.misses,
				Classification: summary.classification,
				RecommendedFix: summary.fix,
				CreatedAt:      time.Now().UTC(),
			}
			if err := contextplane.NewWorkspaceStore(target).RecordGraphCorrectionRun(run); err != nil {
				return err
			}
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/repoindex_search_inspect_suite", map[string]any{
				"workspace_path":  target,
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
	cmd.Flags().StringVar(&suiteRef, "suite", "", "Retrieval suite name or path")
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum search anchors to retain")
	return cmd
}

func buildRepoIndexSearchInspectionReport(ctx context.Context, storageRoot, workspacePath string, suite retrievaleval.Suite, limit int) (graphInspectionSuiteReport, error) {
	store, err := repoindex.Open(ctx, storageRoot, workspacePath)
	if err != nil {
		return graphInspectionSuiteReport{}, err
	}
	defer func() { _ = store.Close() }()
	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))

	inspections := make([]graphInspection, 0, len(suite.Queries))
	for _, item := range suite.Queries {
		result, err := service.SearchWithProjection(ctx, repoquery.SearchRequest{
			Query: strings.TrimSpace(item.Query),
			Limit: limit,
		})
		inspection := graphInspection{
			Query:         item.Query,
			ExpectedPaths: append([]string(nil), item.ExpectedAnyOf...),
		}
		if err != nil {
			inspection.Classification = "engine_error"
			inspection.RecommendedFix = "stabilize repoindex search execution path or ensure the repo index is built"
			inspections = append(inspections, inspection)
			continue
		}
		anchors := extractRepoAnchorPaths(result.Anchors)
		inspection.Anchors = anchors
		switch {
		case len(anchors) == 0:
			inspection.Classification = "no_anchors"
			inspection.RecommendedFix = "improve search query shaping or ensure file and concept nodes exist for this query family"
		case graphPathsMatchExpected(anchors, item.ExpectedAnyOf):
			inspection.Matched = true
			inspection.Classification = "matched"
		default:
			inspection.Classification = "anchor_mismatch"
			inspection.RecommendedFix = "improve search ranking, node summaries, or parser coverage for this query family"
		}
		inspections = append(inspections, inspection)
	}

	return graphInspectionSuiteReport{
		Method:        "repoindex_search",
		Suite:         suite.Name,
		WorkspacePath: workspacePath,
		GeneratedAt:   time.Now().UTC(),
		Inspections:   inspections,
	}, nil
}
