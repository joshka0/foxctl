package fs

import "strings"

var commonExcludeGlobs = []string{".git", "node_modules"}

// IsHiddenName reports whether a name is hidden (dot-prefixed).
func IsHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

// ShouldSkipHidden returns true when hidden names should be excluded.
func ShouldSkipHidden(name string, includeHidden bool) bool {
	return !includeHidden && IsHiddenName(name)
}

// Matches reports whether the path matches any of the globs.
func Matches(path string, globs []string) bool {
	return matches(path, globs)
}

// CommonExcludeGlobs returns the default exclude globs.
func CommonExcludeGlobs() []string {
	return append([]string(nil), commonExcludeGlobs...)
}

// AppendCommonExcludes prepends default excludes to the provided globs.
func AppendCommonExcludes(globs []string) []string {
	out := make([]string, 0, len(commonExcludeGlobs)+len(globs))
	out = append(out, commonExcludeGlobs...)
	out = append(out, globs...)
	return out
}

// AddHiddenExclude appends the hidden glob when hidden entries should be excluded.
func AddHiddenExclude(globs []string, includeHidden bool) []string {
	out := append([]string(nil), globs...)
	if !includeHidden {
		out = append(out, ".*")
	}
	return out
}
