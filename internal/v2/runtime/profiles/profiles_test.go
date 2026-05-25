package profiles_test

import (
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/quick"

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
	for _, want := range []string{"repo_index_build", "repo_index_enrich_summaries", "repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep", "smart_search"} {
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

func TestNormalizeAllowedToolsPropertyDedupesSortsAndCanonicalizesAliases(t *testing.T) {
	t.Parallel()

	property := func(raw []uint8) bool {
		in := make([]string, 0, len(raw)*2+3)
		wantSet := map[string]struct{}{}
		for _, seed := range raw {
			alias, canonical := generatedToolAlias(seed)
			in = append(in, alias)
			if canonical != "" {
				wantSet[canonical] = struct{}{}
			}
			if seed%4 == 0 {
				in = append(in, alias)
			}
		}
		in = append(in, "", " ", "___")

		got := profiles.NormalizeAllowedTools(in)
		want := make([]string, 0, len(wantSet))
		for canonical := range wantSet {
			want = append(want, canonical)
		}
		sort.Strings(want)

		if !slices.Equal(got, want) {
			t.Logf("NormalizeAllowedTools(%q)=%v want %v", in, got, want)
			return false
		}
		if !sort.StringsAreSorted(got) {
			t.Logf("NormalizeAllowedTools output not sorted: %v", got)
			return false
		}
		for i, name := range got {
			if name == "" || strings.ContainsAny(name, "./ ") {
				t.Logf("non-canonical output[%d]=%q from input %q", i, name, in)
				return false
			}
			if i > 0 && got[i] == got[i-1] {
				t.Logf("duplicate output %q in %v", got[i], got)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAcceptsTrimmedCaseInsensitiveKnownProfiles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		input string
		want  coretool.ProcessProfile
	}{
		{input: " OVERSEER ", want: coretool.ProfileOverseer},
		{input: "worker", want: coretool.ProfileWorker},
		{input: " Companion ", want: coretool.ProfileCompanion},
	} {
		got, err := profiles.Resolve(tc.input)
		if err != nil {
			t.Fatalf("Resolve(%q) error=%v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("Resolve(%q)=%q want %q", tc.input, got, tc.want)
		}
	}
}

func TestWithAllowedToolsAddsMissingExtensionProfileAndCopiesSlices(t *testing.T) {
	t.Parallel()

	base := map[coretool.ProcessProfile]profiles.ProfileSpec{
		coretool.ProfileWorker: {
			Profile:      coretool.ProfileWorker,
			AllowedTools: []string{"fs/read", "code.search"},
		},
	}
	extensionProfile := coretool.ProcessProfile("extension")
	overlaid := profiles.WithAllowedTools(base, map[coretool.ProcessProfile][]string{
		extensionProfile: {"heartwood/state", "heartwood.state", " heartwood/action "},
	})

	base[coretool.ProfileWorker].AllowedTools[0] = "mutated"
	if got := overlaid[coretool.ProfileWorker].AllowedTools[0]; got != "fs/read" {
		t.Fatalf("overlay aliased base slice, got worker tools=%v", overlaid[coretool.ProfileWorker].AllowedTools)
	}

	got := overlaid[extensionProfile]
	if got.Profile != extensionProfile {
		t.Fatalf("extension profile=%q want %q", got.Profile, extensionProfile)
	}
	want := []string{"heartwood_action", "heartwood_state"}
	if !slices.Equal(got.AllowedTools, want) {
		t.Fatalf("extension tools=%v want %v", got.AllowedTools, want)
	}
}

func generatedToolAlias(seed uint8) (alias string, canonical string) {
	canonicalNames := []string{
		"code_search",
		"context_retrieve",
		"fs_read_file",
		"memory_query",
		"obsidian_related",
		"repo_index_search",
		"todo_set_active",
	}
	canonical = canonicalNames[int(seed)%len(canonicalNames)]
	switch (seed / 7) % 6 {
	case 0:
		return canonical, canonical
	case 1:
		return strings.ReplaceAll(canonical, "_", "/"), canonical
	case 2:
		return strings.ReplaceAll(canonical, "_", "."), canonical
	case 3:
		return " " + strings.ToUpper(strings.ReplaceAll(canonical, "_", "/")) + " ", canonical
	case 4:
		return "__" + canonical + "__", canonical
	default:
		return " ", ""
	}
}
