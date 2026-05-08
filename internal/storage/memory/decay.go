package memory

import (
	"math"
	"time"
)

const (
	decayBucketRecent = "recent"
	decayBucketWarm   = "warm"
	decayBucketIdle   = "idle"
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
