package toolnames

import "strings"

type ToolMode string

const (
	ToolModeRuntime ToolMode = "runtime"
	ToolModeLegacy  ToolMode = "legacy"
)

var (
	runtimeAliases map[string]string
	legacyAliases  map[string]string
)

var runtimeToolNames = []string{
	"fs_read_file", "fs_list_dir", "fs_write_file", "code_search", "think",
	"context_search", "smart_search", "context_grep", "session_timeline",
	"repo_index_search", "repo_index_expand", "repo_index_open", "repo_index_dag_grep",
	"agent_spawn", "agent_list", "agent_status", "agent_kill", "agent_hierarchy", "agent_wait",
	"mail_inbox", "mail_send", "mail_ack",
	"bb_inbox", "bb_post", "bb_mark_read",
}

func init() {
	runtimeAliases = make(map[string]string, len(runtimeToolNames)*5)
	legacyAliases = make(map[string]string, len(runtimeToolNames)*5)

	for _, canonical := range runtimeToolNames {
		dotted := canonicalToDotted(canonical)
		slash := canonicalToSlash(canonical)
		firstDotted := firstSepReplace(canonical, '.')
		firstSlash := firstSepReplace(canonical, '/')

		addAlias(runtimeAliases, canonical, canonical)
		addAlias(runtimeAliases, dotted, canonical)
		addAlias(runtimeAliases, slash, canonical)
		addAlias(runtimeAliases, firstDotted, canonical)
		addAlias(runtimeAliases, firstSlash, canonical)

		addAlias(legacyAliases, dotted, dotted)
		addAlias(legacyAliases, canonical, dotted)
		addAlias(legacyAliases, slash, dotted)
		addAlias(legacyAliases, firstDotted, dotted)
		addAlias(legacyAliases, firstSlash, dotted)
	}
}

func CanonicalizeToolName(mode ToolMode, name string) (string, bool) {
	key := strings.ToLower(name)
	switch mode {
	case ToolModeRuntime:
		canonical, ok := runtimeAliases[key]
		return canonical, ok
	case ToolModeLegacy:
		canonical, ok := legacyAliases[key]
		return canonical, ok
	default:
		return "", false
	}
}

func NormalizeAllowlist(mode ToolMode, allowlist []string) []string {
	return normalizeAllowlist(mode, allowlist)
}

func ValidateAllowlist(mode ToolMode, allowlist []string) (normalized []string, unknown []string) {
	seenUnknown := make(map[string]struct{})
	seenNormalized := make(map[string]struct{})

	for _, name := range allowlist {
		canonical, ok := CanonicalizeToolName(mode, name)
		if !ok {
			canonical = name
			if _, exists := seenUnknown[canonical]; !exists {
				unknown = append(unknown, canonical)
				seenUnknown[canonical] = struct{}{}
			}
		}
		if _, exists := seenNormalized[canonical]; exists {
			continue
		}
		seenNormalized[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	return normalized, unknown
}

func RuntimeToolNames() []string {
	return append([]string(nil), runtimeToolNames...)
}

func canonicalToDotted(name string) string {
	return strings.ReplaceAll(name, "_", ".")
}

func canonicalToSlash(name string) string {
	return strings.ReplaceAll(name, "_", "/")
}

// firstSepReplace replaces only the first underscore with sep, preserving the rest.
// This handles legacy forms like "fs.read_file" where only the namespace separator is dotted.
func firstSepReplace(name string, sep byte) string {
	if n := strings.IndexByte(name, '_'); n >= 0 {
		return name[:n] + string(sep) + name[n+1:]
	}
	return name
}

func addAlias(m map[string]string, alias string, canonical string) {
	m[strings.ToLower(alias)] = canonical
}

func normalizeAllowlist(mode ToolMode, allowlist []string) (normalized []string) {
	seen := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		canonical, ok := CanonicalizeToolName(mode, name)
		if !ok {
			canonical = name
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		normalized = append(normalized, canonical)
	}
	return normalized
}
