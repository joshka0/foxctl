package cmd

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/intelligence/repoquery"
)

func TestNormalizeEvalModes_IncludesSkillModesWhenRequested(t *testing.T) {
	t.Parallel()

	got := normalizeEvalModes([]string{"baseline", "skill_context", "skill_default_plus_context", "aca_package_fallback", "aca_query_typed", "aca_default", "aca_cochange", "aca_cochange_continuity", "cochange_artifacts", "repoindex_search", "repoindex_dag", "rlm_llm_codeintel", "rlm_llm_code_staged", "skill_context"})
	want := []string{"baseline", "skill_context", "skill_default_plus_context", "aca_package_fallback", "aca_query_typed", "aca_default", "aca_cochange", "aca_cochange_continuity", "cochange_artifacts", "repoindex_search", "repoindex_dag", "rlm_llm_codeintel", "rlm_llm_code_staged"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeEvalModes()=%v want %v", got, want)
	}
}

func TestExtractSemanticSearchResultPaths(t *testing.T) {
	t.Parallel()

	results := []struct {
		Path string `json:"path"`
	}{
		{Path: "notes/repo/agentctl/index.md"},
		{Path: " notes/repo/agentctl/index.md "},
		{Path: ""},
		{Path: "00-home/index.md"},
	}

	got := extractSemanticSearchResultPaths(results)
	want := []string{"notes/repo/agentctl/index.md", "00-home/index.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractSemanticSearchResultPaths()=%v want %v", got, want)
	}
}

func TestExtractRepoAnchorPaths(t *testing.T) {
	t.Parallel()

	got := extractRepoAnchorPaths([]repoquery.Anchor{
		{Path: "internal/web/api/agents.go"},
		{Path: " internal/web/api/agents.go "},
		{Path: "internal/web/api/openapi.go"},
	})
	want := []string{"internal/web/api/agents.go", "internal/web/api/openapi.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractRepoAnchorPaths()=%v want %v", got, want)
	}
}

func TestExtractRetrievalHitPaths_IncludesRepoPaths(t *testing.T) {
	t.Parallel()

	got := extractRetrievalHitPaths([]contextplane.RetrievalHit{
		{Path: "notes/repo/praze/storage-memory.md", RepoPaths: []string{"internal/storage/memory/store.go", " internal/storage/memory/store.go "}},
		{Path: " notes/repo/praze/storage-memory.md "},
		{Path: "00-home/index.md"},
	})
	want := []string{"notes/repo/praze/storage-memory.md", "internal/storage/memory/store.go", "00-home/index.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractRetrievalHitPaths()=%v want %v", got, want)
	}
}

func TestResolveCorrectionSuitePath_DefaultsToCorrectionsDir(t *testing.T) {
	got, err := resolveCorrectionSuitePath("agentctl-inspectors")
	if err != nil {
		t.Fatalf("resolveCorrectionSuitePath: %v", err)
	}
	want := filepath.Join("testdata", "evals", "corrections", "agentctl-inspectors.yaml")
	if got != want {
		t.Fatalf("resolveCorrectionSuitePath()=%q want %q", got, want)
	}
}
