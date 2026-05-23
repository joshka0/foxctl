package tools_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
	"github.com/joshka0/foxctl/internal/v2/runtime/tools"
)

func TestDefaultDefsIncludeContextWikiAndObsidianReadTools(t *testing.T) {
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

func TestNewDefaultCatalogResolvesCompanionContextWikiAndObsidianTools(t *testing.T) {
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

func TestDefaultDefsParameterSchemasUseTypedObjectSubset(t *testing.T) {
	t.Parallel()

	defs := append([]coretool.ToolDef{}, tools.DefaultDefs()...)
	defs = append(defs, tools.ExtensionDefs()...)

	for _, def := range defs {
		var schema tools.JSONSchema
		if err := json.Unmarshal(def.Parameters, &schema); err != nil {
			t.Fatalf("%s parameters are not a typed JSON schema: %v", def.Name, err)
		}
		if schema.Type != tools.JSONSchemaTypeObject {
			t.Fatalf("%s schema type=%q want %q", def.Name, schema.Type, tools.JSONSchemaTypeObject)
		}
		for _, required := range schema.Required {
			if _, ok := schema.Properties[required]; !ok {
				t.Fatalf("%s required field %q is not declared in properties", def.Name, required)
			}
		}
		for name, field := range schema.Properties {
			if strings.TrimSpace(name) == "" {
				t.Fatalf("%s declares an empty property name", def.Name)
			}
			if !knownSchemaType(field.Type) {
				t.Fatalf("%s.%s schema type=%q is not in the supported subset", def.Name, name, field.Type)
			}
			if strings.TrimSpace(field.Description) == "" {
				t.Fatalf("%s.%s is missing a description", def.Name, name)
			}
		}
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

func knownSchemaType(typ tools.JSONSchemaType) bool {
	switch typ {
	case tools.JSONSchemaTypeObject,
		tools.JSONSchemaTypeString,
		tools.JSONSchemaTypeBoolean,
		tools.JSONSchemaTypeNumber,
		tools.JSONSchemaTypeInteger,
		tools.JSONSchemaTypeArray:
		return true
	default:
		return false
	}
}

func profilesSpecWithHeartwood() map[coretool.ProcessProfile]profiles.ProfileSpec {
	specs := profiles.DefaultSpecs()
	overseer := specs[coretool.ProfileOverseer]
	overseer.AllowedTools = append(overseer.AllowedTools, "heartwood/state", "heartwood/action")
	specs[coretool.ProfileOverseer] = overseer
	return specs
}
