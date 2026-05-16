package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	obs "github.com/joshka0/foxctl/internal/adapters/skillslib/obs"
	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
	"github.com/spf13/cobra"
)

type gatherContextEvalResult struct {
	CaseID                     string         `json:"case_id,omitempty"`
	Category                   string         `json:"category,omitempty"`
	TaskType                   string         `json:"task_type,omitempty"`
	Status                     string         `json:"status,omitempty"`
	BundleStatus               string         `json:"bundle_status,omitempty"`
	CertificateStatus          string         `json:"certificate_status,omitempty"`
	Answerable                 bool           `json:"answerable"`
	Paths                      []string       `json:"paths,omitempty"`
	ProviderSeenPaths          []string       `json:"provider_seen_paths,omitempty"`
	ProviderCandidatePaths     []string       `json:"provider_candidate_paths,omitempty"`
	RawEvidencePaths           []string       `json:"raw_evidence_paths,omitempty"`
	SelectedPaths              []string       `json:"selected_paths,omitempty"`
	AnswerCandidatePaths       []string       `json:"answer_candidate_paths,omitempty"`
	MatchedPaths               []string       `json:"matched_paths,omitempty"`
	ExpectedPathsMissingOnDisk []string       `json:"expected_paths_missing_on_disk,omitempty"`
	StaleEval                  bool           `json:"stale_eval,omitempty"`
	ProviderMisses             []string       `json:"provider_misses,omitempty"`
	BundleEvidenceMisses       []string       `json:"bundle_evidence_misses,omitempty"`
	RetrievalMisses            []string       `json:"retrieval_misses,omitempty"`
	ReductionMisses            []string       `json:"reduction_misses,omitempty"`
	PathRecall                 float64        `json:"path_recall,omitempty"`
	ExpectedRoles              []string       `json:"expected_roles,omitempty"`
	MatchedRoles               []string       `json:"matched_roles,omitempty"`
	RoleRecall                 float64        `json:"role_recall,omitempty"`
	RoleCoverage               map[string]int `json:"role_coverage,omitempty"`
	MatchedFacts               []string       `json:"matched_facts,omitempty"`
	FactRecall                 float64        `json:"fact_recall,omitempty"`
	Lanes                      []string       `json:"lanes,omitempty"`
	MemoryStatuses             []string       `json:"memory_statuses,omitempty"`
	SourceCoverage             map[string]int `json:"source_coverage,omitempty"`
	ProviderTelemetry          any            `json:"provider_telemetry,omitempty"`
	RawContextChars            int            `json:"raw_context_chars,omitempty"`
	EmittedContextChars        int            `json:"emitted_context_chars,omitempty"`
	OmittedContextItems        int            `json:"omitted_context_items,omitempty"`
	FactCount                  int            `json:"fact_count,omitempty"`
	EvidenceCount              int            `json:"evidence_count,omitempty"`
	DurationMS                 int64          `json:"duration_ms,omitempty"`
	Passed                     bool           `json:"passed,omitempty"`
	Gaps                       []string       `json:"gaps,omitempty"`
	Error                      string         `json:"error,omitempty"`
}

type gatherContextEvalSummary struct {
	Count                        int                            `json:"count"`
	ScoredCount                  int                            `json:"scored_count,omitempty"`
	StaleEvalCount               int                            `json:"stale_eval_count,omitempty"`
	PassRate                     float64                        `json:"pass_rate"`
	MeanPathRecall               float64                        `json:"mean_path_recall"`
	MeanRoleRecall               float64                        `json:"mean_role_recall,omitempty"`
	RoleRecallByRole             []gatherContextRoleRecallScore `json:"role_recall_by_role,omitempty"`
	PeripheralRoleCoverage       map[string]int                 `json:"peripheral_role_coverage,omitempty"`
	WrongRolePeripheralCaseCount map[string]int                 `json:"wrong_role_peripheral_case_count,omitempty"`
	MeanFactRecall               float64                        `json:"mean_fact_recall"`
	MeanDurationMS               float64                        `json:"mean_duration_ms"`
	MeanRawContextChars          float64                        `json:"mean_raw_context_chars"`
	MeanEmittedContextChars      float64                        `json:"mean_emitted_context_chars"`
	MeanOmittedContextItems      float64                        `json:"mean_omitted_context_items"`
	MeanFactCount                float64                        `json:"mean_fact_count"`
	MeanEvidenceCount            float64                        `json:"mean_evidence_count"`
	ErrorCount                   int                            `json:"error_count"`
}

type gatherContextRoleRecallScore struct {
	Role          string  `json:"role"`
	ExpectedCases int     `json:"expected_cases"`
	MatchedCases  int     `json:"matched_cases"`
	MissingCases  int     `json:"missing_cases"`
	Recall        float64 `json:"recall"`
}

type gatherContextBaselineComparison struct {
	Label                    string  `json:"label"`
	Role                     string  `json:"role,omitempty"`
	Provider                 string  `json:"provider,omitempty"`
	Model                    string  `json:"model,omitempty"`
	Runner                   string  `json:"runner,omitempty"`
	Count                    int     `json:"count"`
	BaselineErrorCount       int     `json:"baseline_error_count,omitempty"`
	GatherPassRate           float64 `json:"gather_pass_rate"`
	BaselinePassRate         float64 `json:"baseline_pass_rate"`
	PassRateDelta            float64 `json:"pass_rate_delta"`
	GatherMeanPathRecall     float64 `json:"gather_mean_path_recall"`
	BaselineMeanPathRecall   float64 `json:"baseline_mean_path_recall"`
	PathRecallDelta          float64 `json:"path_recall_delta"`
	GatherMeanFactRecall     float64 `json:"gather_mean_fact_recall"`
	BaselineMeanFactRecall   float64 `json:"baseline_mean_fact_recall"`
	FactRecallDelta          float64 `json:"fact_recall_delta"`
	GatherMeanDurationMS     float64 `json:"gather_mean_duration_ms"`
	BaselineMeanDurationMS   float64 `json:"baseline_mean_duration_ms"`
	DurationSpeedup          float64 `json:"duration_speedup,omitempty"`
	BaselineMeanTokens       float64 `json:"baseline_mean_tokens,omitempty"`
	BaselineMeanCachedInput  float64 `json:"baseline_mean_cached_input_tokens,omitempty"`
	BaselineMeanReasoningOut float64 `json:"baseline_mean_reasoning_output_tokens,omitempty"`
	BaselineMeanCostUSD      float64 `json:"baseline_mean_cost_usd,omitempty"`
	EmittedCharsPerTokenMean float64 `json:"emitted_chars_per_token_mean,omitempty"`
}

const (
	rlmAgentPlanModePlanner          = "planner"
	rlmAgentPlanModePlannerWithFacts = "planner-with-facts"
	rlmAgentPlanModeRerank           = "rerank"
)

func newEvalGatherContextCommand() *cobra.Command {
	var (
		workspace            string
		evalDatasetFile      string
		vaultPath            string
		timeout              time.Duration
		passThreshold        float64
		reportFile           string
		caseLimit            int
		limit                int
		maxContextChars      int
		lanes                []string
		toolProfile          string
		agentBaselineResults []string
		rlmAgentTargets      []string
		rlmAgentModels       []string
		rlmAgentProvider     string
		rlmAgentMaxIters     int
		rlmAgentRoute        string
		rlmAgentPlanMode     string
		baselineInputPrice   float64
		baselineOutputPrice  float64
	)

	cmd := &cobra.Command{
		Use:   "gather-context",
		Short: "Evaluate gather_context as a bounded repo-context controller",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			cfg, err := commandConfig(ctx)
			if err != nil {
				return writeOptimizeError(out, "eval.gather-context", err.Error())
			}
			absWorkspace, err := absWorkspaceOrWriteError(out, "eval.gather-context", workspace)
			if err != nil {
				return err
			}
			evalCases, err := loadPromptEvalCases(evalDatasetFile)
			if err != nil {
				return writeOptimizeError(out, "eval.gather-context", fmt.Sprintf("load eval dataset: %v", err))
			}
			if len(evalCases) == 0 {
				return writeOptimizeError(out, "eval.gather-context", "eval-dataset-file must contain at least one case")
			}
			loadedEvalCaseCount := len(evalCases)
			evalCases = limitGatherContextEvalCases(evalCases, caseLimit)

			prevPool := sqliteutil.GetGlobalPool()
			sharedPool := sqliteutil.NewPool()
			sqliteutil.SetGlobalPool(sharedPool)
			defer func() {
				sqliteutil.SetGlobalPool(prevPool)
				_ = sharedPool.Close()
			}()

			results := make([]gatherContextEvalResult, 0, len(evalCases))
			for _, evalCase := range evalCases {
				results = append(results, runSingleGatherContextEval(ctx, cfg, absWorkspace, strings.TrimSpace(vaultPath), evalCase, timeout, passThreshold, limit, maxContextChars, lanes, toolProfile))
			}
			summary := summarizeGatherContextEvalResults(results)
			var baselineResults []agentEvalResult
			var baselineSummaries []agentEvalSummary
			var baselineComparisons []gatherContextBaselineComparison
			if len(agentBaselineResults) > 0 {
				baselineResults, err = loadExternalAgentEvalResults(agentBaselineResults, evalCases, passThreshold)
				if err != nil {
					return writeOptimizeError(out, "eval.gather-context", fmt.Sprintf("load agent baseline results: %v", err))
				}
			}
			if len(rlmAgentTargets) > 0 || len(rlmAgentModels) > 0 {
				targets, err := resolveAgentEvalTargets(cfg, rlmAgentProvider, rlmAgentModels, rlmAgentTargets)
				if err != nil {
					return writeOptimizeError(out, "eval.gather-context", fmt.Sprintf("resolve rlm agent targets: %v", err))
				}
				for _, target := range targets {
					target.Runner = "rlm-llm"
					target.Label = "rlm-search-" + strings.TrimSpace(rlmAgentPlanMode) + "@" + target.Label
					for _, evalCase := range evalCases {
						result := runSingleRLMSearchAgentEval(ctx, cfg, absWorkspace, strings.TrimSpace(vaultPath), target, evalCase, timeout, rlmAgentMaxIters, passThreshold, limit, maxContextChars, lanes, toolProfile, rlmAgentRoute, rlmAgentPlanMode)
						result.Passed = shouldPassAgentEval(result, evalCase, passThreshold)
						baselineResults = append(baselineResults, result)
					}
				}
			}
			applyAgentEvalTokenPrice(baselineResults, baselineInputPrice, baselineOutputPrice)
			baselineSummaries = summarizeAgentEvalResults(baselineResults)
			baselineComparisons = compareGatherContextToAgentBaselines(summary, baselineSummaries)
			repoIndexFreshness := gatherContextEvalRepoIndexFreshness(ctx, cfg.Storage.Root, absWorkspace)
			report := map[string]any{
				"operation":               "eval.gather-context",
				"workspace_id":            absWorkspace,
				"repo_state":              gatherContextEvalRepoState(ctx, absWorkspace),
				"eval_case_count":         len(evalCases),
				"eval_dataset_case_count": loadedEvalCaseCount,
				"eval_case_limit":         caseLimit,
				"eval_cases":              evalCases,
				"tool_profile":            toolProfile,
				"lanes":                   lanes,
				"limit":                   limit,
				"max_context_chars":       maxContextChars,
				"pass_threshold":          passThreshold,
				"baseline_input_token_price_per_million_usd":  baselineInputPrice,
				"baseline_output_token_price_per_million_usd": baselineOutputPrice,
				"results":                     results,
				"summary":                     summary,
				"agent_baseline_result_files": append([]string(nil), agentBaselineResults...),
				"agent_baseline_results":      baselineResults,
				"agent_baseline_summaries":    baselineSummaries,
				"baseline_comparisons":        baselineComparisons,
				"rlm_agent_targets":           append([]string(nil), rlmAgentTargets...),
				"rlm_agent_models":            append([]string(nil), rlmAgentModels...),
				"rlm_agent_route":             rlmAgentRoute,
				"rlm_agent_plan_mode":         rlmAgentPlanMode,
				"cli_command":                 cmd.CommandPath(),
				"effective_contract":          map[string]any{"tool_name": "gather_context", "adapter_execution_path": "internal_adapter_bypass_v1"},
			}
			if repoIndexFreshness != nil {
				report["repoindex_freshness"] = repoIndexFreshness
			}
			if strings.TrimSpace(reportFile) != "" {
				payload, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return writeOptimizeError(out, "eval.gather-context", fmt.Sprintf("marshal report: %v", err))
				}
				if err := os.WriteFile(reportFile, append(payload, '\n'), 0o644); err != nil {
					return writeOptimizeError(out, "eval.gather-context", fmt.Sprintf("write report: %v", err))
				}
			}
			return protocol.WriteOK(out, "eval.gather-context", map[string]any{
				"markdown": renderGatherContextEvalMarkdown(absWorkspace, summary, results, baselineComparisons, repoIndexFreshness),
				"report":   report,
			}, protocol.WithSource("run"), protocol.WithWorkspace(absWorkspace))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root")
	cmd.Flags().StringVar(&evalDatasetFile, "eval-dataset-file", "", "JSONL eval dataset file with question/context/expected_paths rows")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Optional vault path for environment bootstrap")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Per-case timeout")
	cmd.Flags().Float64Var(&passThreshold, "pass-threshold", 0.8, "Path-recall threshold for passing cases with expected_paths")
	cmd.Flags().StringVar(&reportFile, "report-file", "", "Optional path to write the JSON report")
	cmd.Flags().IntVar(&caseLimit, "case-limit", 0, "Maximum eval dataset rows to execute (0 means all)")
	cmd.Flags().IntVar(&limit, "limit", 8, "Maximum evidence sources requested from gather_context")
	cmd.Flags().IntVar(&maxContextChars, "max-context-chars", 6000, "Maximum approximate context chars emitted by gather_context")
	cmd.Flags().StringSliceVar(&lanes, "lane", []string{"code"}, "Context lanes to gather (code, memory, context, task, mixed)")
	cmd.Flags().StringVar(&toolProfile, "tool-profile", rlmenv.ToolProfileGatherContext, "RLM tool profile used for environment bootstrap")
	cmd.Flags().StringSliceVar(&agentBaselineResults, "agent-baseline-results", nil, "Optional JSONL external/subagent baseline results to compare against gather_context (repeatable)")
	cmd.Flags().StringSliceVar(&rlmAgentTargets, "rlm-agent-target", nil, "Optional RLM mini-agent benchmark target in provider:model format (repeatable)")
	cmd.Flags().StringSliceVar(&rlmAgentModels, "rlm-agent-model", nil, "Optional RLM mini-agent model using --rlm-agent-provider (repeatable)")
	cmd.Flags().StringVar(&rlmAgentProvider, "rlm-agent-provider", "openrouter", "Default provider for --rlm-agent-model entries")
	cmd.Flags().IntVar(&rlmAgentMaxIters, "rlm-agent-max-iterations", 5, "Max tool/model iterations per RLM mini-agent pass")
	cmd.Flags().StringVar(&rlmAgentRoute, "rlm-agent-route", string(rlm.RouteProfileMixed), "Route profile for RLM mini-agent benchmark")
	cmd.Flags().StringVar(&rlmAgentPlanMode, "rlm-agent-plan-mode", string(rlm.PlanModeFree), "Plan mode for RLM mini-agent benchmark")
	cmd.Flags().Float64Var(&baselineInputPrice, "baseline-input-token-price-per-million-usd", 0, "Optional baseline input token price used for cost comparison when model pricing is unavailable")
	cmd.Flags().Float64Var(&baselineOutputPrice, "baseline-output-token-price-per-million-usd", 0, "Optional baseline output token price used for cost comparison when model pricing is unavailable")
	_ = cmd.MarkFlagRequired("eval-dataset-file")
	return cmd
}

func limitGatherContextEvalCases(cases []promptEvalCase, limit int) []promptEvalCase {
	return limitPromptEvalCases(cases, limit)
}

func gatherContextEvalRepoState(ctx context.Context, workspace string) map[string]any {
	state := map[string]any{
		"workspace": workspace,
	}
	if strings.TrimSpace(workspace) == "" {
		state["git_valid"] = false
		state["error"] = "empty workspace"
		return state
	}
	if out, err := gatherContextEvalGitOutput(ctx, workspace, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		state["git_valid"] = false
		if err != nil {
			state["error"] = err.Error()
		}
		return state
	}
	state["git_valid"] = true
	if out, err := gatherContextEvalGitOutput(ctx, workspace, "rev-parse", "HEAD"); err == nil {
		state["head_sha"] = strings.TrimSpace(out)
	}
	if out, err := gatherContextEvalGitOutput(ctx, workspace, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		state["branch"] = strings.TrimSpace(out)
	}
	if out, err := gatherContextEvalGitOutput(ctx, workspace, "remote", "get-url", "origin"); err == nil {
		state["origin_url"] = strings.TrimSpace(out)
	}
	if out, err := gatherContextEvalGitOutput(ctx, workspace, "status", "--short"); err == nil {
		status := strings.TrimSpace(out)
		state["worktree_dirty"] = status != ""
		if status != "" {
			state["status_short"] = strings.Split(status, "\n")
		}
	}
	return state
}

func gatherContextEvalRepoIndexFreshness(ctx context.Context, storageRoot, workspace string) map[string]any {
	if strings.TrimSpace(storageRoot) == "" || strings.TrimSpace(workspace) == "" {
		return nil
	}
	store, err := repoindex.Open(ctx, storageRoot, workspace)
	if err != nil {
		return nil
	}
	defer store.Close()

	meta, err := store.GetMeta(ctx)
	if err != nil {
		return nil
	}
	current := repoindex.ResolveGitSnapshot(ctx, workspace)
	freshness := repoindex.CompareIndexFreshness(meta, current)
	staleOrDirtyMismatch := freshness.Level == repoindex.FreshnessStale ||
		freshness.Level == repoindex.FreshnessDirty ||
		freshness.Level == repoindex.FreshnessUnknown ||
		meta.HeadSHA != current.HeadSHA ||
		meta.WorktreeDirty != current.WorktreeDirty ||
		(meta.DirtyStatusHash != "" && current.DirtyStatusHash != "" && meta.DirtyStatusHash != current.DirtyStatusHash)
	return map[string]any{
		"available":                 true,
		"index_head_sha":            meta.HeadSHA,
		"current_head_sha":          current.HeadSHA,
		"index_worktree_dirty":      meta.WorktreeDirty,
		"current_worktree_dirty":    current.WorktreeDirty,
		"index_dirty_status_hash":   meta.DirtyStatusHash,
		"current_dirty_status_hash": current.DirtyStatusHash,
		"stale_or_dirty_mismatch":   staleOrDirtyMismatch,
		"freshness":                 freshness,
		"indexed_at":                meta.IndexedAt,
	}
}

func gatherContextEvalGitOutput(ctx context.Context, workspace string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("%s: %s", err, text)
		}
		return "", err
	}
	return string(out), nil
}

func runSingleRLMSearchAgentEval(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	vaultPath string,
	target agentEvalTarget,
	evalCase promptEvalCase,
	timeout time.Duration,
	maxIterations int,
	passThreshold float64,
	limit int,
	maxContextChars int,
	lanes []string,
	toolProfile string,
	routeProfile string,
	planMode string,
) agentEvalResult {
	if strings.EqualFold(strings.TrimSpace(planMode), rlmAgentPlanModeRerank) {
		return runSingleRLMGatherRerankEval(ctx, cfg, workspace, vaultPath, target, evalCase, timeout, passThreshold, limit, maxContextChars, lanes, toolProfile)
	}
	if isRLMGatherPlannerMode(planMode) {
		return runSingleRLMGatherPlannerEval(ctx, cfg, workspace, vaultPath, target, evalCase, timeout, passThreshold, limit, maxContextChars, lanes, toolProfile, routeProfile, planMode)
	}
	result := agentEvalResult{
		CaseID:   strings.TrimSpace(evalCase.ID),
		Category: strings.TrimSpace(evalCase.Category),
		Role:     "rlm_search_agent",
		Label:    target.Label,
		Provider: target.Provider,
		Model:    target.Model,
		Runner:   target.Runner,
		Status:   "ok",
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	companionDB, companionClose, err := openRLMCompanionDB(runCtx, cfg)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	gatherPayload := buildRLMSearchAgentGatherPayload(evalCase, limit, maxContextChars, lanes)
	prompt := buildRLMSearchAgentEvalPrompt(evalCase, gatherPayload)
	task := rlm.Task{
		Prompt:        prompt,
		WorkspaceRoot: workspace,
		WorkspaceID:   workspace,
		MaxDepth:      0,
		MaxIterations: maxIterations,
		MaxSubcalls:   0,
		Metadata: map[string]any{
			"gather_context_payload": gatherPayload,
		},
	}
	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   vaultPath,
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

	adapter := rlmenv.NewReadOnlyAdapter(cfg, workspace, vaultPath, companionDB, env)
	adapter.SetContextEngineStore(bootstrapper.ContextEngineStore())
	adapter.SetTaskStore(bootstrapper.TaskStore())

	llmConfig := rlm.LLMConfig{
		Provider:       target.Provider,
		APIKey:         target.APIKey,
		BaseURL:        target.BaseURL,
		Model:          target.Model,
		Timeout:        timeout,
		MaxIterations:  maxIterations,
		RequireToolUse: len(env.Tools) > 0,
		RouteProfile:   rlm.NormalizeRouteProfile(routeProfile),
		PlanMode:       rlm.NormalizePlanMode(planMode),
		ToolProfile:    toolProfile,
	}
	var runner rlm.Runner
	if llmConfig.PlanMode == rlm.PlanModeLambda {
		runner = rlm.LambdaRunner{
			Config: rlm.LambdaConfig{LLM: llmConfig},
			Tools:  adapter,
		}
	} else {
		runner = rlm.LLMRunner{
			Config: llmConfig,
			Tools:  adapter,
		}
	}

	start := time.Now()
	rlmResult, err := runner.Run(runCtx, task, env)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	result.Output = strings.TrimSpace(rlmResult.Answer)
	result.ToolCallCount = evalIntFromAny(rlmResult.Metadata["tool_calls"])
	result.ToolNames = evalStringsFromAnySlice(rlmResult.Metadata["tool_names"])
	if len(result.ToolNames) == 0 {
		result.ToolNames = collectRLMPhaseToolNames(rlmResult.Metadata)
	}
	result.GatherSelectedPaths = evalStringsFromAnySlice(rlmResult.Metadata["gather_context_selected_paths"])
	result.GatherAnswerSeedPaths = evalStringsFromAnySlice(rlmResult.Metadata["gather_context_answer_seed_paths"])
	result.GatherPathSetMust = evalStringsFromAnySlice(rlmResult.Metadata["gather_context_path_set_must"])
	result.InputTokens = firstPositiveInt(
		evalIntFromAny(rlmResult.Metadata["parent_input_tokens_total"]),
		evalIntFromAny(rlmResult.Metadata["parent_input_tokens"]),
	)
	result.OutputTokens = firstPositiveInt(
		evalIntFromAny(rlmResult.Metadata["parent_output_tokens_total"]),
		evalIntFromAny(rlmResult.Metadata["parent_output_tokens"]),
	)
	result.TotalTokens = firstPositiveInt(
		evalIntFromAny(rlmResult.Metadata["parent_total_tokens_total"]),
		evalIntFromAny(rlmResult.Metadata["parent_total_tokens"]),
	)
	cost := obs.CalculateTokenCost(target.Model, result.InputTokens, result.OutputTokens)
	result.TotalCostUSD = cost.TotalCostUSD

	judgeOutput := result.Output
	structured, ok := parseStructuredAgentEvalOutput(result.Output)
	if !ok && llmConfig.PlanMode == rlm.PlanModeLambda && len(rlmResult.RetrievedPaths) > 0 {
		structured = structuredAgentEvalOutput{
			Summary:   "Lambda retrieval returned deterministic repo paths from gather_context.",
			Paths:     append([]string(nil), rlmResult.RetrievedPaths...),
			Rationale: "The lambda runner supplied retrieved paths directly when final answer text was not structured JSON.",
		}
		if body, err := json.Marshal(structured); err == nil {
			result.Output = string(body)
			judgeOutput = result.Output
			ok = true
		}
	}
	if hasCodeCorrectnessExpectations(evalCase) && !ok {
		result.Error = firstNonEmpty(result.Error, "expected structured JSON output with summary, paths, symbols, snippets, facts, and rationale")
	}
	if hasCodeCorrectnessExpectations(evalCase) && ok {
		validPaths, invalidPaths := validateRepoRelativePaths(workspace, structured.Paths)
		result.InvalidPaths = invalidPaths
		if len(invalidPaths) > 0 {
			result.Error = firstNonEmpty(result.Error, "invalid repo-relative paths in structured output")
		}
		judgeOutput = buildStructuredAgentJudgeText(structured)
		result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(validPaths, "\n"))
		result.PathMatchCount = len(result.MatchedPaths)
		result.Symbols = normalizeExpectedSymbols(structured.Symbols)
		result.Snippets = extractStructuredAgentSnippets(structured)
		result.MatchedSymbols, result.SymbolRecall = scoreExpectedSymbols(evalCase.ExpectedSymbols, result.Symbols)
		result.MatchedSnippets, result.SnippetRecall = scoreExpectedSnippets(workspace, evalCase.ExpectedSnippets, result.Snippets)
		result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, judgeOutput)
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
	}
	if hasCodeCorrectnessExpectations(evalCase) && !ok {
		result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, result.Output)
		result.PathMatchCount = len(result.MatchedPaths)
		result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, result.Output)
	}
	if hasCodeCorrectnessExpectations(evalCase) {
		surfacePaths := uniqueObservedPaths(append(append([]string{}, result.GatherAnswerSeedPaths...), result.GatherPathSetMust...))
		surfacePaths = uniqueObservedPaths(append(surfacePaths, result.GatherSelectedPaths...))
		finalPathText := result.Output
		if ok {
			finalPathText = strings.Join(structured.Paths, "\n")
		}
		result.FinalAnswerLosses = expectedPathsPresentThenMissing(evalCase.ExpectedPaths, strings.Join(surfacePaths, "\n"), finalPathText)
	}
	result.ExcludedPathHits, result.WrongScopePenalty = scoreExcludedPaths(evalCase.ExcludedPaths, result.Output)
	judgeResult := optimization.DefaultPromptJudge().Evaluate(optimization.PromptJudgeInput{
		Question:       evalCase.Question,
		Context:        evalCase.Context,
		TargetResponse: evalCase.TargetResponse,
		Output:         judgeOutput,
	})
	result.QualityScore = judgeResult.Score
	result.AccuracyScore = judgeResult.TargetSimilarity
	result.ThoroughnessScore = judgeResult.QuerySimilarity
	result.LengthQuality = judgeResult.LengthQuality
	result.GenericPenalty = judgeResult.GenericPenalty
	if hasCodeCorrectnessExpectations(evalCase) {
		result.QualityScore = blendedCodingQuality(result.QualityScore, result.PathRecall)
	}
	if result.WrongScopePenalty > 0 {
		result.QualityScore = clampEvalScore(result.QualityScore - result.WrongScopePenalty)
		result.CorrectnessScore = clampEvalScore(result.CorrectnessScore - result.WrongScopePenalty)
	}
	result.Passed = shouldPassAgentEval(result, evalCase, passThreshold)
	return result
}

type gatherContextPlannerOutput struct {
	Query                string                              `json:"query,omitempty"`
	Goal                 string                              `json:"goal,omitempty"`
	TaskType             string                              `json:"task_type,omitempty"`
	Lanes                []string                            `json:"lanes,omitempty"`
	SourceProfiles       []string                            `json:"source_profiles,omitempty"`
	RequiredEvidence     []string                            `json:"required_evidence,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	Languages            []string                            `json:"languages,omitempty"`
	PathPrefixes         []string                            `json:"path_prefixes,omitempty"`
	ExcludedPaths        []string                            `json:"excluded_paths,omitempty"`
	Limit                int                                 `json:"limit,omitempty"`
	MaxContextChars      int                                 `json:"max_context_chars,omitempty"`
	Rationale            string                              `json:"rationale,omitempty"`
}

type gatherContextPlannerNoopTools struct{}

func (gatherContextPlannerNoopTools) Execute(_ context.Context, name string, _ json.RawMessage) (map[string]any, error) {
	return nil, fmt.Errorf("planner mode does not expose tools; attempted %s", name)
}

func runSingleRLMGatherPlannerEval(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	vaultPath string,
	target agentEvalTarget,
	evalCase promptEvalCase,
	timeout time.Duration,
	passThreshold float64,
	limit int,
	maxContextChars int,
	lanes []string,
	toolProfile string,
	routeProfile string,
	planMode string,
) agentEvalResult {
	result := agentEvalResult{
		CaseID:   strings.TrimSpace(evalCase.ID),
		Category: strings.TrimSpace(evalCase.Category),
		Role:     "rlm_search_planner",
		Label:    target.Label,
		Provider: target.Provider,
		Model:    target.Model,
		Runner:   target.Runner,
		Status:   "ok",
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	includeFacts := strings.EqualFold(strings.TrimSpace(planMode), rlmAgentPlanModePlannerWithFacts)
	plannerTask := rlm.Task{
		Prompt:        buildRLMGatherPlannerEvalPrompt(evalCase, includeFacts),
		WorkspaceRoot: workspace,
		WorkspaceID:   workspace,
		MaxDepth:      0,
		MaxIterations: 1,
		MaxSubcalls:   0,
	}
	plannerRunner := rlm.LLMRunner{
		Config: rlm.LLMConfig{
			Provider:       target.Provider,
			APIKey:         target.APIKey,
			BaseURL:        target.BaseURL,
			Model:          target.Model,
			Timeout:        timeout,
			MaxIterations:  1,
			RequireToolUse: false,
			RouteProfile:   rlm.NormalizeRouteProfile(routeProfile),
			PlanMode:       rlm.PlanModeFree,
			ToolProfile:    string(rlm.ToolProfileLongCoTNoModelTools),
		},
		Tools: gatherContextPlannerNoopTools{},
	}

	start := time.Now()
	plannerResult, err := plannerRunner.Run(runCtx, plannerTask, rlm.Environment{})
	if err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Status = "error"
		result.Error = "planner: " + err.Error()
		return result
	}
	result.InputTokens = firstPositiveInt(
		evalIntFromAny(plannerResult.Metadata["parent_input_tokens_total"]),
		evalIntFromAny(plannerResult.Metadata["parent_input_tokens"]),
	)
	result.OutputTokens = firstPositiveInt(
		evalIntFromAny(plannerResult.Metadata["parent_output_tokens_total"]),
		evalIntFromAny(plannerResult.Metadata["parent_output_tokens"]),
	)
	result.TotalTokens = firstPositiveInt(
		evalIntFromAny(plannerResult.Metadata["parent_total_tokens_total"]),
		evalIntFromAny(plannerResult.Metadata["parent_total_tokens"]),
	)
	cost := obs.CalculateTokenCost(target.Model, result.InputTokens, result.OutputTokens)
	result.TotalCostUSD = cost.TotalCostUSD

	plan, ok := parseGatherContextPlannerOutput(plannerResult.Answer)
	if !ok {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Status = "error"
		result.Output = strings.TrimSpace(plannerResult.Answer)
		result.Error = "planner did not return valid gather_context planning JSON"
		return result
	}

	companionDB, companionClose, err := openRLMCompanionDB(runCtx, cfg)
	if err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	gatherTask := rlm.Task{
		Prompt:        buildCodeSearchEnsembleEvalQuery(evalCase),
		WorkspaceRoot: workspace,
		WorkspaceID:   workspace,
		MaxDepth:      0,
		MaxIterations: 1,
		MaxSubcalls:   0,
	}
	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   vaultPath,
		CompanionDB: companionDB,
	})
	env, err := bootstrapper.Build(runCtx, gatherTask)
	if err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	defer func() { _ = bootstrapper.Close() }()
	env.Tools = rlmenv.FilterTools(env.Tools, toolProfile)

	adapter := rlmenv.NewReadOnlyAdapter(cfg, workspace, vaultPath, companionDB, env)
	adapter.SetContextEngineStore(bootstrapper.ContextEngineStore())
	adapter.SetTaskStore(bootstrapper.TaskStore())

	gatherPayload := buildRLMSearchAgentGatherPayloadForPlanner(evalCase, plan, includeFacts, limit, maxContextChars, lanes)
	raw, err := json.Marshal(gatherPayload)
	if err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	out, err := adapter.ExecuteInternal(runCtx, "gather_context", raw)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "error"
		result.Error = "gather_context: " + err.Error()
		return result
	}
	bundle, err := decodeGatherContextBundle(out)
	if err != nil {
		result.Status = "error"
		result.Error = "decode gather_context bundle: " + err.Error()
		return result
	}

	result.ToolCallCount = 1
	result.ToolNames = []string{"gather_context"}
	result.GatherSelectedPaths = extractGatherContextSelectedPaths(bundle)
	result.GatherAnswerSeedPaths = extractGatherContextAnswerCandidatePaths(bundle)
	result.GatherPathSetMust = append([]string(nil), result.GatherSelectedPaths...)
	paths := extractGatherContextPaths(bundle)
	factBlob := buildGatherContextFactBlob(bundle)
	result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(paths, "\n"))
	result.PathMatchCount = len(result.MatchedPaths)
	result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, factBlob)
	result.CorrectnessScore = blendedCorrectnessScore(result.PathRecall, 0, 0, result.FactRecall, len(evalCase.ExpectedPaths) > 0, false, false, len(evalCase.RequiredFacts) > 0)
	structured := structuredAgentEvalOutput{
		Summary:   strings.TrimSpace(bundle.Summary),
		Paths:     paths,
		Facts:     gatherContextBundleFactStrings(bundle),
		Rationale: "Cheap planner produced gather_context retrieval intent; deterministic runtime executed and scored the resulting bundle.",
	}
	if body, err := json.Marshal(structured); err == nil {
		result.Output = string(body)
	}
	result.FinalAnswerLosses = expectedPathsPresentThenMissing(evalCase.ExpectedPaths, strings.Join(paths, "\n"), result.Output)
	result.ExcludedPathHits, result.WrongScopePenalty = scoreExcludedPaths(evalCase.ExcludedPaths, result.Output)
	result.Passed = shouldPassAgentEval(result, evalCase, passThreshold)
	return result
}

type gatherContextRerankCandidate struct {
	Path       string   `json:"path"`
	Rank       int      `json:"rank,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Coverage   []string `json:"coverage,omitempty"`
	Facts      []string `json:"facts,omitempty"`
}

type gatherContextRerankOutput struct {
	Summary         string   `json:"summary,omitempty"`
	Paths           []string `json:"paths,omitempty"`
	MissingCoverage []string `json:"missing_coverage,omitempty"`
	UncertainPaths  []string `json:"uncertain_paths,omitempty"`
	Rationale       string   `json:"rationale,omitempty"`
}

func runSingleRLMGatherRerankEval(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	vaultPath string,
	target agentEvalTarget,
	evalCase promptEvalCase,
	timeout time.Duration,
	passThreshold float64,
	limit int,
	maxContextChars int,
	lanes []string,
	toolProfile string,
) agentEvalResult {
	result := agentEvalResult{
		CaseID:   strings.TrimSpace(evalCase.ID),
		Category: strings.TrimSpace(evalCase.Category),
		Role:     "rlm_search_reranker",
		Label:    target.Label,
		Provider: target.Provider,
		Model:    target.Model,
		Runner:   target.Runner,
		Status:   "ok",
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	companionDB, companionClose, err := openRLMCompanionDB(runCtx, cfg)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	task := rlm.Task{
		Prompt:        buildCodeSearchEnsembleEvalQuery(evalCase),
		WorkspaceRoot: workspace,
		WorkspaceID:   workspace,
		MaxDepth:      0,
		MaxIterations: 1,
		MaxSubcalls:   0,
	}
	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   vaultPath,
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

	adapter := rlmenv.NewReadOnlyAdapter(cfg, workspace, vaultPath, companionDB, env)
	adapter.SetContextEngineStore(bootstrapper.ContextEngineStore())
	adapter.SetTaskStore(bootstrapper.TaskStore())

	payload := buildRLMSearchAgentGatherPayload(evalCase, rerankGatherLimit(limit), rerankGatherMaxContextChars(evalCase, maxContextChars), lanes)
	payload["response_mode"] = "full"
	raw, err := json.Marshal(payload)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	out, err := adapter.ExecuteInternal(runCtx, "gather_context", raw)
	if err != nil {
		result.Status = "error"
		result.Error = "gather_context: " + err.Error()
		return result
	}
	bundle, err := decodeGatherContextBundle(out)
	if err != nil {
		result.Status = "error"
		result.Error = "decode gather_context bundle: " + err.Error()
		return result
	}

	candidates := buildGatherContextRerankCandidates(bundle, 24)
	result.GatherSelectedPaths = extractGatherContextSelectedPaths(bundle)
	result.GatherAnswerSeedPaths = extractGatherContextAnswerCandidatePaths(bundle)
	result.GatherPathSetMust = append([]string(nil), result.GatherSelectedPaths...)
	result.ToolCallCount = 1
	result.ToolNames = []string{"gather_context", "rerank_model"}

	rerankPrompt := buildGatherContextRerankPrompt(evalCase, candidates, rerankGatherLimit(limit))
	llmCfg := engine.DefaultLLMChatConfig()
	llmCfg.Provider = strings.TrimSpace(target.Provider)
	llmCfg.APIKey = strings.TrimSpace(target.APIKey)
	llmCfg.BaseURL = strings.TrimSpace(target.BaseURL)
	llmCfg.Model = strings.TrimSpace(target.Model)
	llmCfg.Timeout = timeout
	llmCfg.MaxTokens = 1200
	llmCfg.Temperature = 0
	llm, err := engine.NewLLMChatEngine(llmCfg)
	if err != nil {
		result.Status = "error"
		result.Error = "rerank model: " + err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	modelOut, err := llm.Run(runCtx, engine.EngineInput{
		SystemPrompt: "You are a bounded repo evidence reranker. Return JSON only. Select only from candidate paths supplied by the runtime.",
		Messages:     []engine.Message{engine.NewUserMessage(rerankPrompt)},
		Workspace:    workspace,
		MaxTokens:    llmCfg.MaxTokens,
		Temperature:  0,
	})
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "error"
		result.Error = "rerank model: " + err.Error()
		return result
	}
	result.InputTokens = modelOut.Tokens.InputTokens
	result.OutputTokens = modelOut.Tokens.OutputTokens
	result.TotalTokens = modelOut.Tokens.TotalTokens
	cost := obs.CalculateTokenCost(target.Model, result.InputTokens, result.OutputTokens)
	result.TotalCostUSD = cost.TotalCostUSD

	rerank, ok := parseGatherContextRerankOutput(modelOut.AssistantText)
	if !ok {
		result.Status = "error"
		result.Output = strings.TrimSpace(modelOut.AssistantText)
		result.Error = "rerank model did not return valid JSON"
		return result
	}
	selectedPaths := filterRerankPathsToCandidates(rerank.Paths, candidates)
	facts := gatherContextBundleFactStrings(bundle)
	structured := structuredAgentEvalOutput{
		Summary: strings.TrimSpace(rerank.Summary),
		Paths:   selectedPaths,
		Facts:   facts,
		Rationale: strings.TrimSpace(firstNonEmpty(
			rerank.Rationale,
			"Gemini reranked a deterministic gather_context candidate set; runtime discarded paths not present in candidates.",
		)),
	}
	if structured.Summary == "" {
		structured.Summary = strings.TrimSpace(bundle.Summary)
	}
	body, _ := json.Marshal(structured)
	result.Output = string(body)

	result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(selectedPaths, "\n"))
	result.PathMatchCount = len(result.MatchedPaths)
	result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, buildStructuredAgentJudgeText(structured))
	result.CorrectnessScore = blendedCorrectnessScore(result.PathRecall, 0, 0, result.FactRecall, len(evalCase.ExpectedPaths) > 0, false, false, len(evalCase.RequiredFacts) > 0)
	result.FinalAnswerLosses = expectedPathsPresentThenMissing(evalCase.ExpectedPaths, strings.Join(result.GatherSelectedPaths, "\n"), strings.Join(selectedPaths, "\n"))
	result.ExcludedPathHits, result.WrongScopePenalty = scoreExcludedPaths(evalCase.ExcludedPaths, result.Output)
	result.Passed = shouldPassAgentEval(result, evalCase, passThreshold)
	return result
}

func buildRLMSearchAgentGatherPayload(evalCase promptEvalCase, limit int, maxContextChars int, lanes []string) map[string]any {
	return buildRLMSearchAgentGatherPayloadWithFacts(evalCase, limit, maxContextChars, lanes, true)
}

func buildRLMSearchAgentGatherPayloadWithFacts(evalCase promptEvalCase, limit int, maxContextChars int, lanes []string, includeFacts bool) map[string]any {
	payload := map[string]any{
		"query":             buildCodeSearchEnsembleEvalQuery(evalCase),
		"goal":              gatherContextEvalCaseString(evalCase, "goal", "repo_grounded_eval"),
		"task_type":         normalizeCodeSearchEvalTaskType(evalCase),
		"lanes":             gatherContextEvalCaseStringSlice(evalCase, "lanes", lanes),
		"limit":             limit,
		"max_context_chars": gatherContextEvalCaseInt(evalCase, "max_context_chars", maxContextChars),
		"response_mode":     "answer_surface",
	}
	if includeFacts && len(evalCase.RequiredFacts) > 0 {
		payload["required_evidence"] = append([]string(nil), evalCase.RequiredFacts...)
	}
	if statuses := gatherContextEvalCaseStringSlice(evalCase, "memory_statuses", nil); len(statuses) > 0 {
		payload["memory_statuses"] = statuses
	}
	if profiles := gatherContextEvalCaseStringSlice(evalCase, "source_profiles", nil); len(profiles) > 0 {
		payload["source_profiles"] = profiles
	}
	if languages := gatherContextEvalCaseStringSlice(evalCase, "languages", nil); len(languages) > 0 {
		payload["languages"] = languages
	}
	if prefixes := gatherContextEvalCaseStringSlice(evalCase, "path_prefixes", nil); len(prefixes) > 0 {
		payload["path_prefixes"] = prefixes
	}
	if excluded := gatherContextEvalExcludedPaths(evalCase); len(excluded) > 0 {
		payload["excluded_paths"] = excluded
	}
	return payload
}

func buildRLMSearchAgentGatherPayloadForPlanner(evalCase promptEvalCase, plan gatherContextPlannerOutput, includeFacts bool, limit int, maxContextChars int, lanes []string) map[string]any {
	payload := buildRLMSearchAgentGatherPayloadWithFacts(evalCase, limit, maxContextChars, lanes, includeFacts)
	payload["response_mode"] = "full"
	if value := strings.TrimSpace(plan.Query); value != "" {
		payload["query"] = value
	}
	if value := strings.TrimSpace(plan.Goal); value != "" {
		payload["goal"] = value
	}
	if value := strings.TrimSpace(plan.TaskType); value != "" {
		payload["task_type"] = value
	}
	if values := compactGatherContextEvalStrings(plan.Lanes); len(values) > 0 {
		payload["lanes"] = values
	}
	if values := compactGatherContextEvalStrings(plan.SourceProfiles); len(values) > 0 {
		payload["source_profiles"] = values
	}
	if values := compactGatherContextEvalStrings(plan.RequiredEvidence); len(values) > 0 {
		payload["required_evidence"] = values
	}
	if len(plan.CoverageRequirements) > 0 {
		payload["coverage_requirements"] = plan.CoverageRequirements
	}
	if values := compactGatherContextEvalStrings(plan.Languages); len(values) > 0 {
		payload["languages"] = values
	}
	if values := compactGatherContextEvalStrings(plan.PathPrefixes); len(values) > 0 {
		payload["path_prefixes"] = values
	}
	if values := compactGatherContextEvalStrings(plan.ExcludedPaths); len(values) > 0 {
		payload["excluded_paths"] = values
	}
	if plan.Limit > 0 && plan.Limit <= 50 {
		payload["limit"] = plan.Limit
	}
	if plan.MaxContextChars > 0 && plan.MaxContextChars <= 50000 {
		payload["max_context_chars"] = plan.MaxContextChars
	}
	return payload
}

func isRLMGatherPlannerMode(planMode string) bool {
	switch strings.ToLower(strings.TrimSpace(planMode)) {
	case rlmAgentPlanModePlanner, rlmAgentPlanModePlannerWithFacts:
		return true
	default:
		return false
	}
}

func buildRLMGatherPlannerEvalPrompt(evalCase promptEvalCase, includeFacts bool) string {
	var b strings.Builder
	b.WriteString("Plan a deterministic gather_context retrieval request for a repo-grounded coding question.\n")
	b.WriteString("Do not answer the question. Do not output final repo paths. Return exactly one JSON object.\n")
	b.WriteString("The runtime will execute your plan and score only runtime evidence, so your job is to describe retrieval intent: task_type, source_profiles, required_evidence, and coverage_requirements.\n")
	b.WriteString("Allowed task_type values: file_locate, symbol_inspect, execution_trace, change_impact, registration_trace, architecture_map, subsystem_map, integration_surface.\n")
	b.WriteString("Useful source_profiles: repo_code, repo_docs, codemaps, cochange_history, memory, task, session, vault_docs.\n")
	b.WriteString("Useful lanes: code, memory, context, task.\n")
	b.WriteString("JSON shape:\n")
	b.WriteString(`{"query":"...","goal":"repo_grounded_eval","task_type":"file_locate","lanes":["code"],"source_profiles":["repo_code"],"required_evidence":["short concept or symbol"],"coverage_requirements":[{"id":"stable_id","kind":"subsystem_role","label":"role label","terms":["term","symbol"],"required":true,"min_paths":1,"weight":1,"source_profiles":["repo_code"]}],"languages":[],"path_prefixes":[],"excluded_paths":[],"limit":8,"max_context_chars":6000,"rationale":"why these coverage slots are needed"}`)
	b.WriteByte('\n')
	if strings.TrimSpace(evalCase.TaskType) != "" {
		b.WriteString("Known task_type hint: " + strings.TrimSpace(evalCase.TaskType) + "\n")
	}
	if profiles := gatherContextEvalCaseStringSlice(evalCase, "source_profiles", nil); len(profiles) > 0 {
		b.WriteString("Known source profile hint: " + strings.Join(profiles, ", ") + "\n")
	}
	if languages := gatherContextEvalCaseStringSlice(evalCase, "languages", nil); len(languages) > 0 {
		b.WriteString("Known language hint: " + strings.Join(languages, ", ") + "\n")
	}
	if prefixes := gatherContextEvalCaseStringSlice(evalCase, "path_prefixes", nil); len(prefixes) > 0 {
		b.WriteString("Known path prefix hint: " + strings.Join(prefixes, ", ") + "\n")
	}
	if excluded := gatherContextEvalExcludedPaths(evalCase); len(excluded) > 0 {
		b.WriteString("Known excluded path hint: " + strings.Join(excluded, ", ") + "\n")
	}
	if includeFacts && len(evalCase.RequiredFacts) > 0 {
		b.WriteString("Known answer coverage hints:\n")
		for _, fact := range evalCase.RequiredFacts {
			if fact = strings.TrimSpace(fact); fact != "" {
				b.WriteString("- " + fact + "\n")
			}
		}
	}
	if strings.TrimSpace(evalCase.Context) != "" {
		b.WriteString("\nContext:\n" + strings.TrimSpace(evalCase.Context) + "\n")
	}
	b.WriteString("\nQuestion:\n" + strings.TrimSpace(evalCase.Question) + "\n")
	return b.String()
}

func parseGatherContextPlannerOutput(raw string) (gatherContextPlannerOutput, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return gatherContextPlannerOutput{}, false
	}
	var out gatherContextPlannerOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil && gatherContextPlannerOutputUsable(out) {
		return out, true
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end <= start {
		return gatherContextPlannerOutput{}, false
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil && gatherContextPlannerOutputUsable(out) {
		return out, true
	}
	return gatherContextPlannerOutput{}, false
}

func gatherContextPlannerOutputUsable(out gatherContextPlannerOutput) bool {
	return strings.TrimSpace(out.Query) != "" ||
		strings.TrimSpace(out.TaskType) != "" ||
		len(out.RequiredEvidence) > 0 ||
		len(out.CoverageRequirements) > 0 ||
		len(out.SourceProfiles) > 0 ||
		len(out.Lanes) > 0
}

func gatherContextBundleFactStrings(bundle contextengine.ContextBundle) []string {
	out := make([]string, 0, len(bundle.Facts))
	for _, fact := range bundle.Facts {
		if value := strings.TrimSpace(fact.Fact); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func rerankGatherLimit(limit int) int {
	if limit <= 0 {
		limit = 8
	}
	expanded := limit * 3
	if expanded < limit {
		expanded = limit
	}
	if expanded > 24 {
		return 24
	}
	return expanded
}

func rerankGatherMaxContextChars(evalCase promptEvalCase, fallback int) int {
	base := gatherContextEvalCaseInt(evalCase, "max_context_chars", fallback)
	if base <= 0 {
		base = 6000
	}
	expanded := base * 2
	if expanded > 24000 {
		return 24000
	}
	return expanded
}

func buildGatherContextRerankCandidates(bundle contextengine.ContextBundle, maxCandidates int) []gatherContextRerankCandidate {
	factsByEvidence := map[string][]string{}
	for _, fact := range bundle.Facts {
		text := truncateEvalText(strings.TrimSpace(fact.Fact), 260)
		if text == "" {
			continue
		}
		for _, id := range fact.EvidenceIDs {
			factsByEvidence[id] = append(factsByEvidence[id], text)
		}
	}
	out := make([]gatherContextRerankCandidate, 0, len(bundle.SelectedPaths))
	seen := map[string]struct{}{}
	for _, selected := range bundle.SelectedPaths {
		path := normalizeGatherContextPath(selected.Path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		facts := make([]string, 0, 2)
		factSeen := map[string]struct{}{}
		for _, id := range selected.EvidenceIDs {
			for _, fact := range factsByEvidence[id] {
				if _, ok := factSeen[fact]; ok {
					continue
				}
				factSeen[fact] = struct{}{}
				facts = append(facts, fact)
				if len(facts) >= 2 {
					break
				}
			}
			if len(facts) >= 2 {
				break
			}
		}
		out = append(out, gatherContextRerankCandidate{
			Path:       path,
			Rank:       selected.Rank,
			Confidence: selected.Confidence,
			Reason:     truncateEvalText(selected.Reason, 220),
			Coverage:   append([]string(nil), selected.CoverageIDs...),
			Facts:      facts,
		})
		if maxCandidates > 0 && len(out) >= maxCandidates {
			break
		}
	}
	return out
}

func buildGatherContextRerankPrompt(evalCase promptEvalCase, candidates []gatherContextRerankCandidate, maxPaths int) string {
	payload := map[string]any{
		"question":        strings.TrimSpace(evalCase.Question),
		"context":         strings.TrimSpace(evalCase.Context),
		"task_type":       normalizeCodeSearchEvalTaskType(evalCase),
		"required_facts":  append([]string(nil), evalCase.RequiredFacts...),
		"candidate_paths": candidates,
		"max_paths":       maxPaths,
	}
	if maxPaths <= 0 {
		payload["max_paths"] = len(candidates)
	}
	body, _ := json.MarshalIndent(payload, "", "  ")
	var b strings.Builder
	b.WriteString("Rerank this runtime-produced repo evidence candidate set.\n")
	b.WriteString("Select only paths from candidate_paths. Do not invent, rename, shorten, or add paths.\n")
	b.WriteString("Prefer the smallest set that covers the question and required facts; keep supporting files only when they are needed for coverage.\n")
	b.WriteString("Return JSON only with this exact shape:\n")
	b.WriteString(`{"summary":"...","paths":["repo/relative/path.go"],"missing_coverage":["..."],"uncertain_paths":["..."],"rationale":"..."}`)
	b.WriteString("\n\nInput:\n")
	b.Write(body)
	b.WriteByte('\n')
	return b.String()
}

func parseGatherContextRerankOutput(raw string) (gatherContextRerankOutput, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return gatherContextRerankOutput{}, false
	}
	var out gatherContextRerankOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out, true
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end <= start {
		return gatherContextRerankOutput{}, false
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
		return out, true
	}
	return gatherContextRerankOutput{}, false
}

func filterRerankPathsToCandidates(paths []string, candidates []gatherContextRerankCandidate) []string {
	allowed := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		path := normalizeGatherContextPath(candidate.Path)
		if path != "" {
			allowed[strings.ToLower(path)] = path
		}
	}
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		key := strings.ToLower(normalizeGatherContextPath(path))
		canonical := allowed[key]
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out
}

func truncateEvalText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return strings.TrimSpace(value[:max-3]) + "..."
}

func buildRLMSearchAgentEvalPrompt(evalCase promptEvalCase, gatherPayload map[string]any) string {
	var b strings.Builder
	b.WriteString("Answer this repo-grounded evaluation using the available RLM retrieval tools.\n")
	b.WriteString("Your first tool call must be gather_context with exactly this JSON payload:\n")
	if payloadJSON, err := json.MarshalIndent(gatherPayload, "", "  "); err == nil {
		b.Write(payloadJSON)
		b.WriteByte('\n')
	}
	b.WriteString("Copy answer_seed.paths and answer_seed.facts exactly when present; they are the runtime-selected answer seed. Use path_set.must for supporting path details. Use load_evidence_ref or retrieve_code only to disprove or verify specific evidence from the bundle, not to narrow the answer to one loaded file.\n")
	b.WriteString("Return JSON only with this exact shape:\n")
	b.WriteString("{\"summary\":\"...\",\"paths\":[\"repo/relative/path.go\"],\"symbols\":[\"path::SymbolName\"],\"snippets\":[{\"path\":\"repo/relative/path.go\",\"start_line\":1,\"end_line\":20,\"reason\":\"...\"}],\"facts\":[\"...\"],\"rationale\":\"...\"}\n")
	b.WriteString("Every path must be a real repo-relative file path copied from tool evidence. Omit unverified paths.\n")
	if len(evalCase.ExcludedPaths) > 0 {
		b.WriteString("Treat these paths or prefixes as out of scope unless the question explicitly asks for them: " + strings.Join(evalCase.ExcludedPaths, ", ") + "\n")
	}
	if strings.TrimSpace(evalCase.Context) != "" {
		b.WriteString("\nContext:\n" + strings.TrimSpace(evalCase.Context) + "\n")
	}
	b.WriteString("\nQuestion:\n" + strings.TrimSpace(evalCase.Question))
	return b.String()
}

func collectRLMPhaseToolNames(metadata map[string]any) []string {
	if metadata == nil {
		return nil
	}
	phases, ok := metadata["phases"].([]map[string]any)
	if !ok {
		rawPhases, ok := metadata["phases"].([]any)
		if !ok {
			return nil
		}
		out := make([]string, 0)
		seen := map[string]struct{}{}
		for _, raw := range rawPhases {
			phase, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, name := range evalStringsFromAnySlice(phase["tool_names"]) {
				if _, exists := seen[name]; exists {
					continue
				}
				seen[name] = struct{}{}
				out = append(out, name)
			}
		}
		return out
	}
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, phase := range phases {
		for _, name := range evalStringsFromAnySlice(phase["tool_names"]) {
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

func evalStringsFromAnySlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compactGatherContextEvalStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return compactGatherContextEvalStrings(out)
	default:
		return nil
	}
}

func evalIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func runSingleGatherContextEval(
	ctx context.Context,
	cfg config.Config,
	workspace string,
	vaultPath string,
	evalCase promptEvalCase,
	timeout time.Duration,
	passThreshold float64,
	limit int,
	maxContextChars int,
	lanes []string,
	toolProfile string,
) gatherContextEvalResult {
	result := gatherContextEvalResult{
		CaseID:   strings.TrimSpace(evalCase.ID),
		Category: strings.TrimSpace(evalCase.Category),
		TaskType: normalizeCodeSearchEvalTaskType(evalCase),
		Status:   "ok",
	}
	effectiveLanes := gatherContextEvalCaseStringSlice(evalCase, "lanes", lanes)
	effectiveMemoryStatuses := gatherContextEvalCaseStringSlice(evalCase, "memory_statuses", nil)
	result.Lanes = append([]string(nil), effectiveLanes...)
	result.MemoryStatuses = append([]string(nil), effectiveMemoryStatuses...)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	companionDB, companionClose, err := openRLMCompanionDB(runCtx, cfg)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	if companionClose != nil {
		defer func() { _ = companionClose() }()
	}

	task := rlm.Task{
		Prompt:        buildCodeSearchEnsembleEvalQuery(evalCase),
		WorkspaceRoot: workspace,
		MaxDepth:      0,
		MaxIterations: 1,
		MaxSubcalls:   0,
	}
	bootstrapper := rlmenv.NewBootstrapper(rlmenv.BootstrapConfig{
		AppConfig:   cfg,
		VaultPath:   vaultPath,
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

	adapter := rlmenv.NewReadOnlyAdapter(cfg, workspace, vaultPath, companionDB, env)
	adapter.SetContextEngineStore(bootstrapper.ContextEngineStore())
	adapter.SetTaskStore(bootstrapper.TaskStore())

	payload := map[string]any{
		"query":             buildCodeSearchEnsembleEvalQuery(evalCase),
		"goal":              gatherContextEvalCaseString(evalCase, "goal", "repo_grounded_eval"),
		"task_type":         result.TaskType,
		"lanes":             effectiveLanes,
		"limit":             limit,
		"max_context_chars": gatherContextEvalCaseInt(evalCase, "max_context_chars", maxContextChars),
		"response_mode":     "full",
	}
	if len(evalCase.RequiredFacts) > 0 {
		payload["required_evidence"] = evalCase.RequiredFacts
	}
	if profiles := gatherContextEvalCaseStringSlice(evalCase, "source_profiles", nil); len(profiles) > 0 {
		payload["source_profiles"] = profiles
	}
	if languages := gatherContextEvalCaseStringSlice(evalCase, "languages", nil); len(languages) > 0 {
		payload["languages"] = languages
	}
	if prefixes := gatherContextEvalCaseStringSlice(evalCase, "path_prefixes", nil); len(prefixes) > 0 {
		payload["path_prefixes"] = prefixes
	}
	if excluded := gatherContextEvalExcludedPaths(evalCase); len(excluded) > 0 {
		payload["excluded_paths"] = excluded
	}
	if len(effectiveMemoryStatuses) > 0 {
		payload["memory_statuses"] = effectiveMemoryStatuses
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	start := time.Now()
	out, err := adapter.ExecuteInternal(runCtx, "gather_context", raw)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	bundle, err := decodeGatherContextBundle(out)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	result.BundleStatus = string(bundle.Status)
	result.Answerable = bundle.Answerable
	if bundle.Certificate != nil {
		result.CertificateStatus = string(bundle.Certificate.Status)
	}
	result.ExpectedPathsMissingOnDisk = gatherContextExpectedPathsMissingOnDisk(workspace, evalCase.ExpectedPaths)
	result.StaleEval = len(result.ExpectedPathsMissingOnDisk) > 0
	result.Paths = extractGatherContextPaths(bundle)
	result.RawEvidencePaths = extractGatherContextEvidencePaths(bundle)
	result.SelectedPaths = extractGatherContextSelectedPaths(bundle)
	result.AnswerCandidatePaths = extractGatherContextAnswerCandidatePaths(bundle)
	result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(result.Paths, "\n"))
	result.ExpectedRoles = gatherContextEvalExpectedRoles(evalCase)
	result.RoleCoverage = gatherContextPathRoleCoverage(result.Paths)
	result.MatchedRoles, result.RoleRecall = scoreGatherContextExpectedRoles(result.ExpectedRoles, result.RoleCoverage)
	if bundle.Metadata != nil {
		result.ProviderTelemetry = bundle.Metadata["code_search_provider_telemetry"]
	}
	result.ProviderCandidatePaths = extractGatherContextProviderCandidatePaths(result.ProviderTelemetry)
	result.ProviderSeenPaths = extractGatherContextProviderSeenPaths(result.ProviderTelemetry)
	result.ProviderMisses = missingExpectedPaths(evalCase.ExpectedPaths, strings.Join(result.ProviderCandidatePaths, "\n"))
	if len(result.ProviderCandidatePaths) > 0 {
		result.RetrievalMisses = result.ProviderMisses
	} else {
		result.RetrievalMisses = missingExpectedPaths(evalCase.ExpectedPaths, strings.Join(result.RawEvidencePaths, "\n"))
	}
	result.BundleEvidenceMisses = expectedPathsPresentThenMissing(evalCase.ExpectedPaths, strings.Join(result.ProviderCandidatePaths, "\n"), strings.Join(result.RawEvidencePaths, "\n"))
	reductionInputPaths := result.RawEvidencePaths
	if len(result.ProviderCandidatePaths) > 0 {
		reductionInputPaths = result.ProviderCandidatePaths
	}
	result.ReductionMisses = expectedPathsPresentThenMissing(evalCase.ExpectedPaths, strings.Join(reductionInputPaths, "\n"), strings.Join(result.SelectedPaths, "\n"))
	result.MatchedFacts, result.FactRecall = scoreRequiredFacts(evalCase.RequiredFacts, buildGatherContextFactBlob(bundle))
	result.SourceCoverage = bundle.SourceCoverage
	result.RawContextChars = bundle.Telemetry.RawContextChars
	result.EmittedContextChars = bundle.Telemetry.EmittedContextChars
	result.OmittedContextItems = bundle.Telemetry.OmittedContextItems
	result.FactCount = len(bundle.Facts)
	result.EvidenceCount = len(bundle.Evidence)
	result.Gaps = extractContextGapReasons(bundle.Missing)
	result.Passed = gatherContextEvalPassed(result, evalCase, passThreshold)
	return result
}

func decodeGatherContextBundle(raw map[string]any) (contextengine.ContextBundle, error) {
	body, err := json.Marshal(raw)
	if err != nil {
		return contextengine.ContextBundle{}, err
	}
	var bundle contextengine.ContextBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return contextengine.ContextBundle{}, err
	}
	return bundle, bundle.Validate()
}

func extractGatherContextPaths(bundle contextengine.ContextBundle) []string {
	paths := make([]string, 0, len(bundle.Evidence))
	for _, selected := range bundle.SelectedPaths {
		paths = append(paths, normalizeGatherContextPath(selected.Path))
	}
	for _, node := range bundle.Evidence {
		if node.Ref.Type == contextengine.RefTypePath {
			paths = append(paths, normalizeGatherContextPath(node.Ref.Ref))
		}
		if path, ok := node.Metadata["path"].(string); ok {
			paths = append(paths, normalizeGatherContextPath(path))
		}
	}
	return uniqueObservedPaths(paths)
}

func extractGatherContextEvidencePaths(bundle contextengine.ContextBundle) []string {
	paths := make([]string, 0, len(bundle.Evidence))
	for _, node := range bundle.Evidence {
		if node.Ref.Type == contextengine.RefTypePath {
			paths = append(paths, normalizeGatherContextPath(node.Ref.Ref))
		}
		if path, ok := node.Metadata["path"].(string); ok {
			paths = append(paths, normalizeGatherContextPath(path))
		}
	}
	return uniqueObservedPaths(paths)
}

func extractGatherContextSelectedPaths(bundle contextengine.ContextBundle) []string {
	paths := make([]string, 0, len(bundle.SelectedPaths))
	for _, selected := range bundle.SelectedPaths {
		paths = append(paths, normalizeGatherContextPath(selected.Path))
	}
	return uniqueObservedPaths(paths)
}

func extractGatherContextAnswerCandidatePaths(bundle contextengine.ContextBundle) []string {
	paths := make([]string, 0, len(bundle.AnswerCandidates))
	for _, candidate := range bundle.AnswerCandidates {
		if !strings.EqualFold(strings.TrimSpace(candidate.Kind), "path") {
			continue
		}
		paths = append(paths, normalizeGatherContextPath(candidate.Value))
	}
	return uniqueObservedPaths(paths)
}

func extractGatherContextProviderCandidatePaths(value any) []string {
	paths := make([]string, 0)
	groups, ok := value.([]any)
	if !ok {
		return nil
	}
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			continue
		}
		paths = append(paths, normalizeGatherContextProviderPaths(groupMap["merged_paths"])...)
	}
	return uniqueObservedPaths(paths)
}

func extractGatherContextProviderSeenPaths(value any) []string {
	paths := make([]string, 0)
	for _, entry := range gatherContextProviderTelemetryEntries(value) {
		paths = append(paths, normalizeGatherContextProviderPaths(entry["paths"])...)
	}
	return uniqueObservedPaths(paths)
}

func normalizeGatherContextProviderPaths(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, path := range typed {
			out = append(out, normalizeGatherContextPath(path))
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if path, ok := item.(string); ok {
				out = append(out, normalizeGatherContextPath(path))
			}
		}
		return out
	default:
		return nil
	}
}

func missingExpectedPaths(expected []string, output string) []string {
	normalized := normalizeExpectedPaths(expected)
	if len(normalized) == 0 {
		return nil
	}
	matched, _ := scoreExpectedPaths(normalized, output)
	matchedSet := make(map[string]struct{}, len(matched))
	for _, path := range matched {
		matchedSet[path] = struct{}{}
	}
	missing := make([]string, 0, len(normalized))
	for _, path := range normalized {
		if _, ok := matchedSet[path]; ok {
			continue
		}
		missing = append(missing, path)
	}
	return missing
}

func expectedPathsPresentThenMissing(expected []string, presentOutput string, missingOutput string) []string {
	present, _ := scoreExpectedPaths(expected, presentOutput)
	if len(present) == 0 {
		return nil
	}
	missing := missingExpectedPaths(present, missingOutput)
	if len(missing) == 0 {
		return nil
	}
	return missing
}

func gatherContextExpectedPathsMissingOnDisk(workspace string, expected []string) []string {
	if len(expected) == 0 || strings.TrimSpace(workspace) == "" {
		return nil
	}
	out := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range normalizeExpectedPaths(expected) {
		pathValue := strings.TrimSpace(item)
		if pathValue == "" {
			continue
		}
		absPath := pathValue
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(workspace, filepath.FromSlash(pathValue))
		}
		if _, err := os.Stat(absPath); err == nil {
			continue
		}
		if _, ok := seen[pathValue]; ok {
			continue
		}
		seen[pathValue] = struct{}{}
		out = append(out, pathValue)
	}
	return out
}

func normalizeGatherContextPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	return path
}

func extractContextGapReasons(gaps []contextengine.ContextGap) []string {
	out := make([]string, 0, len(gaps))
	for _, gap := range gaps {
		item := strings.TrimSpace(gap.Required)
		if strings.TrimSpace(gap.Reason) != "" {
			item += ": " + strings.TrimSpace(gap.Reason)
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func buildGatherContextFactBlob(bundle contextengine.ContextBundle) string {
	var b strings.Builder
	b.WriteString(bundle.Summary)
	for _, fact := range bundle.Facts {
		b.WriteByte('\n')
		b.WriteString(fact.Fact)
		for _, ref := range fact.Refs {
			b.WriteByte('\n')
			b.WriteString(string(ref.Type))
			b.WriteByte(':')
			b.WriteString(ref.Ref)
			if ref.Title != "" {
				b.WriteByte(' ')
				b.WriteString(ref.Title)
			}
			if ref.Excerpt != "" {
				b.WriteByte(' ')
				b.WriteString(ref.Excerpt)
			}
		}
	}
	for _, selected := range bundle.SelectedPaths {
		b.WriteByte('\n')
		b.WriteString(selected.Path)
		b.WriteByte('\n')
		b.WriteString(selected.Reason)
	}
	for _, candidate := range bundle.AnswerCandidates {
		b.WriteByte('\n')
		b.WriteString(candidate.Kind)
		b.WriteByte(':')
		b.WriteString(candidate.Value)
		b.WriteByte('\n')
		b.WriteString(candidate.Reason)
	}
	for _, node := range bundle.Evidence {
		b.WriteByte('\n')
		b.WriteString(node.Statement)
		b.WriteByte('\n')
		b.WriteString(string(node.Ref.Type))
		b.WriteByte(':')
		b.WriteString(node.Ref.Ref)
		if node.Ref.Title != "" {
			b.WriteByte(' ')
			b.WriteString(node.Ref.Title)
		}
		if node.Ref.Excerpt != "" {
			b.WriteByte(' ')
			b.WriteString(node.Ref.Excerpt)
		}
		if path, ok := node.Metadata["path"].(string); ok {
			b.WriteByte('\n')
			b.WriteString(path)
		}
	}
	return b.String()
}

func gatherContextEvalPassed(result gatherContextEvalResult, evalCase promptEvalCase, passThreshold float64) bool {
	if result.Status != "ok" {
		return false
	}
	if result.StaleEval {
		return false
	}
	pathOK := true
	if len(evalCase.ExpectedPaths) > 0 {
		pathOK = result.PathRecall >= passThreshold
	}
	factOK := true
	if len(evalCase.RequiredFacts) > 0 {
		factOK = result.FactRecall >= passThreshold
	}
	if len(evalCase.ExpectedPaths) == 0 && len(evalCase.RequiredFacts) == 0 {
		return result.Answerable && result.EvidenceCount > 0
	}
	return pathOK && factOK && result.Answerable
}

func gatherContextEvalCaseString(evalCase promptEvalCase, key string, fallback string) string {
	if evalCase.Metadata == nil {
		return fallback
	}
	value, ok := evalCase.Metadata[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func gatherContextEvalCaseInt(evalCase promptEvalCase, key string, fallback int) int {
	if evalCase.Metadata == nil {
		return fallback
	}
	switch value := evalCase.Metadata[key].(type) {
	case float64:
		if value > 0 {
			return int(value)
		}
	case int:
		if value > 0 {
			return value
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &parsed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func gatherContextEvalCaseStringSlice(evalCase promptEvalCase, key string, fallback []string) []string {
	if evalCase.Metadata == nil {
		return append([]string(nil), fallback...)
	}
	value, ok := evalCase.Metadata[key]
	if !ok {
		return append([]string(nil), fallback...)
	}
	out := normalizeGatherContextEvalStringSlice(value)
	if len(out) == 0 {
		return append([]string(nil), fallback...)
	}
	return out
}

func gatherContextEvalExcludedPaths(evalCase promptEvalCase) []string {
	out := compactGatherContextEvalStrings(evalCase.ExcludedPaths)
	out = append(out, gatherContextEvalCaseStringSlice(evalCase, "excluded_paths", nil)...)
	return compactGatherContextEvalStrings(out)
}

func gatherContextEvalExpectedRoles(evalCase promptEvalCase) []string {
	out := gatherContextEvalCaseStringSlice(evalCase, "expected_roles", nil)
	out = append(out, gatherContextEvalCaseStringSlice(evalCase, "expected_file_roles", nil)...)
	out = append(out, gatherContextEvalCaseStringSlice(evalCase, "role_requirements", nil)...)
	return normalizeGatherContextEvalRoles(out)
}

func normalizeGatherContextEvalRoles(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		role := normalizeGatherContextEvalRole(value)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}
	return out
}

func normalizeGatherContextEvalRole(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "doc", "docs", "documentation", "readme", "design_doc", "migration_doc":
		return "docs"
	case "route", "routes", "route_file", "layout", "layout_file", "screen", "screen_component", "page":
		return "route"
	case "router", "routing":
		return "router"
	case "controller", "controllers", "handler", "action":
		return "controller"
	case "service", "domain", "repository", "repo", "model":
		return "domain"
	case "api", "api_adapter", "client", "adapter":
		return "api_adapter"
	case "config", "configuration", "deploy", "deployment", "deployment_config", "helm", "values", "manifest":
		return "deploy_config"
	case "test", "tests", "spec", "specs", "test_companion":
		return "test"
	case "data", "fixture", "fixtures", "schema", "migration":
		return "data"
	case "worker", "job", "task":
		return "worker"
	case "tooling", "template", "generated", "vendor":
		return value
	default:
		return value
	}
}

func gatherContextPathRoleCoverage(paths []string) map[string]int {
	coverage := map[string]int{}
	for _, path := range paths {
		for _, role := range inferGatherContextPathRoles(path) {
			coverage[role]++
		}
	}
	if len(coverage) == 0 {
		return nil
	}
	return coverage
}

func inferGatherContextPathRoles(path string) []string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return nil
	}
	lower := strings.ToLower(path)
	base := strings.ToLower(filepath.Base(lower))
	ext := strings.ToLower(filepath.Ext(lower))
	roles := make([]string, 0, 4)
	add := func(role string) {
		role = normalizeGatherContextEvalRole(role)
		if role == "" {
			return
		}
		for _, existing := range roles {
			if existing == role {
				return
			}
		}
		roles = append(roles, role)
	}
	if gatherContextPathHasToolingSegment(lower) {
		add("tooling")
	}
	if strings.Contains(lower, "/generated/") || strings.Contains(lower, "/gen/") || strings.Contains(lower, ".generated.") || strings.Contains(lower, "_generated.") || strings.HasSuffix(lower, ".pb.go") {
		add("generated")
	}
	if strings.Contains(lower, "/vendor/") || strings.Contains(lower, "/node_modules/") || strings.Contains(lower, "/deps/") {
		add("vendor")
	}
	if strings.HasPrefix(lower, "docs/") || strings.Contains(lower, "/docs/") || base == "readme.md" || ext == ".md" || ext == ".mdx" || ext == ".rst" {
		add("docs")
	}
	if isGatherContextRoutePath(lower, base) {
		add("route")
	}
	if strings.Contains(base, "router") || strings.Contains(lower, "/router.") || strings.Contains(lower, "/routes.") || strings.Contains(lower, "/routes/") {
		add("router")
	}
	if strings.Contains(base, "controller") || strings.Contains(base, "handler") || strings.Contains(lower, "/controllers/") || strings.Contains(lower, "/handlers/") {
		add("controller")
	}
	if strings.Contains(lower, "/services/") || strings.Contains(lower, "/service/") || strings.Contains(lower, "/domain/") || strings.Contains(lower, "/repositories/") || strings.Contains(lower, "/repo/") || strings.Contains(base, "service") {
		add("domain")
	}
	if strings.Contains(base, "api") || strings.Contains(base, "client") || strings.Contains(base, "adapter") || strings.Contains(lower, "/api/") || strings.Contains(lower, "/clients/") || strings.Contains(lower, "/adapters/") {
		add("api_adapter")
	}
	if isGatherContextDeployConfigPath(lower, base, ext) {
		add("deploy_config")
	}
	if isGatherContextTestPath(lower, base) {
		add("test")
	}
	if isGatherContextDataPath(lower, ext) {
		add("data")
	}
	if strings.Contains(lower, "/workers/") || strings.Contains(lower, "/jobs/") || strings.Contains(base, "worker") || strings.Contains(base, "job") {
		add("worker")
	}
	return roles
}

func isGatherContextRoutePath(path, base string) bool {
	if strings.Contains(path, "/app/") || strings.HasPrefix(path, "app/") || strings.Contains(path, "/pages/") || strings.HasPrefix(path, "pages/") {
		switch {
		case base == "_layout.tsx" || base == "_layout.ts" || base == "layout.tsx" || base == "layout.ts":
			return true
		case base == "index.tsx" || base == "index.ts" || base == "page.tsx" || base == "page.ts":
			return true
		case strings.Contains(path, "/[") && strings.HasSuffix(base, ".tsx"):
			return true
		case strings.HasSuffix(base, ".tsx") || strings.HasSuffix(base, ".jsx"):
			return true
		}
	}
	return false
}

func gatherContextPathHasToolingSegment(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		switch part {
		case "", ".":
			continue
		case ".github", ".devcontainer", "tooling", "templates":
			return true
		case "scripts", "tools":
			// Treat top-level repo automation as tooling, but do not penalize
			// normal source roots such as Unity's client/Scripts.
			return i == 0
		default:
			if strings.HasPrefix(part, ".") {
				return true
			}
		}
	}
	return false
}

func isGatherContextDeployConfigPath(path, base, ext string) bool {
	if strings.Contains(path, "/helm/") || strings.Contains(path, "/charts/") || strings.Contains(path, "/deploy/") || strings.Contains(path, "/deployment/") || strings.Contains(path, "/k8s/") || strings.Contains(path, "/kubernetes/") {
		return true
	}
	switch base {
	case "config.py", "config.ex", "config.exs", "settings.py", "settings.ex", "settings.exs":
		return true
	}
	if strings.Contains(base, "values") || strings.Contains(base, "deployment") || strings.Contains(base, "serviceaccount") || strings.Contains(base, "ingress") {
		return true
	}
	switch ext {
	case ".yaml", ".yml", ".toml", ".tf":
		return true
	default:
		return false
	}
}

func isGatherContextTestPath(path, base string) bool {
	return strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/") ||
		strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") ||
		strings.Contains(base, "_test.") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(base, "test_") || strings.HasPrefix(base, "test-") || strings.Contains(base, "-test-")
}

func isGatherContextDataPath(path, ext string) bool {
	if strings.Contains(path, "/fixtures/") || strings.Contains(path, "/fixture/") || strings.Contains(path, "/data/") || strings.Contains(path, "/schemas/") || strings.Contains(path, "/migrations/") {
		return true
	}
	switch ext {
	case ".json", ".jsonl", ".sql", ".proto", ".graphql", ".csv":
		return true
	default:
		return false
	}
}

func scoreGatherContextExpectedRoles(expected []string, coverage map[string]int) ([]string, float64) {
	expected = normalizeGatherContextEvalRoles(expected)
	if len(expected) == 0 {
		return nil, 0
	}
	matched := make([]string, 0, len(expected))
	for _, role := range expected {
		if coverage[role] > 0 {
			matched = append(matched, role)
		}
	}
	return matched, float64(len(matched)) / float64(len(expected))
}

func gatherContextPeripheralRoles() []string {
	return []string{"generated", "test", "tooling", "vendor"}
}

func isGatherContextPeripheralRole(role string) bool {
	role = normalizeGatherContextEvalRole(role)
	for _, candidate := range gatherContextPeripheralRoles() {
		if role == candidate {
			return true
		}
	}
	return false
}

func normalizeGatherContextEvalStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compactGatherContextEvalStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return compactGatherContextEvalStrings(out)
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return compactGatherContextEvalStrings(strings.Split(typed, ","))
	default:
		return nil
	}
}

func compactGatherContextEvalStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func summarizeGatherContextEvalResults(results []gatherContextEvalResult) gatherContextEvalSummary {
	summary := gatherContextEvalSummary{Count: len(results)}
	if len(results) == 0 {
		return summary
	}
	var passed int
	roleExpectedCases := map[string]int{}
	roleMatchedCases := map[string]int{}
	peripheralCoverage := map[string]int{}
	peripheralCaseCounts := map[string]int{}
	for _, result := range results {
		if result.StaleEval {
			summary.StaleEvalCount++
			continue
		}
		summary.ScoredCount++
		expectedRoles := normalizeGatherContextEvalRoles(result.ExpectedRoles)
		expectedRoleSet := make(map[string]struct{}, len(expectedRoles))
		for _, role := range expectedRoles {
			expectedRoleSet[role] = struct{}{}
			roleExpectedCases[role]++
		}
		matchedRoleSet := make(map[string]struct{}, len(result.MatchedRoles))
		for _, role := range normalizeGatherContextEvalRoles(result.MatchedRoles) {
			matchedRoleSet[role] = struct{}{}
		}
		for role := range expectedRoleSet {
			if _, ok := matchedRoleSet[role]; ok {
				roleMatchedCases[role]++
			}
		}
		for role, count := range result.RoleCoverage {
			role = normalizeGatherContextEvalRole(role)
			if role == "" || !isGatherContextPeripheralRole(role) {
				continue
			}
			if _, expected := expectedRoleSet[role]; expected {
				continue
			}
			peripheralCoverage[role] += count
			peripheralCaseCounts[role]++
		}
		if result.Passed {
			passed++
		}
		if result.Status == "error" {
			summary.ErrorCount++
		}
		summary.MeanPathRecall += result.PathRecall
		summary.MeanRoleRecall += result.RoleRecall
		summary.MeanFactRecall += result.FactRecall
		summary.MeanDurationMS += float64(result.DurationMS)
		summary.MeanRawContextChars += float64(result.RawContextChars)
		summary.MeanEmittedContextChars += float64(result.EmittedContextChars)
		summary.MeanOmittedContextItems += float64(result.OmittedContextItems)
		summary.MeanFactCount += float64(result.FactCount)
		summary.MeanEvidenceCount += float64(result.EvidenceCount)
	}
	if summary.ScoredCount == 0 {
		return summary
	}
	denom := float64(summary.ScoredCount)
	summary.PassRate = float64(passed) / denom
	summary.MeanPathRecall /= denom
	summary.MeanRoleRecall /= denom
	summary.MeanFactRecall /= denom
	summary.MeanDurationMS /= denom
	summary.MeanRawContextChars /= denom
	summary.MeanEmittedContextChars /= denom
	summary.MeanOmittedContextItems /= denom
	summary.MeanFactCount /= denom
	summary.MeanEvidenceCount /= denom
	summary.RoleRecallByRole = gatherContextRoleRecallScores(roleExpectedCases, roleMatchedCases)
	summary.PeripheralRoleCoverage = nonEmptyIntMap(peripheralCoverage)
	summary.WrongRolePeripheralCaseCount = nonEmptyIntMap(peripheralCaseCounts)
	return summary
}

func gatherContextRoleRecallScores(expected map[string]int, matched map[string]int) []gatherContextRoleRecallScore {
	if len(expected) == 0 {
		return nil
	}
	roles := make([]string, 0, len(expected))
	for role := range expected {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	out := make([]gatherContextRoleRecallScore, 0, len(roles))
	for _, role := range roles {
		expectedCases := expected[role]
		matchedCases := matched[role]
		score := gatherContextRoleRecallScore{
			Role:          role,
			ExpectedCases: expectedCases,
			MatchedCases:  matchedCases,
			MissingCases:  expectedCases - matchedCases,
		}
		if expectedCases > 0 {
			score.Recall = float64(matchedCases) / float64(expectedCases)
		}
		out = append(out, score)
	}
	return out
}

func nonEmptyIntMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]int, len(values))
	for key, value := range values {
		if strings.TrimSpace(key) != "" && value > 0 {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compareGatherContextToAgentBaselines(summary gatherContextEvalSummary, baselines []agentEvalSummary) []gatherContextBaselineComparison {
	out := make([]gatherContextBaselineComparison, 0, len(baselines))
	for _, baseline := range baselines {
		comparison := gatherContextBaselineComparison{
			Label:                    baseline.Label,
			Role:                     baseline.Role,
			Provider:                 baseline.Provider,
			Model:                    baseline.Model,
			Runner:                   baseline.Runner,
			Count:                    baseline.Count,
			BaselineErrorCount:       baseline.ErrorCount,
			GatherPassRate:           summary.PassRate,
			BaselinePassRate:         baseline.PassRate,
			PassRateDelta:            summary.PassRate - baseline.PassRate,
			GatherMeanPathRecall:     summary.MeanPathRecall,
			BaselineMeanPathRecall:   baseline.MeanPathRecall,
			PathRecallDelta:          summary.MeanPathRecall - baseline.MeanPathRecall,
			GatherMeanFactRecall:     summary.MeanFactRecall,
			BaselineMeanFactRecall:   baseline.MeanFactRecall,
			FactRecallDelta:          summary.MeanFactRecall - baseline.MeanFactRecall,
			GatherMeanDurationMS:     summary.MeanDurationMS,
			BaselineMeanDurationMS:   baseline.MeanDurationMS,
			BaselineMeanTokens:       baseline.MeanTokens,
			BaselineMeanCachedInput:  baseline.MeanCachedInput,
			BaselineMeanReasoningOut: baseline.MeanReasoningOut,
			BaselineMeanCostUSD:      baseline.MeanCostUSD,
			EmittedCharsPerTokenMean: emittedCharsPerBaselineToken(summary, baseline),
		}
		if summary.MeanDurationMS > 0 && baseline.MeanDurationMS > 0 {
			comparison.DurationSpeedup = baseline.MeanDurationMS / summary.MeanDurationMS
		}
		out = append(out, comparison)
	}
	return out
}

func emittedCharsPerBaselineToken(summary gatherContextEvalSummary, baseline agentEvalSummary) float64 {
	if baseline.MeanTokens <= 0 {
		return 0
	}
	return summary.MeanEmittedContextChars / baseline.MeanTokens
}

func renderGatherContextEvalMarkdown(workspace string, summary gatherContextEvalSummary, results []gatherContextEvalResult, comparisons []gatherContextBaselineComparison, repoIndexFreshness map[string]any) string {
	var b strings.Builder
	b.WriteString("# gather_context Eval\n\n")
	b.WriteString(fmt.Sprintf("Workspace: `%s`\n\n", workspace))
	if summaryText := renderGatherContextRepoIndexFreshnessMarkdown(repoIndexFreshness); summaryText != "" {
		b.WriteString(summaryText)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("- Cases: %d\n", summary.Count))
	if summary.StaleEvalCount > 0 {
		b.WriteString(fmt.Sprintf("- Stale eval cases excluded from aggregate scoring: %d\n", summary.StaleEvalCount))
	}
	b.WriteString(fmt.Sprintf("- Pass rate: %.2f\n", summary.PassRate))
	b.WriteString(fmt.Sprintf("- Mean path recall: %.2f\n", summary.MeanPathRecall))
	if summary.MeanRoleRecall > 0 {
		b.WriteString(fmt.Sprintf("- Mean role recall: %.2f\n", summary.MeanRoleRecall))
	}
	b.WriteString(fmt.Sprintf("- Mean fact recall: %.2f\n", summary.MeanFactRecall))
	b.WriteString(fmt.Sprintf("- Mean duration ms: %.1f\n", summary.MeanDurationMS))
	b.WriteString(fmt.Sprintf("- Mean emitted context chars: %.1f\n\n", summary.MeanEmittedContextChars))
	if len(summary.RoleRecallByRole) > 0 || len(summary.PeripheralRoleCoverage) > 0 {
		b.WriteString("## Role Diagnostics\n\n")
		if len(summary.RoleRecallByRole) > 0 {
			b.WriteString("| Role | Recall | Matched | Expected | Missing |\n")
			b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
			for _, score := range summary.RoleRecallByRole {
				b.WriteString(fmt.Sprintf(
					"| %s | %.2f | %d | %d | %d |\n",
					markdownCell(score.Role),
					score.Recall,
					score.MatchedCases,
					score.ExpectedCases,
					score.MissingCases,
				))
			}
			b.WriteByte('\n')
		}
		if len(summary.PeripheralRoleCoverage) > 0 {
			b.WriteString("Peripheral roles returned when not expected:\n\n")
			b.WriteString("| Role | Coverage Hits | Cases |\n")
			b.WriteString("| --- | ---: | ---: |\n")
			for _, role := range sortedIntMapKeys(summary.PeripheralRoleCoverage) {
				b.WriteString(fmt.Sprintf(
					"| %s | %d | %d |\n",
					markdownCell(role),
					summary.PeripheralRoleCoverage[role],
					summary.WrongRolePeripheralCaseCount[role],
				))
			}
			b.WriteByte('\n')
		}
	}
	b.WriteString("| Case | Pass | Path Recall | Role Recall | Fact Recall | Duration | Slowest Provider | Evidence | Chars | Paths |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- |\n")
	for _, result := range results {
		pass := "no"
		if result.Passed {
			pass = "yes"
		}
		if result.StaleEval {
			pass = "stale"
		}
		b.WriteString(fmt.Sprintf(
			"| %s | %s | %.2f | %.2f | %.2f | %dms | %s | %d | %d | %s |\n",
			markdownCell(result.CaseID),
			pass,
			result.PathRecall,
			result.RoleRecall,
			result.FactRecall,
			result.DurationMS,
			markdownCell(slowestGatherContextProviderSummary(result.ProviderTelemetry)),
			result.EvidenceCount,
			result.EmittedContextChars,
			markdownCell(strings.Join(result.Paths, "<br>")),
		))
	}
	if len(comparisons) > 0 {
		b.WriteString("\n## Agent Baselines\n\n")
		b.WriteString("| Baseline | Errors | Pass Delta | Path Delta | Fact Delta | Speedup | Baseline Tokens | Cached Input | Reasoning Out |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		for _, comparison := range comparisons {
			b.WriteString(fmt.Sprintf(
				"| %s | %d | %.2f | %.2f | %.2f | %.2fx | %.1f | %.1f | %.1f |\n",
				markdownCell(comparison.Label),
				comparison.BaselineErrorCount,
				comparison.PassRateDelta,
				comparison.PathRecallDelta,
				comparison.FactRecallDelta,
				comparison.DurationSpeedup,
				comparison.BaselineMeanTokens,
				comparison.BaselineMeanCachedInput,
				comparison.BaselineMeanReasoningOut,
			))
		}
	}
	return b.String()
}

func sortedIntMapKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func renderGatherContextRepoIndexFreshnessMarkdown(freshness map[string]any) string {
	if len(freshness) == 0 {
		return ""
	}
	level := gatherContextFreshnessLevelString(freshness["freshness"])
	if level == "" {
		level = "unknown"
	}
	mismatch := boolFromAny(freshness["stale_or_dirty_mismatch"])
	indexHead := shortSHA(stringFromAny(freshness["index_head_sha"]))
	currentHead := shortSHA(stringFromAny(freshness["current_head_sha"]))
	if indexHead == "" {
		indexHead = "unknown"
	}
	if currentHead == "" {
		currentHead = "unknown"
	}
	return fmt.Sprintf(
		"Repoindex freshness: `%s` (index head `%s`, current head `%s`, index dirty `%t`, current dirty `%t`, stale/dirty mismatch `%t`)\n",
		level,
		indexHead,
		currentHead,
		boolFromAny(freshness["index_worktree_dirty"]),
		boolFromAny(freshness["current_worktree_dirty"]),
		mismatch,
	)
}

func gatherContextFreshnessLevelString(value any) string {
	switch typed := value.(type) {
	case repoindex.IndexFreshnessStatus:
		return string(typed.Level)
	case map[string]any:
		return stringFromAny(typed["level"])
	default:
		return ""
	}
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func shortSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func slowestGatherContextProviderSummary(value any) string {
	var slowName string
	var slowDuration float64
	entries := gatherContextProviderTelemetryEntries(value)
	for _, entry := range entries {
		name, _ := entry["name"].(string)
		duration := floatFromGatherContextAny(entry["duration_ms"])
		if name != "" && duration >= slowDuration {
			slowName = name
			slowDuration = duration
		}
	}
	if slowName == "" {
		return ""
	}
	return fmt.Sprintf("%s %.0fms", slowName, slowDuration)
}

func gatherContextProviderTelemetryEntries(value any) []map[string]any {
	var entries []map[string]any
	groups, ok := value.([]any)
	if !ok {
		return nil
	}
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			continue
		}
		providers, ok := groupMap["providers"].([]any)
		if !ok {
			continue
		}
		for _, provider := range providers {
			if providerMap, ok := provider.(map[string]any); ok {
				entries = append(entries, providerMap)
			}
		}
	}
	return entries
}

func floatFromGatherContextAny(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		got, _ := typed.Float64()
		return got
	default:
		return 0
	}
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}
