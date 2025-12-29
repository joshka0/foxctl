package rerank

import (
	"os"
	"strconv"
	"time"
)

// Config holds configuration for reranking operations.
type Config struct {
	// Enabled controls whether reranking is active.
	// Default: false (opt-in feature)
	Enabled bool

	// TopK is the number of candidates to rerank.
	// More candidates = better recall but higher cost/latency.
	// Default: 50
	TopK int

	// FinalK is the number of results to return after reranking.
	// Default: 10
	FinalK int

	// ScoreBlend controls how to combine rerank and original scores.
	// nil = use rerank score only (default, recommended)
	// 0.0 = use rerank score only
	// 1.0 = use original score only (disables reranking effect)
	// 0.3 = 70% rerank + 30% original
	ScoreBlend *float64

	// Instruction is an optional instruction for the reranker.
	// Rerank-2.5 supports instruction-following for domain-specific tuning.
	// Example: "Rank code snippets by relevance to the programming question"
	Instruction string

	// Model is the reranking model to use.
	// Default: "rerank-2.5"
	Model string

	// Timeout is the HTTP request timeout.
	// Default: 60s
	Timeout time.Duration

	// RateLimit is the max requests per window.
	// nil = default (3 for free tier)
	// 0 = disabled (for paid accounts)
	RateLimit *int
}

// DefaultConfig returns the default reranking configuration.
// Reranking is disabled by default - enable via environment or explicit config.
func DefaultConfig() Config {
	return Config{
		Enabled:    false,
		TopK:       50,
		FinalK:     10,
		ScoreBlend: nil, // Pure rerank score (nil means use 0.0)
		Model:      "rerank-2.5",
		Timeout:    60 * time.Second,
		RateLimit:  nil, // nil means use default (3 for free tier)
	}
}

// FromEnv creates a Config from environment variables.
// Environment variables:
//   - AGENTCTL_RERANK_ENABLED: "true" or "1" to enable
//   - AGENTCTL_RERANK_TOP_K: number of candidates to rerank
//   - AGENTCTL_RERANK_FINAL_K: number of results to return
//   - AGENTCTL_RERANK_SCORE_BLEND: blend factor (0.0-1.0)
//   - AGENTCTL_RERANK_MODEL: model name
//   - AGENTCTL_RERANK_RATE_LIMIT: requests per minute (0 to disable)
func FromEnv() Config {
	cfg := DefaultConfig()

	if v := os.Getenv("AGENTCTL_RERANK_ENABLED"); v != "" {
		cfg.Enabled = v == "true" || v == "1"
	}

	if v := os.Getenv("AGENTCTL_RERANK_TOP_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.TopK = n
		}
	}

	if v := os.Getenv("AGENTCTL_RERANK_FINAL_K"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.FinalK = n
		}
	}

	if v := os.Getenv("AGENTCTL_RERANK_SCORE_BLEND"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			if f < 0 {
				f = 0
			}
			if f > 1 {
				f = 1
			}
			cfg.ScoreBlend = &f
		}
	}

	if v := os.Getenv("AGENTCTL_RERANK_MODEL"); v != "" {
		cfg.Model = v
	}

	if v := os.Getenv("AGENTCTL_RERANK_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.RateLimit = &n
		}
	}

	return cfg
}

// Merge applies non-zero values from other to this config.
// Useful for combining environment config with per-request overrides.
func (c Config) Merge(other Config) Config {
	result := c

	// Boolean fields - only override if explicitly set (we can't detect this directly,
	// so we assume any "true" value is an intentional override)
	if other.Enabled {
		result.Enabled = true
	}

	if other.TopK > 0 {
		result.TopK = other.TopK
	}
	if other.FinalK > 0 {
		result.FinalK = other.FinalK
	}
	if other.ScoreBlend != nil {
		result.ScoreBlend = other.ScoreBlend
	}
	if other.Instruction != "" {
		result.Instruction = other.Instruction
	}
	if other.Model != "" {
		result.Model = other.Model
	}
	if other.Timeout > 0 {
		result.Timeout = other.Timeout
	}
	if other.RateLimit != nil {
		result.RateLimit = other.RateLimit
	}

	return result
}

// ToVoyageConfig converts Config to VoyageConfig for provider creation.
func (c Config) ToVoyageConfig() VoyageConfig {
	rateLimitWait := true
	var scoreBlend float64
	if c.ScoreBlend != nil {
		scoreBlend = *c.ScoreBlend
	}
	return VoyageConfig{
		Model:         c.Model,
		Timeout:       c.Timeout,
		RateLimit:     c.RateLimit,
		RateLimitWait: &rateLimitWait,
		ScoreBlend:    scoreBlend,
	}
}
