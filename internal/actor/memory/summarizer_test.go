package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"text/template"
	"time"
)

func TestMockSummarizer_SummarizeTurns(t *testing.T) {
	summarizer := &MockSummarizer{}
	ctx := context.Background()

	turns := []Turn{
		{Index: 0, Role: "user", Content: "Hello"},
		{Index: 1, Role: "assistant", Content: "Hi there"},
		{Index: 2, Role: "user", Content: "How are you?"},
	}

	summary, err := summarizer.SummarizeTurns(ctx, "test task", turns)
	if err != nil {
		t.Fatalf("SummarizeTurns() error = %v", err)
	}

	if summary == nil {
		t.Fatal("summary is nil")
	}

	if summary.TurnRange.Start != 0 {
		t.Errorf("TurnRange.Start = %d, want 0", summary.TurnRange.Start)
	}
	if summary.TurnRange.End != 2 {
		t.Errorf("TurnRange.End = %d, want 2", summary.TurnRange.End)
	}
	if summary.Content == "" {
		t.Error("Content is empty")
	}
}

func TestMockSummarizer_DistillSummaries(t *testing.T) {
	summarizer := &MockSummarizer{}
	ctx := context.Background()

	summaries := []Summary{
		{TurnRange: TurnRange{Start: 0, End: 4}, Content: "Summary 1"},
		{TurnRange: TurnRange{Start: 5, End: 9}, Content: "Summary 2"},
	}

	result, err := summarizer.DistillSummaries(ctx, "test task", summaries)
	if err != nil {
		t.Fatalf("DistillSummaries() error = %v", err)
	}

	if result == "" {
		t.Error("result is empty")
	}
}

func TestMockSummarizer_FilterByRelevance(t *testing.T) {
	summarizer := &MockSummarizer{}
	ctx := context.Background()

	items := []string{"item1", "item2", "item3"}

	filtered, err := summarizer.FilterByRelevance(ctx, "test task", items)
	if err != nil {
		t.Fatalf("FilterByRelevance() error = %v", err)
	}

	// Default mock returns all items
	if len(filtered) != len(items) {
		t.Errorf("len(filtered) = %d, want %d", len(filtered), len(items))
	}
}

func TestMockSummarizer_CustomFunctions(t *testing.T) {
	customCalled := false
	summarizer := &MockSummarizer{
		SummarizeFunc: func(ctx context.Context, task string, turns []Turn) (*Summary, error) {
			customCalled = true
			return &Summary{
				Content:   "custom summary",
				KeyPoints: []string{"custom point"},
			}, nil
		},
	}

	ctx := context.Background()
	summary, err := summarizer.SummarizeTurns(ctx, "task", []Turn{{Index: 0}})
	if err != nil {
		t.Fatalf("SummarizeTurns() error = %v", err)
	}

	if !customCalled {
		t.Error("custom function was not called")
	}
	if summary.Content != "custom summary" {
		t.Errorf("Content = %q, want %q", summary.Content, "custom summary")
	}
}

func TestParseGeminiSummaryResponse(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		wantContent   string
		wantKeyPoints int
		wantDecisions int
	}{
		{
			name:          "plain text",
			response:      "This is a summary without JSON.",
			wantContent:   "This is a summary without JSON.",
			wantKeyPoints: 0,
			wantDecisions: 0,
		},
		{
			name:          "with JSON metadata",
			response:      "This is the summary.\n{\"key_points\": [\"point1\", \"point2\"], \"decisions\": [\"decision1\"]}",
			wantContent:   "This is the summary.",
			wantKeyPoints: 2,
			wantDecisions: 1,
		},
		{
			name:          "invalid JSON",
			response:      "Summary {invalid json}",
			wantContent:   "Summary {invalid json}",
			wantKeyPoints: 0,
			wantDecisions: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, keyPoints, decisions := parseGeminiSummaryResponse(tt.response)

			if content != tt.wantContent {
				t.Errorf("content = %q, want %q", content, tt.wantContent)
			}
			if len(keyPoints) != tt.wantKeyPoints {
				t.Errorf("len(keyPoints) = %d, want %d", len(keyPoints), tt.wantKeyPoints)
			}
			if len(decisions) != tt.wantDecisions {
				t.Errorf("len(decisions) = %d, want %d", len(decisions), tt.wantDecisions)
			}
		})
	}
}

func TestGeminiSummarizer_NoAPIKey(t *testing.T) {
	// Save and clear API key
	originalKey := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("GEMINI_API_KEY", originalKey)
		}
	}()

	_, err := NewGeminiSummarizer()
	if err == nil {
		t.Error("expected error when GEMINI_API_KEY not set")
	}
}

func TestGeminiSummarizer_SummarizeTurns_Integration(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Return mock response
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Test summary of the conversation.\n{\"key_points\": [\"point1\"], \"decisions\": [\"decision1\"]}"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create summarizer with test client that redirects to mock server
	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	// Parse templates
	var err error
	summarizer.summarizeTpl, err = parseTemplate("summarize", SummarizeTurnsPromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	ctx := context.Background()
	turns := []Turn{
		{Index: 0, Role: "user", Content: "Hello"},
		{Index: 1, Role: "assistant", Content: "Hi there"},
	}

	summary, err := summarizer.SummarizeTurns(ctx, "test task", turns)
	if err != nil {
		t.Fatalf("SummarizeTurns() error = %v", err)
	}

	if summary.Content == "" {
		t.Error("summary content is empty")
	}
	if len(summary.KeyPoints) == 0 {
		t.Error("expected key points")
	}
}

func TestGeminiSummarizer_DistillSummaries_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Distilled summary:\n- Major accomplishment\n- Key decision"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	var err error
	summarizer.distillTpl, err = parseTemplate("distill", DistillSummariesPromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	ctx := context.Background()
	summaries := []Summary{
		{TurnRange: TurnRange{Start: 0, End: 4}, Content: "Summary 1"},
		{TurnRange: TurnRange{Start: 5, End: 9}, Content: "Summary 2"},
	}

	result, err := summarizer.DistillSummaries(ctx, "test task", summaries)
	if err != nil {
		t.Fatalf("DistillSummaries() error = %v", err)
	}

	if result == "" {
		t.Error("result is empty")
	}
}

func TestGeminiSummarizer_FilterByRelevance_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "{\"scores\": [{\"index\": 0, \"score\": 8, \"keep\": true}, {\"index\": 1, \"score\": 3, \"keep\": false}, {\"index\": 2, \"score\": 7, \"keep\": true}]}"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	var err error
	summarizer.filterTpl, err = parseTemplate("filter", FilterByRelevancePromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	ctx := context.Background()
	items := []string{"relevant1", "irrelevant", "relevant2"}

	filtered, err := summarizer.FilterByRelevance(ctx, "test task", items)
	if err != nil {
		t.Fatalf("FilterByRelevance() error = %v", err)
	}

	// Should keep items 0 and 2
	if len(filtered) != 2 {
		t.Errorf("len(filtered) = %d, want 2", len(filtered))
	}
}

func TestGeminiSummarizer_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	var err error
	summarizer.summarizeTpl, err = parseTemplate("summarize", SummarizeTurnsPromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	ctx := context.Background()
	_, err = summarizer.SummarizeTurns(ctx, "task", []Turn{{Index: 0}})
	if err == nil {
		t.Error("expected error on API failure")
	}
}

func TestGeminiSummarizer_ContextCancellation(t *testing.T) {
	// Create a server that delays its response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation to take effect
		time.Sleep(500 * time.Millisecond)
		resp := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "This should not be returned"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	var err error
	summarizer.summarizeTpl, err = parseTemplate("summarize", SummarizeTurnsPromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	turns := []Turn{
		{Index: 0, Role: "user", Content: "Hello"},
	}

	_, err = summarizer.SummarizeTurns(ctx, "test task", turns)
	if err == nil {
		t.Error("expected error on context cancellation")
	}
	// The error should be related to context cancellation
	if ctx.Err() == nil {
		t.Error("context should be cancelled")
	}
}

func TestGeminiSummarizer_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"candidates": []map[string]any{},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	var err error
	summarizer.summarizeTpl, err = parseTemplate("summarize", SummarizeTurnsPromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	ctx := context.Background()
	_, err = summarizer.SummarizeTurns(ctx, "task", []Turn{{Index: 0}})
	if err == nil {
		t.Error("expected error on empty response")
	}
}

func TestGeminiSummarizer_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	summarizer := &GeminiSummarizer{
		apiKey: "test-key",
		model:  "test-model",
		httpClient: &http.Client{
			Timeout:   50 * time.Millisecond,
			Transport: &mockTransport{serverURL: server.URL},
		},
	}

	var err error
	summarizer.summarizeTpl, err = parseTemplate("summarize", SummarizeTurnsPromptTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	ctx := context.Background()
	_, err = summarizer.SummarizeTurns(ctx, "task", []Turn{{Index: 0}})
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestMockSummarizer_EmptyInputs(t *testing.T) {
	summarizer := &MockSummarizer{}
	ctx := context.Background()

	// Empty turns
	summary, err := summarizer.SummarizeTurns(ctx, "task", []Turn{})
	if err != nil {
		t.Fatalf("SummarizeTurns() error = %v", err)
	}
	if summary.TurnRange.Start != 0 || summary.TurnRange.End != 0 {
		t.Errorf("unexpected turn range for empty turns: %+v", summary.TurnRange)
	}

	// Empty summaries
	result, err := summarizer.DistillSummaries(ctx, "task", []Summary{})
	if err != nil {
		t.Fatalf("DistillSummaries() error = %v", err)
	}
	if result == "" {
		t.Error("result is empty")
	}

	// Empty items
	filtered, err := summarizer.FilterByRelevance(ctx, "task", []string{})
	if err != nil {
		t.Fatalf("FilterByRelevance() error = %v", err)
	}
	if len(filtered) != 0 {
		t.Error("expected empty filtered list")
	}
}

// mockTransport redirects all requests to the test server.
type mockTransport struct {
	serverURL string
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Parse the server URL to extract scheme and host
	parsed, err := url.Parse(t.serverURL)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = parsed.Scheme
	req.URL.Host = parsed.Host
	return http.DefaultTransport.RoundTrip(req)
}

// parseTemplate is a helper for tests.
func parseTemplate(name, text string) (*template.Template, error) {
	return template.New(name).Parse(text)
}
