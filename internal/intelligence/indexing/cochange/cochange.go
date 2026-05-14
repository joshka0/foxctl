package cochange

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultHalfLifeDays = 90

// Config controls deterministic co-change scoring.
type Config struct {
	CommitLimit          int
	MaxFilesPerCommit    int
	HalfLifeDays         int
	TopKPerFile          int
	Now                  time.Time
	SkipGenerated        bool
	SkipLockfiles        bool
	GiantCommitSoftLimit int
	GiantCommitHardLimit int
	FormattingMultiplier float64
}

// Commit is the side-effect-free input shape used by the scorer.
type Commit struct {
	SHA            string
	Timestamp      time.Time
	Files          []string
	FormattingOnly bool
}

// Neighbor captures one capped file-level co-change relation.
type Neighbor struct {
	Path           string    `json:"path"`
	Count          int       `json:"count"`
	WeightedCount  float64   `json:"weighted_count"`
	Score          float64   `json:"score"`
	LastSeenCommit string    `json:"last_seen_commit,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at,omitempty"`
	Freshness      float64   `json:"freshness"`
	Volatility     float64   `json:"volatility"`
}

// InspectFile captures inspect-friendly co-change output for one file.
type InspectFile struct {
	Path       string     `json:"path"`
	Neighbors  []Neighbor `json:"neighbors,omitempty"`
	Freshness  float64    `json:"freshness"`
	Volatility float64    `json:"volatility"`
}

// Inspect contains a deterministic projection of the co-change graph.
type Inspect struct {
	Files []InspectFile `json:"files"`
}

type pairStats struct {
	count          int
	weightedCount  float64
	lastSeenCommit string
	lastSeenAt     time.Time
}

// CollectGitCommits collects recent committed file changes. It intentionally
// ignores uncommitted worktree state.
func CollectGitCommits(ctx context.Context, workspacePath string, seedPaths []string, cfg Config) ([]Commit, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return nil, nil
	}
	cfg = normalizeConfig(cfg)
	seedPaths = NormalizePaths(seedPaths)
	if len(seedPaths) > 0 {
		return collectGitCommitsForSeeds(ctx, workspacePath, seedPaths, cfg)
	}
	args := []string{"-C", workspacePath, "log", fmt.Sprintf("-n%d", cfg.CommitLimit), "--format=%H%x1f%ct", "--name-only"}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log cochange: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	commits := ParseGitLogNameOnly(stdout.String())
	for i := range commits {
		if len(commits[i].Files) < 2 {
			continue
		}
		formattingOnly, err := whitespaceOnlyCommit(ctx, workspacePath, commits[i].SHA)
		if err != nil {
			return nil, err
		}
		commits[i].FormattingOnly = formattingOnly
	}
	return commits, nil
}

func collectGitCommitsForSeeds(ctx context.Context, workspacePath string, seedPaths []string, cfg Config) ([]Commit, error) {
	args := []string{"-C", workspacePath, "log", fmt.Sprintf("-n%d", cfg.CommitLimit), "--format=%H%x1f%ct"}
	if len(seedPaths) > 0 {
		args = append(args, "--")
		args = append(args, seedPaths...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log cochange: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	seen := map[string]struct{}{}
	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 2)
		if len(parts) != 2 {
			continue
		}
		sha := strings.TrimSpace(parts[0])
		if sha == "" {
			continue
		}
		if _, ok := seen[sha]; ok {
			continue
		}
		seen[sha] = struct{}{}
		sec, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			continue
		}
		files, err := changedFilesForCommit(ctx, workspacePath, sha)
		if err != nil {
			return nil, err
		}
		formattingOnly := false
		if len(files) >= 2 {
			formattingOnly, err = whitespaceOnlyCommit(ctx, workspacePath, sha)
			if err != nil {
				return nil, err
			}
		}
		commits = append(commits, Commit{
			SHA:            sha,
			Timestamp:      time.Unix(sec, 0).UTC(),
			Files:          files,
			FormattingOnly: formattingOnly,
		})
	}
	return commits, nil
}

func changedFilesForCommit(ctx context.Context, workspacePath, sha string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "show", "--name-only", "--pretty=format:", sha)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git show %s: %w (%s)", sha, err, strings.TrimSpace(stderr.String()))
	}
	return NormalizePaths(strings.Split(strings.TrimSpace(stdout.String()), "\n")), nil
}

func whitespaceOnlyCommit(ctx context.Context, workspacePath, sha string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "rev-list", "--parents", "-n1", sha)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git parents %s: %w (%s)", sha, err, strings.TrimSpace(stderr.String()))
	}
	fields := strings.Fields(strings.TrimSpace(stdout.String()))
	if len(fields) != 2 {
		return false, nil
	}
	parent := fields[1]
	diff := exec.CommandContext(ctx, "git", "-C", workspacePath, "diff", "--ignore-all-space", "--ignore-blank-lines", "--quiet", parent, sha, "--")
	var diffStderr bytes.Buffer
	diff.Stderr = &diffStderr
	if err := diff.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git whitespace diff %s: %w (%s)", sha, err, strings.TrimSpace(diffStderr.String()))
	}
	return true, nil
}

// ParseGitLogNameOnly parses `git log --format=%H%x1f%ct --name-only` output.
func ParseGitLogNameOnly(output string) []Commit {
	var commits []Commit
	var current *Commit
	flush := func() {
		if current == nil {
			return
		}
		current.Files = NormalizePaths(current.Files)
		if current.SHA != "" && len(current.Files) > 0 {
			commits = append(commits, *current)
		}
		current = nil
	}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.Contains(line, "\x1f") {
			flush()
			parts := strings.SplitN(line, "\x1f", 2)
			if len(parts) != 2 {
				continue
			}
			sec, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			if err != nil {
				continue
			}
			current = &Commit{
				SHA:       strings.TrimSpace(parts[0]),
				Timestamp: time.Unix(sec, 0).UTC(),
			}
			continue
		}
		if current != nil {
			current.Files = append(current.Files, line)
		}
	}
	flush()
	return commits
}

// Score computes symmetric file-level co-change neighbors.
func Score(commits []Commit, cfg Config) map[string][]Neighbor {
	cfg = normalizeConfig(cfg)
	graph := make(map[string]map[string]*pairStats)
	fileTotals := map[string]float64{}
	for _, commit := range commits {
		files := FilterPaths(commit.Files, cfg)
		if len(files) < 2 {
			continue
		}
		if len(files) > cfg.MaxFilesPerCommit {
			continue
		}
		weight := WeightedCommitWeight(commit, len(files), cfg)
		if weight <= 0 {
			continue
		}
		for i := 0; i < len(files); i++ {
			fileTotals[files[i]] += weight
			for j := i + 1; j < len(files); j++ {
				addPair(graph, files[i], files[j], commit, weight)
				addPair(graph, files[j], files[i], commit, weight)
			}
		}
	}
	out := make(map[string][]Neighbor, len(graph))
	for path, row := range graph {
		items := make([]Neighbor, 0, len(row))
		for dst, stats := range row {
			score := stats.weightedCount
			if fileTotals[path] > 0 {
				score = stats.weightedCount / fileTotals[path]
			}
			items = append(items, Neighbor{
				Path:           dst,
				Count:          stats.count,
				WeightedCount:  stats.weightedCount,
				Score:          score,
				LastSeenCommit: stats.lastSeenCommit,
				LastSeenAt:     stats.lastSeenAt,
				Freshness:      freshness(stats.lastSeenAt, cfg),
				Volatility:     volatility(stats.count, stats.weightedCount),
			})
		}
		sortNeighbors(items)
		if cfg.TopKPerFile > 0 && len(items) > cfg.TopKPerFile {
			items = items[:cfg.TopKPerFile]
		}
		out[path] = items
	}
	return out
}

// PersonalizedRank computes the ContextWiki-style personalized prior from scored
// co-change commits without performing IO.
func PersonalizedRank(seedPaths []string, commits []Commit, cfg Config) map[string]float64 {
	cfg = normalizeConfig(cfg)
	seedPaths = NormalizePaths(seedPaths)
	if len(seedPaths) == 0 {
		return map[string]float64{}
	}
	nodes := map[string]int{}
	var names []string
	addNode := func(path string) int {
		if idx, ok := nodes[path]; ok {
			return idx
		}
		idx := len(names)
		nodes[path] = idx
		names = append(names, path)
		return idx
	}
	type edgeKey struct{ from, to int }
	edges := map[edgeKey]float64{}
	outWeight := map[int]float64{}
	for _, commit := range commits {
		files := FilterPaths(commit.Files, cfg)
		if len(files) < 2 {
			continue
		}
		if len(files) > cfg.MaxFilesPerCommit {
			continue
		}
		commitWeight := WeightedCommitWeight(commit, len(files), cfg)
		if commitWeight <= 0 {
			continue
		}
		indexes := make([]int, 0, len(files))
		for _, file := range files {
			indexes = append(indexes, addNode(file))
		}
		for i := 0; i < len(indexes); i++ {
			for j := i + 1; j < len(indexes); j++ {
				a := indexes[i]
				b := indexes[j]
				edges[edgeKey{from: a, to: b}] += commitWeight
				edges[edgeKey{from: b, to: a}] += commitWeight
				outWeight[a] += commitWeight
				outWeight[b] += commitWeight
			}
		}
	}
	if len(nodes) == 0 {
		return map[string]float64{}
	}
	teleport := make([]float64, len(names))
	seedCount := 0
	for _, seed := range seedPaths {
		if idx, ok := nodes[seed]; ok {
			teleport[idx] = 1
			seedCount++
		}
	}
	if seedCount == 0 {
		return map[string]float64{}
	}
	for i := range teleport {
		teleport[i] /= float64(seedCount)
	}
	rank := append([]float64(nil), teleport...)
	next := make([]float64, len(names))
	alpha := 0.85
	for iter := 0; iter < 24; iter++ {
		for i := range next {
			next[i] = (1 - alpha) * teleport[i]
		}
		for key, weight := range edges {
			if outWeight[key.from] <= 0 {
				continue
			}
			next[key.to] += alpha * rank[key.from] * (weight / outWeight[key.from])
		}
		copy(rank, next)
	}
	out := make(map[string]float64, len(names))
	for idx, path := range names {
		out[path] = rank[idx]
	}
	return out
}

// InspectFromNeighbors projects the scored graph into stable inspect structs.
func InspectFromNeighbors(neighbors map[string][]Neighbor) Inspect {
	files := make([]InspectFile, 0, len(neighbors))
	for path, items := range neighbors {
		fresh, vol := 0.0, 0.0
		for _, item := range items {
			if item.Freshness > fresh {
				fresh = item.Freshness
			}
			if item.Volatility > vol {
				vol = item.Volatility
			}
		}
		files = append(files, InspectFile{
			Path:       path,
			Neighbors:  append([]Neighbor(nil), items...),
			Freshness:  freshnessRound(fresh),
			Volatility: freshnessRound(vol),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Inspect{Files: files}
}

func addPair(graph map[string]map[string]*pairStats, src, dst string, commit Commit, weight float64) {
	row := graph[src]
	if row == nil {
		row = map[string]*pairStats{}
		graph[src] = row
	}
	stats := row[dst]
	if stats == nil {
		stats = &pairStats{}
		row[dst] = stats
	}
	stats.count++
	stats.weightedCount += weight
	if commit.Timestamp.After(stats.lastSeenAt) {
		stats.lastSeenAt = commit.Timestamp
		stats.lastSeenCommit = commit.SHA
	}
}

// CommitWeight returns a pure deterministic weight for a commit.
func CommitWeight(commitTime time.Time, fileCount int, cfg Config) float64 {
	cfg = normalizeConfig(cfg)
	if fileCount < 2 || fileCount > cfg.GiantCommitHardLimit {
		return 0
	}
	ageDays := cfg.Now.Sub(commitTime).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0
	}
	recency := math.Pow(0.5, ageDays/float64(cfg.HalfLifeDays))
	sizePenalty := math.Log1p(float64(fileCount))
	if sizePenalty <= 0 {
		sizePenalty = 1
	}
	weight := recency / sizePenalty
	if fileCount > cfg.GiantCommitSoftLimit {
		weight *= float64(cfg.GiantCommitSoftLimit) / float64(fileCount)
	}
	return weight
}

// WeightedCommitWeight applies commit-level typed signals to the base recency
// and size weighting.
func WeightedCommitWeight(commit Commit, fileCount int, cfg Config) float64 {
	cfg = normalizeConfig(cfg)
	weight := CommitWeight(commit.Timestamp, fileCount, cfg)
	if weight <= 0 {
		return 0
	}
	if commit.FormattingOnly {
		weight *= cfg.FormattingMultiplier
	}
	return weight
}

// NormalizePaths returns slash-separated, de-duplicated, sorted repo paths.
func NormalizePaths(paths []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		path = strings.Trim(path, "/")
		if path == "" || path == "." {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// FilterPaths applies generated, vendor, lockfile, and formatting-noise guards.
func FilterPaths(paths []string, cfg Config) []string {
	cfg = normalizeConfig(cfg)
	normalized := NormalizePaths(paths)
	out := make([]string, 0, len(normalized))
	for _, path := range normalized {
		if cfg.SkipLockfiles && IsLockfile(path) {
			continue
		}
		if cfg.SkipGenerated && IsGeneratedOrVendorPath(path) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func IsLockfile(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb", "go.sum", "cargo.lock", "poetry.lock", "gemfile.lock":
		return true
	default:
		return false
	}
}

func IsGeneratedOrVendorPath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	if strings.HasPrefix(lower, "vendor/") || strings.Contains(lower, "/vendor/") ||
		strings.HasPrefix(lower, "node_modules/") || strings.Contains(lower, "/node_modules/") {
		return true
	}
	base := strings.ToLower(filepath.Base(lower))
	return strings.Contains(base, "generated") || strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, ".gen.go")
}

func sortNeighbors(items []Neighbor) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if items[i].WeightedCount != items[j].WeightedCount {
			return items[i].WeightedCount > items[j].WeightedCount
		}
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Path < items[j].Path
	})
}

func normalizeConfig(cfg Config) Config {
	if cfg.CommitLimit <= 0 {
		cfg.CommitLimit = 40
	}
	if cfg.MaxFilesPerCommit <= 0 {
		cfg.MaxFilesPerCommit = 20
	}
	if cfg.HalfLifeDays <= 0 {
		cfg.HalfLifeDays = defaultHalfLifeDays
	}
	if cfg.TopKPerFile < 0 {
		cfg.TopKPerFile = 0
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.GiantCommitSoftLimit <= 0 {
		cfg.GiantCommitSoftLimit = cfg.MaxFilesPerCommit
	}
	if cfg.GiantCommitHardLimit <= 0 {
		cfg.GiantCommitHardLimit = cfg.MaxFilesPerCommit
	}
	if cfg.GiantCommitHardLimit < cfg.GiantCommitSoftLimit {
		cfg.GiantCommitHardLimit = cfg.GiantCommitSoftLimit
	}
	if cfg.FormattingMultiplier <= 0 || cfg.FormattingMultiplier > 1 {
		cfg.FormattingMultiplier = 0.2
	}
	return cfg
}

func freshness(ts time.Time, cfg Config) float64 {
	if ts.IsZero() {
		return 0
	}
	ageDays := cfg.Now.Sub(ts).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0
	}
	return freshnessRound(math.Pow(0.5, ageDays/float64(cfg.HalfLifeDays)))
}

func volatility(count int, weighted float64) float64 {
	if count <= 0 {
		return 0
	}
	return freshnessRound(math.Log1p(float64(count)) * math.Log1p(weighted))
}

func freshnessRound(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}
