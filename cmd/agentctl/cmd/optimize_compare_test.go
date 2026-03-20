package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	configpkg "github.com/jkatigb/agentctl/internal/platform/config"
)

func TestBuildPromptComparisonUserPrompt(t *testing.T) {
	t.Parallel()

	got := buildPromptComparisonUserPrompt("Summarize the task", "Repo context")
	if !strings.Contains(got, "Context:\nRepo context") {
		t.Fatalf("missing context block: %q", got)
	}
	if !strings.Contains(got, "Question:\nSummarize the task") {
		t.Fatalf("missing question block: %q", got)
	}
}

func TestRunPromptVariantComparisons(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		system := ""
		if len(req.Messages) > 0 {
			system = req.Messages[0].Content
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": req.Model + "::" + system,
					},
				},
			},
		})
	}))
	defer srv.Close()

	results, err := runPromptVariantComparisons(context.Background(), promptComparisonRequest{
		Variants: []optimization.PromptVariant{
			{
				ID:             "pv-1",
				WorkspaceID:    "ws-test",
				AgentRole:      "coder",
				Mode:           "gepa",
				OriginalPrompt: "Original",
				Prompt:         "Optimized system prompt",
				OriginalScore:  0.3,
				OptimizedScore: 0.7,
			},
		},
		Question:    "Optimized system prompt",
		Context:     "",
		Provider:    "lmstudio",
		BaseURL:     srv.URL,
		APIKey:      "lm-studio",
		Models:      []string{"model-a", "model-b"},
		Timeout:     5 * time.Second,
		MaxTokens:   128,
		Temperature: 0.1,
		Scoring: promptComparisonScoring{
			ExpectedSubstrings: []string{"Optimized system prompt"},
			PassThreshold:      0.8,
		},
	})
	if err != nil {
		t.Fatalf("runPromptVariantComparisons() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("len(results)=%d want 2", len(results))
	}
	for _, result := range results {
		if result.Error != "" {
			t.Fatalf("unexpected error for model %s: %s", result.Model, result.Error)
		}
		if result.Provider != "lmstudio" {
			t.Fatalf("provider=%q want lmstudio", result.Provider)
		}
		if !strings.Contains(result.Output, result.Model+"::Optimized system prompt") {
			t.Fatalf("output=%q missing model/system prompt", result.Output)
		}
		if result.Score < 0.8 {
			t.Fatalf("score=%v want >= 0.8", result.Score)
		}
		if !result.Passed {
			t.Fatalf("expected comparison to pass for model %s", result.Model)
		}
	}
}

func TestAggregatePromptVariantComparisons(t *testing.T) {
	t.Parallel()

	aggregates := aggregatePromptVariantComparisons([]promptVariantComparison{
		{VariantID: "v1", AgentRole: "coder", Mode: "gepa", Model: "a", DurationMS: 10, Score: 1, Passed: true, ScoreDelta: 0.2},
		{VariantID: "v1", AgentRole: "coder", Mode: "gepa", Model: "b", DurationMS: 20, Score: 0.5, Passed: false, ScoreDelta: 0.2},
		{VariantID: "v2", AgentRole: "coder", Mode: "copro", Model: "a", DurationMS: 30, Score: 0.9, Passed: true, ScoreDelta: 0.1},
		{VariantID: "v2", AgentRole: "coder", Mode: "copro", Model: "b", DurationMS: 30, Score: 0.8, Passed: true, ScoreDelta: 0.1},
	})

	if len(aggregates) != 2 {
		t.Fatalf("len(aggregates)=%d want 2", len(aggregates))
	}
	if aggregates[0].VariantID != "v2" {
		t.Fatalf("top ranked variant=%q want v2", aggregates[0].VariantID)
	}
	if aggregates[0].MeanScore <= aggregates[1].MeanScore {
		t.Fatalf("mean score ordering incorrect: %+v", aggregates)
	}
	if aggregates[1].PassCount != 1 {
		t.Fatalf("v1 pass_count=%d want 1", aggregates[1].PassCount)
	}
}

func TestRunPromptVariantComparisonsFallsBackToConfiguredTarget(t *testing.T) {
	t.Parallel()

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"upstream unavailable"}}`, http.StatusBadGateway)
	}))
	defer remote.Close()

	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": req.Model + "::fallback"}},
			},
		})
	}))
	defer local.Close()

	results, err := runPromptVariantComparisons(context.Background(), promptComparisonRequest{
		Variants: []optimization.PromptVariant{{
			ID:        "pv-1",
			AgentRole: "coder",
			Mode:      "gepa",
			Prompt:    "Prompt",
		}},
		Question: "Q",
		Provider: "openrouter",
		BaseURL:  remote.URL,
		APIKey:   "key",
		Models:   []string{"openai/gpt-5.4-nano"},
		Fallback: &promptComparisonTargetConfig{
			Provider: "lmstudio",
			BaseURL:  local.URL,
			APIKey:   "lm-studio",
			Models:   []string{"liquid/lfm2.5-1.2b"},
		},
		Timeout: 5 * time.Second,
		Scoring: promptComparisonScoring{PassThreshold: 0.1},
	})
	if err != nil {
		t.Fatalf("runPromptVariantComparisons() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].Provider != "lmstudio" {
		t.Fatalf("provider=%q want lmstudio", results[0].Provider)
	}
	if results[0].Model != "liquid/lfm2.5-1.2b" {
		t.Fatalf("model=%q want fallback model", results[0].Model)
	}
	if !results[0].FailedOver {
		t.Fatal("expected failed_over=true")
	}
	if results[0].Error != "" {
		t.Fatalf("unexpected error: %s", results[0].Error)
	}
}

func TestResolvePromptComparisonTargetsDefaultsToRemoteBest(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{
		LLM: configpkg.LLMSettings{
			OpenRouterAPIKey: "openrouter-key",
		},
	}
	primary, fallback, err := resolvePromptComparisonTargets(cfg, "", "", "", nil)
	if err != nil {
		t.Fatalf("resolvePromptComparisonTargets() error = %v", err)
	}
	if primary.Provider != "openrouter" || len(primary.Models) != 1 || primary.Models[0] != "openai/gpt-5.4-nano" {
		t.Fatalf("unexpected primary target: %+v", primary)
	}
	if fallback == nil || fallback.Provider != "lmstudio" || len(fallback.Models) != 1 || fallback.Models[0] != "liquid/lfm2.5-1.2b" {
		t.Fatalf("unexpected fallback target: %+v", fallback)
	}
}

func TestPromptOptimizerBackendLabel(t *testing.T) {
	t.Parallel()

	if got := promptOptimizerBackendLabel("gepa", "auto"); got != "dspy-go" {
		t.Fatalf("gepa auto backend=%q want dspy-go", got)
	}
	if got := promptOptimizerBackendLabel("copro", "auto"); got != "agentctl" {
		t.Fatalf("copro auto backend=%q want agentctl", got)
	}
	if got := promptOptimizerBackendLabel("gepa", "agentctl"); got != "agentctl" {
		t.Fatalf("explicit backend=%q want agentctl", got)
	}
}

func TestLoadPromptEvalCases(t *testing.T) {
	t.Parallel()

	path := t.TempDir() + "/eval.jsonl"
	body := `{"question":"Q1","context":"C1","target_response":"A1","category":"coder_impl"}
{"question":"Q2","target_response":"A2"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write eval file: %v", err)
	}

	cases, err := loadPromptEvalCases(path)
	if err != nil {
		t.Fatalf("loadPromptEvalCases: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("len(cases)=%d want 2", len(cases))
	}
	if cases[0].Question != "Q1" || cases[1].Question != "Q2" {
		t.Fatalf("unexpected cases: %+v", cases)
	}
}

func TestSamplePromptEvalCasesDeterministic(t *testing.T) {
	t.Parallel()

	evalCases := []promptEvalCase{
		{ID: "a", Question: "A"},
		{ID: "b", Question: "B"},
		{ID: "c", Question: "C"},
		{ID: "d", Question: "D"},
		{ID: "e", Question: "E"},
	}

	first := samplePromptEvalCases(evalCases, 3, 42, 0)
	second := samplePromptEvalCases(evalCases, 3, 42, 0)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("unexpected sample sizes: %d %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("non-deterministic sample at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}

	seen := map[string]struct{}{}
	for _, item := range first {
		if _, ok := seen[item.ID]; ok {
			t.Fatalf("duplicate eval case in sample: %q", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
}

func TestResolvePromptComparisonVariantsByRole(t *testing.T) {
	t.Parallel()

	store, err := optimization.OpenPromptVariantStore(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("OpenPromptVariantStore: %v", err)
	}
	defer store.Close() //nolint:errcheck

	inputs := []optimization.PromptVariant{
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "coder",
			Mode:           "gepa",
			OriginalPrompt: "base",
			Prompt:         "p1",
		},
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "coder",
			Mode:           "gepa",
			OriginalPrompt: "base",
			Prompt:         "p2",
		},
		{
			WorkspaceID:    "ws-test",
			AgentRole:      "reviewer",
			Mode:           "gepa",
			OriginalPrompt: "base",
			Prompt:         "p3",
		},
	}
	for _, input := range inputs {
		if _, err := store.Save(context.Background(), input); err != nil {
			t.Fatalf("Save variant: %v", err)
		}
	}

	variants, err := resolvePromptComparisonVariants(context.Background(), store, "ws-test", nil, "coder", "", 2)
	if err != nil {
		t.Fatalf("resolvePromptComparisonVariants: %v", err)
	}
	if len(variants) != 2 {
		t.Fatalf("len(variants)=%d want 2", len(variants))
	}
	for _, variant := range variants {
		if variant.AgentRole != "coder" {
			t.Fatalf("agent_role=%q want coder", variant.AgentRole)
		}
	}
}

func TestScorePromptComparisonAgainstCase(t *testing.T) {
	t.Parallel()

	result := promptVariantComparison{Output: "The queue worker is running and processing annotation jobs."}
	score := scorePromptComparisonAgainstCase(result, promptComparisonScoring{PassThreshold: 0.1}, promptEvalCase{
		TargetResponse: "The worker is running and processing jobs.",
	})
	if score <= 0.3 {
		t.Fatalf("score=%v want > 0.3", score)
	}
}
