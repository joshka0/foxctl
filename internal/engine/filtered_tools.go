package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// FilterToolDefs returns the subset of toolDefs whose names are present in allowlist.
//
// If allowlist is empty, toolDefs is returned unchanged.
func FilterToolDefs(toolDefs []ToolDef, allowlist []string) []ToolDef {
	allowed := normalizeToolAllowlist(allowlist)
	if len(allowed) == 0 {
		return toolDefs
	}
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}

	filtered := make([]ToolDef, 0, len(toolDefs))
	for _, td := range toolDefs {
		if _, ok := set[td.Name]; ok {
			filtered = append(filtered, td)
		}
	}
	return filtered
}

// FilteredToolExecutor enforces a tool allowlist on top of another ToolExecutor.
// It blocks Execute() calls for tools not in the allowlist and filters List().
type FilteredToolExecutor struct {
	inner   ToolExecutor
	allowed map[string]struct{}
}

// NewFilteredToolExecutor returns an executor that only allows tools listed in allowlist.
// If allowlist is empty, inner is returned unchanged.
func NewFilteredToolExecutor(inner ToolExecutor, allowlist []string) ToolExecutor {
	allowed := normalizeToolAllowlist(allowlist)
	if inner == nil || len(allowed) == 0 {
		return inner
	}
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}
	return &FilteredToolExecutor{
		inner:   inner,
		allowed: set,
	}
}

func (e *FilteredToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if _, ok := e.allowed[name]; !ok {
		return "", fmt.Errorf("tool %q is not allowed", name)
	}
	return e.inner.Execute(ctx, name, args)
}

func (e *FilteredToolExecutor) List() []ToolDef {
	defs := e.inner.List()
	if len(defs) == 0 {
		return defs
	}
	filtered := make([]ToolDef, 0, len(defs))
	for _, td := range defs {
		if _, ok := e.allowed[td.Name]; ok {
			filtered = append(filtered, td)
		}
	}
	return filtered
}

func normalizeToolAllowlist(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, entry := range in {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
