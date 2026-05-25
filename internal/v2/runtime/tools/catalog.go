package tools

import (
	"fmt"
	"sort"

	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
	runtimetoolnames "github.com/joshka0/foxctl/internal/v2/runtime/toolnames"
)

// Catalog is the unified profile-aware v2 tool catalog.
type Catalog struct {
	toolsByName    map[string]compiledTool
	profileAllowed map[coretool.ProcessProfile]map[string]struct{}
}

// NewCatalog builds a deterministic profile-aware catalog.
func NewCatalog(defs []coretool.ToolDef, specs map[coretool.ProcessProfile]profiles.ProfileSpec) (*Catalog, error) {
	if len(specs) == 0 {
		specs = profiles.DefaultSpecs()
	}

	c := &Catalog{
		toolsByName:    map[string]compiledTool{},
		profileAllowed: map[coretool.ProcessProfile]map[string]struct{}{},
	}

	for profile, spec := range specs {
		set := map[string]struct{}{}
		for _, name := range profiles.NormalizeAllowedTools(spec.AllowedTools) {
			set[name] = struct{}{}
		}
		c.profileAllowed[profile] = set
	}

	for _, def := range defs {
		rawName := def.Name
		name := normalizeName(rawName)
		if name == "" {
			return nil, fmt.Errorf("invalid tool name %q", rawName)
		}
		if _, exists := c.toolsByName[name]; exists {
			return nil, fmt.Errorf("duplicate canonical tool name %q (input %q)", name, rawName)
		}
		def.Name = name
		schema, err := compileSchema(def.Parameters)
		if err != nil {
			return nil, err
		}
		c.toolsByName[name] = compiledTool{
			def:    def,
			schema: schema,
		}
	}
	return c, nil
}

// ForProfile returns allowed tools for the profile in deterministic order.
func (c *Catalog) ForProfile(profile coretool.ProcessProfile) []coretool.ToolDef {
	if c == nil {
		return nil
	}
	allowSet := c.profileAllowed[profile]
	out := make([]coretool.ToolDef, 0, len(c.toolsByName))

	names := c.namesSorted()
	for _, name := range names {
		ct := c.toolsByName[name]
		if !isAllowedForProfile(ct.def, profile, allowSet) {
			continue
		}
		out = append(out, ct.def)
	}
	return out
}

func (c *Catalog) Resolve(name string, profile coretool.ProcessProfile) (compiledTool, bool) {
	if c == nil {
		return compiledTool{}, false
	}
	n := normalizeName(name)
	ct, ok := c.toolsByName[n]
	if !ok {
		return compiledTool{}, false
	}
	if !isAllowedForProfile(ct.def, profile, c.profileAllowed[profile]) {
		return compiledTool{}, false
	}
	return ct, true
}

func (c *Catalog) namesSorted() []string {
	names := make([]string, 0, len(c.toolsByName))
	for name := range c.toolsByName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isAllowedForProfile(def coretool.ToolDef, profile coretool.ProcessProfile, allowSet map[string]struct{}) bool {
	// Profile allowlist is deny-by-default.
	if _, ok := allowSet[normalizeName(def.Name)]; !ok {
		return false
	}
	// Tool-level deny-by-default requires explicit profile allowlist on the tool.
	if def.Policy.DenyByDefault && len(def.Policy.AllowProfiles) == 0 {
		return false
	}
	// Optional tool-specific profile constraints.
	if !def.AllowedFor(profile) {
		return false
	}
	return true
}

func normalizeName(name string) string {
	return runtimetoolnames.Canonical(name)
}

var _ coretool.ToolCatalog = (*Catalog)(nil)
