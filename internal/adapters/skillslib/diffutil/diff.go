package diffutil

import (
	"path/filepath"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// DefaultContextLines is the default number of context lines in unified diffs.
const DefaultContextLines = 3

// UnifiedDiff generates a unified diff between original and modified content.
// Returns empty string if contents are identical.
//
// Parameters:
//   - path: file path (used for display in diff header)
//   - original: original file content
//   - modified: modified file content
//   - contextLines: number of context lines (use 0 for DefaultContextLines)
//
// Example:
//
//	diff, err := diffutil.UnifiedDiff("main.go", originalContent, modifiedContent, 3)
func UnifiedDiff(path, original, modified string, contextLines int) (string, error) {
	if original == modified {
		return "", nil
	}

	if contextLines <= 0 {
		contextLines = DefaultContextLines
	}

	baseName := filepath.Base(path)
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(original),
		B:        difflib.SplitLines(modified),
		FromFile: "a/" + baseName,
		ToFile:   "b/" + baseName,
		Context:  contextLines,
	}

	return difflib.GetUnifiedDiffString(diff)
}

// UnifiedDiffWithPaths generates a unified diff with custom from/to file paths.
// Useful when comparing files from different locations or showing renames.
func UnifiedDiffWithPaths(fromPath, toPath, original, modified string, contextLines int) (string, error) {
	if original == modified {
		return "", nil
	}

	if contextLines <= 0 {
		contextLines = DefaultContextLines
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(original),
		B:        difflib.SplitLines(modified),
		FromFile: fromPath,
		ToFile:   toPath,
		Context:  contextLines,
	}

	return difflib.GetUnifiedDiffString(diff)
}

// ContextDiff generates a context diff between original and modified content.
// Context diffs show more surrounding context than unified diffs.
func ContextDiff(path, original, modified string, contextLines int) (string, error) {
	if original == modified {
		return "", nil
	}

	if contextLines <= 0 {
		contextLines = DefaultContextLines
	}

	baseName := filepath.Base(path)
	diff := difflib.ContextDiff{
		A:        difflib.SplitLines(original),
		B:        difflib.SplitLines(modified),
		FromFile: "a/" + baseName,
		ToFile:   "b/" + baseName,
		Context:  contextLines,
	}

	return difflib.GetContextDiffString(diff)
}

// HasChanges returns true if original and modified differ.
func HasChanges(original, modified string) bool {
	return original != modified
}

// DiffStats holds statistics about a diff.
type DiffStats struct {
	Additions int // Lines added
	Deletions int // Lines removed
	Changes   int // Hunks (change regions)
}

// GetStats parses a unified diff string and returns statistics.
func GetStats(diffStr string) DiffStats {
	var stats DiffStats

	lines := strings.Split(diffStr, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			// Skip the +++ header line
			if !strings.HasPrefix(line, "+++") {
				stats.Additions++
			}
		case '-':
			// Skip the --- header line
			if !strings.HasPrefix(line, "---") {
				stats.Deletions++
			}
		case '@':
			// @@ hunk header
			if strings.HasPrefix(line, "@@") {
				stats.Changes++
			}
		}
	}

	return stats
}

// LinesAdded counts lines starting with + (excluding +++ header).
func LinesAdded(diffStr string) int {
	return GetStats(diffStr).Additions
}

// LinesRemoved counts lines starting with - (excluding --- header).
func LinesRemoved(diffStr string) int {
	return GetStats(diffStr).Deletions
}

// Summary returns a short summary of the diff.
// Example: "+15 -3 (2 hunks)"
func Summary(diffStr string) string {
	if diffStr == "" {
		return "no changes"
	}
	stats := GetStats(diffStr)

	var parts []string
	if stats.Additions > 0 {
		parts = append(parts, "+"+itoa(stats.Additions))
	}
	if stats.Deletions > 0 {
		parts = append(parts, "-"+itoa(stats.Deletions))
	}
	if stats.Changes > 0 {
		parts = append(parts, formatHunks(stats.Changes))
	}

	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, " ")
}

func formatHunks(count int) string {
	if count == 0 {
		return ""
	}
	if count == 1 {
		return "(1 hunk)"
	}
	return "(" + itoa(count) + " hunks)"
}

// itoa converts int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
