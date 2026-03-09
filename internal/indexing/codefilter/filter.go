package codefilter

import (
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/fsutil"
)

// ShouldSkipPath reports whether a path should be excluded from app-code indexing.
// It excludes tests, specs, fixtures, golden files, snapshots, and testdata.
func ShouldSkipPath(path string) bool {
	clean := filepath.ToSlash(strings.TrimSpace(path))
	if clean == "" {
		return true
	}
	base := filepath.Base(clean)
	lower := strings.ToLower(clean)
	baseLower := strings.ToLower(base)

	if fsutil.IsTestFile(base) {
		return true
	}
	for _, segment := range strings.Split(lower, "/") {
		if strings.HasSuffix(segment, "_test") {
			return true
		}
	}
	if strings.HasPrefix(lower, "test/") || strings.HasPrefix(lower, "tests/") || strings.Contains(lower, "/test/") || strings.Contains(lower, "/tests/") {
		return true
	}
	if strings.HasPrefix(lower, "testdata/") || strings.HasPrefix(lower, "fixtures/") || strings.HasPrefix(lower, "golden/") {
		return true
	}
	if strings.Contains(lower, "/testdata/") || strings.Contains(lower, "/fixtures/") || strings.Contains(lower, "/golden/") {
		return true
	}
	if strings.Contains(lower, "/__snapshots__/") {
		return true
	}
	if strings.Contains(baseLower, ".spec.") || strings.Contains(baseLower, ".test.") {
		return true
	}
	return false
}
