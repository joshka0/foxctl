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

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/langutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	refscope "github.com/jkatigb/agentctl/internal/refactor/scope"
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

	args := []string{"-C", scope.RepoRoot, "log", "--format=%H%x1f%ct", "--name-only", fmt.Sprintf("%s..HEAD", gitBase), "--"}
	if path := scopeGitPath(scope); path != "" {
		args = append(args, path)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &BuildError{
			Message: fmt.Sprintf("git log since %q failed", gitBase),
			Hint:    fmt.Sprintf("Verify that %q is a valid git ref or snapshot baseline. stderr: %s", gitBase, strings.TrimSpace(stderr.String())),
		}
	}

	scopePath := normalizedScopePath(scope)
	graph := make(map[string]map[string]*cochangeStats)
	flush := func(files map[string]struct{}, ts time.Time) {
		if len(files) < 2 {
			return
		}
		paths := make([]string, 0, len(files))
		for path := range files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		if len(paths) > maxFilesPerCochangeCommit {
			paths = paths[:maxFilesPerCochangeCommit]
		}
		weight := recencyWeight(ts, now, halfLifeDays)
		for i := 0; i < len(paths); i++ {
			for j := i + 1; j < len(paths); j++ {
				addCochangeEdge(graph, paths[i], paths[j], weight, ts)
				addCochangeEdge(graph, paths[j], paths[i], weight, ts)
			}
		}
	}

	currentFiles := map[string]struct{}{}
	var currentTime time.Time
	for _, raw := range strings.Split(stdout.String(), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.Contains(line, "\x1f") {
			flush(currentFiles, currentTime)
			currentFiles = map[string]struct{}{}

			parts := strings.SplitN(line, "\x1f", 2)
			if len(parts) != 2 {
				currentTime = time.Time{}
				continue
			}
			sec, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			if err != nil {
				currentTime = time.Time{}
				continue
			}
			currentTime = time.Unix(sec, 0).UTC()
			continue
		}

		path := pathutil.ToSlash(strings.TrimSpace(line))
		if path == "" || !pathInScope(path, scopePath, scope.IsDir) {
			continue
		}
		if !includeTests && fsutil.IsTestFile(filepath.Base(path)) {
			continue
		}
		if langutil.DetectAllowedWithHint(scope.Language, path, langutil.CommonCodeLanguages) == "" {
			continue
		}
		currentFiles[path] = struct{}{}
	}
	flush(currentFiles, currentTime)

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
		sort.Slice(items, func(i, j int) bool {
			if items[i].Score != items[j].Score {
				return items[i].Score > items[j].Score
			}
			if items[i].Count != items[j].Count {
				return items[i].Count > items[j].Count
			}
			return items[i].Path < items[j].Path
		})
		if len(items) > maxNeighbors {
			items = items[:maxNeighbors]
		}
		out[path] = items
	}
	return out, nil
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
