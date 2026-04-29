package longcoteval

import (
	"sort"
	"strings"
)

type LeakageOptions struct {
	NetworkEnabled                bool
	SubcallsAllowed               bool
	VerifierAccessibleDuringSolve bool
	DatasetAccessibleDuringSolve  bool
	AnswerAccessibleDuringSolve   bool
}

var forbiddenPrimaryTools = map[string]struct{}{
	"get_top_of_mind":          {},
	"get_latest_handoff":       {},
	"search_scenes":            {},
	"get_scene":                {},
	"search_artifacts":         {},
	"load_artifact":            {},
	"semantic_search_code":     {},
	"smart_search_code":        {},
	"ripgrep_code":             {},
	"search_repo":              {},
	"expand_repo_graph":        {},
	"load_file":                {},
	"search_vault":             {},
	"read_note":                {},
	"memory_ensemble_retrieve": {},
	"code_search_ensemble":     {},
	"subcall":                  {},
	"rlm_query":                {},
	"shell":                    {},
	"structured_shell":         {},
}

var (
	filesystemTools = exactSet("load_file")
	repoTools       = exactSet("semantic_search_code", "smart_search_code", "ripgrep_code", "search_repo", "expand_repo_graph", "code_search_ensemble")
	memoryTools     = exactSet("memory_ensemble_retrieve", "search_scenes", "get_scene", "get_top_of_mind", "get_latest_handoff")
	vaultTools      = exactSet("search_vault", "read_note")
	artifactTools   = exactSet("search_artifacts", "load_artifact")
	shellTools      = exactSet("shell", "structured_shell")
	subcallTools    = exactSet("subcall", "rlm_query")
)

// AssessLeakage derives benchmark-contamination flags from exact tool names and
// explicit runtime options. It intentionally avoids prompt/content heuristics.
func AssessLeakage(exposedToolNames []string, opts LeakageOptions) LeakageFlags {
	flags := LeakageFlags{
		NetworkEnabled:                opts.NetworkEnabled,
		SubcallAllowed:                opts.SubcallsAllowed,
		VerifierAccessibleDuringSolve: opts.VerifierAccessibleDuringSolve,
		DatasetAccessibleDuringSolve:  opts.DatasetAccessibleDuringSolve,
		AnswerAccessibleDuringSolve:   opts.AnswerAccessibleDuringSolve,
	}
	for _, raw := range exposedToolNames {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := forbiddenPrimaryTools[name]; ok && !opts.toolAllowed(name) {
			flags.ForbiddenToolNames = append(flags.ForbiddenToolNames, name)
		}
		if _, ok := filesystemTools[name]; ok {
			flags.FilesystemEnabled = true
		}
		if _, ok := repoTools[name]; ok {
			flags.RepoSearchEnabled = true
		}
		if _, ok := memoryTools[name]; ok {
			flags.MemoryEnabled = true
		}
		if _, ok := vaultTools[name]; ok {
			flags.VaultEnabled = true
		}
		if _, ok := artifactTools[name]; ok {
			flags.ArtifactEnabled = true
		}
		if _, ok := shellTools[name]; ok {
			flags.ShellEnabled = true
		}
		if _, ok := subcallTools[name]; ok {
			flags.SubcallEnabled = true
		}
	}
	flags.ForbiddenToolNames = uniqueSorted(flags.ForbiddenToolNames)
	return flags
}

func (opts LeakageOptions) toolAllowed(name string) bool {
	if opts.SubcallsAllowed {
		if _, ok := subcallTools[name]; ok {
			return true
		}
	}
	return false
}

func exactSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
