package rlm

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
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

func TestSelectLambdaRetrievedPathsPrefersAnswerCitations(t *testing.T) {
	t.Parallel()

	candidates := []string{
		"cmd/foxctl/cmd/eval_longcot.go",
		"archive/cmd/foxctl_viewer/util.go",
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
		"archive/cmd/foxctl_viewer/util.go",
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

type lambdaFakeToolExecutor struct {
	delays map[string]time.Duration
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
