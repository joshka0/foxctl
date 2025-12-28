package observability

import (
	"math/rand"
	"os"
	"strconv"
	"sync"
)

// Environment variable names for sampling configuration.
const (
	EnvSampleErrors    = "AGENTCTL_OBS_SAMPLE_ERRORS"     // Always sample errors (default: true)
	EnvSlowThresholdMS = "AGENTCTL_OBS_SLOW_THRESHOLD_MS" // Slow request threshold (default: 1000)
	EnvSampleRate      = "AGENTCTL_OBS_SAMPLE_RATE"       // Random sample rate 0.0-1.0 (default: 0.05)
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
	ShouldSample(event *WideEvent) SampleDecision
}

// TailSampler implements tail-based sampling following loggingsucks.com principles:
//   - Always sample errors (5xx, exceptions, failures)
//   - Always sample slow requests (above threshold)
//   - Randomly sample healthy requests at configured rate
type TailSampler struct {
	sampleErrors    bool
	slowThresholdMS int64
	randomRate      float64
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
func DefaultSampler() *TailSampler {
	defaultSamplerOnce.Do(func() {
		defaultSampler = NewTailSamplerFromEnv()
	})
	return defaultSampler
}

// NewTailSampler creates a TailSampler with explicit configuration.
func NewTailSampler(sampleErrors bool, slowThresholdMS int64, randomRate float64) *TailSampler {
	if randomRate < 0 {
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

	return NewTailSampler(sampleErrors, slowThresholdMS, randomRate)
}

// ShouldSample implements Sampler interface.
func (s *TailSampler) ShouldSample(event *WideEvent) SampleDecision {
	if event == nil {
		return Drop
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
	if s.slowThresholdMS > 0 && event.DurationMS >= s.slowThresholdMS {
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
func (SampleAll) ShouldSample(event *WideEvent) SampleDecision {
	return AlwaysSample
}

// SampleNone is a sampler that drops everything.
type SampleNone struct{}

// ShouldSample always returns Drop.
func (SampleNone) ShouldSample(event *WideEvent) SampleDecision {
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
