package repoindex

import (
	"context"
	"encoding/json"
	"time"

	gitcochange "github.com/joshka0/foxctl/internal/intelligence/indexing/cochange"
)

const (
	defaultCoChangeTopK        = 5
	defaultCoChangeCommitLimit = 200
	defaultCoChangeMaxFiles    = 80
	defaultCoChangeSoftLimit   = 20
)

type coChangeEdgeMeta struct {
	Count          int       `json:"count"`
	WeightedCount  float64   `json:"weighted_count"`
	LastSeenCommit string    `json:"last_seen_commit,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at,omitempty"`
	Freshness      float64   `json:"freshness"`
	Volatility     float64   `json:"volatility"`
	Source         string    `json:"source"`
}

func applyCoChangeEdges(ctx context.Context, opts BuildOptions, nodes map[string]Node, edges map[string]Edge) error {
	pathNodes := fileNodesByPath(nodes)
	if len(pathNodes) < 2 {
		return nil
	}
	commits, err := gitcochange.CollectGitCommits(ctx, opts.RepoRoot, nil, repoindexCoChangeConfig())
	if err != nil {
		return err
	}
	neighbors := gitcochange.Score(commits, repoindexCoChangeConfig())
	for srcPath, items := range neighbors {
		src, ok := pathNodes[srcPath]
		if !ok {
			continue
		}
		for _, item := range items {
			dst, ok := pathNodes[item.Path]
			if !ok {
				continue
			}
			meta, err := json.Marshal(coChangeEdgeMeta{
				Count:          item.Count,
				WeightedCount:  item.WeightedCount,
				LastSeenCommit: item.LastSeenCommit,
				LastSeenAt:     item.LastSeenAt,
				Freshness:      item.Freshness,
				Volatility:     item.Volatility,
				Source:         "git",
			})
			if err != nil {
				return err
			}
			addEdge(edges, Edge{
				Src:    src.ID,
				Dst:    dst.ID,
				Type:   EdgeCoChangesWith,
				Weight: item.Score,
				Meta:   meta,
			})
		}
	}
	return nil
}

func fileNodesByPath(nodes map[string]Node) map[string]Node {
	out := make(map[string]Node)
	for _, node := range nodes {
		if node.Kind != NodeFile || node.File == "" {
			continue
		}
		path := gitcochange.NormalizePaths([]string{node.File})
		if len(path) != 1 {
			continue
		}
		out[path[0]] = node
	}
	return out
}

func repoindexCoChangeConfig() gitcochange.Config {
	return gitcochange.Config{
		CommitLimit:          defaultCoChangeCommitLimit,
		MaxFilesPerCommit:    defaultCoChangeMaxFiles,
		HalfLifeDays:         90,
		TopKPerFile:          defaultCoChangeTopK,
		Now:                  time.Now().UTC(),
		SkipGenerated:        true,
		SkipLockfiles:        true,
		GiantCommitSoftLimit: defaultCoChangeSoftLimit,
		GiantCommitHardLimit: defaultCoChangeMaxFiles,
	}
}
