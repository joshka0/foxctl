package rerank

import (
	"os"
	"strconv"
	"time"
)

// Environment variable names for rerank configuration.
// FC/IS: Constants ensure consistency between FromEnv and ConfigFromMap.
const (
	EnvRerankEnabled       = "AGENTCTL_RERANK_ENABLED"
	EnvRerankTopK          = "AGENTCTL_RERANK_TOP_K"
	EnvRerankFinalK        = "AGENTCTL_RERANK_FINAL_K"
	EnvRerankScoreBlend    = "AGENTCTL_RERANK_SCORE_BLEND"
	EnvRerankModel         = "AGENTCTL_RERANK_MODEL"
	EnvRerankRateLimit     = "AGENTCTL_RERANK_RATE_LIMIT"
	EnvRerankRateLimitWait = "AGENTCTL_RERANK_RATE_LIMIT_WAIT"
	EnvRerankTimeout       = "AGENTCTL_RERANK_TIMEOUT"
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
	// nil or 0 = disabled (no rate limit)
	// >0 = limit to N requests per window
	RateLimit *int

	// RateLimitWait controls behavior when rate limited.
	// nil = default (true, wait for slot)
	// true = wait until a slot is available
	// false = return error immediately
	RateLimitWait *bool
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
		RateLimit:  nil, // nil = no rate limit
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
//   - AGENTCTL_RERANK_RATE_LIMIT_WAIT: "true" to wait, "false" to error immediately
//   - AGENTCTL_RERANK_TIMEOUT: timeout duration (e.g., "30s", "1m")
func FromEnv() Config {
	// FC/IS: Collect env values at boundary, delegate parsing to pure function.
	envMap := map[string]string{
		EnvRerankEnabled:       os.Getenv(EnvRerankEnabled),
		EnvRerankTopK:          os.Getenv(EnvRerankTopK),
		EnvRerankFinalK:        os.Getenv(EnvRerankFinalK),
		EnvRerankScoreBlend:    os.Getenv(EnvRerankScoreBlend),
		EnvRerankModel:         os.Getenv(EnvRerankModel),
		EnvRerankRateLimit:     os.Getenv(EnvRerankRateLimit),
		EnvRerankRateLimitWait: os.Getenv(EnvRerankRateLimitWait),
		EnvRerankTimeout:       os.Getenv(EnvRerankTimeout),
	}
	return ConfigFromMap(envMap)
}

// ConfigFromMap creates a Config from a string map.
// FC/IS: Pure function for parsing - no os.Getenv calls.
// Tests can call this directly with controlled values.
func ConfigFromMap(envMap map[string]string) Config {
	cfg := DefaultConfig()

	if v := envMap[EnvRerankEnabled]; v != "" {
		cfg.Enabled = v == "true" || v == "1"
	}

	if v := envMap[EnvRerankTopK]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.TopK = n
		}
	}

	if v := envMap[EnvRerankFinalK]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.FinalK = n
		}
	}

	if v := envMap[EnvRerankScoreBlend]; v != "" {
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

	if v := envMap[EnvRerankModel]; v != "" {
		cfg.Model = v
	}

	if v := envMap[EnvRerankRateLimit]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.RateLimit = &n
		}
	}

	if v := envMap[EnvRerankRateLimitWait]; v != "" {
		wait := v == "true" || v == "1"
		cfg.RateLimitWait = &wait
	}

	if v := envMap[EnvRerankTimeout]; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Timeout = d
		}
	}

	return cfg
}

// Merge applies non-zero values from other to this config.
// Useful for combining environment config with per-request overrides.
// Note: Enabled field can only be set to true via merge (cannot merge false over true).
// To disable, set Enabled=false in the base config before merging.
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
	if other.RateLimitWait != nil {
		result.RateLimitWait = other.RateLimitWait
	}

	return result
}

// ToVoyageConfig converts Config to VoyageConfig for provider creation.
// Default RateLimitWait is true (wait for slot rather than fail immediately).
func (c Config) ToVoyageConfig() VoyageConfig {
	// Default to true if not explicitly configured
	rateLimitWait := true
	if c.RateLimitWait != nil {
		rateLimitWait = *c.RateLimitWait
	}
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
