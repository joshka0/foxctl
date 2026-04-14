package profiles_test

import (
	"slices"
	"testing"

	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
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
	for _, want := range []string{"context_show", "context_retrieve", "obsidian_index_search", "obsidian_read", "obsidian_related"} {
		if !slices.Contains(normalized, want) {
			t.Fatalf("companion allowed tools=%v want %s", normalized, want)
		}
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

func TestDefaultSpecs_OverseerIncludesRepoIndexTools(t *testing.T) {
	t.Parallel()

	specs := profiles.DefaultSpecs()
	spec, ok := specs[coretool.ProfileOverseer]
	if !ok {
		t.Fatalf("missing %q profile", coretool.ProfileOverseer)
	}

	normalized := profiles.NormalizeAllowedTools(spec.AllowedTools)
	for _, want := range []string{"repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep", "smart_search"} {
		if !slices.Contains(normalized, want) {
			t.Fatalf("overseer allowed tools=%v want %s", normalized, want)
		}
	}
	for _, want := range []string{"context_show", "context_retrieve", "obsidian_index_search", "obsidian_read", "obsidian_related"} {
		if !slices.Contains(normalized, want) {
			t.Fatalf("overseer allowed tools=%v want %s", normalized, want)
		}
	}
}

func TestWithAllowedTools_AddsExtensionOverlayWithoutMutatingDefaults(t *testing.T) {
	t.Parallel()

	base := profiles.DefaultSpecs()
	overlaid := profiles.WithAllowedTools(base, map[coretool.ProcessProfile][]string{
		coretool.ProfileOverseer: {"heartwood/state", "heartwood/action"},
	})

	baseNormalized := profiles.NormalizeAllowedTools(base[coretool.ProfileOverseer].AllowedTools)
	if slices.Contains(baseNormalized, "heartwood_state") || slices.Contains(baseNormalized, "heartwood_action") {
		t.Fatalf("base specs unexpectedly mutated: %v", baseNormalized)
	}

	overlayNormalized := profiles.NormalizeAllowedTools(overlaid[coretool.ProfileOverseer].AllowedTools)
	for _, want := range []string{"heartwood_state", "heartwood_action"} {
		if !slices.Contains(overlayNormalized, want) {
			t.Fatalf("overlay allowed tools=%v want %s", overlayNormalized, want)
		}
	}
}
