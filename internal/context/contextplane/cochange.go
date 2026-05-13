package contextplane

import (
	"context"
	"strings"
	"time"

	gitcochange "github.com/joshka0/foxctl/internal/intelligence/indexing/cochange"
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
	sharedCfg := sharedCoChangeConfig(cfg, 0)
	commits, err := gitcochange.CollectGitCommits(ctx, workspacePath, seedPaths, sharedCfg)
	if err != nil || len(commits) == 0 {
		return emptyCoChangePrior(), err
	}
	return personalizedCoChangeRank(seedPaths, commits, cfg), nil
}

func personalizedCoChangeRank(seedPaths []string, commits []gitcochange.Commit, cfg coChangeConfig) coChangePrior {
	pathScores := gitcochange.PersonalizedRank(seedPaths, commits, sharedCoChangeConfig(cfg, 0))
	maxScore := 0.0
	for _, score := range pathScores {
		if score > maxScore {
			maxScore = score
		}
	}
	return coChangePrior{
		pathScores: pathScores,
		maxScore:   maxScore,
	}
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
	return gitcochange.NormalizePaths(paths)
}

func filterNoisyPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		normalized := normalizeRepoPaths([]string{path})
		if len(normalized) != 1 {
			continue
		}
		path = normalized[0]
		if gitcochange.IsLockfile(path) || gitcochange.IsGeneratedOrVendorPath(path) {
			continue
		}
		out = append(out, path)
	}
	return out
}

func sharedCoChangeConfig(cfg coChangeConfig, topK int) gitcochange.Config {
	if cfg.CommitLimit <= 0 {
		cfg.CommitLimit = 40
	}
	if cfg.MaxFilesPerCommit <= 0 {
		cfg.MaxFilesPerCommit = 20
	}
	if cfg.HalfLifeDays <= 0 {
		cfg.HalfLifeDays = 90
	}
	return gitcochange.Config{
		CommitLimit:          cfg.CommitLimit,
		MaxFilesPerCommit:    cfg.MaxFilesPerCommit,
		HalfLifeDays:         cfg.HalfLifeDays,
		TopKPerFile:          topK,
		Now:                  time.Now().UTC(),
		SkipGenerated:        true,
		SkipLockfiles:        true,
		GiantCommitSoftLimit: cfg.MaxFilesPerCommit,
		GiantCommitHardLimit: cfg.MaxFilesPerCommit,
	}
}
