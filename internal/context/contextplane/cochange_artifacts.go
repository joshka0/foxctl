package contextplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage"
)

const (
	CoChangeClusterType       = "cochange_cluster"
	coChangeClusterNamePrefix = "cochange://"
)

type CoChangeArtifactBuildOptions struct {
	CommitLimit       int
	MaxFilesPerCommit int
	HalfLifeDays      int
	MaxClusters       int
	MaxNeighbors      int
}

type CoChangeCluster struct {
	AnchorPath         string    `json:"anchor_path"`
	NeighborPaths      []string  `json:"neighbor_paths"`
	RecentCommitThemes []string  `json:"recent_commit_themes,omitempty"`
	SupportCount       int       `json:"support_count"`
	GeneratedAt        time.Time `json:"generated_at"`
	Summary            string    `json:"summary"`
}

type CoChangeClusterSearchHit struct {
	Name       string    `json:"name"`
	AnchorPath string    `json:"anchor_path"`
	Summary    string    `json:"summary"`
	Score      float64   `json:"score"`
	Neighbors  []string  `json:"neighbors,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

func DefaultCoChangeArtifactBuildOptions() CoChangeArtifactBuildOptions {
	return CoChangeArtifactBuildOptions{
		CommitLimit:       80,
		MaxFilesPerCommit: 20,
		HalfLifeDays:      90,
		MaxClusters:       25,
		MaxNeighbors:      6,
	}
}

func BuildCoChangeArtifacts(ctx context.Context, workspacePath string, memStore storage.MemoryStore, provider semantic.EmbeddingProvider, opts CoChangeArtifactBuildOptions) ([]CoChangeCluster, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return nil, fmt.Errorf("workspace path required")
	}
	if memStore == nil {
		return nil, fmt.Errorf("memory store required")
	}
	cfg := coChangeConfig{
		CommitLimit:       opts.CommitLimit,
		MaxFilesPerCommit: opts.MaxFilesPerCommit,
		HalfLifeDays:      opts.HalfLifeDays,
	}
	if cfg.CommitLimit <= 0 {
		cfg = coChangeConfigFromOptions(DefaultRetrievalOptions())
	}
	if opts.MaxClusters <= 0 {
		opts.MaxClusters = 25
	}
	if opts.MaxNeighbors <= 0 {
		opts.MaxNeighbors = 6
	}

	anchors, err := deriveCoChangeAnchors(ctx, workspacePath, cfg, opts.MaxClusters)
	if err != nil {
		return nil, err
	}
	if _, err := memStore.DeleteByNamePrefix(ctx, workspacePath, coChangeClusterNamePrefix); err != nil {
		return nil, fmt.Errorf("delete stale cochange artifacts: %w", err)
	}

	clusters := make([]CoChangeCluster, 0, len(anchors))
	for _, anchor := range anchors {
		prior, err := buildCoChangePrior(ctx, workspacePath, []string{anchor.Path}, cfg)
		if err != nil {
			return nil, err
		}
		neighbors := topCoChangeNeighbors(prior, anchor.Path, opts.MaxNeighbors)
		if len(neighbors) == 0 {
			continue
		}
		themes, err := recentCommitThemesForPath(ctx, workspacePath, anchor.Path, 4)
		if err != nil {
			return nil, err
		}
		cluster := CoChangeCluster{
			AnchorPath:         anchor.Path,
			NeighborPaths:      neighbors,
			RecentCommitThemes: themes,
			SupportCount:       anchor.SupportCount,
			GeneratedAt:        time.Now().UTC(),
		}
		cluster.Summary = summarizeCoChangeCluster(cluster)
		payload, err := json.Marshal(cluster)
		if err != nil {
			return nil, fmt.Errorf("marshal cochange cluster: %w", err)
		}
		name := coChangeClusterName(anchor.Path)
		if _, err := memStore.SaveFromResult(ctx, name, CoChangeClusterType, workspacePath, cluster.Summary, payload); err != nil {
			return nil, fmt.Errorf("save cochange artifact %s: %w", name, err)
		}
		if provider != nil {
			text := clusterEmbeddingText(cluster)
			vec, err := provider.Embed(ctx, text)
			if err != nil {
				return nil, fmt.Errorf("embed cochange artifact %s: %w", name, err)
			}
			if err := memStore.UpdateEmbedding(ctx, name, workspacePath, vec); err != nil {
				return nil, fmt.Errorf("store cochange artifact embedding %s: %w", name, err)
			}
		}
		clusters = append(clusters, cluster)
	}

	return clusters, nil
}

func SearchCoChangeArtifacts(ctx context.Context, workspacePath, query string, limit int, memStore storage.MemoryStore, provider semantic.EmbeddingProvider) ([]CoChangeClusterSearchHit, error) {
	if memStore == nil {
		return nil, fmt.Errorf("memory store required")
	}
	if limit <= 0 {
		limit = 10
	}

	type memoryByType interface {
		ListFiltered(ctx context.Context, workspace string, filter storage.MemoryListFilter, limit, offset int) ([]storage.NamedEntry, int, error)
		SearchSimilarByType(ctx context.Context, workspace, entryType string, embedding []float32, limit int) ([]storage.ScoredEntry, error)
	}
	typedStore, ok := memStore.(memoryByType)
	if !ok {
		return nil, fmt.Errorf("memory store does not support typed cochange search")
	}

	if provider != nil && strings.TrimSpace(query) != "" {
		vec, err := provider.Embed(ctx, query)
		if err == nil {
			scored, err := typedStore.SearchSimilarByType(ctx, workspacePath, CoChangeClusterType, vec, limit)
			if err == nil {
				return coChangeHitsFromScored(scored), nil
			}
		}
	}

	entries, _, err := typedStore.ListFiltered(ctx, workspacePath, storage.MemoryListFilter{Types: []string{CoChangeClusterType}}, max(limit*5, 50), 0)
	if err != nil {
		return nil, err
	}
	return coChangeHitsFromEntries(entries, query, limit), nil
}

type coChangeAnchor struct {
	Path         string
	SupportCount int
}

func deriveCoChangeAnchors(ctx context.Context, workspacePath string, cfg coChangeConfig, maxClusters int) ([]coChangeAnchor, error) {
	args := []string{"-C", workspacePath, "log", fmt.Sprintf("-n%d", cfg.CommitLimit), "--format=%H%x1f%ct"}
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log anchors: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	counts := map[string]int{}
	seenCommits := map[string]struct{}{}
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
		if _, ok := seenCommits[sha]; ok {
			continue
		}
		seenCommits[sha] = struct{}{}
		files, err := changedFilesForCommit(ctx, workspacePath, sha)
		if err != nil {
			return nil, err
		}
		files = filterNoisyPaths(normalizeRepoPaths(files))
		if len(files) < 2 || len(files) > cfg.MaxFilesPerCommit {
			continue
		}
		for _, file := range files {
			counts[file]++
		}
	}

	anchors := make([]coChangeAnchor, 0, len(counts))
	for path, count := range counts {
		if count <= 0 {
			continue
		}
		anchors = append(anchors, coChangeAnchor{Path: path, SupportCount: count})
	}
	sort.SliceStable(anchors, func(i, j int) bool {
		if anchors[i].SupportCount != anchors[j].SupportCount {
			return anchors[i].SupportCount > anchors[j].SupportCount
		}
		return anchors[i].Path < anchors[j].Path
	})
	if maxClusters > 0 && len(anchors) > maxClusters {
		anchors = anchors[:maxClusters]
	}
	return anchors, nil
}

func topCoChangeNeighbors(prior coChangePrior, anchorPath string, limit int) []string {
	type candidate struct {
		path  string
		score float64
	}
	var items []candidate
	for path, score := range prior.pathScores {
		if path == anchorPath || score <= 0 {
			continue
		}
		items = append(items, candidate{path: path, score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].path < items[j].path
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.path)
	}
	return out
}

func recentCommitThemesForPath(ctx context.Context, workspacePath, repoPath string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 4
	}
	cmd := exec.CommandContext(ctx, "git", "-C", workspacePath, "log", fmt.Sprintf("-n%d", limit), "--format=%s", "--", repoPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log themes %s: %w (%s)", repoPath, err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	return normalizeThemes(lines), nil
}

func normalizeThemes(lines []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out
}

func summarizeCoChangeCluster(cluster CoChangeCluster) string {
	base := filepath.Base(cluster.AnchorPath)
	neighbors := shortenStrings(cluster.NeighborPaths, 3)
	themes := shortenStrings(cluster.RecentCommitThemes, 2)
	parts := []string{
		fmt.Sprintf("%s changes with %s", base, strings.Join(neighbors, ", ")),
		fmt.Sprintf("%d supporting commits", cluster.SupportCount),
	}
	if len(themes) > 0 {
		parts = append(parts, "themes: "+strings.Join(themes, "; "))
	}
	return strings.Join(parts, ". ")
}

func clusterEmbeddingText(cluster CoChangeCluster) string {
	var b strings.Builder
	b.WriteString("Co-change cluster\n")
	b.WriteString("Anchor: ")
	b.WriteString(cluster.AnchorPath)
	b.WriteString("\nNeighbors: ")
	b.WriteString(strings.Join(cluster.NeighborPaths, ", "))
	if len(cluster.RecentCommitThemes) > 0 {
		b.WriteString("\nThemes: ")
		b.WriteString(strings.Join(cluster.RecentCommitThemes, "; "))
	}
	b.WriteString("\nSummary: ")
	b.WriteString(cluster.Summary)
	return b.String()
}

func coChangeClusterName(anchorPath string) string {
	return coChangeClusterNamePrefix + normalizeRepoPaths([]string{anchorPath})[0]
}

func coChangeHitsFromScored(entries []storage.ScoredEntry) []CoChangeClusterSearchHit {
	out := make([]CoChangeClusterSearchHit, 0, len(entries))
	for _, scored := range entries {
		cluster := decodeCoChangeCluster(scored.Entry.Result)
		out = append(out, CoChangeClusterSearchHit{
			Name:       scored.Entry.Name,
			AnchorPath: cluster.AnchorPath,
			Summary:    scored.Entry.Summary,
			Score:      scored.Score,
			Neighbors:  append([]string(nil), cluster.NeighborPaths...),
			UpdatedAt:  scored.Entry.UpdatedAt,
		})
	}
	return out
}

func coChangeHitsFromEntries(entries []storage.NamedEntry, query string, limit int) []CoChangeClusterSearchHit {
	query = strings.ToLower(strings.TrimSpace(query))
	type candidate struct {
		hit   CoChangeClusterSearchHit
		score float64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		cluster := decodeCoChangeCluster(entry.Result)
		text := strings.ToLower(strings.TrimSpace(entry.Summary + " " + cluster.AnchorPath + " " + strings.Join(cluster.NeighborPaths, " ")))
		score := 0.0
		if query == "" {
			score = 1
		} else if strings.Contains(text, query) {
			score = 2
		} else {
			for _, token := range strings.Fields(query) {
				if strings.Contains(text, token) {
					score += 0.4
				}
			}
		}
		if score <= 0 {
			continue
		}
		candidates = append(candidates, candidate{
			hit: CoChangeClusterSearchHit{
				Name:       entry.Name,
				AnchorPath: cluster.AnchorPath,
				Summary:    entry.Summary,
				Score:      score,
				Neighbors:  append([]string(nil), cluster.NeighborPaths...),
				UpdatedAt:  entry.UpdatedAt,
			},
			score: score,
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].hit.AnchorPath < candidates[j].hit.AnchorPath
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	out := make([]CoChangeClusterSearchHit, 0, len(candidates))
	for _, item := range candidates {
		out = append(out, item.hit)
	}
	return out
}

func decodeCoChangeCluster(body []byte) CoChangeCluster {
	var cluster CoChangeCluster
	_ = json.Unmarshal(body, &cluster)
	return cluster
}

func shortenStrings(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return append([]string(nil), items...)
	}
	return append([]string(nil), items[:limit]...)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
