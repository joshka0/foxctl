package foxcular

import "math/rand"

// SampleDecision indicates whether an event should be sampled.
type SampleDecision int

const (
	// Drop indicates the event should not be recorded.
	Drop SampleDecision = iota
	// Record indicates the event should be recorded.
	Record
)

// Sampler determines whether events should be recorded. Implementations must
// be safe for concurrent use.
type Sampler interface {
	// ShouldSample returns the sampling decision for the given event.
	ShouldSample(event *Event) SampleDecision
}

// AlwaysSample records every event. Useful for testing and debug modes.
type AlwaysSample struct{}

// ShouldSample always returns Record.
func (AlwaysSample) ShouldSample(_ *Event) SampleDecision { return Record }

// NeverSample drops every event unless it is forced.
type NeverSample struct{}

// ShouldSample always returns Drop.
func (NeverSample) ShouldSample(_ *Event) SampleDecision { return Drop }

// TailSampler implements tail-based sampling:
//   - Always sample errors and canceled events.
//   - Always sample events that exceed a duration threshold.
//   - Randomly sample healthy events at a configured rate.
//   - Forced events are always sampled regardless of policy.
type TailSampler struct {
	sampleErrors  bool
	slowThreshold int64 // milliseconds
	randomRate    float64
	randSource    randSource
}

// TailSamplerOption configures a TailSampler.
type TailSamplerOption func(*tailSamplerOpts)

type tailSamplerOpts struct {
	sampleErrors  bool
	slowThreshold int64 // milliseconds
	randomRate    float64
	randSource    randSource
}

type randSource interface {
	Float64() float64
}

type wrappedRand struct {
	*rand.Rand
}

// WithSampleErrors controls whether error/canceled events are always sampled.
func WithSampleErrors(v bool) TailSamplerOption {
	return func(o *tailSamplerOpts) { o.sampleErrors = v }
}

// WithSlowThreshold sets the duration threshold (in milliseconds) above which
// events are always sampled.
func WithSlowThreshold(ms int64) TailSamplerOption {
	return func(o *tailSamplerOpts) { o.slowThreshold = ms }
}

// WithRandomRate sets the random sampling rate for healthy events (0.0-1.0).
func WithRandomRate(rate float64) TailSamplerOption {
	return func(o *tailSamplerOpts) { o.randomRate = rate }
}

// WithRandSource sets a deterministic random source (for testing).
func WithRandSource(src randSource) TailSamplerOption {
	return func(o *tailSamplerOpts) { o.randSource = src }
}

// NewTailSampler creates a TailSampler with the given options.
func NewTailSampler(opts ...TailSamplerOption) *TailSampler {
	o := tailSamplerOpts{
		sampleErrors:  true,
		slowThreshold: 1000,
		randomRate:    0.05,
		randSource:    wrappedRand{rand.New(rand.NewSource(0))},
	}
	for _, opt := range opts {
		opt(&o)
	}
	if o.randomRate < 0 {
		o.randomRate = 0
	}
	if o.randomRate > 1 {
		o.randomRate = 1
	}
	return &TailSampler{
		sampleErrors:  o.sampleErrors,
		slowThreshold: o.slowThreshold,
		randomRate:    o.randomRate,
		randSource:    o.randSource,
	}
}

// ShouldSample returns the sampling decision for the given event.
func (s *TailSampler) ShouldSample(event *Event) SampleDecision {
	if event == nil {
		return Drop
	}

	// Forced events always pass.
	if event.Forced {
		return Record
	}

	// Always sample errors.
	if s.sampleErrors && event.Status == StatusError {
		return Record
	}

	// Always sample canceled events.
	if event.Status == StatusCanceled {
		return Record
	}

	// Always sample slow events.
	if s.slowThreshold > 0 && event.Duration.Milliseconds() >= s.slowThreshold {
		return Record
	}

	// Random sampling for healthy events.
	if s.randomRate > 0 && s.randSource.Float64() < s.randomRate {
		return Record
	}

	return Drop
}

// DeterministicSampler uses a fixed sequence of decisions for testing.
type DeterministicSampler struct {
	decisions []SampleDecision
	idx       int
}

// NewDeterministicSampler creates a sampler that cycles through the given
// decisions. If all decisions are consumed, it returns Drop.
func NewDeterministicSampler(decisions ...SampleDecision) *DeterministicSampler {
	return &DeterministicSampler{decisions: decisions}
}

// ShouldSample returns the next decision in sequence.
func (d *DeterministicSampler) ShouldSample(_ *Event) SampleDecision {
	if d.idx < len(d.decisions) {
		dec := d.decisions[d.idx]
		d.idx++
		return dec
	}
	return Drop
}

// forcedFromData checks whether the event data indicates a forced/audit event.
func forcedFromData(data map[string]any) bool {
	if data == nil {
		return false
	}
	if isTruthy(data["forced"]) || isTruthy(data["audit"]) || isTruthy(data["always_sample"]) {
		return true
	}
	return false
}

func isTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "yes"
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	default:
		return false
	}
}
