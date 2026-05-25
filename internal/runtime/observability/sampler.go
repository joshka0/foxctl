package observability

import (
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Environment variable names for sampling configuration.
const (
	EnvSampleErrors           = "FOXCTL_OBS_SAMPLE_ERRORS"            // Always sample errors (default: true)
	EnvSlowThresholdMS        = "FOXCTL_OBS_SLOW_THRESHOLD_MS"        // Slow request threshold (default: 1000)
	EnvSampleRate             = "FOXCTL_OBS_SAMPLE_RATE"              // Random sample rate 0.0-1.0 (default: 0.05)
	EnvAlwaysSampleSessions   = "FOXCTL_OBS_ALWAYS_SAMPLE_SESSIONS"   // Comma-separated session IDs
	EnvAlwaysSampleWorkspaces = "FOXCTL_OBS_ALWAYS_SAMPLE_WORKSPACES" // Comma-separated workspace IDs
)

// SampleDecision indicates whether an event should be sampled.
type SampleDecision int

const (
	// Drop indicates the event should not be recorded.
	Drop SampleDecision = iota
	// Sample indicates the event should be recorded (random sample).
	Sample
	// AlwaysSample indicates the event must be recorded (errors, slow, VIP).
	AlwaysSample
)

// Sampler determines whether events should be recorded.
type Sampler interface {
	// ShouldSample returns the sampling decision for the given event.
	ShouldSample(event *Event) SampleDecision
}

// TailSampler implements tail-based sampling following loggingsucks.com principles:
//   - Always sample errors (5xx, exceptions, failures)
//   - Always sample slow requests (above threshold)
//   - Randomly sample healthy requests at configured rate
type TailSampler struct {
	sampleErrors    bool
	slowThresholdMS int64
	randomRate      float64

	alwaysSampleSessions   map[string]struct{}
	alwaysSampleWorkspaces map[string]struct{}
}

// Default sampling configuration.
const (
	DefaultSampleErrors    = true
	DefaultSlowThresholdMS = 1000 // 1 second
	DefaultSampleRate      = 0.05 // 5%
)

var (
	defaultSampler     *TailSampler
	defaultSamplerOnce sync.Once
)

// DefaultSampler returns a shared TailSampler configured from environment variables.
//
// Index:
//
//	Purpose: Provide a singleton sampler configured from environment values
//	Flow: sync.Once init → read env → build TailSampler
//	Related: NewTailSamplerFromEnv, TailSampler.ShouldSample
//	Keywords: sampler, tail_sampler, env, singleton, observability
//
// [[invariant:singleton-once-init]]
// [[domain:observability-sampling]]
func DefaultSampler() *TailSampler {
	defaultSamplerOnce.Do(func() {
		defaultSampler = NewTailSamplerFromEnv()
	})
	return defaultSampler
}

// NewTailSampler creates a TailSampler with explicit configuration.
//
// Index:
//
//	Purpose: Construct a TailSampler with validated configuration
//	Flow: clamp rate → populate struct → return sampler
//	Related: TailSampler.ShouldSample
//	Keywords: tail_sampler, sample_errors, slow_threshold_ms, sample_rate
//
// [[invariant:rate-clamped-0-to-1]]
// [[domain:observability-sampling]]
func NewTailSampler(sampleErrors bool, slowThresholdMS int64, randomRate float64) *TailSampler {
	if math.IsNaN(randomRate) || randomRate < 0 {
		randomRate = 0
	}
	if randomRate > 1 {
		randomRate = 1
	}
	return &TailSampler{
		sampleErrors:    sampleErrors,
		slowThresholdMS: slowThresholdMS,
		randomRate:      randomRate,
	}
}

// NewTailSamplerFromEnv creates a TailSampler from environment variables.
//
// Index:
//
//	Purpose: Build a TailSampler from environment configuration
//	Flow: read env → parse values → apply defaults → construct sampler
//	Related: DefaultSampler, NewTailSampler
//	Keywords: tail_sampler, env, sample_rate, slow_threshold_ms, sample_errors
//
// [[domain:observability-sampling]]
// [[decision:env-fallback-defaults]]
func NewTailSamplerFromEnv() *TailSampler {
	sampleErrors := DefaultSampleErrors
	if v := os.Getenv(EnvSampleErrors); v != "" {
		sampleErrors = parseBool(v, DefaultSampleErrors)
	}

	slowThresholdMS := int64(DefaultSlowThresholdMS)
	if v := os.Getenv(EnvSlowThresholdMS); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil && parsed >= 0 {
			slowThresholdMS = parsed
		}
	}

	randomRate := DefaultSampleRate
	if v := os.Getenv(EnvSampleRate); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			randomRate = parsed
		}
	}

	alwaysSessions := parseListEnv(os.Getenv(EnvAlwaysSampleSessions))
	alwaysWorkspaces := parseListEnv(os.Getenv(EnvAlwaysSampleWorkspaces))

	sampler := NewTailSampler(sampleErrors, slowThresholdMS, randomRate)
	sampler.alwaysSampleSessions = alwaysSessions
	sampler.alwaysSampleWorkspaces = alwaysWorkspaces
	return sampler
}

// ShouldSample implements Sampler interface.
//
// Index:
//
//	Purpose: Decide whether to sample a Event based on tail-sampling rules
//	Flow: check nil → evaluate error/canceled → evaluate slow → random sample → drop
//	Related: TailSampler, SampleDecision
//	Keywords: should_sample, status, duration_ms, random_rate, tail_sampling
//
// [[protocol:tail-sampling-rules]]
// [[domain:observability-sampling]]
func (s *TailSampler) ShouldSample(event *Event) SampleDecision {
	if event == nil {
		return Drop
	}

	// Rule 0: Always sample explicit allowlist/flags
	if s.shouldAlwaysSample(event) {
		return AlwaysSample
	}

	// Rule 1: Always sample errors
	if s.sampleErrors && event.Status == StatusError {
		return AlwaysSample
	}

	// Rule 2: Always sample canceled operations (often indicates problems)
	if event.Status == StatusCanceled {
		return AlwaysSample
	}

	// Rule 3: Always sample slow requests
	if s.slowThresholdMS > 0 && event.Duration.Milliseconds() >= s.slowThresholdMS {
		return AlwaysSample
	}

	// Rule 4: Random sampling for healthy requests
	if s.randomRate > 0 && rand.Float64() < s.randomRate {
		return Sample
	}

	return Drop
}

// SampleAll is a sampler that samples everything (useful for testing/debugging).
type SampleAll struct{}

// ShouldSample always returns AlwaysSample.
func (SampleAll) ShouldSample(event *Event) SampleDecision {
	return AlwaysSample
}

// SampleNone is a sampler that drops everything.
type SampleNone struct{}

// ShouldSample always returns Drop.
func (SampleNone) ShouldSample(event *Event) SampleDecision {
	return Drop
}

func parseBool(s string, defaultVal bool) bool {
	switch s {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func (s *TailSampler) shouldAlwaysSample(event *Event) bool {
	if s == nil || event == nil {
		return false
	}
	if sessionID := eventDataString(event, "session_id"); sessionID != "" {
		if _, ok := s.alwaysSampleSessions[sessionID]; ok {
			return true
		}
	}
	if workspaceID := eventDataString(event, "workspace_id"); workspaceID != "" {
		if _, ok := s.alwaysSampleWorkspaces[workspaceID]; ok {
			return true
		}
	}
	if event.Data != nil {
		if isTruthy(event.Data["debug"]) || isTruthy(event.Data["always_sample"]) {
			return true
		}
	}
	return false
}

func parseListEnv(v string) map[string]struct{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	out := make(map[string]struct{})
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			out[trimmed] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return parseBool(strings.ToLower(strings.TrimSpace(t)), false)
	case float32:
		return t != 0
	case float64:
		return t != 0
	case int:
		return t != 0
	case int8:
		return t != 0
	case int16:
		return t != 0
	case int32:
		return t != 0
	case int64:
		return t != 0
	case uint:
		return t != 0
	case uint8:
		return t != 0
	case uint16:
		return t != 0
	case uint32:
		return t != 0
	case uint64:
		return t != 0
	default:
		return false
	}
}
