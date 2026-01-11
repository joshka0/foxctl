package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
)

// applyDefaultsAndValidate applies defaults and validates required fields (mirrors run function).
func applyDefaultsAndValidate(in *Input) error {
	// Validate required fields
	if in.Question == "" {
		return fmt.Errorf("question is required")
	}
	if len(in.Candidates) == 0 {
		return fmt.Errorf("candidates is required")
	}
	// Apply defaults
	if in.WorkspaceID == "" {
		in.WorkspaceID = "default"
	}
	if in.Limits.MaxCandidates <= 0 {
		in.Limits.MaxCandidates = DefaultMaxCandidates
	}
	if in.Limits.Timeout <= 0 {
		in.Limits.Timeout = DefaultTimeout
	}
	// Limit candidates
	if len(in.Candidates) > in.Limits.MaxCandidates {
		in.Candidates = in.Candidates[:in.Limits.MaxCandidates]
	}
	return nil
}

// parseInput is a test helper that parses JSON, applies defaults, and validates.
func parseInput(r io.Reader) (Input, error) {
	in, err := skilltest.ParseInput[Input](r)
	if err != nil {
		return in, err
	}
	if err := applyDefaultsAndValidate(&in); err != nil {
		return in, err
	}
	return in, nil
}

func TestParseInput_RequiresQuestion(t *testing.T) {
	input := `{"candidates": [{"path": "test.go"}]}`
	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for missing question")
	}
	if !strings.Contains(err.Error(), "question is required") {
		t.Errorf("expected 'question is required' error, got: %v", err)
	}
}

func TestParseInput_RequiresCandidates(t *testing.T) {
	input := `{"question": "test"}`
	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for missing candidates")
	}
	if !strings.Contains(err.Error(), "candidates is required") {
		t.Errorf("expected 'candidates is required' error, got: %v", err)
	}
}

func TestParseInput_DefaultWorkspaceID(t *testing.T) {
	input := `{"question": "test", "candidates": [{"path": "test.go"}]}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.WorkspaceID != "default" {
		t.Errorf("expected default workspace_id, got: %s", in.WorkspaceID)
	}
}

func TestParseInput_DefaultLimits(t *testing.T) {
	input := `{"question": "test", "candidates": [{"path": "test.go"}]}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Limits.MaxCandidates != DefaultMaxCandidates {
		t.Errorf("expected max_candidates=%d, got: %d", DefaultMaxCandidates, in.Limits.MaxCandidates)
	}
	if in.Limits.Timeout != DefaultTimeout {
		t.Errorf("expected timeout=%v, got: %v", DefaultTimeout, in.Limits.Timeout)
	}
}

func TestParseInput_CustomProviders(t *testing.T) {
	input := `{"question": "test", "candidates": [{"path": "test.go"}], "providers": ["openai"]}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(in.Providers) != 1 || in.Providers[0] != "openai" {
		t.Errorf("expected [openai], got: %v", in.Providers)
	}
}

func TestParseInput_LimitsCandidates(t *testing.T) {
	// Create input with more candidates than limit
	candidates := make([]map[string]string, 100)
	for i := range candidates {
		candidates[i] = map[string]string{"path": "test.go"}
	}
	inputData := map[string]any{
		"question":   "test",
		"candidates": candidates,
		"limits":     map[string]int{"max_candidates": 10},
	}
	inputJSON, _ := json.Marshal(inputData)

	in, err := parseInput(strings.NewReader(string(inputJSON)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(in.Candidates) != 10 {
		t.Errorf("expected 10 candidates, got: %d", len(in.Candidates))
	}
}

func TestBuildRankingPrompt(t *testing.T) {
	candidates := []Candidate{
		{Path: "auth.go", SymbolID: "auth.go:Login"},
		{Path: "handler.go", Snippet: "func Handle() {}"},
	}

	prompt := buildRankingPrompt("How does login work?", candidates)

	// Check prompt contains key elements
	if !strings.Contains(prompt, "How does login work?") {
		t.Error("prompt should contain question")
	}
	if !strings.Contains(prompt, "auth.go") {
		t.Error("prompt should contain file path")
	}
	if !strings.Contains(prompt, "Login") {
		t.Error("prompt should contain symbol")
	}
	if !strings.Contains(prompt, "func Handle()") {
		t.Error("prompt should contain snippet")
	}
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should request JSON response")
	}
}

func TestParseRankingResponse_ValidJSON(t *testing.T) {
	response := `[{"path": "auth.go", "score": 0.9, "explanation": "Main auth file"}]`
	candidates := []Candidate{{Path: "auth.go"}}

	scores, err := parseRankingResponse(response, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got: %d", len(scores))
	}
	if scores[0].Path != "auth.go" {
		t.Errorf("expected auth.go, got: %s", scores[0].Path)
	}
	if scores[0].Score != 0.9 {
		t.Errorf("expected 0.9, got: %f", scores[0].Score)
	}
	if scores[0].Explanation != "Main auth file" {
		t.Errorf("expected 'Main auth file', got: %s", scores[0].Explanation)
	}
}

func TestParseRankingResponse_WithCodeBlock(t *testing.T) {
	response := "```json\n[{\"path\": \"test.go\", \"score\": 0.8}]\n```"
	candidates := []Candidate{{Path: "test.go"}}

	scores, err := parseRankingResponse(response, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got: %d", len(scores))
	}
	if scores[0].Score != 0.8 {
		t.Errorf("expected 0.8, got: %f", scores[0].Score)
	}
}

func TestParseRankingResponse_NormalizesScores(t *testing.T) {
	response := `[{"path": "a.go", "score": 1.5}, {"path": "b.go", "score": -0.5}]`
	candidates := []Candidate{{Path: "a.go"}, {Path: "b.go"}}

	scores, err := parseRankingResponse(response, candidates)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scores[0].Score != 1.0 {
		t.Errorf("expected score capped at 1.0, got: %f", scores[0].Score)
	}
	if scores[1].Score != 0.0 {
		t.Errorf("expected score floor at 0.0, got: %f", scores[1].Score)
	}
}

func TestParseRankingResponse_NoJSONArray(t *testing.T) {
	response := "I couldn't understand the question."
	candidates := []Candidate{{Path: "test.go"}}

	_, err := parseRankingResponse(response, candidates)
	if err == nil {
		t.Error("expected error for no JSON array")
	}
}

func TestMergeProviderResults_SingleProvider(t *testing.T) {
	candidates := []Candidate{
		{Path: "a.go", Priority: 0.5},
		{Path: "b.go", Priority: 0.3},
	}

	results := map[string]ProviderResult{
		"anthropic": {
			Status: "ok",
			Rankings: []ProviderScore{
				{Path: "a.go", Score: 0.9},
				{Path: "b.go", Score: 0.4},
			},
		},
	}

	ranked := mergeProviderResults(candidates, results)

	if len(ranked) != 2 {
		t.Fatalf("expected 2 candidates, got: %d", len(ranked))
	}
	// First should be a.go (higher score)
	if ranked[0].Path != "a.go" {
		t.Errorf("expected a.go first, got: %s", ranked[0].Path)
	}
	if ranked[0].Rank != 1 {
		t.Errorf("expected rank 1, got: %d", ranked[0].Rank)
	}
	if ranked[0].Score != 0.9 {
		t.Errorf("expected score 0.9, got: %f", ranked[0].Score)
	}
}

func TestMergeProviderResults_MultipleProviders(t *testing.T) {
	candidates := []Candidate{
		{Path: "a.go"},
		{Path: "b.go"},
	}

	results := map[string]ProviderResult{
		"anthropic": {
			Status: "ok",
			Rankings: []ProviderScore{
				{Path: "a.go", Score: 0.8},
				{Path: "b.go", Score: 0.4},
			},
		},
		"openai": {
			Status: "ok",
			Rankings: []ProviderScore{
				{Path: "a.go", Score: 0.6},
				{Path: "b.go", Score: 0.8},
			},
		},
	}

	ranked := mergeProviderResults(candidates, results)

	// a.go: avg(0.8, 0.6) = 0.7
	// b.go: avg(0.4, 0.8) = 0.6
	// So a.go should be first
	if ranked[0].Path != "a.go" {
		t.Errorf("expected a.go first, got: %s", ranked[0].Path)
	}
	// Use approximate comparison for floating point
	if !approxEqual(ranked[0].Score, 0.7, 0.001) {
		t.Errorf("expected score ~0.7, got: %f", ranked[0].Score)
	}
	if !approxEqual(ranked[1].Score, 0.6, 0.001) {
		t.Errorf("expected score ~0.6, got: %f", ranked[1].Score)
	}

	// Check by_provider values
	if ranked[0].ByProvider["anthropic"] != 0.8 {
		t.Errorf("expected anthropic score 0.8, got: %f", ranked[0].ByProvider["anthropic"])
	}
	if ranked[0].ByProvider["openai"] != 0.6 {
		t.Errorf("expected openai score 0.6, got: %f", ranked[0].ByProvider["openai"])
	}
}

// approxEqual compares two floats with a tolerance
func approxEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

func TestMergeProviderResults_ProviderError(t *testing.T) {
	candidates := []Candidate{
		{Path: "a.go", Priority: 0.5},
	}

	results := map[string]ProviderResult{
		"anthropic": {
			Status: "error",
			Error:  "API error",
		},
	}

	ranked := mergeProviderResults(candidates, results)

	// Should fall back to original priority
	if ranked[0].Score != 0.5 {
		t.Errorf("expected fallback to priority 0.5, got: %f", ranked[0].Score)
	}
}

func TestCandidateJSON(t *testing.T) {
	c := Candidate{
		Path:     "internal/auth/login.go",
		SymbolID: "internal/auth/login.go:Login",
		Snippet:  "func Login(ctx context.Context) error { ... }",
		Priority: 0.85,
	}

	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Candidate
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Path != c.Path {
		t.Errorf("path mismatch: got %s, want %s", decoded.Path, c.Path)
	}
	if decoded.SymbolID != c.SymbolID {
		t.Errorf("symbol_id mismatch: got %s, want %s", decoded.SymbolID, c.SymbolID)
	}
	if decoded.Snippet != c.Snippet {
		t.Errorf("snippet mismatch: got %s, want %s", decoded.Snippet, c.Snippet)
	}
	if decoded.Priority != c.Priority {
		t.Errorf("priority mismatch: got %f, want %f", decoded.Priority, c.Priority)
	}
}

func TestRankedCandidateJSON(t *testing.T) {
	rc := RankedCandidate{
		Path:        "test.go",
		SymbolID:    "test.go:TestFunc",
		Score:       0.85,
		Rank:        1,
		Explanation: "Most relevant",
		ByProvider: map[string]float64{
			"anthropic": 0.9,
			"openai":    0.8,
		},
	}

	b, err := json.Marshal(rc)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded RankedCandidate
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Score != rc.Score {
		t.Errorf("score mismatch: got %f, want %f", decoded.Score, rc.Score)
	}
	if decoded.Rank != rc.Rank {
		t.Errorf("rank mismatch: got %d, want %d", decoded.Rank, rc.Rank)
	}
	if len(decoded.ByProvider) != 2 {
		t.Errorf("by_provider length mismatch: got %d, want 2", len(decoded.ByProvider))
	}
}

func TestDetectAvailableProviders_Fallback(t *testing.T) {
	// Just test that the function returns at least one provider as fallback
	// (when no environment variables are set, it defaults to anthropic)
	providers := detectAvailableProviders()
	if len(providers) == 0 {
		t.Error("expected at least one fallback provider")
	}
}
