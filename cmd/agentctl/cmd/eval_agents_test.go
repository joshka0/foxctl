package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configpkg "github.com/jkatigb/agentctl/internal/platform/config"
)

func TestResolveAgentEvalTargetsDefaults(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{
		LLM: configpkg.LLMSettings{
			OpenRouterAPIKey: "openrouter-key",
		},
	}

	targets, err := resolveAgentEvalTargets(cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("resolveAgentEvalTargets() error = %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("len(targets)=%d want 3", len(targets))
	}
	if targets[0].Provider != "openrouter" || targets[0].Model != "openai/gpt-5.4-nano" {
		t.Fatalf("targets[0]=%+v", targets[0])
	}
	if targets[1].Provider != "openrouter" || targets[1].Model != "minimax/minimax-m2.7" {
		t.Fatalf("targets[1]=%+v", targets[1])
	}
	if targets[2].Provider != "lmstudio" || targets[2].Model != "liquid/lfm2.5-1.2b" {
		t.Fatalf("targets[2]=%+v", targets[2])
	}
}

func TestResolveAgentEvalTargetsExplicit(t *testing.T) {
	t.Parallel()

	cfg := configpkg.Config{
		LLM: configpkg.LLMSettings{
			OpenRouterAPIKey: "openrouter-key",
		},
	}

	targets, err := resolveAgentEvalTargets(cfg, "lmstudio", nil, []string{
		"lmstudio:liquid/lfm2.5-1.2b",
		"openrouter:minimax/minimax-m2.7",
	})
	if err != nil {
		t.Fatalf("resolveAgentEvalTargets() error = %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets)=%d want 2", len(targets))
	}
	if targets[0].Provider != "lmstudio" || targets[0].Model != "liquid/lfm2.5-1.2b" {
		t.Fatalf("targets[0]=%+v", targets[0])
	}
	if targets[1].Provider != "openrouter" || targets[1].Model != "minimax/minimax-m2.7" {
		t.Fatalf("targets[1]=%+v", targets[1])
	}
}

func TestBuildAgentEvalPromptIncludesMemoryTarget(t *testing.T) {
	t.Parallel()

	got := buildAgentEvalPrompt("memory_fact_scout", promptEvalCase{
		Question: "What is the current codename?",
		Context:  "Prefer the latest retained answer.",
	}, "agent-123", "conv-456")

	if got == "" {
		t.Fatal("expected non-empty prompt")
	}
	if !containsAll(got, "Target memory agent ref: agent-123", "Target conversation lineage: conv-456", "Question:\nWhat is the current codename?") {
		t.Fatalf("prompt=%q", got)
	}
}

func TestBuildAgentEvalPromptForCodingLocateNarrowsResearcher(t *testing.T) {
	t.Parallel()

	got := buildAgentEvalPrompt("researcher", promptEvalCase{
		Question:      "Which files must change to add a new scout role?",
		TaskType:      "file_locate",
		ExpectedPaths: []string{"internal/agent/runtime/runtime.go"},
		ExcludedPaths: []string{"internal/rlm/env/**"},
	}, "", "")

	if !containsAll(got,
		"This is a repo-grounded file-location task.",
		"Task type: file_locate.",
		"Use only the shortest path to verified repo files",
		"Prefer semantic_search_code, repo_index_search, smart_search, code_search, code_symbols, and fs_read_file",
		"Treat these paths or prefixes as out of scope unless the question explicitly asks for them: internal/rlm/env/**",
	) {
		t.Fatalf("prompt=%q", got)
	}
	if strings.Contains(got, "This is a memory-focused evaluation.") {
		t.Fatalf("prompt should not use memory-focused language: %q", got)
	}
}

func TestBuildAgentEvalPromptForSemanticScoutMentionsCodeAlias(t *testing.T) {
	t.Parallel()

	got := buildAgentEvalPrompt("semantic_scout", promptEvalCase{
		Question:      "Which files must change to add a new scout role?",
		ExpectedPaths: []string{"internal/agent/runtime/runtime.go"},
	}, "", "")

	if !strings.Contains(got, "Prefer semantic_search_code as your first discovery lane") {
		t.Fatalf("prompt=%q", got)
	}
}

func TestLoadExternalAgentEvalResults(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "external.jsonl")
	body := `{"case_id":"case-1","label":"codex_spark","role":"memory_fact_scout","provider":"codex","model":"gpt-5.3-codex-spark","runner":"external","output":"The current codename is amber-river-19.","duration_ms":1200,"input_tokens":100,"output_tokens":50,"total_cost_usd":0.02}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	results, err := loadExternalAgentEvalResults([]string{path}, []promptEvalCase{{
		ID:             "case-1",
		Question:       "What is the current codename?",
		TargetResponse: "The current codename is amber-river-19.",
	}}, 0.8)
	if err != nil {
		t.Fatalf("loadExternalAgentEvalResults() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].Runner != "external" || results[0].Provider != "codex" {
		t.Fatalf("result=%+v", results[0])
	}
	if !results[0].Passed {
		t.Fatalf("expected imported result to pass: %+v", results[0])
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}

func TestScoreExpectedPaths(t *testing.T) {
	t.Parallel()

	matched, recall := scoreExpectedPaths([]string{
		"internal/agent/runtime/runtime.go",
		"cmd/agentctl/cmd/agent_memory.go",
	}, "Touch internal/agent/runtime/runtime.go and cmd/agentctl/cmd/agent_memory.go next.")
	if recall != 1 {
		t.Fatalf("recall=%v want 1", recall)
	}
	if len(matched) != 2 {
		t.Fatalf("matched=%v", matched)
	}
}

func TestShouldPassAgentEvalUsesPathRecallWhenExpectedPathsPresent(t *testing.T) {
	t.Parallel()

	passed := shouldPassAgentEval(agentEvalResult{
		PathRecall:   1,
		QualityScore: 0.1,
	}, promptEvalCase{
		ExpectedPaths: []string{"internal/agent/runtime/runtime.go"},
	}, 0.8)
	if !passed {
		t.Fatal("expected path-based pass")
	}
}

func TestShouldPassAgentEvalFailsOnExcludedPathHits(t *testing.T) {
	t.Parallel()

	passed := shouldPassAgentEval(agentEvalResult{
		PathRecall:       1,
		ExcludedPathHits: []string{"internal/rlm/env/scout_roles.go"},
	}, promptEvalCase{
		ExpectedPaths: []string{"internal/agent/runtime/runtime.go"},
		ExcludedPaths: []string{"internal/rlm/env/scout_roles.go"},
	}, 0.8)
	if passed {
		t.Fatal("expected excluded path hit to fail the eval")
	}
}

func TestScoreExcludedPaths(t *testing.T) {
	t.Parallel()

	hits, penalty := scoreExcludedPaths([]string{
		"internal/rlm/env/scout_roles.go",
	}, "Consider internal/rlm/env/scout_roles.go and internal/agent/runtime/runtime.go.")
	if len(hits) != 1 || hits[0] != "internal/rlm/env/scout_roles.go" {
		t.Fatalf("hits=%v", hits)
	}
	if penalty != 1 {
		t.Fatalf("penalty=%v want 1", penalty)
	}
}

func TestParseStructuredAgentEvalOutput(t *testing.T) {
	t.Parallel()

	got, ok := parseStructuredAgentEvalOutput(`{"summary":"Update runtime wiring","paths":["internal/agent/runtime/runtime.go","internal/agent/types/types.go"],"symbols":["internal/agent/runtime/runtime.go::BuildToolDefsForRole"],"snippets":[{"path":"internal/agent/runtime/runtime.go","start_line":10,"end_line":20,"reason":"tool bundle"}],"facts":["role constant"],"rationale":"role constant plus tool bundle"}`)
	if !ok {
		t.Fatal("expected structured output to parse")
	}
	if len(got.Paths) != 2 {
		t.Fatalf("paths=%v", got.Paths)
	}
	if len(got.Symbols) != 1 || len(got.Snippets) != 1 || len(got.Facts) != 1 {
		t.Fatalf("structured=%+v", got)
	}
}

func TestShouldPassAgentEvalUsesCorrectnessWhenExpectedEvidencePresent(t *testing.T) {
	t.Parallel()

	passed := shouldPassAgentEval(agentEvalResult{
		PathRecall:       1,
		SymbolRecall:     1,
		SnippetRecall:    1,
		FactRecall:       1,
		CorrectnessScore: 1,
	}, promptEvalCase{
		ExpectedPaths:   []string{"internal/rlm/env/code_search_ensemble.go"},
		ExpectedSymbols: []string{"internal/rlm/env/code_search_ensemble.go::codeSearchEnsembleInput"},
		RequiredFacts:   []string{"codeSearchEnsembleInput"},
	}, 0.8)
	if !passed {
		t.Fatal("expected correctness-based pass")
	}
}

func TestJudgeTextFromStructuredScoutOutput(t *testing.T) {
	t.Parallel()

	got, ok := judgeTextFromStructuredScoutOutput(`{"summary":"Current codename is amber-river-19.","claims":[{"key":"codename","value":"amber-river-19"}],"gaps":["rollback color not found"]}`)
	if !ok {
		t.Fatal("expected structured scout output to parse")
	}
	if !strings.Contains(got, "amber-river-19") {
		t.Fatalf("judge text=%q", got)
	}
	if !strings.Contains(got, "Gap: rollback color not found") {
		t.Fatalf("judge text=%q", got)
	}
}

func TestEvalSkillsAllowForResearcherCodingCase(t *testing.T) {
	t.Parallel()

	got := evalSkillsAllow("researcher", promptEvalCase{ExpectedPaths: []string{"internal/agent/runtime/runtime.go"}})
	if len(got) == 0 {
		t.Fatal("expected non-empty allowlist")
	}
	for _, deny := range []string{"session_recall", "session_timeline", "memory_query", "context_show", "context_retrieve", "context_grep", "repo_index_expand", "repo_index_dag_grep"} {
		for _, item := range got {
			if item == deny {
				t.Fatalf("allowlist unexpectedly contains %q: %v", deny, got)
			}
		}
	}
}

func TestValidateRepoRelativePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "internal", "agent", "runtime", "runtime.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("package runtime\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	valid, invalid := validateRepoRelativePaths(root, []string{
		"internal/agent/runtime/runtime.go",
		"repo/fake/path.go",
	})
	if len(valid) != 1 || valid[0] != "internal/agent/runtime/runtime.go" {
		t.Fatalf("valid=%v", valid)
	}
	if len(invalid) != 1 || invalid[0] != "repo/fake/path.go" {
		t.Fatalf("invalid=%v", invalid)
	}
}

func TestValidateRepoRelativePaths_NormalizesPrefixAndUniqueBasename(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "internal", "rlm", "env", "memory_ensemble.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("package env\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	valid, invalid := validateRepoRelativePaths(root, []string{
		"repo/internal/rlm/env/memory_ensemble.go",
		"repo/models/memory_ensemble.go",
	})
	if len(invalid) != 0 {
		t.Fatalf("invalid=%v want none", invalid)
	}
	if len(valid) != 1 || valid[0] != "internal/rlm/env/memory_ensemble.go" {
		t.Fatalf("valid=%v", valid)
	}
}

func TestAgentEvalSupportsRequiredToolUse(t *testing.T) {
	t.Parallel()

	if agentEvalSupportsRequiredToolUse("openrouter", "minimax/minimax-m2.7") {
		t.Fatal("expected minimax target to disable required tool use")
	}
	if !agentEvalSupportsRequiredToolUse("openrouter", "openai/gpt-5.4-nano") {
		t.Fatal("expected gpt-5.4-nano target to support required tool use")
	}
}
