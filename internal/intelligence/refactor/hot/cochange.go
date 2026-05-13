package hot

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/langutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	refscope "github.com/joshka0/foxctl/internal/intelligence/refactor/scope"
)

const maxFilesPerCochangeCommit = 64

// CochangeNeighbor captures one neighboring file that tends to change with the
// current file in the observed git window.
type CochangeNeighbor struct {
	Path        string    `json:"path"`
	Count       int       `json:"count"`
	Score       float64   `json:"score"`
	LastTouched time.Time `json:"last_touched_at,omitempty"`
}

type cochangeStats struct {
	Count       int
	Score       float64
	LastTouched time.Time
}

type cochangeParseOptions struct {
	ScopePath    string
	IsDir        bool
	IncludeTests bool
	Language     string
	HalfLifeDays int
	Now          time.Time
}

// BuildCochangeIndex returns per-file co-change neighbors within the requested
// scope and git window.
func BuildCochangeIndex(ctx context.Context, scope refscope.Scope, includeTests bool, gitBase string, halfLifeDays int, now time.Time, maxNeighbors int) (map[string][]CochangeNeighbor, error) {
	gitBase = strings.TrimSpace(gitBase)
	if gitBase == "" {
		return nil, nil
	}
	if halfLifeDays <= 0 {
		halfLifeDays = 90
	}
	if maxNeighbors <= 0 {
		maxNeighbors = 5
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	logText, err := readCochangeLog(ctx, scope, gitBase)
	if err != nil {
		return nil, err
	}

	graph := buildCochangeGraphFromLog(logText, cochangeParseOptions{
		ScopePath:    normalizedScopePath(scope),
		IsDir:        scope.IsDir,
		IncludeTests: includeTests,
		Language:     scope.Language,
		HalfLifeDays: halfLifeDays,
		Now:          now,
	})
	return extractCochangeNeighbors(graph, maxNeighbors), nil
}

func readCochangeLog(ctx context.Context, scope refscope.Scope, gitBase string) (string, error) {
	args := []string{"-C", scope.RepoRoot, "log", "--format=%H%x1f%ct", "--name-only", fmt.Sprintf("%s..HEAD", gitBase), "--"}
	if path := scopeGitPath(scope); path != "" {
		args = append(args, path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &BuildError{
			Message: fmt.Sprintf("git log since %q failed", gitBase),
			Hint:    fmt.Sprintf("Verify that %q is a valid git ref or snapshot baseline. stderr: %s", gitBase, strings.TrimSpace(stderr.String())),
		}
	}
	return stdout.String(), nil
}

func buildCochangeGraphFromLog(logText string, opts cochangeParseOptions) map[string]map[string]*cochangeStats {
	graph := make(map[string]map[string]*cochangeStats)
	currentFiles := map[string]struct{}{}
	var currentTime time.Time
	flush := func() {
		accumulateCochangePairs(graph, currentFiles, currentTime, opts.Now, opts.HalfLifeDays)
	}

	for _, raw := range strings.Split(logText, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if isCochangeCommitHeader(line) {
			flush()
			currentFiles = map[string]struct{}{}
			currentTime = parseCochangeCommitTime(line)
			continue
		}

		path, ok := filterCochangeCommitPath(line, opts)
		if !ok {
			continue
		}
		currentFiles[path] = struct{}{}
	}
	flush()
	return graph
}

func isCochangeCommitHeader(line string) bool {
	return strings.Contains(line, "\x1f")
}

func parseCochangeCommitTime(line string) time.Time {
	parts := strings.SplitN(line, "\x1f", 2)
	if len(parts) != 2 {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

func filterCochangeCommitPath(rawPath string, opts cochangeParseOptions) (string, bool) {
	path := pathutil.ToSlash(strings.TrimSpace(rawPath))
	if path == "" || !pathInScope(path, opts.ScopePath, opts.IsDir) {
		return "", false
	}
	if !opts.IncludeTests && fsutil.IsTestFile(filepath.Base(path)) {
		return "", false
	}
	if langutil.DetectAllowedWithHint(opts.Language, path, langutil.CommonCodeLanguages) == "" {
		return "", false
	}
	return path, true
}

func accumulateCochangePairs(graph map[string]map[string]*cochangeStats, files map[string]struct{}, ts, now time.Time, halfLifeDays int) {
	paths := sortedCochangePaths(files)
	if len(paths) < 2 {
		return
	}
	weight := recencyWeight(ts, now, halfLifeDays)
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			addCochangeEdge(graph, paths[i], paths[j], weight, ts)
			addCochangeEdge(graph, paths[j], paths[i], weight, ts)
		}
	}
}

func sortedCochangePaths(files map[string]struct{}) []string {
	if len(files) < 2 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > maxFilesPerCochangeCommit {
		paths = paths[:maxFilesPerCochangeCommit]
	}
	return paths
}

func extractCochangeNeighbors(graph map[string]map[string]*cochangeStats, maxNeighbors int) map[string][]CochangeNeighbor {
	out := make(map[string][]CochangeNeighbor, len(graph))
	for path, neighbors := range graph {
		items := make([]CochangeNeighbor, 0, len(neighbors))
		for neighborPath, stats := range neighbors {
			items = append(items, CochangeNeighbor{
				Path:        neighborPath,
				Count:       stats.Count,
				Score:       stats.Score,
				LastTouched: stats.LastTouched,
			})
		}
		sortCochangeNeighbors(items)
		if len(items) > maxNeighbors {
			items = items[:maxNeighbors]
		}
		out[path] = items
	}
	return out
}

func sortCochangeNeighbors(items []CochangeNeighbor) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Path < items[j].Path
	})
}

func addCochangeEdge(graph map[string]map[string]*cochangeStats, src, dst string, weight float64, ts time.Time) {
	row := graph[src]
	if row == nil {
		row = make(map[string]*cochangeStats)
		graph[src] = row
	}
	stats := row[dst]
	if stats == nil {
		stats = &cochangeStats{}
		row[dst] = stats
	}
	stats.Count++
	stats.Score += weight
	if ts.After(stats.LastTouched) {
		stats.LastTouched = ts
	}
}
