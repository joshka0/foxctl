package rlm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestPlanLambda_SmallProblem_NoSplit(t *testing.T) {
	t.Parallel()

	plan := PlanLambda(TaskTypeCodeLocate, 5, LambdaConfig{
		ContextBudget:    10,
		MaxK:             8,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.6,
		AccuracyTarget:   0.5,
	})
	if plan.KStar != 1 {
		t.Fatalf("k_star=%d want 1 (fits in context budget)", plan.KStar)
	}
	if plan.Depth != 0 {
		t.Fatalf("depth=%d want 0", plan.Depth)
	}
	if plan.TauStar != 5 {
		t.Fatalf("tau_star=%d want 5", plan.TauStar)
	}
}

func TestPlanLambda_ExactFit_NoSplit(t *testing.T) {
	t.Parallel()

	plan := PlanLambda(TaskTypeGeneral, 10, LambdaConfig{ContextBudget: 10})
	if plan.KStar != 1 {
		t.Fatalf("k_star=%d want 1 (exactly fits)", plan.KStar)
	}
	if plan.Depth != 0 {
		t.Fatalf("depth=%d want 0", plan.Depth)
	}
}

func TestPlanLambda_LargeProblem_OptimalK(t *testing.T) {
	t.Parallel()

	// n=100, K=10, c_search=1, c_inspect=3
	// k* = ceil(sqrt(100 * 1 / 3)) = ceil(sqrt(33.3)) = ceil(5.77) = 6
	plan := PlanLambda(TaskTypeCodeLocate, 100, LambdaConfig{
		ContextBudget:    10,
		MaxK:             8,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.9,
		AccuracyTarget:   0.5,
	})
	if plan.KStar != 6 {
		t.Fatalf("k_star=%d want 6", plan.KStar)
	}
	if plan.Depth < 1 {
		t.Fatalf("depth=%d want >=1", plan.Depth)
	}
}

func TestPlanLambda_AccuracyConstraint_IncreasesK(t *testing.T) {
	t.Parallel()

	// With very low per-phase accuracy, the accuracy constraint should push k* up.
	lowAccuracy := LambdaConfig{
		ContextBudget:    10,
		MaxK:             8,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.3,
		AccuracyTarget:   0.5,
	}

	planLow := PlanLambda(TaskTypeCodeLocate, 100, lowAccuracy)

	highAccuracy := LambdaConfig{
		ContextBudget:    10,
		MaxK:             8,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.9,
		AccuracyTarget:   0.5,
	}
	planHigh := PlanLambda(TaskTypeCodeLocate, 100, highAccuracy)

	// The low-accuracy plan should fan out wider (higher k*) or have shallower depth.
	if planLow.KStar == planHigh.KStar && planLow.Depth == planHigh.Depth {
		t.Fatalf("low_accuracy and high_accuracy produced identical plans -- constraint should differentiate")
	}
	// Low accuracy should generally produce higher k* to compensate.
	if planLow.KStar < planHigh.KStar {
		t.Fatalf("low_accuracy k_star=%d should be >= high_accuracy k_star=%d (wider fan-out to compensate)",
			planLow.KStar, planHigh.KStar)
	}
}

func TestPlanLambda_ComposeOpByTaskType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		taskType TaskType
		wantOp   ComposeOp
	}{
		{TaskTypeCodeLocate, ComposeRerank},
		{TaskTypeCodeUnderstand, ComposeSynthesize},
		{TaskTypeMemoryRecall, ComposeChronological},
		{TaskTypeEvidenceAudit, ComposeIntersection},
		{TaskTypeGeneral, ComposeSynthesize},
	}
	for _, tc := range cases {
		plan := PlanLambda(tc.taskType, 100, LambdaConfig{ContextBudget: 10})
		if plan.ComposeOp != tc.wantOp {
			t.Errorf("task_type=%s compose_op=%s want %s", tc.taskType, plan.ComposeOp, tc.wantOp)
		}
	}
}

func TestPlanLambda_CostEstimatePositive(t *testing.T) {
	t.Parallel()

	plan := PlanLambda(TaskTypeCodeLocate, 100, LambdaConfig{
		ContextBudget:    10,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.9,
		AccuracyTarget:   0.5,
	})
	if plan.CostEstimate <= 0 {
		t.Fatalf("cost_estimate=%.2f want positive", plan.CostEstimate)
	}
}

func TestPlanLambda_NIsRecorded(t *testing.T) {
	t.Parallel()

	plan := PlanLambda(TaskTypeGeneral, 42, LambdaConfig{ContextBudget: 10})
	if plan.N != 42 {
		t.Fatalf("n=%d want 42", plan.N)
	}
}

func TestPlanLambda_KStarCappedByMaxK(t *testing.T) {
	t.Parallel()

	// Very large n with low MaxK should cap k*.
	plan := PlanLambda(TaskTypeGeneral, 10000, LambdaConfig{
		ContextBudget:    10,
		MaxK:             3,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.9,
		AccuracyTarget:   0.5,
	})
	if plan.KStar > 3 {
		t.Fatalf("k_star=%d want <=3 (MaxK cap)", plan.KStar)
	}
}

func TestPlanLambda_DefaultsApplied(t *testing.T) {
	t.Parallel()

	// Zero config should use defaults and not panic.
	plan := PlanLambda(TaskTypeGeneral, 50, LambdaConfig{})
	if plan.KStar < 1 {
		t.Fatalf("k_star=%d want >=1", plan.KStar)
	}
}

func TestCapLambdaPlanForTask(t *testing.T) {
	t.Parallel()

	plan, caps := capLambdaPlanForTask(LambdaPlan{
		KStar: 5,
		Depth: 4,
	}, Task{MaxDepth: 2, MaxSubcalls: 3}, LambdaConfig{})

	if plan.KStar != 3 {
		t.Fatalf("k_star=%d want 3", plan.KStar)
	}
	if plan.Depth != 2 {
		t.Fatalf("depth=%d want 2", plan.Depth)
	}
	if caps["lambda_k_star_capped_by_task"] != true {
		t.Fatalf("expected k cap metadata, got %#v", caps)
	}
	if caps["lambda_depth_capped_by_task"] != true {
		t.Fatalf("expected depth cap metadata, got %#v", caps)
	}
}

func TestCapLambdaPlanForTask_DisablesRecursionWhenKCollapses(t *testing.T) {
	t.Parallel()

	plan, _ := capLambdaPlanForTask(LambdaPlan{
		KStar: 4,
		Depth: 3,
		N:     50,
	}, Task{MaxSubcalls: 1}, LambdaConfig{ContextBudget: 10})

	if plan.KStar != 1 {
		t.Fatalf("k_star=%d want 1", plan.KStar)
	}
	if plan.Depth != 0 {
		t.Fatalf("depth=%d want 0", plan.Depth)
	}
	if plan.TauStar != 10 {
		t.Fatalf("tau_star=%d want 10", plan.TauStar)
	}
}

func TestCapLambdaPlanForTask_RecomputesTauAfterKCap(t *testing.T) {
	t.Parallel()

	plan, _ := capLambdaPlanForTask(LambdaPlan{
		KStar:   3,
		TauStar: 6,
		Depth:   1,
		N:       18,
	}, Task{MaxSubcalls: 2}, LambdaConfig{ContextBudget: 10})

	if plan.KStar != 2 {
		t.Fatalf("k_star=%d want 2", plan.KStar)
	}
	if plan.TauStar != 9 {
		t.Fatalf("tau_star=%d want 9", plan.TauStar)
	}
}

func TestBuildLambdaProgram_SmallProblemDirectLeaf(t *testing.T) {
	t.Parallel()

	plan := PlanLambda(TaskTypeCodeLocate, 4, LambdaConfig{ContextBudget: 10})
	program := BuildLambdaProgram(plan, LambdaConfig{ContextBudget: 10})

	if program.SplitMode != LambdaSplitNone {
		t.Fatalf("split_mode=%s want none", program.SplitMode)
	}
	if program.BranchingFactor != 1 {
		t.Fatalf("branching_factor=%d want 1", program.BranchingFactor)
	}
	if program.MaxDepth != 0 {
		t.Fatalf("max_depth=%d want 0", program.MaxDepth)
	}
	if program.LeafThreshold != 4 {
		t.Fatalf("leaf_threshold=%d want 4", program.LeafThreshold)
	}
	if program.EstimatedLeafCalls != 1 {
		t.Fatalf("estimated_leaf_calls=%d want 1", program.EstimatedLeafCalls)
	}
}

func TestBuildLambdaProgram_BoundedDepthAndCalls(t *testing.T) {
	t.Parallel()

	plan := LambdaPlan{
		TaskType:     TaskTypeGeneral,
		ComposeOp:    ComposeSynthesize,
		KStar:        3,
		TauStar:      5,
		Depth:        2,
		CostEstimate: 123,
		N:            45,
	}
	program := BuildLambdaProgram(plan, LambdaConfig{ContextBudget: 10})

	if program.SplitMode != LambdaSplitQueryVariants {
		t.Fatalf("split_mode=%s want query_variants", program.SplitMode)
	}
	if program.ReduceMode != LambdaReduceSynthesize {
		t.Fatalf("reduce_mode=%s want synthesize", program.ReduceMode)
	}
	if program.ReduceVerifierMode != LambdaVerifierReduceOracle {
		t.Fatalf("reduce_verifier_mode=%s want reduce_oracle", program.ReduceVerifierMode)
	}
	if program.EstimatedLeafCalls != 9 {
		t.Fatalf("estimated_leaf_calls=%d want 9", program.EstimatedLeafCalls)
	}
	if program.EstimatedReduceCalls != 4 {
		t.Fatalf("estimated_reduce_calls=%d want 4", program.EstimatedReduceCalls)
	}
	if program.EstimatedTotalCalls != 67 {
		t.Fatalf("estimated_total_calls=%d want 67", program.EstimatedTotalCalls)
	}
	if program.EstimatedCost != 123 {
		t.Fatalf("estimated_cost=%.1f want 123", program.EstimatedCost)
	}
}

func TestLambdaRunnerRunExposesProgramMetadataForDirectLeaf(t *testing.T) {
	t.Parallel()

	runner := LambdaRunner{
		Config: LambdaConfig{
			ContextBudget: 10,
			LLM: LLMConfig{
				Provider: "lambda-test-no-provider",
				AuthMode: "header",
			},
		},
		Tools: lambdaFakeToolExecutor{},
	}
	result, err := runner.Run(context.Background(), Task{
		Prompt:      "find answer",
		MaxSubcalls: 1,
		Metadata: map[string]any{
			"gather_context_payload": map[string]any{"task_type": "file_locate"},
		},
	}, Environment{Tools: []Tool{{Name: "gather_context", ReadOnly: true}, {Name: "load_evidence_ref", ReadOnly: true}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	program, ok := result.Metadata["lambda_program"].(LambdaProgram)
	if !ok {
		t.Fatalf("lambda_program metadata type=%T want LambdaProgram", result.Metadata["lambda_program"])
	}
	if program.SplitMode != LambdaSplitNone {
		t.Fatalf("split_mode=%s want none", program.SplitMode)
	}
	if program.MaxDepth != 0 {
		t.Fatalf("max_depth=%d want 0", program.MaxDepth)
	}
	if result.Metadata["lambda_program_split_mode"] != string(LambdaSplitNone) {
		t.Fatalf("lambda_program_split_mode=%v", result.Metadata["lambda_program_split_mode"])
	}
	if intFromAny(result.Metadata["lambda_program_estimated_total_calls"]) <= 0 {
		t.Fatalf("missing positive estimated calls metadata: %#v", result.Metadata)
	}
}

func TestComposeOpForTask_Unknown(t *testing.T) {
	if got := ComposeOpForTask(TaskType("bogus")); got != ComposeSynthesize {
		t.Fatalf("got=%s want synthesize", got)
	}
}

func TestNormalizeTaskType(t *testing.T) {
	cases := []struct {
		input string
		want  TaskType
	}{
		{"code_locate", TaskTypeCodeLocate},
		{"code_understand", TaskTypeCodeUnderstand},
		{"memory_recall", TaskTypeMemoryRecall},
		{"evidence_audit", TaskTypeEvidenceAudit},
		{"general", TaskTypeGeneral},
		{"unknown", TaskTypeGeneral},
		{"", TaskTypeGeneral},
	}
	for _, tc := range cases {
		got := NormalizeTaskType(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeTaskType(%q)=%s want %s", tc.input, got, tc.want)
		}
	}
}

func TestPlanLambda_AccuracyConstraint_Math(t *testing.T) {
	t.Parallel()

	// Verify: when k* has room to grow, perPhaseAccuracy^depth >= accuracyTarget.
	// When k* hits the MaxK cap before the constraint is met, the plan degrades gracefully.
	cfg := LambdaConfig{
		ContextBudget:    10,
		MaxK:             8,
		CSearch:          1.0,
		CInspect:         3.0,
		PerPhaseAccuracy: 0.6,
		AccuracyTarget:   0.5,
	}
	for _, n := range []int{20, 50, 100, 500, 1000} {
		plan := PlanLambda(TaskTypeCodeLocate, n, cfg)
		actualAccuracy := math.Pow(cfg.PerPhaseAccuracy, float64(plan.Depth))
		if plan.Depth > 0 && n > cfg.ContextBudget {
			// When k* is at or near MaxK, accuracy may not reach the target.
			// That's acceptable -- the constraint is best-effort, not a hard guarantee.
			if plan.KStar < cfg.MaxK && actualAccuracy < cfg.AccuracyTarget {
				t.Errorf("n=%d k_star=%d (below MaxK=%d) depth=%d accuracy=%.3f < target=%.3f -- constraint should have pushed k* higher",
					n, plan.KStar, cfg.MaxK, plan.Depth, actualAccuracy, cfg.AccuracyTarget)
			}
		}
	}
}

func TestQueryVariantsPreferContentTermsBeforeTail(t *testing.T) {
	t.Parallel()

	prompt := "Research the LongCoT eval implementation including command wiring, official loader and verifier integration, condition handling, artifact output, tests, and how bridge pieces connect. Return key files and a concise grounded summary."
	variants := queryVariants(prompt, 2)
	if len(variants) != 2 {
		t.Fatalf("len=%d want 2", len(variants))
	}
	if strings.Contains(variants[1], "concise grounded summary") {
		t.Fatalf("second variant should not be the generic instruction tail: %q", variants[1])
	}
	if !strings.Contains(strings.ToLower(variants[1]), "verifier") {
		t.Fatalf("second variant should preserve content terms, got %q", variants[1])
	}
}

func TestSearchToolForTask(t *testing.T) {
	if got := SearchToolForTask(TaskTypeMemoryRecall); got != "gather_context" {
		t.Fatalf("got=%s want gather_context", got)
	}
	if got := SearchToolForTask(TaskTypeCodeLocate); got != "gather_context" {
		t.Fatalf("got=%s want gather_context", got)
	}
	if got := SearchToolForTask(TaskTypeGeneral); got != "gather_context" {
		t.Fatalf("got=%s want gather_context", got)
	}
}

func TestLambdaRunnerEphemeralSkill(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"source":"func Solve(input map[string]any) map[string]any { return map[string]any{\"ok\": true, \"answer\": \"solution = 42\"} }",
						"input":{}
					}`,
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer server.Close()

	runner := LambdaRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LambdaConfig{
			EphemeralSkills:     true,
			ExtractSolutionLine: true,
			LLM: LLMConfig{
				Provider: "lmstudio",
				BaseURL:  server.URL + "/v1",
				AuthMode: "none",
				Model:    "test-model",
				Timeout:  5 * time.Second,
			},
		},
	}
	result, err := runner.Run(context.Background(), Task{Prompt: "compute 17+25"}, Environment{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Answer != "solution = 42" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if result.Metadata["lambda_mode"] != "ephemeral_helper" {
		t.Fatalf("lambda_mode=%v", result.Metadata["lambda_mode"])
	}
}

func TestLambdaRunnerEphemeralSkillSanitizesFinalAnswer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role": "assistant",
					"content": `{
						"source":"func Solve(input map[string]any) map[string]any { return map[string]any{\"answer\": \"<|channel>thought notes<channel|>solution = 42\"} }",
						"input":{}
					}`,
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 7, "total_tokens": 19},
		})
	}))
	defer server.Close()

	runner := LambdaRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LambdaConfig{
			EphemeralSkills:     true,
			ExtractSolutionLine: true,
			LLM: LLMConfig{
				Provider: "lmstudio",
				BaseURL:  server.URL + "/v1",
				AuthMode: "none",
				Model:    "test-model",
				Timeout:  5 * time.Second,
			},
		},
	}
	result, err := runner.Run(context.Background(), Task{Prompt: "compute 17+25"}, Environment{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Answer != "solution = 42" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if result.Metadata["output_sanitization"] == nil {
		t.Fatalf("missing output_sanitization metadata: %#v", result.Metadata)
	}
}

func TestLambdaLeafSanitizesJudgeAnswer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "<|channel>thought\nprivate<channel|>Leaf answer",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 21, "completion_tokens": 8, "total_tokens": 29},
		})
	}))
	defer server.Close()

	runner := LambdaRunner{
		Tools: lambdaFakeToolExecutor{},
		Config: LambdaConfig{
			LLM: LLMConfig{
				Provider: "lmstudio",
				BaseURL:  server.URL + "/v1",
				AuthMode: "none",
				Model:    "test-model",
				Timeout:  5 * time.Second,
			},
		},
	}
	result, err := runner.leaf(context.Background(), Task{Prompt: "find answer"}, Environment{}, LambdaPlan{
		TaskType: TaskTypeGeneral,
		TauStar:  1,
	})
	if err != nil {
		t.Fatalf("leaf() error = %v", err)
	}
	if result.Answer != "Leaf answer" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if result.Metadata["output_sanitization"] == nil {
		t.Fatalf("missing output_sanitization metadata: %#v", result.Metadata)
	}
	if got := intFromAny(result.Metadata["parent_total_tokens"]); got != 29 {
		t.Fatalf("parent_total_tokens=%d want 29", got)
	}
}

func TestLambdaReduceSanitizesSynthesizedAnswer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "<|tool_call>{\"name\":\"noop\"}<tool_call|><|channel>thought\nscratch<channel|>Synthesized answer",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	runner := LambdaRunner{
		Config: LambdaConfig{
			LLM: LLMConfig{
				Provider: "lmstudio",
				BaseURL:  server.URL + "/v1",
				AuthMode: "none",
				Model:    "test-model",
				Timeout:  5 * time.Second,
			},
		},
	}
	result, err := runner.reduce(context.Background(), Task{Prompt: "merge"}, LambdaPlan{
		ComposeOp: ComposeSynthesize,
	}, []Result{{Answer: "partial"}})
	if err != nil {
		t.Fatalf("reduce() error = %v", err)
	}
	if result.Answer != "Synthesized answer" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if result.Metadata["output_sanitization"] == nil {
		t.Fatalf("missing output_sanitization metadata: %#v", result.Metadata)
	}
}

func TestDecodeHelperSolveDraftSkipsTrailingMarkdown(t *testing.T) {
	t.Parallel()

	raw := "```json\n{\"source\":\"func Solve(input map[string]any) map[string]any { return map[string]any{\\\"answer\\\": \\\"solution = 1\\\"} }\",\"input\":{}}\n```\nextra"
	var draft lambdaEphemeralDraft
	if err := decodeHelperSolveDraft(raw, &draft); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if !strings.Contains(draft.Source, "Solve") {
		t.Fatalf("source=%q", draft.Source)
	}
}

func TestLambdaLLMChatConfigCarriesRuntimeFields(t *testing.T) {
	t.Parallel()

	got := lambdaLLMChatConfig(LLMConfig{
		Provider:      "lmstudio",
		APIKey:        "local-key",
		BaseURL:       "http://localhost:1234/v1",
		AuthMode:      "header",
		AuthHeader:    "X-Test-Key",
		AuthPrefix:    "Token ",
		Model:         "local-model",
		Timeout:       7 * time.Second,
		MaxTokens:     1234,
		Temperature:   0.25,
		MaxIterations: 3,
	})

	if got.Provider != "lmstudio" {
		t.Fatalf("provider=%q want lmstudio", got.Provider)
	}
	if got.APIKey != "local-key" {
		t.Fatalf("api key was not propagated")
	}
	if got.BaseURL != "http://localhost:1234/v1" {
		t.Fatalf("base_url=%q", got.BaseURL)
	}
	if got.AuthMode != "header" || got.AuthHeader != "X-Test-Key" || got.AuthPrefix != "Token " {
		t.Fatalf("auth fields not propagated: mode=%q header=%q prefix=%q", got.AuthMode, got.AuthHeader, got.AuthPrefix)
	}
	if got.Model != "local-model" {
		t.Fatalf("model=%q", got.Model)
	}
	if got.Timeout != 7*time.Second {
		t.Fatalf("timeout=%s", got.Timeout)
	}
	if got.MaxTokens != 1234 {
		t.Fatalf("max_tokens=%d", got.MaxTokens)
	}
	if got.Temperature != 0.25 {
		t.Fatalf("temperature=%.2f", got.Temperature)
	}
	if got.MaxIterations != 3 {
		t.Fatalf("max_iterations=%d", got.MaxIterations)
	}
}

func TestLambdaExecutePhiPreservesSplitOrder(t *testing.T) {
	t.Parallel()

	query := "Where is LambdaRunner implemented in foxctl?"
	variants := queryVariants(query, 3)
	runner := LambdaRunner{
		Config: LambdaConfig{
			LLM: LLMConfig{
				Provider: "lambda-test-no-provider",
				AuthMode: "header",
			},
		},
		Tools: lambdaFakeToolExecutor{
			delays: map[string]time.Duration{
				variants[0]: 30 * time.Millisecond,
				variants[1]: 5 * time.Millisecond,
				variants[2]: 0,
			},
		},
	}

	result, err := runner.executePhi(context.Background(), Task{Prompt: query}, Environment{}, LambdaPlan{
		TaskType:  TaskTypeGeneral,
		ComposeOp: ComposeUnion,
		KStar:     3,
		TauStar:   1,
		Depth:     1,
	})
	if err != nil {
		t.Fatalf("executePhi error: %v", err)
	}

	last := -1
	for _, variant := range variants {
		idx := strings.Index(result.Answer, variant)
		if idx < 0 {
			t.Fatalf("answer missing variant %q:\n%s", variant, result.Answer)
		}
		if idx < last {
			t.Fatalf("variant %q appeared out of split order:\n%s", variant, result.Answer)
		}
		last = idx
	}
}

func TestExtractCandidatePathsFromTypedToolPayload(t *testing.T) {
	t.Parallel()

	type typedFile struct {
		Path string `json:"path"`
	}
	result := map[string]any{
		"files": []typedFile{
			{Path: "cmd/foxctl/cmd/eval_longcot.go"},
			{Path: "internal/tooling/evals/longcotbridge/verifier.go"},
		},
	}

	got := extractCandidatePaths(result, "")
	want := []string{
		"cmd/foxctl/cmd/eval_longcot.go",
		"internal/tooling/evals/longcotbridge/verifier.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths=%v want %v", got, want)
	}
}

func TestExtractCandidateEvidenceRefsIncludesLoadRefs(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"path_set": map[string]any{
			"must": []any{
				map[string]any{"load_ref": "path:internal/rlm/env/tools.go"},
				map[string]any{"type": "path", "ref": "internal/context/contextengine/context_gather.go"},
				map[string]any{"ref": "internal/context/contextengine/bare-ref-should-not-load.go"},
			},
		},
		"load_refs": []any{"path:internal/rlm/env/adapter.go"},
	}

	got := extractCandidateEvidenceRefs(result)
	want := []string{
		"path:internal/rlm/env/tools.go",
		"path:internal/context/contextengine/context_gather.go",
		"path:internal/rlm/env/adapter.go",
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("refs=%v want %v", got, want)
	}
}

func TestLambdaSearchArgsUsesGatherPayloadAndAnswerSurface(t *testing.T) {
	t.Parallel()

	task := Task{
		Prompt: "fallback query",
		Metadata: map[string]any{
			"gather_context_payload": map[string]any{
				"query":     "runtime-selected query",
				"task_type": "file_locate",
			},
		},
	}

	got := lambdaSearchArgs(task, LambdaPlan{TaskType: TaskTypeCodeLocate, TauStar: 4})
	if got["query"] != "runtime-selected query" {
		t.Fatalf("query=%v", got["query"])
	}
	if got["task_type"] != "file_locate" {
		t.Fatalf("task_type=%v", got["task_type"])
	}
	if got["response_mode"] != "answer_surface" {
		t.Fatalf("response_mode=%v", got["response_mode"])
	}
	if got["limit"] != 4 {
		t.Fatalf("limit=%v", got["limit"])
	}
}

func TestExplicitLambdaTaskTypeUsesGatherPayload(t *testing.T) {
	t.Parallel()

	for _, taskTypeName := range []string{"file_locate", "symbol_inspect", "registration_trace"} {
		taskTypeName := taskTypeName
		t.Run(taskTypeName, func(t *testing.T) {
			t.Parallel()
			taskType, source := explicitLambdaTaskType(Task{Metadata: map[string]any{
				"gather_context_payload": map[string]any{"task_type": taskTypeName},
			}})
			if taskType != TaskTypeCodeLocate || source != "gather_context_payload" {
				t.Fatalf("taskType=%s source=%s", taskType, source)
			}
		})
	}
	for _, taskTypeName := range []string{"execution_trace", "change_impact", "architecture_map", "subsystem_map", "integration_surface"} {
		taskTypeName := taskTypeName
		t.Run(taskTypeName, func(t *testing.T) {
			t.Parallel()
			taskType, source := explicitLambdaTaskType(Task{Metadata: map[string]any{
				"gather_context_payload": map[string]any{"task_type": taskTypeName},
			}})
			if taskType != TaskTypeCodeUnderstand || source != "gather_context_payload" {
				t.Fatalf("taskType=%s source=%s", taskType, source)
			}
		})
	}
}

func TestLambdaShouldUseSingleGatherSurfaceForCodeLocateAnswerSurface(t *testing.T) {
	t.Parallel()

	task := Task{Metadata: map[string]any{
		"gather_context_payload": map[string]any{
			"task_type":     "file_locate",
			"response_mode": "answer_surface",
		},
	}}
	if !lambdaShouldUseSingleGatherSurface(task, TaskTypeCodeLocate) {
		t.Fatal("expected single gather surface for code locate answer_surface payload")
	}
	if lambdaShouldUseSingleGatherSurface(task, TaskTypeCodeUnderstand) {
		t.Fatal("did not expect single gather surface for code understand")
	}
	task.Metadata["gather_context_payload"].(map[string]any)["task_type"] = "execution_trace"
	if lambdaShouldUseSingleGatherSurface(task, TaskTypeCodeLocate) {
		t.Fatal("did not expect single gather surface for dependency-sensitive payload")
	}
	if lambdaShouldUseSingleGatherSurface(task, TaskTypeCodeUnderstand) {
		t.Fatal("did not expect single gather surface for dependency-sensitive task")
	}
}

func TestNormalizePlanModeAcceptsLambdaAlias(t *testing.T) {
	t.Parallel()

	if got := NormalizePlanMode("lambda"); got != PlanModeLambda {
		t.Fatalf("NormalizePlanMode(lambda)=%s want %s", got, PlanModeLambda)
	}
	if got, err := resolveRunPlanMode("lambda"); err != nil || got != PlanModeLambda {
		t.Fatalf("resolveRunPlanMode(lambda)=%s err=%v want %s", got, err, PlanModeLambda)
	}
}

func TestLambdaAnswerFromAnswerSurface(t *testing.T) {
	t.Parallel()

	answer, ok := lambdaAnswerFromAnswerSurface(map[string]any{
		"schema_version": "context_answer_surface/v2",
		"answerable":     true,
		"certificate": map[string]any{
			"status":               "certified",
			"required_evidence_ok": true,
			"checks": []any{
				map[string]any{"name": "selected_refs_loadable", "status": "pass"},
			},
		},
		"answer_seed": map[string]any{
			"paths": []any{"internal/rlm/env/code_search_ensemble.go"},
			"facts": []any{"code_search_ensemble is implemented in the RLM env package."},
		},
	}, nil)
	if !ok {
		t.Fatal("lambdaAnswerFromAnswerSurface() ok=false")
	}
	var out structuredLambdaAnswerForTest
	if err := json.Unmarshal([]byte(answer), &out); err != nil {
		t.Fatalf("answer JSON error: %v", err)
	}
	if strings.Join(out.Paths, "\n") != "internal/rlm/env/code_search_ensemble.go" {
		t.Fatalf("paths=%v", out.Paths)
	}
	if len(out.Facts) != 1 {
		t.Fatalf("facts=%v", out.Facts)
	}
}

func TestLambdaAnswerFromAnswerSurfaceRejectsUntrustedSurface(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string]map[string]any{
		"not_answerable": {
			"schema_version": "context_answer_surface/v2",
			"answerable":     false,
			"answer_seed":    map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
		"failed_certificate": {
			"schema_version": "context_answer_surface/v2",
			"answerable":     true,
			"certificate":    map[string]any{"status": "failed"},
			"answer_seed":    map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
		"required_evidence_missing": {
			"schema_version": "context_answer_surface/v2",
			"answerable":     true,
			"certificate":    map[string]any{"status": "partial", "required_evidence_ok": false},
			"answer_seed":    map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
		"partial_certificate": {
			"schema_version": "context_answer_surface/v2",
			"answerable":     true,
			"certificate":    map[string]any{"status": "partial", "required_evidence_ok": true},
			"answer_seed":    map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
		"coverage_missing": {
			"schema_version": "context_answer_surface/v2",
			"answerable":     true,
			"certificate":    map[string]any{"status": "certified", "required_evidence_ok": true},
			"coverage_report": map[string]any{
				"missing": []any{"certifier"},
			},
			"answer_seed": map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
		"copy_seed_disabled": {
			"schema_version":  "context_answer_surface/v2",
			"answerable":      true,
			"certificate":     map[string]any{"status": "certified", "required_evidence_ok": true, "checks": []any{map[string]any{"name": "selected_refs_loadable", "status": "pass"}}},
			"answer_contract": map[string]any{"copy_answer_seed": false},
			"answer_seed":     map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
		"selected_refs_not_loadable": {
			"schema_version": "context_answer_surface/v2",
			"answerable":     true,
			"certificate": map[string]any{
				"status":               "certified",
				"required_evidence_ok": true,
				"checks":               []any{map[string]any{"name": "selected_refs_loadable", "status": "fail"}},
			},
			"answer_seed": map[string]any{"paths": []any{"internal/rlm/env/code_search_ensemble.go"}},
		},
	} {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if answer, ok := lambdaAnswerFromAnswerSurface(payload, nil); ok {
				t.Fatalf("lambdaAnswerFromAnswerSurface() ok=true answer=%s", answer)
			}
		})
	}
}

func TestLambdaContextGraphTrusted(t *testing.T) {
	t.Parallel()

	trusted := map[string]any{
		"confidence": map[string]any{
			"overall":             0.9,
			"completeness":        "high",
			"trusted_for_proceed": true,
		},
	}
	if !lambdaContextGraphTrusted(trusted) {
		t.Fatal("expected high-confidence graph to be trusted")
	}
	for name, payload := range map[string]map[string]any{
		"low_confidence": {
			"confidence": map[string]any{"overall": 0.5, "completeness": "low", "trusted_for_proceed": false},
		},
		"missing_gap": {
			"confidence": map[string]any{"overall": 0.9, "completeness": "high"},
			"missing":    []any{map[string]any{"kind": "index_stale"}},
		},
	} {
		payload := payload
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if lambdaContextGraphTrusted(payload) {
				t.Fatalf("expected graph %s to be untrusted", name)
			}
		})
	}
}

func TestLambdaLeafDoesNotSkipJudgeWhenRequiredGraphIsWeak(t *testing.T) {
	t.Parallel()

	runner := LambdaRunner{
		Config: LambdaConfig{LLM: LLMConfig{Provider: "lambda-test-no-provider"}},
		Tools:  lambdaGraphGateToolExecutor{},
	}
	result, err := runner.leaf(context.Background(), Task{
		Prompt: "Trace execution through the service.",
		Metadata: map[string]any{
			"gather_context_payload": map[string]any{
				"query":         "Trace execution through the service.",
				"task_type":     "execution_trace",
				"response_mode": "answer_surface",
			},
		},
	}, Environment{}, LambdaPlan{TaskType: TaskTypeCodeUnderstand, TauStar: 2})
	if err != nil {
		t.Fatalf("leaf: %v", err)
	}
	if result.Metadata["lambda_answer_surface"] == true {
		t.Fatalf("expected weak graph to prevent answer-surface skip: metadata=%v", result.Metadata)
	}
	if result.Metadata["lambda_context_graph_trusted"] != false {
		t.Fatalf("graph trust metadata=%v", result.Metadata)
	}
	if result.Metadata["lambda_judge_error"] == nil {
		t.Fatalf("expected fallback judge path with no-provider LLM: metadata=%v", result.Metadata)
	}
}

func TestLambdaReduceSkipsLLMForTrustedAnswerSurfacePartials(t *testing.T) {
	t.Parallel()

	runner := LambdaRunner{}
	partials := []Result{
		{
			Answer: `{"summary":"one","paths":["internal/rlm/env/code_search_ensemble.go"],"facts":["code search fact"]}`,
			Metadata: map[string]any{
				"lambda_answer_surface":               true,
				"gather_context_answer_seed_paths":    []string{"internal/rlm/env/code_search_ensemble.go"},
				"gather_context_certificate_statuses": []string{"certified"},
				"parent_input_tokens":                 0,
				"parent_output_tokens":                0,
				"parent_total_tokens":                 0,
			},
			RetrievedPaths: []string{"internal/rlm/env/code_search_ensemble.go"},
		},
		{
			Answer: `{"summary":"two","paths":["internal/rlm/env/adapter.go"],"facts":["adapter fact"]}`,
			Metadata: map[string]any{
				"lambda_answer_surface":               true,
				"gather_context_answer_seed_paths":    []string{"internal/rlm/env/adapter.go"},
				"gather_context_certificate_statuses": []string{"certified"},
			},
			RetrievedPaths: []string{"internal/rlm/env/adapter.go"},
		},
	}

	result, err := runner.reduce(context.Background(), Task{Prompt: "Where is code search wired?"}, LambdaPlan{ComposeOp: ComposeRerank}, partials)
	if err != nil {
		t.Fatalf("reduce() error = %v", err)
	}
	if result.Metadata["lambda_reduce_answer_surface"] != true {
		t.Fatalf("lambda_reduce_answer_surface=%v", result.Metadata["lambda_reduce_answer_surface"])
	}
	if got := result.Metadata["parent_total_tokens"]; got != 0 {
		t.Fatalf("parent_total_tokens=%v, want 0", got)
	}
	var out structuredLambdaAnswerForTest
	if err := json.Unmarshal([]byte(result.Answer), &out); err != nil {
		t.Fatalf("answer JSON error: %v\n%s", err, result.Answer)
	}
	wantPaths := "internal/rlm/env/code_search_ensemble.go\ninternal/rlm/env/adapter.go"
	if strings.Join(out.Paths, "\n") != wantPaths {
		t.Fatalf("paths=%v", out.Paths)
	}
	if len(out.Facts) != 2 {
		t.Fatalf("facts=%v", out.Facts)
	}
}

func TestSelectLambdaRetrievedPathsPrefersAnswerCitations(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"cmd/foxctl/cmd/eval_longcot.go",
		"docs/archive/source/cmd/foxctl_viewer/util.go",
		"internal/tooling/evals/longcotbridge/verifier.go",
	}
	answer := "Implemented in `cmd/foxctl/cmd/eval_longcot.go` and `internal/tooling/evals/longcotbridge/verifier.go`."

	got, answerPaths, source := selectLambdaRetrievedPaths(answer, candidates, "")
	want := []string{
		"cmd/foxctl/cmd/eval_longcot.go",
		"internal/tooling/evals/longcotbridge/verifier.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths=%v want %v", got, want)
	}
	if strings.Join(answerPaths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("answer_paths=%v want %v", answerPaths, want)
	}
	if source != "answer_paths" {
		t.Fatalf("source=%q want answer_paths", source)
	}
}

func TestSelectLambdaRetrievedPathsRecognizesCommonRepoExtensions(t *testing.T) {
	t.Parallel()

	answer := "Relevant paths: `web/src/AuthPanel.tsx`, `scripts/solve.py`, and `lib/jido/agent_server.ex`."
	got, answerPaths, source := selectLambdaRetrievedPaths(answer, nil, "")
	want := []string{
		"web/src/AuthPanel.tsx",
		"scripts/solve.py",
		"lib/jido/agent_server.ex",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths=%v want %v", got, want)
	}
	if strings.Join(answerPaths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("answer_paths=%v want %v", answerPaths, want)
	}
	if source != "answer_paths" {
		t.Fatalf("source=%q want answer_paths", source)
	}
}

func TestSelectLambdaRetrievedPathsFallsBackToCandidates(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"cmd/foxctl/cmd/eval_longcot.go",
		"docs/archive/source/cmd/foxctl_viewer/util.go",
	}
	got, answerPaths, source := selectLambdaRetrievedPaths("No explicit file citation.", candidates, "")
	if strings.Join(got, "\n") != strings.Join(candidates, "\n") {
		t.Fatalf("paths=%v want %v", got, candidates)
	}
	if len(answerPaths) != 0 {
		t.Fatalf("answer_paths=%v want empty", answerPaths)
	}
	if source != "candidate_paths_fallback" {
		t.Fatalf("source=%q want candidate_paths_fallback", source)
	}
}

func TestLambdaReducePreservesGatherSurfaceMetadata(t *testing.T) {
	t.Parallel()

	runner := LambdaRunner{}
	result, err := runner.reduce(context.Background(), Task{Prompt: "merge"}, LambdaPlan{
		ComposeOp: ComposeUnion,
	}, []Result{
		{
			Answer:         "partial",
			RetrievedPaths: []string{"internal/rlm/env/code_search_ensemble.go"},
			Metadata: map[string]any{
				"candidate_paths":                     []string{"internal/rlm/env/code_search_ensemble.go"},
				"gather_context_selected_paths":       []string{"internal/rlm/env/code_search_ensemble.go"},
				"gather_context_answer_seed_paths":    []string{"internal/rlm/env/code_search_ensemble.go"},
				"gather_context_path_set_must":        []string{"internal/rlm/env/code_search_ensemble.go"},
				"gather_context_certificate_statuses": []string{"certified"},
			},
		},
	})
	if err != nil {
		t.Fatalf("reduce() error = %v", err)
	}
	for _, key := range []string{
		"gather_context_selected_paths",
		"gather_context_answer_seed_paths",
		"gather_context_path_set_must",
		"gather_context_certificate_statuses",
	} {
		if got := stringSliceFromAny(result.Metadata[key]); len(got) != 1 {
			t.Fatalf("%s=%v want one value", key, result.Metadata[key])
		}
	}
}

type lambdaFakeToolExecutor struct {
	delays map[string]time.Duration
}

type lambdaGraphGateToolExecutor struct{}

type structuredLambdaAnswerForTest struct {
	Paths []string `json:"paths"`
	Facts []string `json:"facts"`
}

func (lambdaGraphGateToolExecutor) Execute(_ context.Context, name string, _ json.RawMessage) (map[string]any, error) {
	switch name {
	case "gather_context":
		return map[string]any{
			"schema_version": "context_answer_surface/v2",
			"answerable":     true,
			"certificate": map[string]any{
				"status":               "certified",
				"required_evidence_ok": true,
				"checks":               []any{map[string]any{"name": "selected_refs_loadable", "status": "pass"}},
			},
			"answer_contract": map[string]any{"copy_answer_seed": true},
			"answer_seed":     map[string]any{"paths": []any{"internal/service.go"}},
			"path_set":        map[string]any{"must": []any{map[string]any{"path": "internal/service.go", "load_ref": "path:internal/service.go"}}},
		}, nil
	case "expand_context_graph":
		return map[string]any{
			"confidence": map[string]any{
				"overall":             0.45,
				"completeness":        "low",
				"trusted_for_proceed": false,
			},
			"missing": []any{map[string]any{"kind": "index_stale"}},
		}, nil
	case "load_evidence_ref":
		return map[string]any{"content": "package service"}, nil
	default:
		return map[string]any{}, nil
	}
}

func (f lambdaFakeToolExecutor) Execute(ctx context.Context, _ string, args json.RawMessage) (map[string]any, error) {
	var input struct {
		Query string `json:"query"`
	}
	_ = json.Unmarshal(args, &input)
	if delay := f.delays[input.Query]; delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return map[string]any{
		"content": "evidence for " + input.Query,
		"query":   input.Query,
	}, nil
}
