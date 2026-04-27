package rlm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

	got := collectRetrievedPaths(results, "/repo", "See internal/auth/service.go for the final entrypoint.")
	want := []string{"internal/auth/handler.go", "internal/auth/middleware.go", "internal/auth/service.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectRetrievedPaths()=%v want %v", got, want)
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
			ToolNames:               []string{"code_search_ensemble"},
			ToolResultTokenEstimate: 42,
		},
		{
			Iteration:        2,
			PromptTokens:     145,
			CompletionTokens: 20,
		},
	}, "code_search_ensemble")

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
