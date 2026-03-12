package tools_test

import (
	"slices"
	"strings"
	"testing"

	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	"github.com/jkatigb/agentctl/internal/v2/runtime/profiles"
	"github.com/jkatigb/agentctl/internal/v2/runtime/tools"
)

func TestDefaultDefsIncludeACAAndObsidianReadTools(t *testing.T) {
	t.Parallel()

	names := canonicalNames(tools.DefaultDefs())
	for _, want := range []string{
		"context_show",
		"context_retrieve",
		"obsidian_index_search",
		"obsidian_read",
		"obsidian_related",
	} {
		if !slices.Contains(names, want) {
			t.Fatalf("default defs=%v want %s", names, want)
		}
	}
}

func TestExtensionDefsKeepHeartwoodOutOfPortableDefaults(t *testing.T) {
	t.Parallel()

	defaultNames := canonicalNames(tools.DefaultDefs())
	if slices.Contains(defaultNames, "heartwood_state") || slices.Contains(defaultNames, "heartwood_action") {
		t.Fatalf("portable default defs must not include heartwood extension tools: %v", defaultNames)
	}

	extensionNames := canonicalNames(tools.ExtensionDefs())
	for _, want := range []string{"heartwood_state", "heartwood_action"} {
		if !slices.Contains(extensionNames, want) {
			t.Fatalf("extension defs=%v want %s", extensionNames, want)
		}
	}
}

func TestNewDefaultCatalogResolvesCompanionACAAndObsidianTools(t *testing.T) {
	t.Parallel()

	catalog, err := tools.NewDefaultCatalog(nil, false)
	if err != nil {
		t.Fatalf("NewDefaultCatalog returned error: %v", err)
	}

	for _, want := range []string{
		"context/show",
		"context/retrieve",
		"obsidian/index_search",
		"obsidian/read",
		"obsidian/related",
	} {
		if _, ok := catalog.Resolve(want, coretool.ProfileCompanion); !ok {
			t.Fatalf("companion catalog missing %s", want)
		}
	}
}

func TestNewDefaultCatalogExcludesHeartwoodUnlessExtensionsRequested(t *testing.T) {
	t.Parallel()

	catalog, err := tools.NewDefaultCatalog(nil, false)
	if err != nil {
		t.Fatalf("NewDefaultCatalog returned error: %v", err)
	}
	if _, ok := catalog.Resolve("heartwood/state", coretool.ProfileOverseer); ok {
		t.Fatal("heartwood extension unexpectedly present in default catalog")
	}

	withExtensions, err := tools.NewDefaultCatalog(profilesSpecWithHeartwood(), true)
	if err != nil {
		t.Fatalf("NewDefaultCatalog with extensions returned error: %v", err)
	}
	if _, ok := withExtensions.Resolve("heartwood/state", coretool.ProfileOverseer); !ok {
		t.Fatal("heartwood extension missing when extensions enabled and profile allows it")
	}
}

func canonicalNames(defs []coretool.ToolDef) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, toolsCanonical(def.Name))
	}
	slices.Sort(out)
	return out
}

func toolsCanonical(name string) string {
	return strings.NewReplacer(".", "_", "/", "_").Replace(name)
}

func profilesSpecWithHeartwood() map[coretool.ProcessProfile]profiles.ProfileSpec {
	specs := profiles.DefaultSpecs()
	overseer := specs[coretool.ProfileOverseer]
	overseer.AllowedTools = append(overseer.AllowedTools, "heartwood/state", "heartwood/action")
	specs[coretool.ProfileOverseer] = overseer
	return specs
}
