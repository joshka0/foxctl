package rerank

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment variable names for rerank configuration.
// FC/IS: Constants ensure consistency between FromEnv and ConfigFromMap.
const (
	EnvRerankEnabled       = "FOXCTL_RERANK_ENABLED"
	EnvRerankProvider      = "FOXCTL_RERANK_PROVIDER"
	EnvRerankBaseURL       = "FOXCTL_RERANK_BASE_URL"
	EnvRerankAPIKey        = "FOXCTL_RERANK_API_KEY"
	EnvRerankTopK          = "FOXCTL_RERANK_TOP_K"
	EnvRerankFinalK        = "FOXCTL_RERANK_FINAL_K"
	EnvRerankScoreBlend    = "FOXCTL_RERANK_SCORE_BLEND"
	EnvRerankModel         = "FOXCTL_RERANK_MODEL"
	EnvRerankRateLimit     = "FOXCTL_RERANK_RATE_LIMIT"
	EnvRerankRateLimitWait = "FOXCTL_RERANK_RATE_LIMIT_WAIT"
	EnvRerankTimeout       = "FOXCTL_RERANK_TIMEOUT"
)

// Config holds configuration for reranking operations.
type Config struct {
	// Enabled controls whether reranking is active.
	// Default: false (opt-in feature)
	Enabled bool

	// Provider is the reranking provider family.
	// Default: qwen (OpenAI-compatible /rerank endpoint)
	Provider string

	// BaseURL is the rerank endpoint base URL. The provider posts to baseURL + "/rerank"
	// unless the base already ends with /rerank.
	BaseURL string

	// APIKey is optional for local servers and sent as Bearer auth when present.
	APIKey string

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

	// Instruction is an optional instruction for instruction-aware rerankers.
	// Example: "Rank code snippets by relevance to the programming question"
	Instruction string

	// Model is the reranking model to use.
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
		Provider:   "qwen",
		TopK:       50,
		FinalK:     10,
		ScoreBlend: nil, // Pure rerank score (nil means use 0.0)
		Model:      DefaultQwenRerankModel,
		Timeout:    60 * time.Second,
		RateLimit:  nil, // nil = no rate limit
	}
}

// FromEnv creates a Config from environment variables.
// Environment variables:
//   - FOXCTL_RERANK_ENABLED: "true" or "1" to enable
//   - FOXCTL_RERANK_PROVIDER: rerank provider (default: qwen)
//   - FOXCTL_RERANK_BASE_URL: base URL for the OpenAI-compatible rerank server
//   - FOXCTL_RERANK_API_KEY: optional bearer token for the rerank server
//   - FOXCTL_RERANK_TOP_K: number of candidates to rerank
//   - FOXCTL_RERANK_FINAL_K: number of results to return
//   - FOXCTL_RERANK_SCORE_BLEND: blend factor (0.0-1.0)
//   - FOXCTL_RERANK_MODEL: model name
//   - FOXCTL_RERANK_RATE_LIMIT: requests per minute (0 to disable)
//   - FOXCTL_RERANK_RATE_LIMIT_WAIT: "true" to wait, "false" to error immediately
//   - FOXCTL_RERANK_TIMEOUT: timeout duration (e.g., "30s", "1m")
func FromEnv() Config {
	// FC/IS: Collect env values at boundary, delegate parsing to pure function.
	envMap := map[string]string{
		EnvRerankEnabled:       os.Getenv(EnvRerankEnabled),
		EnvRerankProvider:      os.Getenv(EnvRerankProvider),
		EnvRerankBaseURL:       os.Getenv(EnvRerankBaseURL),
		EnvRerankAPIKey:        os.Getenv(EnvRerankAPIKey),
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

	if v := envMap[EnvRerankProvider]; v != "" {
		cfg.Provider = v
	}

	if v := envMap[EnvRerankBaseURL]; v != "" {
		cfg.BaseURL = v
	}

	if v := envMap[EnvRerankAPIKey]; v != "" {
		cfg.APIKey = v
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

	if other.Provider != "" {
		result.Provider = other.Provider
	}
	if other.BaseURL != "" {
		result.BaseURL = other.BaseURL
	}
	if other.APIKey != "" {
		result.APIKey = other.APIKey
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

// ToQwenConfig converts shared rerank config into the canonical local reranker config.
func (c Config) ToQwenConfig() QwenConfig {
	scoreBlend := 0.0
	if c.ScoreBlend != nil {
		scoreBlend = *c.ScoreBlend
	}
	return QwenConfig{
		APIKey:        c.APIKey,
		Model:         c.Model,
		BaseURL:       c.BaseURL,
		Timeout:       c.Timeout,
		RateLimit:     c.RateLimit,
		RateLimitWait: c.RateLimitWait,
		ScoreBlend:    scoreBlend,
	}
}

// NewProviderFromConfig constructs the configured rerank provider.
func NewProviderFromConfig(cfg Config) (Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "", "qwen", "openai_compat", "openai-compatible", "local":
		return NewQwenProvider(cfg.ToQwenConfig())
	case "noop":
		return NewNoOpProvider(), nil
	default:
		return nil, fmt.Errorf("unsupported rerank provider %q: use qwen, openai_compat, local, or noop", cfg.Provider)
	}
}
