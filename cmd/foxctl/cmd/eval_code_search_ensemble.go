package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type codeSearchEnsembleEvalResult struct {
	CaseID                     string                             `json:"case_id,omitempty"`
	Category                   string                             `json:"category,omitempty"`
	TaskType                   string                             `json:"task_type,omitempty"`
	RouteFamily                string                             `json:"route_family,omitempty"`
	AdapterExecutionPath       string                             `json:"adapter_execution_path,omitempty"`
	Status                     string                             `json:"status,omitempty"`
	Summary                    string                             `json:"summary,omitempty"`
	AnswerBasis                string                             `json:"answer_basis,omitempty"`
	Confidence                 float64                            `json:"confidence,omitempty"`
	Files                      []string                           `json:"files,omitempty"`
	MatchedPaths               []string                           `json:"matched_paths,omitempty"`
	PathRecall                 float64                            `json:"path_recall,omitempty"`
	Symbols                    []string                           `json:"symbols,omitempty"`
	MatchedSymbols             []string                           `json:"matched_symbols,omitempty"`
	SymbolRecall               float64                            `json:"symbol_recall,omitempty"`
	Snippets                   []evalObservedSnippet              `json:"snippets,omitempty"`
	MatchedSnippets            []string                           `json:"matched_snippets,omitempty"`
	SnippetRecall              float64                            `json:"snippet_recall,omitempty"`
	MatchedFacts               []string                           `json:"matched_facts,omitempty"`
	FactRecall                 float64                            `json:"fact_recall,omitempty"`
	CorrectnessScore           float64                            `json:"correctness_score,omitempty"`
	BridgeQueries              []string                           `json:"bridge_queries,omitempty"`
	CandidateTrace             []codeSearchEnsembleCandidateTrace `json:"candidate_trace,omitempty"`
	FileLocateEvidenceBuckets  map[string][]string                `json:"file_locate_evidence_buckets,omitempty"`
	DirectDispatchFiles        []string                           `json:"direct_dispatch_files,omitempty"`
	ExposureFiles              []string                           `json:"exposure_files,omitempty"`
	StructuralSupportFiles     []string                           `json:"structural_support_files,omitempty"`
	RegistrationFiles          []string                           `json:"registration_files,omitempty"`
	ExcludedPathHits           []string                           `json:"excluded_path_hits,omitempty"`
	WrongScopePenalty          float64                            `json:"wrong_scope_penalty,omitempty"`
	Grounded                   bool                               `json:"grounded,omitempty"`
	DurationMS                 int64                              `json:"duration_ms,omitempty"`
	TotalToolCalls             int                                `json:"total_tool_calls,omitempty"`
	ToolUsage                  map[string]int                     `json:"tool_usage,omitempty"`
	InputTokens                int                                `json:"input_tokens,omitempty"`
	OutputTokens               int                                `json:"output_tokens,omitempty"`
	TotalTokens                int                                `json:"total_tokens,omitempty"`
	TotalCostUSD               float64                            `json:"total_cost_usd,omitempty"`
	LoadedTokenEstimate        int                                `json:"loaded_token_estimate,omitempty"`
	EmittedTokenEstimate       int                                `json:"emitted_token_estimate,omitempty"`
	ParentTokenSavingsEstimate int                                `json:"parent_input_token_savings_estimate,omitempty"`
	CompactionRatio            float64                            `json:"compaction_ratio,omitempty"`
	Models                     []string                           `json:"models,omitempty"`
	Passed                     bool                               `json:"passed,omitempty"`
	Gaps                       []string                           `json:"gaps,omitempty"`
	Error                      string                             `json:"error,omitempty"`
}

type codeSearchEnsembleEvalSummary struct {
	Count               int                                       `json:"count"`
	PassRate            float64                                   `json:"pass_rate"`
	MeanPathRecall      float64                                   `json:"mean_path_recall"`
	MeanSymbolRecall    float64                                   `json:"mean_symbol_recall"`
	MeanSnippetRecall   float64                                   `json:"mean_snippet_recall"`
	MeanFactRecall      float64                                   `json:"mean_fact_recall"`
	MeanCorrectness     float64                                   `json:"mean_correctness"`
	MeanWrongScope      float64                                   `json:"mean_wrong_scope_penalty"`
	MeanConfidence      float64                                   `json:"mean_confidence"`
	MeanDurationMS      float64                                   `json:"mean_duration_ms"`
	MeanToolCalls       float64                                   `json:"mean_tool_calls"`
	MeanTokens          float64                                   `json:"mean_tokens"`
	MeanCostUSD         float64                                   `json:"mean_cost_usd"`
	MeanLoadedTokens    float64                                   `json:"mean_loaded_token_estimate"`
	MeanEmittedTokens   float64                                   `json:"mean_emitted_token_estimate"`
	MeanParentSavings   float64                                   `json:"mean_parent_input_token_savings_estimate"`
	MeanCompactionRatio float64                                   `json:"mean_compaction_ratio"`
	GroundedRate        float64                                   `json:"grounded_rate"`
	ErrorCount          int                                       `json:"error_count"`
	RouteFamilies       map[string]codeSearchEnsembleRouteSummary `json:"route_families,omitempty"`
}

type codeSearchEnsembleRouteSummary struct {
	Count            int                `json:"count"`
	PassRate         float64            `json:"pass_rate"`
	MeanCorrectness  float64            `json:"mean_correctness"`
	MeanBucketCounts map[string]float64 `json:"mean_bucket_counts,omitempty"`
	Alerts           []string           `json:"alerts,omitempty"`
}

type codeSearchRouteAlertPolicy struct {
	MinPrimaryAnchor             float64 `json:"min_primary_anchor"`
	MaxSecondaryAnchor           float64 `json:"max_secondary_anchor"`
	MinPackageRepoEvidence       float64 `json:"min_package_repo_evidence"`
	MinInfraDeclarativeCompanion float64 `json:"min_infra_declarative_companion"`
	FailOnRouteAlerts            bool    `json:"fail_on_route_alerts"`
}

type codeSearchRouteAlertPolicyFile struct {
	MinPrimaryAnchor             *float64 `yaml:"min_primary_anchor"`
	MaxSecondaryAnchor           *float64 `yaml:"max_secondary_anchor"`
	MinPackageRepoEvidence       *float64 `yaml:"min_package_repo_evidence"`
	MinInfraDeclarativeCompanion *float64 `yaml:"min_infra_declarative_companion"`
	FailOnRouteAlerts            *bool    `yaml:"fail_on_route_alerts"`
}

type codeSearchEnsembleCandidateTrace struct {
	Path          string   `json:"path,omitempty"`
	Why           string   `json:"why,omitempty"`
	SupportScore  float64  `json:"support_score,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	LineHints     []int    `json:"line_hints,omitempty"`
	Symbols       []string `json:"symbols,omitempty"`
	EvidenceClass string   `json:"evidence_class,omitempty"`
	AnchorRole    string   `json:"anchor_role,omitempty"`
	RepoNodeCount int      `json:"repo_node_count,omitempty"`
	Selected      bool     `json:"selected,omitempty"`
	SelectedRank  int      `json:"selected_rank,omitempty"`
	PruneReason   string   `json:"prune_reason,omitempty"`
}

type codeSearchEnsembleOutput struct {
	Summary     string  `json:"summary,omitempty"`
	TaskType    string  `json:"task_type,omitempty"`
	AnswerBasis string  `json:"answer_basis,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Files       []struct {
		Path string `json:"path,omitempty"`
		Why  string `json:"why,omitempty"`
	} `json:"files,omitempty"`
	Symbols []struct {
		Path   string `json:"path,omitempty"`
		Symbol string `json:"symbol,omitempty"`
		Line   int    `json:"line,omitempty"`
	} `json:"symbols,omitempty"`
	Snippets []struct {
		Path      string `json:"path,omitempty"`
		StartLine int    `json:"start_line,omitempty"`
		EndLine   int    `json:"end_line,omitempty"`
		Reason    string `json:"reason,omitempty"`
	} `json:"snippets,omitempty"`
	CallPaths []struct {
		Path       string `json:"path,omitempty"`
		SymbolName string `json:"symbol_name,omitempty"`
		LineHint   int    `json:"line_hint,omitempty"`
	} `json:"call_paths,omitempty"`
	DirectDispatchFiles    []string `json:"direct_dispatch_files,omitempty"`
	ExposureFiles          []string `json:"exposure_files,omitempty"`
	StructuralSupportFiles []string `json:"structural_support_files,omitempty"`
	RegistrationFiles      []string `json:"registration_files,omitempty"`
	Gaps                   []string `json:"gaps,omitempty"`
	Metadata               struct {
		Grounded                  bool                               `json:"grounded,omitempty"`
		RouteFamily               string                             `json:"route_family,omitempty"`
		BridgeQueries             []string                           `json:"bridge_queries,omitempty"`
		CandidateTrace            []codeSearchEnsembleCandidateTrace `json:"candidate_trace,omitempty"`
		FileLocateEvidenceBuckets map[string][]string                `json:"file_locate_evidence_buckets,omitempty"`
		Telemetry                 struct {
			ToolUsage                  map[string]int `json:"tool_usage,omitempty"`
			TotalToolCalls             int            `json:"total_tool_calls,omitempty"`
			InputTokens                int            `json:"input_tokens,omitempty"`
			OutputTokens               int            `json:"output_tokens,omitempty"`
			TotalTokens                int            `json:"total_tokens,omitempty"`
			TotalCostUSD               float64        `json:"total_cost_usd,omitempty"`
			LoadedTokenEstimate        int            `json:"loaded_token_estimate,omitempty"`
			EmittedTokenEstimate       int            `json:"emitted_token_estimate,omitempty"`
			ParentTokenSavingsEstimate int            `json:"parent_input_token_savings_estimate,omitempty"`
			CompactionRatio            float64        `json:"compaction_ratio,omitempty"`
			Models                     []string       `json:"models,omitempty"`
		} `json:"telemetry,omitempty"`
	} `json:"metadata,omitempty"`
}

type codeSearchEnsembleInternalExecutor interface {
	ExecuteInternal(ctx context.Context, name string, args json.RawMessage) (map[string]any, error)
}

func newEvalCodeSearchEnsembleCommand() *cobra.Command {
	var (
		workspace               string
		evalDatasetFile         string
		vaultPath               string
		timeout                 time.Duration
		passThreshold           float64
		reportFile              string
		policyFile              string
		failOnRouteAlerts       bool
		minPrimaryAnchor        float64
		maxSecondaryAnchor      float64
		minPackageRepoEvidence  float64
		minInfraDeclarative     float64
		toolProfile             string
		maxCandidates           int
		maxFiles                int
		maxSnippets             int
		llmPlanner              bool
		llmReplanner            bool
		llmPlannerProvider      string
		llmPlannerModel         string
		llmPlannerMaxCandidates int
		includeACA              bool
		llmSelector             bool
		llmProvider             string
		llmModel                string
		llmMaxCandidates        int
	)

	cmd := &cobra.Command{
		Use:   "code-search-ensemble",
		Short: "Evaluate the direct code_search_ensemble retrieval controller against a prompt-eval dataset",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, "eval.code-search-ensemble", err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, "eval.code-search-ensemble", workspace)
			if err != nil {
				return err
			}

			evalCases, err := loadPromptEvalCases(evalDatasetFile)
			if err != nil {
				return writeOptimizeError(out, "eval.code-search-ensemble", fmt.Sprintf("load eval dataset: %v", err))
			}
			if len(evalCases) == 0 {
				return writeOptimizeError(out, "eval.code-search-ensemble", "eval-dataset-file must contain at least one case")
			}

			prevPool := sqliteutil.GetGlobalPool()
			sharedPool := sqliteutil.NewPool()
			sqliteutil.SetGlobalPool(sharedPool)
			defer func() {
				sqliteutil.SetGlobalPool(prevPool)
				_ = sharedPool.Close()
			}()

			alertPolicy := codeSearchRouteAlertPolicy{
				MinPrimaryAnchor:             minPrimaryAnchor,
				MaxSecondaryAnchor:           maxSecondaryAnchor,
				MinPackageRepoEvidence:       minPackageRepoEvidence,
				MinInfraDeclarativeCompanion: minInfraDeclarative,
				FailOnRouteAlerts:            failOnRouteAlerts,
			}
			if strings.TrimSpace(policyFile) != "" {
				filePolicy, err := loadCodeSearchRouteAlertPolicyFile(policyFile)
				if err != nil {
					return writeOptimizeError(out, "eval.code-search-ensemble", fmt.Sprintf("load policy file: %v", err))
				}
				alertPolicy = mergeCodeSearchRouteAlertPolicy(alertPolicy, filePolicy)
			}

			results := make([]codeSearchEnsembleEvalResult, 0, len(evalCases))
			for _, evalCase := range evalCases {
				results = append(results, runSingleCodeSearchEnsembleEval(ctx, cfg, absWorkspace, strings.TrimSpace(vaultPath), evalCase, timeout, toolProfile, maxCandidates, maxFiles, maxSnippets, passThreshold, llmPlanner, llmReplanner, llmPlannerProvider, llmPlannerModel, llmPlannerMaxCandidates, includeACA, llmSelector, llmProvider, llmModel, llmMaxCandidates))
			}

			summary := summarizeCodeSearchEnsembleEvalResults(results, alertPolicy)
			routeAlerts := collectCodeSearchRouteAlerts(summary.RouteFamilies)
			report := map[string]any{
				"operation":               "eval.code-search-ensemble",
				"workspace_id":            absWorkspace,
				"eval_case_count":         len(evalCases),
				"eval_cases":              evalCases,
				"tool_profile":            toolProfile,
				"pass_threshold":          passThreshold,
				"route_alert_policy":      alertPolicy,
				"route_alert_policy_file": strings.TrimSpace(policyFile),
				"route_alerts":            routeAlerts,
				"results":                 results,
				"summary":                 summary,
				"cli_command":             cmd.CommandPath(),
				"effective_contract": map[string]any{
					"adapter_execution_path": "internal_adapter_bypass_v1",
					"tool_name":              "code_search_ensemble",
				},
			}

			if strings.TrimSpace(reportFile) != "" {
				payload, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return writeOptimizeError(out, "eval.code-search-ensemble", fmt.Sprintf("marshal report: %v", err))
				}
				if err := os.WriteFile(reportFile, append(payload, '\n'), 0o644); err != nil {
					return writeOptimizeError(out, "eval.code-search-ensemble", fmt.Sprintf("write report: %v", err))
				}
			}
			if failOnRouteAlerts && len(routeAlerts) > 0 {
				return writeOptimizeError(out, "eval.code-search-ensemble", "route alerts: "+strings.Join(routeAlerts, "; "))
			}

			return protocol.WriteOK(out, "eval.code-search-ensemble", map[string]any{
				"markdown": renderCodeSearchEnsembleEvalMarkdown(absWorkspace, evalCases, summary, results),
				"report":   report,
			}, protocol.WithSource("run"), protocol.WithWorkspace(absWorkspace))
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&evalDatasetFile, "eval-dataset-file", "", "JSONL eval dataset file with question/context/expected_paths rows")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for environment bootstrap")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Per-case timeout")
	cmd.Flags().Float64Var(&passThreshold, "pass-threshold", 0.8, "Path-recall threshold for passing coding/path cases")
	cmd.Flags().StringVar(&reportFile, "report-file", "", "Optional path to write the JSON report")
	cmd.Flags().StringVar(&policyFile, "policy-file", "", "Optional YAML file with route alert thresholds")
	cmd.Flags().BoolVar(&failOnRouteAlerts, "fail-on-route-alerts", false, "Exit with an error when any route-family alert is present")
	cmd.Flags().Float64Var(&minPrimaryAnchor, "min-primary-anchor", 1.0, "Minimum mean primary_anchor count before a route alert is raised")
	cmd.Flags().Float64Var(&maxSecondaryAnchor, "max-secondary-anchor", 0.5, "Maximum mean secondary_anchor count before a route alert is raised")
	cmd.Flags().Float64Var(&minPackageRepoEvidence, "min-package-repo-evidence", 1.5, "Minimum mean repo_evidence count for package_ownership before a route alert is raised")
	cmd.Flags().Float64Var(&minInfraDeclarative, "min-infra-declarative-companion", 1.0, "Minimum mean declarative_companion count for infra_resource before a route alert is raised")
	cmd.Flags().StringVar(&toolProfile, "tool-profile", rlmenv.ToolProfileCodeIntel, "Experimental RLM tool profile to expose during the eval")
	cmd.Flags().IntVar(&maxCandidates, "max-candidates", 8, "Maximum candidates passed to code_search_ensemble")
	cmd.Flags().IntVar(&maxFiles, "max-files", 4, "Maximum grounded files passed to code_search_ensemble")
	cmd.Flags().IntVar(&maxSnippets, "max-snippets", 4, "Maximum snippets passed to code_search_ensemble")
	cmd.Flags().BoolVar(&llmPlanner, "llm-planner", false, "Enable bounded LLM planning for seed queries and path biases")
	cmd.Flags().BoolVar(&llmReplanner, "llm-replanner", false, "Enable a bounded second-wave LLM replanner after the first retrieval wave")
	cmd.Flags().StringVar(&llmPlannerProvider, "llm-planner-provider", "", "Provider override for bounded planner")
	cmd.Flags().StringVar(&llmPlannerModel, "llm-planner-model", "", "Model override for bounded planner")
	cmd.Flags().IntVar(&llmPlannerMaxCandidates, "llm-planner-max-candidates", 8, "Maximum probes supplied to the bounded planner")
	cmd.Flags().BoolVar(&includeACA, "include-aca", false, "Enable bounded ACA note/concept guidance during candidate bootstrap")
	cmd.Flags().BoolVar(&llmSelector, "llm-selector", false, "Enable bounded LLM adjudication over top execution-trace candidates")
	cmd.Flags().StringVar(&llmProvider, "llm-selector-provider", "", "Provider override for bounded selector")
	cmd.Flags().StringVar(&llmModel, "llm-selector-model", "", "Model override for bounded selector")
	cmd.Flags().IntVar(&llmMaxCandidates, "llm-selector-max-candidates", 8, "Maximum candidates passed to the bounded selector")
	_ = cmd.MarkFlagRequired("eval-dataset-file")
	return cmd
}

func runSingleCodeSearchEnsembleEval(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	vaultPath string,
	evalCase promptEvalCase,
	timeout time.Duration,
	toolProfile string,
	maxCandidates, maxFiles, maxSnippets int,
	passThreshold float64,
	llmPlanner bool,
	llmReplanner bool,
	llmPlannerProvider, llmPlannerModel string,
	llmPlannerMaxCandidates int,
	includeACA bool,
	llmSelector bool,
	llmProvider, llmModel string,
	llmMaxCandidates int,
) codeSearchEnsembleEvalResult {
	result := codeSearchEnsembleEvalResult{
		CaseID:               strings.TrimSpace(evalCase.ID),
		Category:             strings.TrimSpace(evalCase.Category),
		TaskType:             normalizeCodeSearchEvalTaskType(evalCase),
		AdapterExecutionPath: "internal_adapter_bypass_v1",
		Status:               "ok",
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	task := rlm.Task{
		Prompt:        buildCodeSearchEnsembleEvalQuery(evalCase),
		WorkspaceRoot: workspace,
		MaxDepth:      0,
		MaxIterations: 1,
		MaxSubcalls:   0,
	}

	companionDB, companionClose, err := openRLMCompanionDB(runCtx, cfg)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   strings.TrimSpace(vaultPath),
		CompanionDB: companionDB,
	})
	env, err := bootstrapper.Build(runCtx, task)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	defer func() { _ = bootstrapper.Close() }()
	env.Tools = rlmenv.FilterTools(env.Tools, toolProfile)

	adapter := rlmenv.NewReadOnlyAdapter(cfg, workspace, strings.TrimSpace(vaultPath), companionDB, env)
	adapter.SetContextEngineStore(bootstrapper.ContextEngineStore())
	adapter.SetTaskStore(bootstrapper.TaskStore())

	start := time.Now()
	payload := map[string]any{
		"query":     buildCodeSearchEnsembleEvalQuery(evalCase),
		"task_type": result.TaskType,
		"llm_planner": map[string]any{
			"enabled":        llmPlanner,
			"enable_replan":  llmReplanner,
			"provider":       strings.TrimSpace(llmPlannerProvider),
			"model":          strings.TrimSpace(llmPlannerModel),
			"max_candidates": llmPlannerMaxCandidates,
		},
		"llm_selector": map[string]any{
			"enabled":        llmSelector,
			"provider":       strings.TrimSpace(llmProvider),
			"model":          strings.TrimSpace(llmModel),
			"max_candidates": llmMaxCandidates,
		},
		"constraints": map[string]any{
			"exclude_paths":     append([]string(nil), evalCase.ExcludedPaths...),
			"include_aca":       includeACA,
			"require_grounding": evalCase.RequireGrounding || len(evalCase.ExpectedPaths) > 0,
		},
		"budget": map[string]any{
			"max_candidates": maxCandidates,
			"max_files":      maxFiles,
			"max_snippets":   maxSnippets,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	out, err := runCodeSearchEnsembleInternal(runCtx, adapter, raw)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	decoded, err := decodeCodeSearchEnsembleOutput(out)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	result.Summary = strings.TrimSpace(decoded.Summary)
	result.AnswerBasis = strings.TrimSpace(decoded.AnswerBasis)
	result.Confidence = decoded.Confidence
	result.Grounded = decoded.Metadata.Grounded
	result.RouteFamily = strings.TrimSpace(decoded.Metadata.RouteFamily)
	result.ToolUsage = decoded.Metadata.Telemetry.ToolUsage
	result.TotalToolCalls = decoded.Metadata.Telemetry.TotalToolCalls
	result.InputTokens = decoded.Metadata.Telemetry.InputTokens
	result.OutputTokens = decoded.Metadata.Telemetry.OutputTokens
	result.TotalTokens = decoded.Metadata.Telemetry.TotalTokens
	result.TotalCostUSD = decoded.Metadata.Telemetry.TotalCostUSD
	result.LoadedTokenEstimate = decoded.Metadata.Telemetry.LoadedTokenEstimate
	result.EmittedTokenEstimate = decoded.Metadata.Telemetry.EmittedTokenEstimate
	result.ParentTokenSavingsEstimate = decoded.Metadata.Telemetry.ParentTokenSavingsEstimate
	result.CompactionRatio = decoded.Metadata.Telemetry.CompactionRatio
	result.Models = append([]string(nil), decoded.Metadata.Telemetry.Models...)
	result.BridgeQueries = decodeStringSliceField(out["metadata"], "bridge_queries")
	result.CandidateTrace = decodeCodeSearchCandidateTraceField(out["metadata"], "candidate_trace")
	result.FileLocateEvidenceBuckets = decodeStringSliceMapField(out["metadata"], "file_locate_evidence_buckets")
	result.DirectDispatchFiles = decodeStringSliceTopLevel(out, "direct_dispatch_files")
	result.ExposureFiles = decodeStringSliceTopLevel(out, "exposure_files")
	result.StructuralSupportFiles = decodeStringSliceTopLevel(out, "structural_support_files")
	result.RegistrationFiles = decodeStringSliceTopLevel(out, "registration_files")
	result.Gaps = append([]string(nil), decoded.Gaps...)
	result.Files = extractCodeSearchEnsemblePaths(decoded)
	result.Symbols = extractCodeSearchEnsembleSymbols(decoded)
	result.Snippets = extractCodeSearchEnsembleSnippets(decoded)
	result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(result.Files, "\n"))
	result.MatchedSymbols, result.SymbolRecall = scoreExpectedSymbols(evalCase.ExpectedSymbols, result.Symbols)
	result.MatchedSnippets, result.SnippetRecall = scoreExpectedSnippets(workspace, evalCase.ExpectedSnippets, result.Snippets)
	result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, buildCodeSearchEnsembleFactBlob(decoded))
	result.CorrectnessScore = blendedCorrectnessScore(
		result.PathRecall,
		result.SymbolRecall,
		result.SnippetRecall,
		result.FactRecall,
		len(evalCase.ExpectedPaths) > 0,
		len(evalCase.ExpectedSymbols) > 0,
		len(evalCase.ExpectedSnippets) > 0,
		len(evalCase.RequiredFacts) > 0,
	)
	result.ExcludedPathHits, result.WrongScopePenalty = scoreExcludedPaths(evalCase.ExcludedPaths, strings.Join(result.Files, "\n"))
	result.Passed = shouldPassCodeSearchEnsembleEval(result, evalCase, passThreshold)
	return result
}

func runCodeSearchEnsembleInternal(ctx context.Context, executor codeSearchEnsembleInternalExecutor, payload json.RawMessage) (map[string]any, error) {
	return executor.ExecuteInternal(ctx, "code_search_ensemble", payload)
}

func buildCodeSearchEnsembleEvalQuery(evalCase promptEvalCase) string {
	question := strings.TrimSpace(evalCase.Question)
	context := strings.TrimSpace(evalCase.Context)
	if context == "" {
		return question
	}
	return question + "\n\nContext:\n" + context
}

func normalizeCodeSearchEvalTaskType(evalCase promptEvalCase) string {
	switch strings.TrimSpace(evalCase.TaskType) {
	case "execution_trace", "symbol_inspect", "file_locate", "change_impact", "registration_trace", "architecture_map", "subsystem_map", "integration_surface":
		return strings.TrimSpace(evalCase.TaskType)
	default:
		if len(evalCase.ExpectedPaths) > 0 {
			return "file_locate"
		}
		return "file_locate"
	}
}

func decodeCodeSearchEnsembleOutput(raw map[string]any) (codeSearchEnsembleOutput, error) {
	body, err := json.Marshal(raw)
	if err != nil {
		return codeSearchEnsembleOutput{}, err
	}
	var out codeSearchEnsembleOutput
	if err := json.Unmarshal(body, &out); err != nil {
		return codeSearchEnsembleOutput{}, err
	}
	return out, nil
}

func extractCodeSearchEnsemblePaths(out codeSearchEnsembleOutput) []string {
	paths := make([]string, 0, len(out.Files))
	for _, file := range out.Files {
		path := filepath.ToSlash(strings.TrimSpace(file.Path))
		path = strings.TrimPrefix(path, "./")
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return uniqueObservedPaths(paths)
}

func uniqueObservedPaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		path = strings.TrimPrefix(path, "./")
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func extractCodeSearchEnsembleSymbols(out codeSearchEnsembleOutput) []string {
	seen := map[string]struct{}{}
	symbols := make([]string, 0, len(out.Symbols)+len(out.CallPaths))
	for _, symbol := range out.Symbols {
		name := strings.TrimSpace(symbol.Symbol)
		if name == "" {
			continue
		}
		id := name
		if path := normalizeCodeSearchPath(symbol.Path); path != "" {
			id = path + "::" + name
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		symbols = append(symbols, id)
	}
	for _, path := range out.CallPaths {
		name := strings.TrimSpace(path.SymbolName)
		if name == "" {
			continue
		}
		id := name
		if p := normalizeCodeSearchPath(path.Path); p != "" {
			id = p + "::" + name
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		symbols = append(symbols, id)
	}
	sort.Strings(symbols)
	return symbols
}

func extractCodeSearchEnsembleSnippets(out codeSearchEnsembleOutput) []evalObservedSnippet {
	snippets := make([]evalObservedSnippet, 0, len(out.Snippets))
	for _, snippet := range out.Snippets {
		path := normalizeCodeSearchPath(snippet.Path)
		if path == "" {
			continue
		}
		snippets = append(snippets, evalObservedSnippet{
			Path:      path,
			StartLine: snippet.StartLine,
			EndLine:   snippet.EndLine,
		})
	}
	return normalizeObservedSnippets(snippets)
}

func buildCodeSearchEnsembleFactBlob(out codeSearchEnsembleOutput) string {
	parts := make([]string, 0, 2+len(out.Files)+len(out.Symbols)+len(out.CallPaths)+len(out.Gaps))
	if strings.TrimSpace(out.Summary) != "" {
		parts = append(parts, strings.TrimSpace(out.Summary))
	}
	if strings.TrimSpace(out.AnswerBasis) != "" {
		parts = append(parts, strings.TrimSpace(out.AnswerBasis))
	}
	for _, file := range out.Files {
		if path := strings.TrimSpace(file.Path); path != "" {
			parts = append(parts, path)
		}
		if why := strings.TrimSpace(file.Why); why != "" {
			parts = append(parts, why)
		}
	}
	for _, symbol := range out.Symbols {
		if name := strings.TrimSpace(symbol.Symbol); name != "" {
			parts = append(parts, name)
		}
	}
	for _, snippet := range out.Snippets {
		if path := strings.TrimSpace(snippet.Path); path != "" {
			parts = append(parts, path)
		}
		if reason := strings.TrimSpace(snippet.Reason); reason != "" {
			parts = append(parts, reason)
		}
	}
	for _, path := range out.CallPaths {
		if name := strings.TrimSpace(path.SymbolName); name != "" {
			parts = append(parts, name)
		}
	}
	for _, gap := range out.Gaps {
		if strings.TrimSpace(gap) != "" {
			parts = append(parts, gap)
		}
	}
	return strings.Join(parts, "\n")
}

func decodeStringSliceField(raw any, key string) []string {
	meta, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	value, ok := meta[strings.TrimSpace(key)]
	if !ok {
		return nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []string
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func decodeStringSliceTopLevel(raw map[string]any, key string) []string {
	value, ok := raw[strings.TrimSpace(key)]
	if !ok {
		return nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []string
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func decodeStringSliceMapField(raw any, key string) map[string][]string {
	meta, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	value, ok := meta[strings.TrimSpace(key)]
	if !ok {
		return nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string][]string
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decodeCodeSearchCandidateTraceField(raw any, key string) []codeSearchEnsembleCandidateTrace {
	meta, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	value, ok := meta[strings.TrimSpace(key)]
	if !ok {
		return nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []codeSearchEnsembleCandidateTrace
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}

func shouldPassCodeSearchEnsembleEval(result codeSearchEnsembleEvalResult, evalCase promptEvalCase, passThreshold float64) bool {
	if strings.TrimSpace(result.Error) != "" {
		return false
	}
	if len(result.ExcludedPathHits) > 0 {
		return false
	}
	if evalCase.RequireGrounding && !result.Grounded {
		return false
	}
	if hasCodeCorrectnessExpectations(evalCase) {
		if len(evalCase.ExpectedPaths) > 0 && result.PathRecall == 0 {
			return false
		}
		return result.CorrectnessScore >= passThreshold
	}
	return result.Confidence >= passThreshold
}

func summarizeCodeSearchEnsembleEvalResults(results []codeSearchEnsembleEvalResult, alertPolicy codeSearchRouteAlertPolicy) codeSearchEnsembleEvalSummary {
	var summary codeSearchEnsembleEvalSummary
	summary.Count = len(results)
	if len(results) == 0 {
		return summary
	}
	type routeAccumulator struct {
		count           int
		passRate        float64
		meanCorrectness float64
		bucketCountSums map[string]float64
	}
	routeAccums := map[string]*routeAccumulator{}
	for _, result := range results {
		if result.Passed {
			summary.PassRate++
		}
		summary.MeanPathRecall += result.PathRecall
		summary.MeanSymbolRecall += result.SymbolRecall
		summary.MeanSnippetRecall += result.SnippetRecall
		summary.MeanFactRecall += result.FactRecall
		summary.MeanCorrectness += result.CorrectnessScore
		summary.MeanWrongScope += result.WrongScopePenalty
		summary.MeanConfidence += result.Confidence
		summary.MeanDurationMS += float64(result.DurationMS)
		summary.MeanToolCalls += float64(result.TotalToolCalls)
		summary.MeanTokens += float64(result.TotalTokens)
		summary.MeanCostUSD += result.TotalCostUSD
		summary.MeanLoadedTokens += float64(result.LoadedTokenEstimate)
		summary.MeanEmittedTokens += float64(result.EmittedTokenEstimate)
		summary.MeanParentSavings += float64(result.ParentTokenSavingsEstimate)
		summary.MeanCompactionRatio += result.CompactionRatio
		if result.Grounded {
			summary.GroundedRate++
		}
		if strings.TrimSpace(result.Error) != "" {
			summary.ErrorCount++
		}
		routeFamily := strings.TrimSpace(result.RouteFamily)
		if routeFamily == "" {
			continue
		}
		acc := routeAccums[routeFamily]
		if acc == nil {
			acc = &routeAccumulator{bucketCountSums: map[string]float64{}}
			routeAccums[routeFamily] = acc
		}
		acc.count++
		if result.Passed {
			acc.passRate++
		}
		acc.meanCorrectness += result.CorrectnessScore
		for bucket, paths := range result.FileLocateEvidenceBuckets {
			acc.bucketCountSums[bucket] += float64(len(paths))
		}
	}
	scale := float64(len(results))
	summary.PassRate /= scale
	summary.MeanPathRecall /= scale
	summary.MeanSymbolRecall /= scale
	summary.MeanSnippetRecall /= scale
	summary.MeanFactRecall /= scale
	summary.MeanCorrectness /= scale
	summary.MeanWrongScope /= scale
	summary.MeanConfidence /= scale
	summary.MeanDurationMS /= scale
	summary.MeanToolCalls /= scale
	summary.MeanTokens /= scale
	summary.MeanCostUSD /= scale
	summary.MeanLoadedTokens /= scale
	summary.MeanEmittedTokens /= scale
	summary.MeanParentSavings /= scale
	summary.MeanCompactionRatio /= scale
	summary.GroundedRate /= scale
	if len(routeAccums) > 0 {
		summary.RouteFamilies = make(map[string]codeSearchEnsembleRouteSummary, len(routeAccums))
		for routeFamily, acc := range routeAccums {
			if acc == nil || acc.count == 0 {
				continue
			}
			routeScale := float64(acc.count)
			item := codeSearchEnsembleRouteSummary{
				Count:           acc.count,
				PassRate:        acc.passRate / routeScale,
				MeanCorrectness: acc.meanCorrectness / routeScale,
			}
			if len(acc.bucketCountSums) > 0 {
				item.MeanBucketCounts = make(map[string]float64, len(acc.bucketCountSums))
				for bucket, value := range acc.bucketCountSums {
					item.MeanBucketCounts[bucket] = value / routeScale
				}
			}
			item.Alerts = buildCodeSearchRouteAlerts(routeFamily, item.MeanBucketCounts, alertPolicy)
			summary.RouteFamilies[routeFamily] = item
		}
	}
	return summary
}

func buildCodeSearchRouteAlerts(routeFamily string, bucketCounts map[string]float64, alertPolicy codeSearchRouteAlertPolicy) []string {
	if len(bucketCounts) == 0 {
		return nil
	}
	alerts := make([]string, 0, 4)
	if bucketCounts["primary_anchor"] < alertPolicy.MinPrimaryAnchor {
		alerts = append(alerts, "low primary_anchor coverage")
	}
	if bucketCounts["secondary_anchor"] > alertPolicy.MaxSecondaryAnchor {
		alerts = append(alerts, "high secondary_anchor usage")
	}
	switch strings.TrimSpace(routeFamily) {
	case "package_ownership":
		if bucketCounts["repo_evidence"] < alertPolicy.MinPackageRepoEvidence {
			alerts = append(alerts, "low repo_evidence coverage")
		}
	case "infra_resource":
		if bucketCounts["declarative_companion"] < alertPolicy.MinInfraDeclarativeCompanion {
			alerts = append(alerts, "low declarative_companion coverage")
		}
	}
	if len(alerts) == 0 {
		return nil
	}
	return alerts
}

func collectCodeSearchRouteAlerts(routeFamilies map[string]codeSearchEnsembleRouteSummary) []string {
	if len(routeFamilies) == 0 {
		return nil
	}
	keys := make([]string, 0, len(routeFamilies))
	for key := range routeFamilies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		item := routeFamilies[key]
		for _, alert := range item.Alerts {
			alert = strings.TrimSpace(alert)
			if alert == "" {
				continue
			}
			out = append(out, key+": "+alert)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func loadCodeSearchRouteAlertPolicyFile(path string) (codeSearchRouteAlertPolicyFile, error) {
	body, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return codeSearchRouteAlertPolicyFile{}, err
	}
	var out codeSearchRouteAlertPolicyFile
	if err := yaml.Unmarshal(body, &out); err != nil {
		return codeSearchRouteAlertPolicyFile{}, err
	}
	return out, nil
}

func mergeCodeSearchRouteAlertPolicy(base codeSearchRouteAlertPolicy, override codeSearchRouteAlertPolicyFile) codeSearchRouteAlertPolicy {
	if override.MinPrimaryAnchor != nil {
		base.MinPrimaryAnchor = *override.MinPrimaryAnchor
	}
	if override.MaxSecondaryAnchor != nil {
		base.MaxSecondaryAnchor = *override.MaxSecondaryAnchor
	}
	if override.MinPackageRepoEvidence != nil {
		base.MinPackageRepoEvidence = *override.MinPackageRepoEvidence
	}
	if override.MinInfraDeclarativeCompanion != nil {
		base.MinInfraDeclarativeCompanion = *override.MinInfraDeclarativeCompanion
	}
	if override.FailOnRouteAlerts != nil {
		base.FailOnRouteAlerts = *override.FailOnRouteAlerts
	}
	return base
}

func renderCodeSearchEnsembleEvalMarkdown(workspace string, evalCases []promptEvalCase, summary codeSearchEnsembleEvalSummary, results []codeSearchEnsembleEvalResult) string {
	var b strings.Builder
	b.WriteString("# Code Search Ensemble Eval\n\n")
	b.WriteString("- Workspace: `" + workspace + "`\n")
	b.WriteString(fmt.Sprintf("- Eval cases: `%d`\n\n", len(evalCases)))
	b.WriteString("## Summary\n\n")
	b.WriteString(fmt.Sprintf("- Pass rate: `%.2f`\n", summary.PassRate))
	b.WriteString(fmt.Sprintf("- Mean path recall: `%.2f`\n", summary.MeanPathRecall))
	b.WriteString(fmt.Sprintf("- Mean symbol recall: `%.2f`\n", summary.MeanSymbolRecall))
	b.WriteString(fmt.Sprintf("- Mean snippet recall: `%.2f`\n", summary.MeanSnippetRecall))
	b.WriteString(fmt.Sprintf("- Mean fact recall: `%.2f`\n", summary.MeanFactRecall))
	b.WriteString(fmt.Sprintf("- Mean correctness: `%.2f`\n", summary.MeanCorrectness))
	b.WriteString(fmt.Sprintf("- Mean wrong-scope penalty: `%.2f`\n", summary.MeanWrongScope))
	b.WriteString(fmt.Sprintf("- Grounded rate: `%.2f`\n", summary.GroundedRate))
	b.WriteString(fmt.Sprintf("- Mean confidence: `%.2f`\n", summary.MeanConfidence))
	b.WriteString(fmt.Sprintf("- Mean tool calls: `%.2f`\n", summary.MeanToolCalls))
	b.WriteString(fmt.Sprintf("- Mean tokens: `%.0f`\n", summary.MeanTokens))
	b.WriteString(fmt.Sprintf("- Mean cost: `%.4f`\n", summary.MeanCostUSD))
	b.WriteString(fmt.Sprintf("- Mean loaded tokens: `%.0f`\n", summary.MeanLoadedTokens))
	b.WriteString(fmt.Sprintf("- Mean emitted tokens: `%.0f`\n", summary.MeanEmittedTokens))
	b.WriteString(fmt.Sprintf("- Mean parent savings: `%.0f`\n", summary.MeanParentSavings))
	b.WriteString(fmt.Sprintf("- Mean compaction ratio: `%.2f`\n", summary.MeanCompactionRatio))
	b.WriteString(fmt.Sprintf("- Mean duration: `%.0fms`\n\n", summary.MeanDurationMS))
	if len(summary.RouteFamilies) > 0 {
		b.WriteString("## Route Families\n\n")
		keys := make([]string, 0, len(summary.RouteFamilies))
		for key := range summary.RouteFamilies {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			item := summary.RouteFamilies[key]
			b.WriteString("### " + key + "\n\n")
			b.WriteString(fmt.Sprintf("- Cases: `%d`\n", item.Count))
			b.WriteString(fmt.Sprintf("- Pass rate: `%.2f`\n", item.PassRate))
			b.WriteString(fmt.Sprintf("- Mean correctness: `%.2f`\n", item.MeanCorrectness))
			if len(item.MeanBucketCounts) > 0 {
				bucketKeys := make([]string, 0, len(item.MeanBucketCounts))
				for bucket := range item.MeanBucketCounts {
					bucketKeys = append(bucketKeys, bucket)
				}
				sort.Strings(bucketKeys)
				parts := make([]string, 0, len(bucketKeys))
				for _, bucket := range bucketKeys {
					parts = append(parts, fmt.Sprintf("%s=%.2f", bucket, item.MeanBucketCounts[bucket]))
				}
				b.WriteString("- Mean bucket counts: `" + strings.Join(parts, ", ") + "`\n")
			}
			if len(item.Alerts) > 0 {
				b.WriteString("- Alerts: `" + strings.Join(item.Alerts, ", ") + "`\n")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("## Results\n\n")
	sort.Slice(results, func(i, j int) bool {
		return firstNonEmpty(results[i].CaseID, results[i].TaskType) < firstNonEmpty(results[j].CaseID, results[j].TaskType)
	})
	for _, result := range results {
		b.WriteString("### " + firstNonEmpty(result.CaseID, result.TaskType) + "\n\n")
		b.WriteString(fmt.Sprintf("- Task type: `%s`\n", result.TaskType))
		if result.RouteFamily != "" {
			b.WriteString(fmt.Sprintf("- Route family: `%s`\n", result.RouteFamily))
		}
		b.WriteString(fmt.Sprintf("- Status: `%s`\n", result.Status))
		b.WriteString(fmt.Sprintf("- Path recall: `%.2f`\n", result.PathRecall))
		b.WriteString(fmt.Sprintf("- Symbol recall: `%.2f`\n", result.SymbolRecall))
		b.WriteString(fmt.Sprintf("- Snippet recall: `%.2f`\n", result.SnippetRecall))
		b.WriteString(fmt.Sprintf("- Fact recall: `%.2f`\n", result.FactRecall))
		b.WriteString(fmt.Sprintf("- Correctness: `%.2f`\n", result.CorrectnessScore))
		b.WriteString(fmt.Sprintf("- Wrong-scope penalty: `%.2f`\n", result.WrongScopePenalty))
		b.WriteString(fmt.Sprintf("- Grounded: `%t`\n", result.Grounded))
		b.WriteString(fmt.Sprintf("- Confidence: `%.2f`\n", result.Confidence))
		b.WriteString(fmt.Sprintf("- Duration: `%dms`\n", result.DurationMS))
		b.WriteString(fmt.Sprintf("- Tool calls: `%d`\n", result.TotalToolCalls))
		b.WriteString(fmt.Sprintf("- Tokens: `%d`\n", result.TotalTokens))
		b.WriteString(fmt.Sprintf("- Loaded tokens: `%d`\n", result.LoadedTokenEstimate))
		b.WriteString(fmt.Sprintf("- Emitted tokens: `%d`\n", result.EmittedTokenEstimate))
		b.WriteString(fmt.Sprintf("- Parent savings: `%d`\n", result.ParentTokenSavingsEstimate))
		if result.CompactionRatio > 0 {
			b.WriteString(fmt.Sprintf("- Compaction ratio: `%.2f`\n", result.CompactionRatio))
		}
		if result.TotalCostUSD > 0 {
			b.WriteString(fmt.Sprintf("- Cost: `%.4f`\n", result.TotalCostUSD))
		}
		if result.AnswerBasis != "" {
			b.WriteString("- Answer basis: `" + result.AnswerBasis + "`\n")
		}
		if len(result.Models) > 0 {
			b.WriteString("- Models: `" + strings.Join(result.Models, ", ") + "`\n")
		}
		if len(result.ToolUsage) > 0 {
			keys := make([]string, 0, len(result.ToolUsage))
			for key := range result.ToolUsage {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, fmt.Sprintf("%s=%d", key, result.ToolUsage[key]))
			}
			b.WriteString("- Tool usage: `" + strings.Join(parts, ", ") + "`\n")
		}
		if len(result.Files) > 0 {
			b.WriteString("- Files: `" + strings.Join(result.Files, ", ") + "`\n")
		}
		if len(result.FileLocateEvidenceBuckets) > 0 {
			keys := make([]string, 0, len(result.FileLocateEvidenceBuckets))
			for key := range result.FileLocateEvidenceBuckets {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				if len(result.FileLocateEvidenceBuckets[key]) == 0 {
					continue
				}
				parts = append(parts, fmt.Sprintf("%s=%s", key, strings.Join(result.FileLocateEvidenceBuckets[key], ", ")))
			}
			if len(parts) > 0 {
				b.WriteString("- Evidence buckets: `" + strings.Join(parts, " | ") + "`\n")
			}
		}
		if len(result.Symbols) > 0 {
			b.WriteString("- Symbols: `" + strings.Join(result.Symbols, ", ") + "`\n")
		}
		if len(result.MatchedSymbols) > 0 {
			b.WriteString("- Matched symbols: `" + strings.Join(result.MatchedSymbols, ", ") + "`\n")
		}
		if len(result.MatchedSnippets) > 0 {
			b.WriteString("- Matched snippets: `" + strings.Join(result.MatchedSnippets, ", ") + "`\n")
		}
		if len(result.MatchedFacts) > 0 {
			b.WriteString("- Matched facts: `" + strings.Join(result.MatchedFacts, ", ") + "`\n")
		}
		if len(result.ExcludedPathHits) > 0 {
			b.WriteString("- Excluded path hits: `" + strings.Join(result.ExcludedPathHits, ", ") + "`\n")
		}
		if result.Error != "" {
			b.WriteString("- Error: " + result.Error + "\n")
		}
		if result.Summary != "" {
			b.WriteString("- Summary: " + result.Summary + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
