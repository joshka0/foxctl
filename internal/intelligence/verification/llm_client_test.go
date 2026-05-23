package verification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClientChatAppliesQwenDefaults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path=%q want /chat/completions", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) == 0 {
			t.Fatalf("messages missing: %#v", body["messages"])
		}
		first, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("first message=%#v", messages[0])
		}
		content, _ := first["content"].(string)
		if !strings.HasPrefix(content, "/no_think\n") {
			t.Fatalf("system prompt missing /no_think prefix: %q", content)
		}
		kwargs, ok := body["chat_template_kwargs"].(map[string]any)
		if !ok {
			t.Fatalf("chat_template_kwargs missing: %#v", body["chat_template_kwargs"])
		}
		if enabled, ok := kwargs["enable_thinking"].(bool); !ok || enabled {
			t.Fatalf("enable_thinking=%#v want false", kwargs["enable_thinking"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient(OpenAIConfig{
		Provider: "lmstudio",
		BaseURL:  server.URL,
		APIKey:   "test-key",
		Model:    "qwen3.5-4b-mlx",
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	got, err := client.Chat(context.Background(), "Return JSON only.", "Question", LLMCallOptions{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "ok" {
		t.Fatalf("content=%q want ok", got)
	}
}

func TestOpenAIClientChatAllowsLocalNoAuth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty for local no-auth client", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient(OpenAIConfig{
		Provider: "lmstudio",
		BaseURL:  server.URL,
		Model:    "qwen3.5-4b-mlx",
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	got, err := client.Chat(context.Background(), "Return JSON only.", "Question", LLMCallOptions{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "ok" {
		t.Fatalf("content=%q want ok", got)
	}
}

func TestOpenAIClientChatFallsBackToReasoningContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","reasoning_content":"{\"keep\":true}"}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient(OpenAIConfig{
		Provider: "lmstudio",
		BaseURL:  server.URL,
		APIKey:   "test-key",
		Model:    "qwen3.5-4b-mlx",
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	got, err := client.Chat(context.Background(), "Return JSON only.", "Question", LLMCallOptions{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != `{"keep":true}` {
		t.Fatalf("content=%q want reasoning_content", got)
	}
}

func TestOpenAIClientChatRejectsEmptyContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"","reasoning_content":"   "}}]}`))
	}))
	defer server.Close()

	client, err := NewOpenAIClient(OpenAIConfig{
		Provider: "lmstudio",
		BaseURL:  server.URL,
		APIKey:   "test-key",
		Model:    "qwen3.5-4b-mlx",
	})
	if err != nil {
		t.Fatalf("NewOpenAIClient: %v", err)
	}
	_, err = client.Chat(context.Background(), "Return JSON only.", "Question", LLMCallOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty completion content") {
		t.Fatalf("error=%v want empty completion content", err)
	}
}
