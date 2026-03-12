package cmd

import (
	"reflect"
	"testing"
)

func TestNormalizeEvalModes_IncludesSkillModesWhenRequested(t *testing.T) {
	t.Parallel()

	got := normalizeEvalModes([]string{"baseline", "skill_context", "skill_default_plus_context", "skill_context"})
	want := []string{"baseline", "skill_context", "skill_default_plus_context"}
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
