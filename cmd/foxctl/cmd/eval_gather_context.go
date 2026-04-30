package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	obs "github.com/joshka0/foxctl/internal/adapters/skillslib/obs"
	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/rlm"
	rlmenv "github.com/joshka0/foxctl/internal/rlm/env"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
	"github.com/spf13/cobra"
)

type gatherContextEvalResult struct {
	CaseID               string         `json:"case_id,omitempty"`
	Category             string         `json:"category,omitempty"`
	TaskType             string         `json:"task_type,omitempty"`
	Status               string         `json:"status,omitempty"`
	BundleStatus         string         `json:"bundle_status,omitempty"`
	CertificateStatus    string         `json:"certificate_status,omitempty"`
	Answerable           bool           `json:"answerable"`
	Paths                []string       `json:"paths,omitempty"`
	RawEvidencePaths     []string       `json:"raw_evidence_paths,omitempty"`
	SelectedPaths        []string       `json:"selected_paths,omitempty"`
	AnswerCandidatePaths []string       `json:"answer_candidate_paths,omitempty"`
	MatchedPaths         []string       `json:"matched_paths,omitempty"`
	RetrievalMisses      []string       `json:"retrieval_misses,omitempty"`
	ReductionMisses      []string       `json:"reduction_misses,omitempty"`
	PathRecall           float64        `json:"path_recall,omitempty"`
	MatchedFacts         []string       `json:"matched_facts,omitempty"`
	FactRecall           float64        `json:"fact_recall,omitempty"`
	Lanes                []string       `json:"lanes,omitempty"`
	MemoryStatuses       []string       `json:"memory_statuses,omitempty"`
	SourceCoverage       map[string]int `json:"source_coverage,omitempty"`
	RawContextChars      int            `json:"raw_context_chars,omitempty"`
	EmittedContextChars  int            `json:"emitted_context_chars,omitempty"`
	OmittedContextItems  int            `json:"omitted_context_items,omitempty"`
	FactCount            int            `json:"fact_count,omitempty"`
	EvidenceCount        int            `json:"evidence_count,omitempty"`
	DurationMS           int64          `json:"duration_ms,omitempty"`
	Passed               bool           `json:"passed,omitempty"`
	Gaps                 []string       `json:"gaps,omitempty"`
	Error                string         `json:"error,omitempty"`
}

type gatherContextEvalSummary struct {
	Count                   int     `json:"count"`
	PassRate                float64 `json:"pass_rate"`
	MeanPathRecall          float64 `json:"mean_path_recall"`
	MeanFactRecall          float64 `json:"mean_fact_recall"`
	MeanDurationMS          float64 `json:"mean_duration_ms"`
	MeanRawContextChars     float64 `json:"mean_raw_context_chars"`
	MeanEmittedContextChars float64 `json:"mean_emitted_context_chars"`
	MeanOmittedContextItems float64 `json:"mean_omitted_context_items"`
	MeanFactCount           float64 `json:"mean_fact_count"`
	MeanEvidenceCount       float64 `json:"mean_evidence_count"`
	ErrorCount              int     `json:"error_count"`
}

type gatherContextBaselineComparison struct {
	Label                    string  `json:"label"`
	Role                     string  `json:"role,omitempty"`
	Provider                 string  `json:"provider,omitempty"`
	Model                    string  `json:"model,omitempty"`
	Runner                   string  `json:"runner,omitempty"`
	Count                    int     `json:"count"`
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
	BaselineMeanCostUSD      float64 `json:"baseline_mean_cost_usd,omitempty"`
	EmittedCharsPerTokenMean float64 `json:"emitted_chars_per_token_mean,omitempty"`
}

func newEvalGatherContextCommand() *cobra.Command {
	var (
		workspace            string
		evalDatasetFile      string
		vaultPath            string
		timeout              time.Duration
		passThreshold        float64
		reportFile           string
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
			baselineSummaries = summarizeAgentEvalResults(baselineResults)
			baselineComparisons = compareGatherContextToAgentBaselines(summary, baselineSummaries)
			report := map[string]any{
				"operation":                   "eval.gather-context",
				"workspace_id":                absWorkspace,
				"eval_case_count":             len(evalCases),
				"eval_cases":                  evalCases,
				"tool_profile":                toolProfile,
				"lanes":                       lanes,
				"limit":                       limit,
				"max_context_chars":           maxContextChars,
				"pass_threshold":              passThreshold,
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
				"markdown": renderGatherContextEvalMarkdown(absWorkspace, summary, results, baselineComparisons),
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
	_ = cmd.MarkFlagRequired("eval-dataset-file")
	return cmd
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

func buildRLMSearchAgentGatherPayload(evalCase promptEvalCase, limit int, maxContextChars int, lanes []string) map[string]any {
	payload := map[string]any{
		"query":             buildCodeSearchEnsembleEvalQuery(evalCase),
		"goal":              gatherContextEvalCaseString(evalCase, "goal", "repo_grounded_eval"),
		"task_type":         normalizeCodeSearchEvalTaskType(evalCase),
		"lanes":             gatherContextEvalCaseStringSlice(evalCase, "lanes", lanes),
		"limit":             limit,
		"max_context_chars": gatherContextEvalCaseInt(evalCase, "max_context_chars", maxContextChars),
		"response_mode":     "answer_surface",
	}
	if len(evalCase.RequiredFacts) > 0 {
		payload["required_evidence"] = append([]string(nil), evalCase.RequiredFacts...)
	}
	if statuses := gatherContextEvalCaseStringSlice(evalCase, "memory_statuses", nil); len(statuses) > 0 {
		payload["memory_statuses"] = statuses
	}
	return payload
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
	}
	if len(evalCase.RequiredFacts) > 0 {
		payload["required_evidence"] = evalCase.RequiredFacts
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
	result.Paths = extractGatherContextPaths(bundle)
	result.RawEvidencePaths = extractGatherContextEvidencePaths(bundle)
	result.SelectedPaths = extractGatherContextSelectedPaths(bundle)
	result.AnswerCandidatePaths = extractGatherContextAnswerCandidatePaths(bundle)
	result.MatchedPaths, result.PathRecall = scoreExpectedPaths(evalCase.ExpectedPaths, strings.Join(result.Paths, "\n"))
	result.RetrievalMisses = missingExpectedPaths(evalCase.ExpectedPaths, strings.Join(result.RawEvidencePaths, "\n"))
	result.ReductionMisses = expectedPathsPresentThenMissing(evalCase.ExpectedPaths, strings.Join(result.RawEvidencePaths, "\n"), strings.Join(result.SelectedPaths, "\n"))
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
	for _, result := range results {
		if result.Passed {
			passed++
		}
		if result.Status == "error" {
			summary.ErrorCount++
		}
		summary.MeanPathRecall += result.PathRecall
		summary.MeanFactRecall += result.FactRecall
		summary.MeanDurationMS += float64(result.DurationMS)
		summary.MeanRawContextChars += float64(result.RawContextChars)
		summary.MeanEmittedContextChars += float64(result.EmittedContextChars)
		summary.MeanOmittedContextItems += float64(result.OmittedContextItems)
		summary.MeanFactCount += float64(result.FactCount)
		summary.MeanEvidenceCount += float64(result.EvidenceCount)
	}
	denom := float64(len(results))
	summary.PassRate = float64(passed) / denom
	summary.MeanPathRecall /= denom
	summary.MeanFactRecall /= denom
	summary.MeanDurationMS /= denom
	summary.MeanRawContextChars /= denom
	summary.MeanEmittedContextChars /= denom
	summary.MeanOmittedContextItems /= denom
	summary.MeanFactCount /= denom
	summary.MeanEvidenceCount /= denom
	return summary
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

func renderGatherContextEvalMarkdown(workspace string, summary gatherContextEvalSummary, results []gatherContextEvalResult, comparisons []gatherContextBaselineComparison) string {
	var b strings.Builder
	b.WriteString("# gather_context Eval\n\n")
	b.WriteString(fmt.Sprintf("Workspace: `%s`\n\n", workspace))
	b.WriteString(fmt.Sprintf("- Cases: %d\n", summary.Count))
	b.WriteString(fmt.Sprintf("- Pass rate: %.2f\n", summary.PassRate))
	b.WriteString(fmt.Sprintf("- Mean path recall: %.2f\n", summary.MeanPathRecall))
	b.WriteString(fmt.Sprintf("- Mean fact recall: %.2f\n", summary.MeanFactRecall))
	b.WriteString(fmt.Sprintf("- Mean duration ms: %.1f\n", summary.MeanDurationMS))
	b.WriteString(fmt.Sprintf("- Mean emitted context chars: %.1f\n\n", summary.MeanEmittedContextChars))
	b.WriteString("| Case | Pass | Path Recall | Fact Recall | Evidence | Chars | Paths |\n")
	b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | --- |\n")
	for _, result := range results {
		pass := "no"
		if result.Passed {
			pass = "yes"
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %.2f | %.2f | %d | %d | %s |\n",
			markdownCell(result.CaseID),
			pass,
			result.PathRecall,
			result.FactRecall,
			result.EvidenceCount,
			result.EmittedContextChars,
			markdownCell(strings.Join(result.Paths, "<br>")),
		))
	}
	if len(comparisons) > 0 {
		b.WriteString("\n## Agent Baselines\n\n")
		b.WriteString("| Baseline | Pass Delta | Path Delta | Fact Delta | Speedup | Baseline Tokens |\n")
		b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
		for _, comparison := range comparisons {
			b.WriteString(fmt.Sprintf("| %s | %.2f | %.2f | %.2f | %.2fx | %.1f |\n",
				markdownCell(comparison.Label),
				comparison.PassRateDelta,
				comparison.PathRecallDelta,
				comparison.FactRecallDelta,
				comparison.DurationSpeedup,
				comparison.BaselineMeanTokens,
			))
		}
	}
	return b.String()
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}
