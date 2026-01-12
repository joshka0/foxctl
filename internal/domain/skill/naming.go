package skill

import "strings"

// NormalizeSkillName converts canonical skill names to the filesystem storage format.
// Canonical names use slashes for namespacing (e.g., "code/semantic_search").
// Filesystem names use underscores for flat directory structure (e.g., "code_semantic_search").
//
// Examples:
//   - "text/grep" -> "text_grep"
//   - "code/semantic_search" -> "code_semantic_search"
//   - "my-skill" -> "my_skill"
func NormalizeSkillName(name string) string {
	n := strings.ReplaceAll(name, "/", "_")
	n = strings.ReplaceAll(n, "-", "_")
	return n
}
