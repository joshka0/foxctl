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
			Name:        "search_repo",
			Description: "repo",
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
		MaxIterations: 2,
	}, Environment{
		Tools: []Tool{{Name: "search_repo", Description: "repo", ReadOnly: true}},
	})
	if err == nil {
		t.Fatal("expected error on empty assistant response")
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
		Tools: []Tool{{Name: "search_repo", Description: "repo", ReadOnly: true}},
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
			Name:        "search_repo",
			Description: "repo",
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

	semanticPaths := rerankCandidatePaths("semantic indexing package", []string{
		"internal/v2/core/events/repository.go",
		"internal/intelligence/indexing/semantic/indexer.go",
		"builder.go",
	})
	if len(semanticPaths) == 0 || semanticPaths[0] != "internal/intelligence/indexing/semantic/indexer.go" {
		t.Fatalf("semantic rerank=%v", semanticPaths)
	}

	webPaths := rerankCandidatePaths("web api handlers transport", []string{
		"internal/tooling/tools/obsidian/client.go",
		"internal/interfaces/web/transport.go",
		"internal/http/handler.go",
	})
	if len(webPaths) == 0 {
		t.Fatal("web rerank produced no paths")
	}
	if webPaths[0] == "internal/tooling/tools/obsidian/client.go" {
		t.Fatalf("web rerank=%v should prefer web/http paths over obsidian", webPaths)
	}
}
