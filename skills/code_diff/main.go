// Package main implements the code/diff skill.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textutil"
)

type input struct {
	OldPath          string `json:"old_path" validate:"required"`
	NewPath          string `json:"new_path" validate:"required"`
	Format           string `json:"format"`
	ContextLines     int    `json:"context_lines"`
	IgnoreWhitespace bool   `json:"ignore_whitespace"`
}

type diffResult struct {
	OldFile    string     `json:"old_file"`
	NewFile    string     `json:"new_file"`
	Statistics diffStats  `json:"statistics"`
	Unified    string     `json:"unified,omitempty"`
	Hunks      []diffHunk `json:"hunks,omitempty"`
}

type diffStats struct {
	LinesAdded   int     `json:"lines_added"`
	LinesRemoved int     `json:"lines_removed"`
	LinesChanged int     `json:"lines_changed"`
	TotalChanges int     `json:"total_changes"`
	Similarity   float64 `json:"similarity_percent"`
}

type diffHunk struct {
	OldStart int      `json:"old_start"`
	OldLines int      `json:"old_lines"`
	NewStart int      `json:"new_start"`
	NewLines int      `json:"new_lines"`
	Header   string   `json:"header"`
	Lines    []string `json:"lines"`
}

func main() {
	skillmain.Main("code/diff", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.Format == "" {
		in.Format = "unified"
	}
	if in.ContextLines < 0 {
		in.ContextLines = 3
	}

	// Resolve and validate paths
	workspace := rc.PathValidator.Workspace()

	oldPath, err := skillmain.ValidatePath(rc, in.OldPath, skillmain.WithPathMessage("old_path validation failed"))
	if err != nil {
		return err
	}

	newPath, err := skillmain.ValidatePath(rc, in.NewPath, skillmain.WithPathMessage("new_path validation failed"))
	if err != nil {
		return err
	}

	// Read files
	oldContent, err := os.ReadFile(oldPath)
	if err != nil {
		return skillerr.WrapIO("read old file", err)
	}

	newContent, err := os.ReadFile(newPath)
	if err != nil {
		return skillerr.WrapIO("read new file", err)
	}

	// Process whitespace if needed
	if in.IgnoreWhitespace {
		oldContent = normalizeWhitespace(oldContent)
		newContent = normalizeWhitespace(newContent)
	}

	// Split into lines
	oldLines := textutil.SplitLines(string(oldContent))
	newLines := textutil.SplitLines(string(newContent))

	// Compute diff
	hunks := computeDiff(oldLines, newLines, in.ContextLines)
	stats := computeStats(hunks, len(oldLines), len(newLines))

	// Build result
	result := diffResult{
		OldFile:    pathutil.RelTo(workspace, oldPath),
		NewFile:    pathutil.RelTo(workspace, newPath),
		Statistics: stats,
		Hunks:      hunks,
	}

	// Generate unified diff if requested
	if in.Format == "unified" {
		result.Unified = generateUnifiedDiff(result.OldFile, result.NewFile, hunks)
	}

	// Prepare artifact for large diffs
	var artifact skillmain.Artifact
	if in.Format == "unified" && len(result.Unified) > 2048 {
		buf := bytes.NewBufferString(result.Unified)
		artifact, err = skillmain.PersistBuffer(ctx, rc, buf, "text/plain", "code_diff")
		if err != nil {
			return skillerr.WrapIO("persist diff artifact", err)
		}
		// Keep a preview
		lines := strings.Split(result.Unified, "\n")
		if len(lines) > 50 {
			result.Unified = strings.Join(lines[:50], "\n") + "\n... (truncated, see artifact)"
		}
	}

	// Build response based on format
	var data map[string]any
	switch in.Format {
	case "stats":
		data = map[string]any{
			"statistics": stats,
			"old_file":   result.OldFile,
			"new_file":   result.NewFile,
		}
	case "summary":
		data = map[string]any{
			"old_file":   result.OldFile,
			"new_file":   result.NewFile,
			"statistics": stats,
			"hunks":      len(hunks),
		}
	default: // unified
		data = map[string]any{
			"diff": result,
		}
	}

	skillout.AddArtifact(data, &artifact)

	return skillout.Emit(rc, "code/diff", data)
}

func normalizeWhitespace(content []byte) []byte {
	// Normalize whitespace: trim trailing spaces, convert tabs to spaces
	lines := strings.Split(string(content), "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " \t")
		line = strings.ReplaceAll(line, "\t", "    ")
		lines[i] = line
	}
	return []byte(strings.Join(lines, "\n"))
}

func computeDiff(oldLines, newLines []string, context int) []diffHunk {
	// Simple LCS-based diff algorithm
	lcs := longestCommonSubsequence(oldLines, newLines)
	return buildHunks(oldLines, newLines, lcs, context)
}

func longestCommonSubsequence(a, b []string) [][]int {
	m, n := len(a), len(b)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] > lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	return lcs
}

func buildHunks(oldLines, newLines []string, lcs [][]int, context int) []diffHunk {
	var hunks []diffHunk
	var currentHunk *diffHunk

	i, j := len(oldLines), len(newLines)

	// Backtrack through LCS to find differences
	changes := make([]struct{ op, oldPos, newPos int }, 0)

	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			// Common line
			changes = append(changes, struct{ op, oldPos, newPos int }{0, i - 1, j - 1})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			// Addition
			changes = append(changes, struct{ op, oldPos, newPos int }{1, i, j - 1})
			j--
		} else if i > 0 {
			// Deletion
			changes = append(changes, struct{ op, oldPos, newPos int }{-1, i - 1, j})
			i--
		}
	}

	// Reverse changes (we built them backwards)
	for i, j := 0, len(changes)-1; i < j; i, j = i+1, j-1 {
		changes[i], changes[j] = changes[j], changes[i]
	}

	// Build hunks from changes
	for idx, change := range changes {
		if change.op != 0 {
			// Start new hunk or extend existing
			if currentHunk == nil {
				// Collect context before
				start := max(0, change.oldPos-context)
				currentHunk = &diffHunk{
					OldStart: start + 1,
					NewStart: max(0, change.newPos-context) + 1,
				}

				for k := start; k < change.oldPos && k < len(oldLines); k++ {
					currentHunk.Lines = append(currentHunk.Lines, " "+oldLines[k])
				}
			}

			// Add the change
			switch change.op {
			case -1:
				currentHunk.Lines = append(currentHunk.Lines, "-"+oldLines[change.oldPos])
				currentHunk.OldLines++
			case 1:
				currentHunk.Lines = append(currentHunk.Lines, "+"+newLines[change.newPos])
				currentHunk.NewLines++
			}
		} else {
			// Common line - might be context
			if currentHunk != nil {
				currentHunk.Lines = append(currentHunk.Lines, " "+oldLines[change.oldPos])
				currentHunk.OldLines++
				currentHunk.NewLines++

				// Check if we should close this hunk
				hasMoreChanges := false
				for k := idx + 1; k < min(idx+1+context*2, len(changes)); k++ {
					if changes[k].op != 0 {
						hasMoreChanges = true
						break
					}
				}

				if !hasMoreChanges {
					// Close hunk
					currentHunk.Header = fmt.Sprintf("@@ -%d,%d +%d,%d @@",
						currentHunk.OldStart, currentHunk.OldLines,
						currentHunk.NewStart, currentHunk.NewLines)
					hunks = append(hunks, *currentHunk)
					currentHunk = nil
				}
			}
		}
	}

	// Close final hunk if open
	if currentHunk != nil {
		currentHunk.Header = fmt.Sprintf("@@ -%d,%d +%d,%d @@",
			currentHunk.OldStart, currentHunk.OldLines,
			currentHunk.NewStart, currentHunk.NewLines)
		hunks = append(hunks, *currentHunk)
	}

	return hunks
}

func computeStats(hunks []diffHunk, oldTotal, newTotal int) diffStats {
	stats := diffStats{}

	for _, hunk := range hunks {
		for _, line := range hunk.Lines {
			if len(line) == 0 {
				continue
			}
			switch line[0] {
			case '+':
				stats.LinesAdded++
			case '-':
				stats.LinesRemoved++
			}
		}
	}

	stats.TotalChanges = stats.LinesAdded + stats.LinesRemoved
	stats.LinesChanged = min(stats.LinesAdded, stats.LinesRemoved)

	// Calculate similarity
	commonLines := min(oldTotal, newTotal) - stats.TotalChanges/2
	maxLines := max(oldTotal, newTotal)
	if maxLines > 0 {
		stats.Similarity = float64(commonLines) / float64(maxLines) * 100
	}

	return stats
}

func generateUnifiedDiff(oldFile, newFile string, hunks []diffHunk) string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("--- %s\n", oldFile))
	builder.WriteString(fmt.Sprintf("+++ %s\n", newFile))

	for _, hunk := range hunks {
		builder.WriteString(hunk.Header + "\n")
		for _, line := range hunk.Lines {
			builder.WriteString(line + "\n")
		}
	}

	return builder.String()
}
