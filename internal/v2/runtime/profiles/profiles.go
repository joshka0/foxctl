package profiles

import (
	"fmt"
	"sort"
	"strings"

	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	runtimetoolnames "github.com/jkatigb/agentctl/internal/v2/runtime/toolnames"
)

// ProfileSpec defines a deny-by-default allowlist for one process profile.
type ProfileSpec struct {
	Profile      coretool.ProcessProfile `json:"profile"`
	AllowedTools []string                `json:"allowed_tools"`
}

// DefaultSpecs returns deterministic built-in allowlists for v2 PR-04.
func DefaultSpecs() map[coretool.ProcessProfile]ProfileSpec {
	return map[coretool.ProcessProfile]ProfileSpec{
		coretool.ProfileOverseer: {
			Profile: coretool.ProfileOverseer,
			AllowedTools: []string{
				"agent_hierarchy",
				"agent_kill",
				"agent_list",
				"agent_spawn",
				"agent_status",
				"agent_wait",
				"code/search",
				"context_grep",
				"context_search",
				"fs_list_dir",
				"fs_read_file",
				"repo_index_dag_grep",
				"repo_index_expand",
				"repo_index_open",
				"repo_index_search",
				"session_timeline",
				"smart_search",
				"think",
				"todo/add",
				"todo/complete",
				"todo/graph_insights",
				"todo/query",
			},
		},
		coretool.ProfileWorker: {
			Profile: coretool.ProfileWorker,
			AllowedTools: []string{
				"code/search",
				"fs_list_dir",
				"fs_read_file",
				"fs_write_file",
				"think",
			},
		},
		coretool.ProfileCompanion: {
			Profile: coretool.ProfileCompanion,
			AllowedTools: []string{
				"code/search",
				"context_search",
				"fs_list_dir",
				"fs_read_file",
				"memory/query",
				"session_timeline",
				"smart_search",
				"think",
				"todo/ensure_active",
				"todo/query",
				"todo/set_active",
			},
		},
	}
}

// Resolve normalizes a textual profile name.
func Resolve(name string) (coretool.ProcessProfile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case string(coretool.ProfileOverseer):
		return coretool.ProfileOverseer, nil
	case string(coretool.ProfileWorker):
		return coretool.ProfileWorker, nil
	case string(coretool.ProfileCompanion):
		return coretool.ProfileCompanion, nil
	default:
		return "", fmt.Errorf("unknown process profile %q", name)
	}
}

// NormalizeAllowedTools returns a deduplicated sorted allowlist.
func NormalizeAllowedTools(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, name := range in {
		n := runtimetoolnames.Canonical(name)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
