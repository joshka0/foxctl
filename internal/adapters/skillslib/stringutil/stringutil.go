package stringutil

import "strings"

// NormalizeStrings trims whitespace, drops empty entries, and returns a non-nil slice.
func NormalizeStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
