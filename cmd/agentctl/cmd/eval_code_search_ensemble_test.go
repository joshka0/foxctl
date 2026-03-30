package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCodeSearchEnsembleEvalQuery(t *testing.T) {
	t.Parallel()

	got := buildCodeSearchEnsembleEvalQuery(promptEvalCase{
		Question: "Which files matter?",
		Context:  "Legacy runtime only.",
	})
	want := "Which files matter?\n\nContext:\nLegacy runtime only."
	if got != want {
		t.Fatalf("query=%q want %q", got, want)
	}
}

func TestExtractCodeSearchEnsemblePaths(t *testing.T) {
	t.Parallel()

	out := codeSearchEnsembleOutput{
		Files: []struct {
			Path string `json:"path,omitempty"`
			Why  string `json:"why,omitempty"`
		}{
			{Path: "internal/agent/runtime/runtime.go"},
			{Path: "./internal/agent/runtime/runtime.go"},
			{Path: "internal/agent/types/types.go"},
		},
	}
	got := extractCodeSearchEnsemblePaths(out)
	if len(got) != 2 {
		t.Fatalf("paths=%v", got)
	}
	if got[0] != "internal/agent/runtime/runtime.go" || got[1] != "internal/agent/types/types.go" {
		t.Fatalf("paths=%v", got)
	}
}

func TestExtractCodeSearchEnsemblePathsPreservesObservedOrder(t *testing.T) {
	t.Parallel()

	out := codeSearchEnsembleOutput{
		Files: []struct {
			Path string `json:"path,omitempty"`
			Why  string `json:"why,omitempty"`
		}{
			{Path: "zeta/file.go"},
			{Path: "alpha/file.go"},
			{Path: "zeta/file.go"},
			{Path: "./beta/file.go"},
		},
	}
	got := extractCodeSearchEnsemblePaths(out)
	if len(got) != 3 {
		t.Fatalf("paths=%v", got)
	}
	if got[0] != "zeta/file.go" || got[1] != "alpha/file.go" || got[2] != "beta/file.go" {
		t.Fatalf("paths=%v", got)
	}
}

func TestExtractCodeSearchEnsembleSymbolsAndSnippets(t *testing.T) {
	t.Parallel()

	out := codeSearchEnsembleOutput{
		Symbols: []struct {
			Path   string `json:"path,omitempty"`
			Symbol string `json:"symbol,omitempty"`
			Line   int    `json:"line,omitempty"`
		}{
			{Path: "internal/rlm/env/code_search_ensemble.go", Symbol: "codeSearchEnsembleInput"},
		},
		Snippets: []struct {
			Path      string `json:"path,omitempty"`
			StartLine int    `json:"start_line,omitempty"`
			EndLine   int    `json:"end_line,omitempty"`
			Reason    string `json:"reason,omitempty"`
		}{
			{Path: "internal/rlm/env/code_search_ensemble.go", StartLine: 10, EndLine: 20},
		},
	}
	symbols := extractCodeSearchEnsembleSymbols(out)
	if len(symbols) != 1 || symbols[0] != "internal/rlm/env/code_search_ensemble.go::codeSearchEnsembleInput" {
		t.Fatalf("symbols=%v", symbols)
	}
	snippets := extractCodeSearchEnsembleSnippets(out)
	if len(snippets) != 1 || snippets[0].Path != "internal/rlm/env/code_search_ensemble.go" {
		t.Fatalf("snippets=%v", snippets)
	}
}

func TestBuildCodeSearchEnsembleFactBlobIncludesReasons(t *testing.T) {
	t.Parallel()

	out := codeSearchEnsembleOutput{
		Summary:     "summary",
		AnswerBasis: "basis",
		Files: []struct {
			Path string `json:"path,omitempty"`
			Why  string `json:"why,omitempty"`
		}{
			{Path: "skills/hooks_bash_guard/main.go", Why: "Build code/context_grep command from sed range info"},
		},
		Snippets: []struct {
			Path      string `json:"path,omitempty"`
			StartLine int    `json:"start_line,omitempty"`
			EndLine   int    `json:"end_line,omitempty"`
			Reason    string `json:"reason,omitempty"`
		}{
			{Path: "skills/hooks_bash_guard/main.go", StartLine: 300, EndLine: 330, Reason: "Build code/context_grep command from sed range info"},
		},
	}
	blob := buildCodeSearchEnsembleFactBlob(out)
	if !strings.Contains(blob, "code/context_grep") {
		t.Fatalf("blob=%q", blob)
	}
}

func TestShouldPassCodeSearchEnsembleEval(t *testing.T) {
	t.Parallel()

	evalCase := promptEvalCase{
		ExpectedPaths:    []string{"internal/agent/runtime/runtime.go"},
		RequireGrounding: true,
	}
	passed := shouldPassCodeSearchEnsembleEval(codeSearchEnsembleEvalResult{
		PathRecall:       1,
		CorrectnessScore: 1,
		Grounded:         true,
	}, evalCase, 0.8)
	if !passed {
		t.Fatal("expected passing ensemble result")
	}
}

func TestShouldPassCodeSearchEnsembleEvalFailsOnWrongScope(t *testing.T) {
	t.Parallel()

	evalCase := promptEvalCase{
		ExpectedPaths: []string{"internal/agent/runtime/runtime.go"},
		ExcludedPaths: []string{"internal/rlm/env/scout_roles.go"},
	}
	passed := shouldPassCodeSearchEnsembleEval(codeSearchEnsembleEvalResult{
		PathRecall:       1,
		Grounded:         true,
		ExcludedPathHits: []string{"internal/rlm/env/scout_roles.go"},
	}, evalCase, 0.8)
	if passed {
		t.Fatal("expected wrong-scope hit to fail")
	}
}

func TestNormalizeCodeSearchEvalTaskType(t *testing.T) {
	t.Parallel()

	got := normalizeCodeSearchEvalTaskType(promptEvalCase{TaskType: "registration_trace"})
	if got != "registration_trace" {
		t.Fatalf("task_type=%q", got)
	}
	got = normalizeCodeSearchEvalTaskType(promptEvalCase{TaskType: "change_impact"})
	if got != "change_impact" {
		t.Fatalf("task_type=%q", got)
	}
}

func TestLoadCheckedInCodeSearchEnsembleDataset(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "evals", "code-search-ensemble", "agentctl-repo-grounded.jsonl")
	cases, err := loadPromptEvalCases(path)
	if err != nil {
		t.Fatalf("loadPromptEvalCases() error = %v", err)
	}
	if len(cases) != 7 {
		t.Fatalf("len(cases)=%d want 7", len(cases))
	}
	if cases[3].TaskType != "registration_trace" {
		t.Fatalf("task_type=%q want registration_trace", cases[3].TaskType)
	}
}

func TestLoadCheckedInCodeSearchTraceSymbolDataset(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "evals", "code-search-ensemble", "agentctl-trace-symbol.jsonl")
	cases, err := loadPromptEvalCases(path)
	if err != nil {
		t.Fatalf("loadPromptEvalCases() error = %v", err)
	}
	if len(cases) != 6 {
		t.Fatalf("len(cases)=%d want 6", len(cases))
	}
	if cases[0].TaskType != "execution_trace" || cases[3].TaskType != "symbol_inspect" {
		t.Fatalf("unexpected task types: %q %q", cases[0].TaskType, cases[3].TaskType)
	}
}

func TestLoadCheckedInCodeSearchChangeImpactDataset(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "evals", "code-search-ensemble", "agentctl-change-impact.jsonl")
	cases, err := loadPromptEvalCases(path)
	if err != nil {
		t.Fatalf("loadPromptEvalCases() error = %v", err)
	}
	if len(cases) != 4 {
		t.Fatalf("len(cases)=%d want 4", len(cases))
	}
	for _, item := range cases {
		if item.TaskType != "change_impact" {
			t.Fatalf("unexpected task type %q", item.TaskType)
		}
	}
}

func TestLoadCheckedInCodeSearchBridgeEsotericDataset(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "testdata", "evals", "code-search-ensemble", "agentctl-bridge-esoteric.jsonl")
	cases, err := loadPromptEvalCases(path)
	if err != nil {
		t.Fatalf("loadPromptEvalCases() error = %v", err)
	}
	if len(cases) != 7 {
		t.Fatalf("len(cases)=%d want 7", len(cases))
	}
	if cases[0].TaskType != "execution_trace" || cases[6].TaskType != "symbol_inspect" {
		t.Fatalf("unexpected task types: %q %q", cases[0].TaskType, cases[6].TaskType)
	}
}

func TestSummarizeCodeSearchEnsembleEvalResultsIncludesTelemetry(t *testing.T) {
	t.Parallel()

	summary := summarizeCodeSearchEnsembleEvalResults([]codeSearchEnsembleEvalResult{
		{TotalToolCalls: 4, TotalTokens: 120, TotalCostUSD: 0.12, Grounded: true, Passed: true, CorrectnessScore: 1, SymbolRecall: 1, SnippetRecall: 1, FactRecall: 1},
		{TotalToolCalls: 6, TotalTokens: 80, TotalCostUSD: 0.08, Grounded: false, Passed: false, CorrectnessScore: 0.5, SymbolRecall: 0.5, SnippetRecall: 0.5, FactRecall: 0.5},
	}, codeSearchRouteAlertPolicy{})
	if summary.MeanToolCalls != 5 {
		t.Fatalf("mean tool calls=%v", summary.MeanToolCalls)
	}
	if summary.MeanTokens != 100 {
		t.Fatalf("mean tokens=%v", summary.MeanTokens)
	}
	if summary.MeanCostUSD != 0.1 {
		t.Fatalf("mean cost=%v", summary.MeanCostUSD)
	}
	if summary.MeanCorrectness != 0.75 {
		t.Fatalf("mean correctness=%v", summary.MeanCorrectness)
	}
}

func TestSummarizeCodeSearchEnsembleEvalResultsIncludesRouteFamilies(t *testing.T) {
	t.Parallel()

	summary := summarizeCodeSearchEnsembleEvalResults([]codeSearchEnsembleEvalResult{
		{
			RouteFamily:               "package_ownership",
			Passed:                    true,
			CorrectnessScore:          1,
			FileLocateEvidenceBuckets: map[string][]string{"primary_anchor": {"a.go"}, "repo_evidence": {"b.go", "c.go"}},
		},
		{
			RouteFamily:               "package_ownership",
			Passed:                    false,
			CorrectnessScore:          0.5,
			FileLocateEvidenceBuckets: map[string][]string{"primary_anchor": {"x.go"}, "secondary_anchor": {"y.go"}},
		},
		{
			RouteFamily:               "infra_resource",
			Passed:                    true,
			CorrectnessScore:          1,
			FileLocateEvidenceBuckets: map[string][]string{"primary_anchor": {"infra.yaml"}, "declarative_companion": {"companion.yaml"}},
		},
	}, codeSearchRouteAlertPolicy{
		MinPrimaryAnchor:             1.0,
		MaxSecondaryAnchor:           0.5,
		MinPackageRepoEvidence:       1.5,
		MinInfraDeclarativeCompanion: 1.0,
	})
	if len(summary.RouteFamilies) != 2 {
		t.Fatalf("route families=%v", summary.RouteFamilies)
	}
	pkg := summary.RouteFamilies["package_ownership"]
	if pkg.Count != 2 || pkg.PassRate != 0.5 || pkg.MeanCorrectness != 0.75 {
		t.Fatalf("package summary=%v", pkg)
	}
	if pkg.MeanBucketCounts["primary_anchor"] != 1 {
		t.Fatalf("package bucket counts=%v", pkg.MeanBucketCounts)
	}
	if pkg.MeanBucketCounts["repo_evidence"] != 1 {
		t.Fatalf("package bucket counts=%v", pkg.MeanBucketCounts)
	}
	if len(pkg.Alerts) == 0 {
		t.Fatalf("expected package alerts, got %v", pkg.Alerts)
	}
	infra := summary.RouteFamilies["infra_resource"]
	if infra.Count != 1 || infra.PassRate != 1 || infra.MeanCorrectness != 1 {
		t.Fatalf("infra summary=%v", infra)
	}
	if infra.MeanBucketCounts["declarative_companion"] != 1 {
		t.Fatalf("infra bucket counts=%v", infra.MeanBucketCounts)
	}
}
