package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/evals/retrievaleval"
	"github.com/jkatigb/agentctl/internal/indexing/repoindex"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
	"github.com/spf13/cobra"
)

type graphInspection struct {
	Query          string   `json:"query"`
	ExpectedPaths  []string `json:"expected_paths,omitempty"`
	Anchors        []string `json:"anchors,omitempty"`
	Matched        bool     `json:"matched"`
	Classification string   `json:"classification"`
	RecommendedFix string   `json:"recommended_fix,omitempty"`
}

type graphInspectionSuiteReport struct {
	Method        string            `json:"method"`
	Suite         string            `json:"suite"`
	WorkspacePath string            `json:"workspace_path"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Inspections   []graphInspection `json:"inspections"`
}

var buildRepoIndexDAGInspectionReportHook = buildRepoIndexDAGInspectionReport

func newContextRepoIndexDAGInspectSuiteCommand() *cobra.Command {
	var workspacePath string
	var suiteRef string
	var limit int

	cmd := &cobra.Command{
		Use:   "repoindex-dag-inspect-suite",
		Short: "Inspect repoindex DAG misses across a suite and persist a correction report",
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
			report, err := buildRepoIndexDAGInspectionReportHook(ctx, cfg.Storage.Root, target, suite, limit)
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
				Method:         "repoindex_dag",
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
			return envelope.Write(cmd.OutOrStdout(), envelope.OK("context/repoindex_dag_inspect_suite", map[string]any{
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
	cmd.Flags().IntVar(&limit, "limit", 5, "Maximum DAG anchors to retain")
	return cmd
}

func buildRepoIndexDAGInspectionReport(ctx context.Context, storageRoot, workspacePath string, suite retrievaleval.Suite, limit int) (graphInspectionSuiteReport, error) {
	store, err := repoindex.Open(ctx, storageRoot, workspacePath)
	if err != nil {
		return graphInspectionSuiteReport{}, err
	}
	defer func() { _ = store.Close() }()
	service := repoquery.NewQueryService(repoindex.NewQueryEngine(store))

	inspections := make([]graphInspection, 0, len(suite.Queries))
	for _, item := range suite.Queries {
		result, err := service.DAGGrepWithProjection(ctx, repoquery.DAGGrepRequest{
			Query:          strings.TrimSpace(item.Query),
			K:              3,
			EdgeTypes:      repoindex.EdgeSetStructural,
			Direction:      repoindex.DirOut,
			Depth:          2,
			Budget:         dagBudget(limit),
			PerNodeCap:     20,
			IncludeAnchors: true,
		})
		inspection := graphInspection{
			Query:         item.Query,
			ExpectedPaths: append([]string(nil), item.ExpectedAnyOf...),
		}
		if err != nil {
			inspection.Classification = "engine_error"
			inspection.RecommendedFix = "stabilize repoindex DAG engine or ensure the repo index is built"
			inspections = append(inspections, inspection)
			continue
		}
		anchors := extractRepoAnchorPaths(result.Anchors)
		if limit > 0 && len(anchors) > limit {
			anchors = anchors[:limit]
		}
		inspection.Anchors = anchors
		switch {
		case len(anchors) == 0:
			inspection.Classification = "no_anchors"
			inspection.RecommendedFix = "improve DAG seed selection or ensure structural edges exist for this query"
		case graphPathsMatchExpected(anchors, item.ExpectedAnyOf):
			inspection.Matched = true
			inspection.Classification = "matched"
		default:
			inspection.Classification = "anchor_mismatch"
			inspection.RecommendedFix = "improve DAG ranking, edge coverage, or anchor projection for this query family"
		}
		inspections = append(inspections, inspection)
	}

	return graphInspectionSuiteReport{
		Method:        "repoindex_dag",
		Suite:         suite.Name,
		WorkspacePath: workspacePath,
		GeneratedAt:   time.Now().UTC(),
		Inspections:   inspections,
	}, nil
}

type graphInspectionSummary struct {
	matched        int
	misses         int
	classification string
	fix            string
}

func summarizeGraphInspection(items []graphInspection) graphInspectionSummary {
	counts := map[string]int{}
	for _, item := range items {
		if item.Matched {
			counts["matched"]++
		} else {
			counts[item.Classification]++
		}
	}
	summary := graphInspectionSummary{matched: counts["matched"], misses: len(items) - counts["matched"]}
	bestClass := ""
	bestCount := 0
	for class, count := range counts {
		if class == "matched" {
			continue
		}
		if count > bestCount {
			bestClass, bestCount = class, count
		}
	}
	summary.classification = bestClass
	switch bestClass {
	case "no_anchors":
		summary.fix = "improve DAG seed selection and structural edge coverage"
	case "anchor_mismatch":
		summary.fix = "improve DAG ranking and anchor projection"
	case "engine_error":
		summary.fix = "stabilize repoindex DAG execution path"
	default:
		summary.fix = ""
	}
	return summary
}

func graphPathsMatchExpected(got, expected []string) bool {
	expectedSet := map[string]struct{}{}
	for _, item := range expected {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		expectedSet[item] = struct{}{}
	}
	for _, item := range got {
		if _, ok := expectedSet[strings.TrimSpace(item)]; ok {
			return true
		}
	}
	return false
}
