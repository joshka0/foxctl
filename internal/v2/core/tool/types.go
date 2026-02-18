package tool

import (
	"encoding/json"
	"strings"
)

// ProcessProfile identifies a tool policy scope for a runtime actor/process.
type ProcessProfile string

const (
	ProfileOverseer  ProcessProfile = "overseer"
	ProfileWorker    ProcessProfile = "worker"
	ProfileCompanion ProcessProfile = "companion"
)

// ToolPolicy carries profile-level constraints.
type ToolPolicy struct {
	// AllowProfiles further restricts which profiles may invoke this tool.
	AllowProfiles []ProcessProfile `json:"allow_profiles,omitempty"`

	// DenyByDefault indicates the tool requires explicit allowlisting.
	DenyByDefault bool `json:"deny_by_default,omitempty"`
}

// ToolDef is the canonical v2 tool definition.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Policy      ToolPolicy      `json:"policy,omitempty"`
}

// AllowedFor reports whether policy-level explicit profile restrictions allow profile.
func (d ToolDef) AllowedFor(profile ProcessProfile) bool {
	if len(d.Policy.AllowProfiles) == 0 {
		return !d.Policy.DenyByDefault
	}
	for _, p := range d.Policy.AllowProfiles {
		if strings.EqualFold(string(p), string(profile)) {
			return true
		}
	}
	return false
}

// ToolCatalog returns tool definitions available to a profile.
type ToolCatalog interface {
	ForProfile(profile ProcessProfile) []ToolDef
}
