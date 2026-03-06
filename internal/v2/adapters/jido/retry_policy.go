package jido

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

type RetryFailureClass string

const (
	RetryFailureTimeout   RetryFailureClass = "timeout"
	RetryFailureTransport RetryFailureClass = "transport"
	RetryFailureStorage   RetryFailureClass = "storage"
	RetryFailureTransient RetryFailureClass = "transient"
)

type RetryClassPolicy struct {
	Enabled     bool
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	MaxAttempts int
	Suggestion  string
	Patterns    []string
}

type RetryPolicy struct {
	Classes map[RetryFailureClass]RetryClassPolicy
}

type retryPolicyJSON struct {
	Classes map[string]retryClassPolicyJSON `json:"classes"`
}

type retryClassPolicyJSON struct {
	Enabled     *bool    `json:"enabled,omitempty"`
	BaseDelayMS int64    `json:"base_delay_ms,omitempty"`
	MaxDelayMS  int64    `json:"max_delay_ms,omitempty"`
	MaxAttempts int      `json:"max_attempts,omitempty"`
	Suggestion  string   `json:"suggestion,omitempty"`
	Patterns    []string `json:"patterns,omitempty"`
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Classes: map[RetryFailureClass]RetryClassPolicy{
			RetryFailureTimeout: {
				Enabled:     true,
				BaseDelay:   5 * time.Second,
				MaxDelay:    2 * time.Minute,
				MaxAttempts: 5,
				Suggestion:  "retry scheduled after timeout",
				Patterns:    []string{"timeout", "deadline exceeded"},
			},
			RetryFailureTransport: {
				Enabled:     true,
				BaseDelay:   10 * time.Second,
				MaxDelay:    5 * time.Minute,
				MaxAttempts: 8,
				Suggestion:  "retry scheduled after transient runtime transport failure",
				Patterns: []string{
					"connection refused",
					"connection reset",
					"connection closed",
					"broken pipe",
					"dial unix",
					"no such file or directory",
					"transport is closing",
					"econnreset",
					"econnrefused",
				},
			},
			RetryFailureStorage: {
				Enabled:     true,
				BaseDelay:   2 * time.Second,
				MaxDelay:    1 * time.Minute,
				MaxAttempts: 6,
				Suggestion:  "retry scheduled after transient storage contention",
				Patterns: []string{
					"database is locked",
					"resource temporarily unavailable",
				},
			},
			RetryFailureTransient: {
				Enabled:     true,
				BaseDelay:   5 * time.Second,
				MaxDelay:    2 * time.Minute,
				MaxAttempts: 5,
				Suggestion:  "retry scheduled after transient runtime failure",
				Patterns: []string{
					"temporarily unavailable",
					"temporary failure",
					"try again",
				},
			},
		},
	}
}

func resolveRetryPolicyConfig(policy RetryPolicy) RetryPolicy {
	resolved := normalizeRetryPolicy(policy)
	if len(resolved.Classes) == 0 {
		resolved = DefaultRetryPolicy()
	}
	if override := strings.TrimSpace(os.Getenv(EnvJidoRetryPolicy)); override != "" {
		resolved = mergeRetryPolicyJSON(resolved, override)
	}
	return resolved
}

func normalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	if len(policy.Classes) == 0 {
		return RetryPolicy{}
	}
	out := RetryPolicy{Classes: map[RetryFailureClass]RetryClassPolicy{}}
	for class, cfg := range policy.Classes {
		out.Classes[class] = normalizeRetryClassPolicy(cfg)
	}
	return out
}

func normalizeRetryClassPolicy(cfg RetryClassPolicy) RetryClassPolicy {
	cfg.Suggestion = strings.TrimSpace(cfg.Suggestion)
	cfg.Patterns = normalizeRetryPatterns(cfg.Patterns)
	return cfg
}

func normalizeRetryPatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	out := make([]string, 0, len(patterns))
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		normalized := strings.ToLower(strings.TrimSpace(pattern))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func mergeRetryPolicyJSON(base RetryPolicy, raw string) RetryPolicy {
	var parsed retryPolicyJSON
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return base
	}
	if len(base.Classes) == 0 {
		base = DefaultRetryPolicy()
	}
	if base.Classes == nil {
		base.Classes = map[RetryFailureClass]RetryClassPolicy{}
	}
	for rawClass, cfg := range parsed.Classes {
		class := RetryFailureClass(strings.ToLower(strings.TrimSpace(rawClass)))
		if class == "" {
			continue
		}
		current := base.Classes[class]
		if cfg.Enabled != nil {
			current.Enabled = *cfg.Enabled
		}
		if cfg.BaseDelayMS > 0 {
			current.BaseDelay = time.Duration(cfg.BaseDelayMS) * time.Millisecond
		}
		if cfg.MaxDelayMS > 0 {
			current.MaxDelay = time.Duration(cfg.MaxDelayMS) * time.Millisecond
		}
		if cfg.MaxAttempts > 0 {
			current.MaxAttempts = cfg.MaxAttempts
		}
		if suggestion := strings.TrimSpace(cfg.Suggestion); suggestion != "" {
			current.Suggestion = suggestion
		}
		if len(cfg.Patterns) > 0 {
			current.Patterns = normalizeRetryPatterns(cfg.Patterns)
		}
		base.Classes[class] = normalizeRetryClassPolicy(current)
	}
	return base
}

func classifyRetryFailure(message string, policy RetryPolicy) (RetryFailureClass, RetryClassPolicy, bool) {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" || len(policy.Classes) == 0 {
		return "", RetryClassPolicy{}, false
	}
	order := []RetryFailureClass{
		RetryFailureTimeout,
		RetryFailureTransport,
		RetryFailureStorage,
		RetryFailureTransient,
	}
	for _, class := range order {
		cfg, ok := policy.Classes[class]
		if !ok || !cfg.Enabled {
			continue
		}
		for _, pattern := range cfg.Patterns {
			if strings.Contains(normalized, pattern) {
				return class, cfg, true
			}
		}
	}
	return "", RetryClassPolicy{}, false
}
