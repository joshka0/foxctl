// Package fsutil re-exports platform fsutil helpers for skills.
package fsutil

import (
	"io/fs"
	"strings"

	platformfs "github.com/jkatigb/agentctl/internal/platform/fsutil"
)

// DetectLanguage returns the language identifier based on file extension.
func DetectLanguage(path string) string {
	return platformfs.DetectLanguage(path)
}

// DetectLanguageWithHint returns the language based on hint or extension.
func DetectLanguageWithHint(hint, path string) string {
	return platformfs.DetectLanguageWithHint(hint, path)
}

// RelativeTo returns the path relative to base, or the original path if it cannot be made relative.
func RelativeTo(base, path string) string {
	return platformfs.RelativeTo(base, path)
}

// IsCommonExclude returns true if the directory name should be excluded from traversal.
func IsCommonExclude(name string) bool {
	return platformfs.IsCommonExclude(name)
}

// IsTestFile returns true if the filename indicates a test file.
func IsTestFile(name string) bool {
	return platformfs.IsTestFile(name)
}

// IsBinaryFile returns true if the file extension suggests binary content.
func IsBinaryFile(path string) bool {
	return platformfs.IsBinaryFile(path)
}

// IsBinaryContent returns true if the content contains null bytes.
func IsBinaryContent(content []byte) bool {
	for _, b := range content {
		if b == 0 {
			return true
		}
	}
	return false
}

// IsSymlinkMode returns true if the file mode represents a symlink.
func IsSymlinkMode(mode fs.FileMode) bool {
	return mode&fs.ModeSymlink != 0
}

// ShouldSkipHiddenOrCommon returns true if the name is hidden or commonly excluded.
func ShouldSkipHiddenOrCommon(name string) bool {
	return strings.HasPrefix(name, ".") || IsCommonExclude(name)
}
