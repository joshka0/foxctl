package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseInput_RequiresQuestion(t *testing.T) {
	input := `{"workspace_id": "test"}`
	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for missing question")
	}
	if !strings.Contains(err.Error(), "question is required") {
		t.Errorf("expected 'question is required' error, got: %v", err)
	}
}

func TestParseInput_DefaultWorkspaceID(t *testing.T) {
	input := `{"question": "test question"}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.WorkspaceID != "default" {
		t.Errorf("expected default workspace_id, got: %s", in.WorkspaceID)
	}
}

func TestParseInput_DefaultSources(t *testing.T) {
	input := `{"question": "test question"}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(in.Sources) != 2 {
		t.Errorf("expected 2 default sources, got: %d", len(in.Sources))
	}
	if in.Sources[0] != "symbols" || in.Sources[1] != "ripgrep" {
		t.Errorf("expected [symbols, ripgrep], got: %v", in.Sources)
	}
}

func TestParseInput_DefaultLimits(t *testing.T) {
	input := `{"question": "test question"}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Limits.MaxCandidates != DefaultMaxCandidates {
		t.Errorf("expected max_candidates=%d, got: %d", DefaultMaxCandidates, in.Limits.MaxCandidates)
	}
	if in.Limits.MaxSnippets != DefaultMaxSnippets {
		t.Errorf("expected max_snippets=%d, got: %d", DefaultMaxSnippets, in.Limits.MaxSnippets)
	}
	if in.Limits.MaxBytesPerFile != DefaultMaxBytesPerFile {
		t.Errorf("expected max_bytes_per_file=%d, got: %d", DefaultMaxBytesPerFile, in.Limits.MaxBytesPerFile)
	}
}

func TestParseInput_CustomSources(t *testing.T) {
	input := `{"question": "test", "sources": ["semantic"]}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(in.Sources) != 1 || in.Sources[0] != "semantic" {
		t.Errorf("expected [semantic], got: %v", in.Sources)
	}
}

func TestParseInput_CustomLimits(t *testing.T) {
	input := `{"question": "test", "limits": {"max_candidates": 100, "max_snippets": 50}}`
	in, err := parseInput(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if in.Limits.MaxCandidates != 100 {
		t.Errorf("expected max_candidates=100, got: %d", in.Limits.MaxCandidates)
	}
	if in.Limits.MaxSnippets != 50 {
		t.Errorf("expected max_snippets=50, got: %d", in.Limits.MaxSnippets)
	}
	// MaxBytesPerFile should use default when not specified
	if in.Limits.MaxBytesPerFile != DefaultMaxBytesPerFile {
		t.Errorf("expected max_bytes_per_file=%d, got: %d", DefaultMaxBytesPerFile, in.Limits.MaxBytesPerFile)
	}
}

func TestParseInput_InvalidJSON(t *testing.T) {
	input := `{invalid json`
	_, err := parseInput(strings.NewReader(input))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestCandidateOutput_JSONMarshal(t *testing.T) {
	co := CandidateOutput{
		Path:     "internal/foo/bar.go",
		SymbolID: "internal/foo/bar.go:MyFunc",
		Score:    0.95,
		Source:   "symbol",
	}

	b, err := json.Marshal(co)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded CandidateOutput
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Path != co.Path {
		t.Errorf("path mismatch: got %s, want %s", decoded.Path, co.Path)
	}
	if decoded.SymbolID != co.SymbolID {
		t.Errorf("symbol_id mismatch: got %s, want %s", decoded.SymbolID, co.SymbolID)
	}
	if decoded.Score != co.Score {
		t.Errorf("score mismatch: got %f, want %f", decoded.Score, co.Score)
	}
	if decoded.Source != co.Source {
		t.Errorf("source mismatch: got %s, want %s", decoded.Source, co.Source)
	}
}

func TestSummary_JSONMarshal(t *testing.T) {
	s := Summary{
		CandidatesGenerated:   10,
		CandidatesBySource:    map[string]int{"symbol": 5, "ripgrep": 5},
		FilesRelevant:         8,
		SnippetsEmitted:       15,
		DurationMS:            200,
		CandidateGenerationMS: 50,
		SnippetExtractionMS:   150,
	}

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Summary
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.CandidatesGenerated != s.CandidatesGenerated {
		t.Errorf("candidates_generated mismatch")
	}
	if decoded.FilesRelevant != s.FilesRelevant {
		t.Errorf("files_relevant mismatch")
	}
	if decoded.SnippetsEmitted != s.SnippetsEmitted {
		t.Errorf("snippets_emitted mismatch")
	}
}

func TestFindAgentctlBin_EnvOverride(t *testing.T) {
	// Test that AGENTCTL_BIN environment variable overrides default behavior
	customPath := "/custom/path/to/agentctl"
	t.Setenv("AGENTCTL_BIN", customPath)

	bin := findAgentctlBin()
	if bin != customPath {
		t.Errorf("expected AGENTCTL_BIN override %q, got %q", customPath, bin)
	}
}

func TestFindAgentctlBin_DefaultFallback(t *testing.T) {
	// Test that without env var, function returns a non-empty default
	// Note: t.Setenv automatically restores the original value after the test
	t.Setenv("AGENTCTL_BIN", "")

	bin := findAgentctlBin()
	if bin == "" {
		t.Error("findAgentctlBin returned empty string when AGENTCTL_BIN is empty")
	}
}
