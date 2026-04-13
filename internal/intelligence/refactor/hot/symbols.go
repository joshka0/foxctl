package hot

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	refscope "github.com/jkatigb/agentctl/internal/intelligence/refactor/scope"
	refsnapshot "github.com/jkatigb/agentctl/internal/intelligence/refactor/snapshot"
)

var diffHunkRangePattern = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

type lineRange struct {
	Start int
	End   int
}

// SymbolHotspot is a churn-ranked symbol within a refactor scope.
type SymbolHotspot struct {
	Path             string    `json:"path"`
	SymbolID         string    `json:"symbol_id"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind,omitempty"`
	TouchCount       int       `json:"touch_count"`
	Score            float64   `json:"score"`
	ChangedLineCount int       `json:"changed_line_count"`
	LastTouched      time.Time `json:"last_touched_at,omitempty"`
	LineStart        int       `json:"line_start,omitempty"`
	LineEnd          int       `json:"line_end,omitempty"`
	CurrentHash      string    `json:"current_hash,omitempty"`
}

// BuildSymbolHotspots projects file-level hotness down onto current symbols by
// intersecting current symbol spans with changed line ranges since the provided
// git baseline. It uses the current snapshot as the stable source of symbol
// spans and body hashes.
func BuildSymbolHotspots(ctx context.Context, scope refscope.Scope, gitBase string, current refsnapshot.Payload, fileHotspots map[string]FileHotspot, now time.Time) ([]SymbolHotspot, error) {
	gitBase = strings.TrimSpace(gitBase)
	if gitBase == "" || len(current.Symbols) == 0 {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	fileLineCounts := make(map[string]int, len(current.Files))
	symbolsByPath := make(map[string][]refsnapshot.SymbolSnapshot)
	for _, file := range current.Files {
		fileLineCounts[strings.TrimSpace(file.Path)] = file.LineCount
	}
	for _, symbol := range current.Symbols {
		path := strings.TrimSpace(symbol.Path)
		if path == "" {
			continue
		}
		symbolsByPath[path] = append(symbolsByPath[path], symbol)
	}

	changedRanges, err := collectChangedLineRanges(ctx, scope, gitBase, fileLineCounts)
	if err != nil {
		return nil, err
	}
	if err := addUntrackedChangedLineRanges(ctx, scope, fileLineCounts, changedRanges); err != nil {
		return nil, err
	}

	out := make([]SymbolHotspot, 0, len(current.Symbols))
	for path, ranges := range changedRanges {
		ranges = mergeLineRanges(ranges)
		if len(ranges) == 0 {
			continue
		}
		symbols := symbolsByPath[path]
		if len(symbols) == 0 {
			continue
		}

		totalChangedLines := rangeSpanSum(ranges)
		fileHot, hasFileHot := fileHotspots[path]
		for _, symbol := range symbols {
			if symbol.LineStart <= 0 || symbol.LineEnd <= 0 {
				continue
			}
			changedLines := overlapLineRanges(ranges, symbol.LineStart, symbol.LineEnd)
			if changedLines == 0 {
				continue
			}

			hot := SymbolHotspot{
				Path:             path,
				SymbolID:         strings.TrimSpace(symbol.SymbolID),
				Name:             strings.TrimSpace(symbol.Name),
				Kind:             string(symbol.Kind),
				ChangedLineCount: changedLines,
				LineStart:        symbol.LineStart,
				LineEnd:          symbol.LineEnd,
				CurrentHash:      strings.TrimSpace(symbol.Hash),
			}

			if hasFileHot {
				hot.TouchCount = fileHot.TouchCount
				hot.LastTouched = fileHot.LastTouched
				if totalChangedLines > 0 {
					hot.Score = fileHot.Score * float64(changedLines) / float64(totalChangedLines)
				} else {
					hot.Score = fileHot.Score
				}
			} else {
				hot.TouchCount = 1
				hot.LastTouched = now
				hot.Score = float64(changedLines)
			}

			out = append(out, hot)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].ChangedLineCount != out[j].ChangedLineCount {
			return out[i].ChangedLineCount > out[j].ChangedLineCount
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].LineStart != out[j].LineStart {
			return out[i].LineStart < out[j].LineStart
		}
		return out[i].Name < out[j].Name
	})

	return out, nil
}

func collectChangedLineRanges(ctx context.Context, scope refscope.Scope, gitBase string, currentFiles map[string]int) (map[string][]lineRange, error) {
	args := []string{"-C", scope.RepoRoot, "diff", "--unified=0", "--no-color", "--find-renames", gitBase, "--"}
	if path := scopeGitPath(scope); path != "" {
		args = append(args, path)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff %q failed: %w (stderr: %s)", gitBase, err, strings.TrimSpace(stderr.String()))
	}

	out := make(map[string][]lineRange)
	currentFile := ""
	for _, raw := range strings.Split(stdout.String(), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "+++ "):
			currentFile = normalizeDiffTargetPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
			if currentFile == "" {
				continue
			}
			if _, ok := currentFiles[currentFile]; !ok {
				currentFile = ""
			}
		case strings.HasPrefix(line, "@@ "):
			if currentFile == "" {
				continue
			}
			rng, ok := parseDiffAddedRange(line)
			if !ok {
				continue
			}
			out[currentFile] = append(out[currentFile], rng)
		}
	}
	return out, nil
}

func addUntrackedChangedLineRanges(ctx context.Context, scope refscope.Scope, currentFiles map[string]int, ranges map[string][]lineRange) error {
	args := []string{"-C", scope.RepoRoot, "ls-files", "--others", "--exclude-standard"}
	if path := scopeGitPath(scope); path != "" {
		args = append(args, "--", path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git ls-files failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	for _, raw := range strings.Split(stdout.String(), "\n") {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		lineCount, ok := currentFiles[path]
		if !ok {
			continue
		}
		if lineCount <= 0 {
			lineCount = 1
		}
		ranges[path] = append(ranges[path], lineRange{Start: 1, End: lineCount})
	}
	return nil
}

func normalizeDiffTargetPath(path string) string {
	path = strings.TrimSpace(path)
	switch {
	case path == "", path == "/dev/null":
		return ""
	case strings.HasPrefix(path, "b/"), strings.HasPrefix(path, "a/"):
		return strings.TrimSpace(path[2:])
	default:
		return path
	}
}

func parseDiffAddedRange(line string) (lineRange, bool) {
	match := diffHunkRangePattern.FindStringSubmatch(strings.TrimSpace(line))
	if len(match) == 0 {
		return lineRange{}, false
	}

	start, err := strconv.Atoi(match[1])
	if err != nil {
		return lineRange{}, false
	}
	count := 1
	if len(match) > 2 && strings.TrimSpace(match[2]) != "" {
		parsed, err := strconv.Atoi(match[2])
		if err != nil {
			return lineRange{}, false
		}
		count = parsed
	}
	if start <= 0 {
		start = 1
	}
	end := start
	if count > 0 {
		end = start + count - 1
	}
	if end < start {
		end = start
	}
	return lineRange{Start: start, End: end}, true
}

func mergeLineRanges(in []lineRange) []lineRange {
	if len(in) == 0 {
		return nil
	}
	ranges := append([]lineRange(nil), in...)
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start != ranges[j].Start {
			return ranges[i].Start < ranges[j].Start
		}
		return ranges[i].End < ranges[j].End
	})

	merged := []lineRange{ranges[0]}
	for _, item := range ranges[1:] {
		last := &merged[len(merged)-1]
		if item.Start <= last.End+1 {
			if item.End > last.End {
				last.End = item.End
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func rangeSpanSum(ranges []lineRange) int {
	total := 0
	for _, rng := range ranges {
		if rng.End < rng.Start {
			continue
		}
		total += rng.End - rng.Start + 1
	}
	return total
}

func overlapLineRanges(ranges []lineRange, start, end int) int {
	if start <= 0 || end <= 0 || end < start {
		return 0
	}
	total := 0
	for _, rng := range ranges {
		overlapStart := maxInt(start, rng.Start)
		overlapEnd := minInt(end, rng.End)
		if overlapEnd < overlapStart {
			continue
		}
		total += overlapEnd - overlapStart + 1
	}
	return total
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
