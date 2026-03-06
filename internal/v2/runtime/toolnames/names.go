package toolnames

import "strings"

// Canonical normalizes tool names for v2 runtime matching and execution.
//
// Accepted aliases include slash-delimited (code/search), dot-delimited
// (code.search), and underscore-delimited (code_search) forms. Canonical output
// uses underscore delimiters.
func Canonical(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	if n == "" {
		return ""
	}
	n = strings.NewReplacer(".", "_", "/", "_").Replace(n)
	n = strings.Trim(n, "_")
	if n == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(n))
	prevUnderscore := false
	for _, r := range n {
		if r == '_' {
			if prevUnderscore {
				continue
			}
			prevUnderscore = true
			b.WriteRune(r)
			continue
		}
		prevUnderscore = false
		b.WriteRune(r)
	}
	return b.String()
}
