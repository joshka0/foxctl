package memory

import (
	"math"
	"sort"
	"time"

	"github.com/joshka0/foxctl/internal/storage"
)

const (
	decayBucketRecent = "recent"
	decayBucketWarm   = "warm"
	decayBucketIdle   = "idle"

	decayCandidateMin = 100
	decayCandidateMax = 5000
)

// DecayConfig controls bounded rerank decay behavior.
type DecayConfig struct {
	Enabled        bool
	MinFactor      float64
	MaxFactor      float64
	HalfLife       time.Duration
	RecentWindow   time.Duration
	RecentBoost    float64
	AccessWeight   float64
	AccessBoostCap float64
}

// DecayScore includes base score, decay multiplier, and final clamped score.
type DecayScore struct {
	BaseScore       float64
	DecayFactor     float64
	FinalScore      float64
	Bucket          string
	TimestampSource string
}

// DecayRerankStats summarizes a search-time decay pass without exposing memory content.
type DecayRerankStats struct {
	Enabled          bool
	CandidatesBefore int
	CandidatesAfter  int
	FactorMin        float64
	FactorMax        float64
	FactorAvg        float64
}

// DefaultDecayConfig returns conservative defaults where relevance remains primary.
func DefaultDecayConfig() DecayConfig {
	return DecayConfig{
		Enabled:        true,
		MinFactor:      0.3,
		MaxFactor:      1.5,
		HalfLife:       14 * 24 * time.Hour,
		RecentWindow:   4 * time.Hour,
		RecentBoost:    0.05,
		AccessWeight:   0.03,
		AccessBoostCap: 0.15,
	}
}

// ScoreWithDecay computes a bounded recency/frequency multiplier over baseScore.
// It is pure: callers must provide now.
func ScoreWithDecay(baseScore float64, lastAccessed, updatedAt, now time.Time, accessCount int, cfg DecayConfig) DecayScore {
	cfg = normalizeDecayConfig(cfg)
	if !cfg.Enabled {
		return DecayScore{
			BaseScore:       baseScore,
			DecayFactor:     1,
			FinalScore:      clampFloat(baseScore, 0, 1),
			Bucket:          "disabled",
			TimestampSource: "disabled",
		}
	}
	referenceAt, source := chooseReferenceTime(lastAccessed, updatedAt)
	age := now.Sub(referenceAt)
	if age < 0 {
		age = 0
	}

	halfLifeHours := cfg.HalfLife.Hours()
	ageHours := age.Hours()
	decay := math.Exp(-ageHours / halfLifeHours)
	factor := cfg.MinFactor + (cfg.MaxFactor-cfg.MinFactor)*decay

	if age <= cfg.RecentWindow {
		factor += cfg.RecentBoost
	}

	if accessCount > 0 {
		accessBoost := math.Log1p(float64(accessCount)) * cfg.AccessWeight
		if accessBoost > cfg.AccessBoostCap {
			accessBoost = cfg.AccessBoostCap
		}
		factor += accessBoost
	}

	factor = clampFloat(factor, cfg.MinFactor, cfg.MaxFactor)

	score := DecayScore{
		BaseScore:       baseScore,
		DecayFactor:     factor,
		FinalScore:      clampFloat(baseScore*factor, 0, 1),
		Bucket:          decayBucket(age),
		TimestampSource: source,
	}
	return score
}

func normalizeDecayConfig(cfg DecayConfig) DecayConfig {
	defaults := DefaultDecayConfig()

	if !cfg.Enabled {
		return DecayConfig{Enabled: false, MinFactor: defaults.MinFactor, MaxFactor: defaults.MaxFactor, HalfLife: defaults.HalfLife}
	}
	if cfg.MinFactor <= 0 {
		cfg.MinFactor = defaults.MinFactor
	}
	if cfg.MaxFactor <= cfg.MinFactor {
		cfg.MaxFactor = defaults.MaxFactor
		if cfg.MaxFactor <= cfg.MinFactor {
			cfg.MinFactor = defaults.MinFactor
		}
	}
	if cfg.HalfLife <= 0 {
		cfg.HalfLife = defaults.HalfLife
	}
	if cfg.RecentWindow <= 0 {
		cfg.RecentWindow = defaults.RecentWindow
	}
	if cfg.RecentBoost <= 0 {
		cfg.RecentBoost = defaults.RecentBoost
	}
	if cfg.AccessWeight <= 0 {
		cfg.AccessWeight = defaults.AccessWeight
	}
	if cfg.AccessBoostCap <= 0 {
		cfg.AccessBoostCap = defaults.AccessBoostCap
	}
	return cfg
}

func chooseReferenceTime(lastAccessed, updatedAt time.Time) (time.Time, string) {
	if !lastAccessed.IsZero() {
		return lastAccessed, "last_accessed"
	}
	if !updatedAt.IsZero() {
		return updatedAt, "updated_at"
	}
	return time.Time{}, "zero"
}

func decayBucket(age time.Duration) string {
	switch {
	case age <= 24*time.Hour:
		return decayBucketRecent
	case age <= 7*24*time.Hour:
		return decayBucketWarm
	default:
		return decayBucketIdle
	}
}

func clampFloat(v, minV, maxV float64) float64 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

type decayRankedEntry struct {
	entry     storage.ScoredEntry
	baseScore float64
}

// RerankScoredEntriesWithDecay applies bounded search-time decay before final truncation.
//
// Index:
//
//	Purpose: Shared memory decay rerank boundary for retrieval skills and stores.
//	Keywords: memory decay, recency rerank, candidate widening
//	Related: ScoreWithDecay, RerankScoredEntriesWithDecayStats, DecayCandidateLimit
//
// [[domain:memory-decay-ranking]]
// [[invariant:memory-decay-relevance-primary]]
// [[doc:docs/architecture/memory-core.md#Memory Decay Ranking]]
// [[test:internal/storage/memory/decay_test.go#TestRerankScoredEntriesWithDecay_AppliesLimitAfterRerank]]
func RerankScoredEntriesWithDecay(
	scored []storage.ScoredEntry,
	now time.Time,
	cfg DecayConfig,
	limit int,
) []storage.ScoredEntry {
	reranked, _ := RerankScoredEntriesWithDecayStats(scored, now, cfg, limit)
	return reranked
}

// RerankScoredEntriesWithDecayStats applies decay and returns aggregate factor stats.
func RerankScoredEntriesWithDecayStats(
	scored []storage.ScoredEntry,
	now time.Time,
	cfg DecayConfig,
	limit int,
) ([]storage.ScoredEntry, DecayRerankStats) {
	cfg = normalizeDecayConfig(cfg)
	stats := DecayRerankStats{
		Enabled:          cfg.Enabled,
		CandidatesBefore: len(scored),
	}
	if len(scored) == 0 {
		return nil, stats
	}

	ranked := make([]decayRankedEntry, len(scored))
	var factorSum float64
	for i, candidate := range scored {
		baseScore := candidate.Score
		decay := ScoreWithDecay(
			baseScore,
			candidate.Entry.LastAccess,
			candidate.Entry.UpdatedAt,
			now,
			candidate.Entry.AccessCount,
			cfg,
		)
		if i == 0 || decay.DecayFactor < stats.FactorMin {
			stats.FactorMin = decay.DecayFactor
		}
		if i == 0 || decay.DecayFactor > stats.FactorMax {
			stats.FactorMax = decay.DecayFactor
		}
		factorSum += decay.DecayFactor
		candidate.Score = decay.FinalScore
		ranked[i] = decayRankedEntry{
			entry:     candidate,
			baseScore: baseScore,
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].entry.Score != ranked[j].entry.Score {
			return ranked[i].entry.Score > ranked[j].entry.Score
		}
		if ranked[i].baseScore != ranked[j].baseScore {
			return ranked[i].baseScore > ranked[j].baseScore
		}
		if !ranked[i].entry.Entry.UpdatedAt.Equal(ranked[j].entry.Entry.UpdatedAt) {
			return ranked[i].entry.Entry.UpdatedAt.After(ranked[j].entry.Entry.UpdatedAt)
		}
		if !ranked[i].entry.Entry.LastAccess.Equal(ranked[j].entry.Entry.LastAccess) {
			return ranked[i].entry.Entry.LastAccess.After(ranked[j].entry.Entry.LastAccess)
		}
		if ranked[i].entry.Entry.Name != ranked[j].entry.Entry.Name {
			return ranked[i].entry.Entry.Name < ranked[j].entry.Entry.Name
		}
		return ranked[i].entry.Entry.ID < ranked[j].entry.Entry.ID
	})

	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	stats.CandidatesAfter = len(ranked)
	stats.FactorAvg = factorSum / float64(len(scored))

	reranked := make([]storage.ScoredEntry, len(ranked))
	for i, candidate := range ranked {
		reranked[i] = candidate.entry
	}
	return reranked, stats
}

// DecayCandidateLimit widens a requested result limit so decay can rerank before truncation.
func DecayCandidateLimit(limit int) int {
	if limit <= 0 {
		return limit
	}
	candidateLimit := limit * 3
	if candidateLimit < decayCandidateMin {
		return decayCandidateMin
	}
	if candidateLimit > decayCandidateMax {
		return decayCandidateMax
	}
	return candidateLimit
}
