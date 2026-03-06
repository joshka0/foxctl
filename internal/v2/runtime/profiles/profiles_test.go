package profiles_test

import (
	"slices"
	"testing"

	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	"github.com/jkatigb/agentctl/internal/v2/runtime/profiles"
)

func TestNormalizeAllowedTools_CanonicalizesAliases(t *testing.T) {
	t.Parallel()

	got := profiles.NormalizeAllowedTools([]string{
		"code/search",
		"code_search",
		"code.search",
		" memory/query ",
		"memory_query",
		"memory.query",
		"",
		" ",
	})
	want := []string{
		"code_search",
		"memory_query",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("NormalizeAllowedTools()=%v want %v", got, want)
	}
}

func TestDefaultSpecs_CompanionIncludesMemoryQuery(t *testing.T) {
	t.Parallel()

	specs := profiles.DefaultSpecs()
	spec, ok := specs[coretool.ProfileCompanion]
	if !ok {
		t.Fatalf("missing %q profile", coretool.ProfileCompanion)
	}

	normalized := profiles.NormalizeAllowedTools(spec.AllowedTools)
	if !slices.Contains(normalized, "memory_query") {
		t.Fatalf("companion allowed tools=%v want memory_query", normalized)
	}
	for _, want := range []string{"todo_ensure_active", "todo_query", "todo_set_active"} {
		if !slices.Contains(normalized, want) {
			t.Fatalf("companion allowed tools=%v want %s", normalized, want)
		}
	}
}

func TestDefaultSpecs_OverseerIncludesTodoTools(t *testing.T) {
	t.Parallel()

	specs := profiles.DefaultSpecs()
	spec, ok := specs[coretool.ProfileOverseer]
	if !ok {
		t.Fatalf("missing %q profile", coretool.ProfileOverseer)
	}

	normalized := profiles.NormalizeAllowedTools(spec.AllowedTools)
	for _, want := range []string{"todo_add", "todo_complete", "todo_graph_insights", "todo_query"} {
		if !slices.Contains(normalized, want) {
			t.Fatalf("overseer allowed tools=%v want %s", normalized, want)
		}
	}
}
