package env

import (
	"github.com/joshka0/foxctl/internal/rlm"
)

const (
	ToolProfileDefault             = string(rlm.ToolProfileDefault)
	ToolProfileCodeIntel           = string(rlm.ToolProfileCodeIntel)
	ToolProfileMemoryRecall        = string(rlm.ToolProfileMemoryRecall)
	ToolProfileLongCoTNoModelTools = string(rlm.ToolProfileLongCoTNoModelTools)
)

// ResolveToolProfile resolves one tool policy profile against available tools.
func ResolveToolProfile(tools []rlm.Tool, profile string) (rlm.ToolPolicy, error) {
	return rlm.ResolveToolPolicy(tools, profile)
}

// FilterTools returns a constrained tool set for one experimental profile.
// Unknown profiles fail closed to an empty model-visible tool set.
func FilterTools(tools []rlm.Tool, profile string) []rlm.Tool {
	resolved, err := ResolveToolProfile(tools, profile)
	if err != nil {
		return []rlm.Tool{}
	}
	return resolved.Tools
}
