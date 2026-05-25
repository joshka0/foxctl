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
	for i := 0; i < len(n); i++ {
		c := n[i]
		if c == '_' {
			if prevUnderscore {
				continue
			}
			prevUnderscore = true
			b.WriteByte(c)
			continue
		}
		if !isToolNameChar(c) {
			return ""
		}
		prevUnderscore = false
		b.WriteByte(c)
	}
	return b.String()
}

func isToolNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}
