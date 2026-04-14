package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScoreExpectedSymbols(t *testing.T) {
	t.Parallel()

	matched, recall := scoreExpectedSymbols(
		[]string{"internal/rlm/env/code_search_ensemble.go::codeSearchEnsembleInput", "ReadOnlyAdapter"},
		[]string{"internal/rlm/env/code_search_ensemble.go::codeSearchEnsembleInput", "internal/rlm/env/adapter.go::ReadOnlyAdapter"},
	)
	if recall != 1 {
		t.Fatalf("recall=%v want 1", recall)
	}
	if len(matched) != 2 {
		t.Fatalf("matched=%v", matched)
	}
}

func TestScoreExpectedSnippets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "internal", "rlm", "env", "code_search_ensemble.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	body := "package env\n\ntype codeSearchEnsembleInput struct {\n\tQuery string\n\tTaskType string\n\tCandidatePaths []string\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	matched, recall := scoreExpectedSnippets(root, []promptEvalSnippet{{
		Path:     "internal/rlm/env/code_search_ensemble.go",
		Contains: []string{"type codeSearchEnsembleInput struct", "CandidatePaths"},
	}}, []evalObservedSnippet{{
		Path:      "internal/rlm/env/code_search_ensemble.go",
		StartLine: 3,
		EndLine:   6,
	}})
	if recall != 1 {
		t.Fatalf("recall=%v want 1", recall)
	}
	if len(matched) != 1 {
		t.Fatalf("matched=%v", matched)
	}
}

func TestScoreRequiredFacts(t *testing.T) {
	t.Parallel()

	matched, recall := scoreRequiredFacts([]string{"newEvalCodeSearchEnsembleCommand"}, "Registration uses newEvalCodeSearchEnsembleCommand in eval.go")
	if recall != 1 {
		t.Fatalf("recall=%v want 1", recall)
	}
	if len(matched) != 1 {
		t.Fatalf("matched=%v", matched)
	}
}

func TestBlendedCorrectnessScore(t *testing.T) {
	t.Parallel()

	got := blendedCorrectnessScore(1, 1, 0.5, 0, true, true, true, false)
	if got <= 0.7 || got >= 1 {
		t.Fatalf("correctness=%v", got)
	}
}
