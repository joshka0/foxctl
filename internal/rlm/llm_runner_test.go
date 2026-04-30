package rlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/engine"
)

type fakeLLMToolExecutor struct{}

func (fakeLLMToolExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func TestLLMRunnerUsesOpenAICompatibleChatPath(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/chat/completions" {
			t.Fatalf("path=%q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "LLM inspection summary",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{
			Name:        "retrieve_code",
			Description: "code retrieval",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			ReadOnly:    true,
		}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Answer != "LLM inspection summary" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if got := result.Metadata["provider"]; got != "lmstudio" {
		t.Fatalf("provider=%v", got)
	}
	tools, ok := captured["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%T %v", captured["tools"], captured["tools"])
	}
	firstTool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool=%T", tools[0])
	}
	function, ok := firstTool["function"].(map[string]any)
	if !ok {
		t.Fatalf("function=%T", firstTool["function"])
	}
	parameters, ok := function["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters=%T", function["parameters"])
	}
	properties, ok := parameters["properties"].(map[string]any)
	if !ok || properties["query"] == nil {
		t.Fatalf("properties=%v", parameters["properties"])
	}
}

func TestLLMRunnerSanitizesFinalAnswer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "<|channel>thought\ninternal notes<channel|>Final answer",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider: "lmstudio",
			APIKey:   "lm-studio",
			BaseURL:  server.URL + "/v1",
			Model:    "test-model",
			Timeout:  5 * time.Second,
		},
	}

	result, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 1,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "code retrieval", ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Answer != "Final answer" {
		t.Fatalf("answer=%q", result.Answer)
	}
	if result.Metadata["output_sanitization"] == nil {
		t.Fatalf("missing output_sanitization metadata: %#v", result.Metadata)
	}
}

func TestLLMRunnerReturnsErrorOnModelFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 0,
				"total_tokens":      10,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 1,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "code retrieval", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected error on empty assistant response")
	}
}

func TestLLMRunnerPreservesStopReasonBeforeAssistantResponse(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-tool",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role": "assistant",
							"tool_calls": []map[string]any{
								{
									"id":   "call-1",
									"type": "function",
									"function": map[string]any{
										"name":      "retrieve_code",
										"arguments": `{}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-empty-finalize",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 1,
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 1,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "code retrieval", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected max-iterations error")
	}
	want := "rlm llm runner: max_iterations before assistant response"
	if err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
}

func TestLLMRunnerPreservesCancelledBeforeAssistantResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when context is already cancelled")
	}))
	defer server.Close()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:      "lmstudio",
			APIKey:        "lm-studio",
			BaseURL:       server.URL + "/v1",
			Model:         "test-model",
			Timeout:       5 * time.Second,
			MaxIterations: 2,
		},
	}

	_, err := runner.Run(ctx, Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{Name: "retrieve_code", Description: "repo", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	want := "rlm llm runner: cancelled before assistant response: context deadline exceeded"
	if err.Error() != want {
		t.Fatalf("err=%v want %q", err, want)
	}
}

func TestLLMRunnerRequireToolUseRejectsZeroToolCallAnswer(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Generic answer without tools",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 4,
				"total_tokens":      14,
			},
		})
	}))
	defer server.Close()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			Provider:       "lmstudio",
			APIKey:         "lm-studio",
			BaseURL:        server.URL + "/v1",
			Model:          "test-model",
			Timeout:        5 * time.Second,
			MaxIterations:  2,
			RequireToolUse: true,
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{
			Name:        "retrieve_code",
			Description: "code retrieval",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}`),
			ReadOnly:    true,
		}},
	})
	if err == nil || err.Error() != "rlm llm runner: model answered without using tools" {
		t.Fatalf("err=%v", err)
	}
	if got := captured["tool_choice"]; got != "required" {
		t.Fatalf("tool_choice=%v want required", got)
	}
}

func TestLLMRunnerRequireToolUseRejectsEmptyToolSurface(t *testing.T) {
	t.Parallel()

	runner := LLMRunner{
		Tools: fakeLLMToolExecutor{},
		Config: LLMConfig{
			RequireToolUse: true,
		},
	}

	_, err := runner.Run(context.Background(), Task{
		Prompt:        "inspect auth flow",
		WorkspaceRoot: "/tmp/workspace",
		MaxIterations: 2,
	}, Environment{})
	if err == nil || err.Error() != "rlm llm runner: require-tool-use is enabled but no tools are available" {
		t.Fatalf("err=%v", err)
	}
}

func TestLLMSystemPromptUsesCompositeToolsOnlyWhenLegacyAbsent(t *testing.T) {
	t.Parallel()

	prompt := BuildLLMSystemPrompt(Environment{
		Tools: compositeOnlyTools(),
	}, Task{
		Prompt:        "find the retrieval prompt wiring",
		WorkspaceRoot: "/repo",
	})

	assertMentionsTools(t, prompt, []string{
		"retrieve_code",
		"retrieve_memory",
		"retrieve_context",
		"retrieve_task",
		"gather_context",
		"retrieve_mixed",
		"load_evidence_ref",
	})
}

func TestStagedPromptsUseLoadEvidenceRefWhenLegacyAbsent(t *testing.T) {
	t.Parallel()

	base := BuildLLMSystemPrompt(Environment{
		Tools: compositeOnlyTools(),
	}, Task{Prompt: "inspect staged prompts", WorkspaceRoot: "/repo"})
	phase := Phase{
		Name:         "inspection",
		Objective:    "Open and inspect the strongest candidates from discovery.",
		AllowedTools: []string{"load_evidence_ref", "retrieve_code"},
		RequireOneOf: []string{"load_evidence_ref"},
	}
	systemPrompt := buildPhaseSystemPrompt(base, "inspect staged prompts", phase, []string{"internal/rlm/llm_runner.go"}, nil)
	userPrompt := buildPhasePrompt("inspect staged prompts", phase, []string{"internal/rlm/llm_runner.go"}, nil)
	combined := systemPrompt + "\n" + userPrompt

	assertMentionsTools(t, combined, []string{"retrieve_code", "load_evidence_ref"})
	if !strings.Contains(userPrompt, "load_evidence_ref") {
		t.Fatalf("staged inspection prompt did not require load_evidence_ref:\n%s", userPrompt)
	}
}

func compositeOnlyTools() []Tool {
	return []Tool{
		{Name: "retrieve_code", ReadOnly: true},
		{Name: "retrieve_memory", ReadOnly: true},
		{Name: "retrieve_context", ReadOnly: true},
		{Name: "retrieve_task", ReadOnly: true},
		{Name: "gather_context", ReadOnly: true},
		{Name: "retrieve_mixed", ReadOnly: true},
		{Name: "load_evidence_ref", ReadOnly: true},
	}
}

func assertMentionsTools(t *testing.T, prompt string, tools []string) {
	t.Helper()
	for _, tool := range tools {
		if !strings.Contains(prompt, tool) {
			t.Fatalf("prompt missing tool %q:\n%s", tool, prompt)
		}
	}
}

func TestCollectRetrievedPaths(t *testing.T) {
	t.Parallel()

	results := []engine.ToolResult{
		{
			Content: `{"results":[{"path":"internal/auth/handler.go"},{"full_path":"/repo/internal/auth/middleware.go"},{"path":".foxctl/runtime/handoff.json"}]}`,
		},
		{
			Content: `{"path":"internal/auth/handler.go"}`,
		},
		{
			Content: `not-json`,
		},
	}

	got := collectRetrievedPaths(results, "/repo", "See internal/auth/service.go and web/src/AuthPanel.tsx for the final entrypoints.")
	want := []string{"internal/auth/handler.go", "internal/auth/middleware.go", "internal/auth/service.go", "web/src/AuthPanel.tsx"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectRetrievedPaths()=%v want %v", got, want)
	}
}

func TestCollectGatherContextSurfaceMetadata(t *testing.T) {
	t.Parallel()

	metadata := collectGatherContextSurfaceMetadata(
		[]engine.ToolCall{{ID: "call-1", Name: "gather_context"}},
		[]engine.ToolResult{{
			ToolCallID: "call-1",
			Content: `{
				"schema_version": "context_answer_surface/v2",
				"selected_paths": [{"path": "internal/rlm/env/tool_exec.go"}],
				"answer_seed": {"paths": ["internal/context/contextengine/context_gather.go"]},
				"path_set": {"must": [{"path": "internal/rlm/env/tools.go"}]},
				"certificate": {"status": "certified"}
			}`,
		}},
		"/workspace",
	)
	if !reflect.DeepEqual(metadata["gather_context_selected_paths"], []string{"internal/rlm/env/tool_exec.go"}) {
		t.Fatalf("selected paths=%v", metadata["gather_context_selected_paths"])
	}
	if !reflect.DeepEqual(metadata["gather_context_answer_seed_paths"], []string{"internal/context/contextengine/context_gather.go"}) {
		t.Fatalf("answer seed paths=%v", metadata["gather_context_answer_seed_paths"])
	}
	if !reflect.DeepEqual(metadata["gather_context_path_set_must"], []string{"internal/rlm/env/tools.go"}) {
		t.Fatalf("path set=%v", metadata["gather_context_path_set_must"])
	}
	if !reflect.DeepEqual(metadata["gather_context_certificate_statuses"], []string{"certified"}) {
		t.Fatalf("certificate statuses=%v", metadata["gather_context_certificate_statuses"])
	}
}

func TestSummarizeParentToolUsage(t *testing.T) {
	t.Parallel()

	got := summarizeParentToolUsage([]engine.IterationUsage{
		{
			Iteration:               1,
			PromptTokens:            100,
			CompletionTokens:        10,
			ToolCalls:               1,
			ToolNames:               []string{"retrieve_code"},
			ToolResultTokenEstimate: 42,
		},
		{
			Iteration:        2,
			PromptTokens:     145,
			CompletionTokens: 20,
		},
	}, "retrieve_code")

	if intFromAny(got["target_tool_invocations"]) != 1 {
		t.Fatalf("usage=%v", got)
	}
	if intFromAny(got["target_tool_prompt_delta_total"]) != 45 {
		t.Fatalf("usage=%v", got)
	}
	if intFromAny(got["target_tool_result_token_estimate_total"]) != 42 {
		t.Fatalf("usage=%v", got)
	}
}

func TestRerankCandidatePaths(t *testing.T) {
	t.Parallel()

	// After hard-cut: reranking uses path depth ordering, not keyword scoring.
	paths := rerankCandidatePaths("semantic indexing package", []string{
		"builder.go",
		"internal/intelligence/indexing/semantic/indexer.go",
		"internal/v2/core/events/repository.go",
	})
	if len(paths) == 0 {
		t.Fatal("rerank produced no paths")
	}
	// Deeper paths should come first (depth-based ordering).
	if pathDepth(paths[0]) < pathDepth(paths[len(paths)-1]) {
		t.Fatalf("rerank=%v should order deeper paths first", paths)
	}
}
