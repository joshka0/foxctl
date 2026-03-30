package env

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/rlm"
)

const (
	ToolProfileDefault   = "default"
	ToolProfileCodeIntel = "code-intel"
)

// FilterTools returns a constrained tool set for one experimental profile.
func FilterTools(tools []rlm.Tool, profile string) []rlm.Tool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", ToolProfileDefault:
		return append([]rlm.Tool(nil), tools...)
	case ToolProfileCodeIntel:
		allowed := map[string]struct{}{
			"semantic_search_code": {},
			"smart_search_code":    {},
			"ripgrep_code":         {},
			"code_search_ensemble": {},
			"load_file":            {},
			"search_vault":         {},
			"read_note":            {},
			"subcall":              {},
		}
		out := make([]rlm.Tool, 0, len(tools))
		for _, tool := range tools {
			if _, ok := allowed[strings.TrimSpace(tool.Name)]; ok {
				out = append(out, tool)
			}
		}
		return out
	default:
		return append([]rlm.Tool(nil), tools...)
	}
}
