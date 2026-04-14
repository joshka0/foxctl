package contextplane

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

type coChangePrior struct {
	pathScores map[string]float64
	maxScore   float64
}

type coChangeConfig struct {
	CommitLimit       int
	MaxFilesPerCommit int
	HalfLifeDays      int
}

type coChangeCommit struct {
	sha       string
	timestamp time.Time
	files     []string
}

func emptyCoChangePrior() coChangePrior {
	return coChangePrior{pathScores: map[string]float64{}}
}

func coChangeConfigFromOptions(opts RetrievalOptions) coChangeConfig {
	cfg := coChangeConfig{
		CommitLimit:       opts.CoChangeCommitLimit,
		MaxFilesPerCommit: opts.CoChangeMaxFilesPerCommit,
		HalfLifeDays:      opts.CoChangeHalfLifeDays,
	}
	if cfg.CommitLimit <= 0 {
		cfg.CommitLimit = 40
	}
	if cfg.MaxFilesPerCommit <= 0 {
		cfg.MaxFilesPerCommit = 20
	}
	if cfg.HalfLifeDays <= 0 {
		cfg.HalfLifeDays = 90
	}
	return cfg
}

func buildCoChangePrior(ctx context.Context, workspacePath string, seedPaths []string, cfg coChangeConfig) (coChangePrior, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" || len(seedPaths) == 0 {
		return emptyCoChangePrior(), nil
	}
	seedPaths = normalizeRepoPaths(seedPaths)
	if len(seedPaths) == 0 {
		return emptyCoChangePrior(), nil
	}
	commits, err := collectCoChangeCommits(ctx, workspacePath, seedPaths, cfg)
	if err != nil || len(commits) == 0 {
		return emptyCoChangePrior(), err
	}
	return personalizedCoChangeRank(seedPaths, commits, cfg), nil
}

func collectCoChangeCommits(ctx context.Context, workspacePath string, seedPaths []string, cfg coChangeConfig) ([]coChangeCommit, error) {
	args := []string{"-C", workspacePath, "log", fmt.Sprintf("-n%d", cfg.CommitLimit), "--format=%H%x1f%ct", "--"}
	args = append(args, seedPaths...)
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log cochange: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	seen := map[string]struct{}{}
	commits := make([]coChangeCommit, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
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
		if err != nil || len(files) < 2 {
			continue
		}
		if len(files) > cfg.MaxFilesPerCommit {
			continue
		}
		commits = append(commits, coChangeCommit{
			sha:       sha,
			timestamp: time.Unix(sec, 0).UTC(),
			files:     files,
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
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	return normalizeRepoPaths(lines), nil
}

func personalizedCoChangeRank(seedPaths []string, commits []coChangeCommit, cfg coChangeConfig) coChangePrior {
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
	now := time.Now().UTC()

	for _, commit := range commits {
		files := filterNoisyPaths(normalizeRepoPaths(commit.files))
		if len(files) < 2 {
			continue
		}
		commitWeight := coChangeCommitWeight(commit.timestamp, len(files), now, cfg.HalfLifeDays)
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
		return emptyCoChangePrior()
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
		return emptyCoChangePrior()
	}
	for i := range teleport {
		teleport[i] /= float64(seedCount)
	}

	rank := make([]float64, len(names))
	copy(rank, teleport)
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
		for i := range rank {
			rank[i] = next[i]
		}
	}

	pathScores := make(map[string]float64, len(names))
	maxScore := 0.0
	for idx, path := range names {
		score := rank[idx]
		pathScores[path] = score
		if score > maxScore {
			maxScore = score
		}
	}
	return coChangePrior{
		pathScores: pathScores,
		maxScore:   maxScore,
	}
}

func coChangeCommitWeight(commitTime time.Time, fileCount int, now time.Time, halfLifeDays int) float64 {
	if fileCount < 2 {
		return 0
	}
	ageDays := now.Sub(commitTime).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0
	}
	if halfLifeDays <= 0 {
		halfLifeDays = 90
	}
	recency := math.Pow(0.5, ageDays/float64(halfLifeDays))
	sizePenalty := math.Log1p(float64(fileCount))
	if sizePenalty <= 0 {
		sizePenalty = 1
	}
	return recency / sizePenalty
}

func coChangeBoostForHit(repoPaths []string, prior coChangePrior, weights RetrievalWeights) int {
	if len(prior.pathScores) == 0 || prior.maxScore <= 0 || weights.CoChange <= 0 {
		return 0
	}
	best := 0.0
	for _, path := range normalizeRepoPaths(repoPaths) {
		if score := prior.pathScores[path]; score > best {
			best = score
		}
	}
	if best <= 0 {
		return 0
	}
	normalized := best / prior.maxScore
	return int(normalized * float64(weights.CoChange*10))
}

func normalizeRepoPaths(paths []string) []string {
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

func filterNoisyPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		base := strings.ToLower(filepath.Base(path))
		if base == "" {
			continue
		}
		switch base {
		case "package-lock.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "go.sum":
			continue
		}
		if strings.HasPrefix(path, "vendor/") || strings.HasPrefix(path, "node_modules/") {
			continue
		}
		out = append(out, path)
	}
	return out
}
