package retry

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultMaxAttempts  = 3
	defaultInitialDelay = time.Second
	defaultMaxDelay     = 30 * time.Second
	defaultMultiplier   = 2.0
)

// Config configures the retry behavior.
type Config struct {
	MaxAttempts  int           `json:"max_attempts"`
	InitialDelay time.Duration `json:"initial_delay,format:units"`
	MaxDelay     time.Duration `json:"max_delay,format:units"`
	Multiplier   float64       `json:"multiplier"`
	Jitter       *bool         `json:"jitter"`
}

// Retryer performs retries for transient HTTP errors.
type Retryer struct {
	config    Config
	sleep     func(context.Context, time.Duration) error
	randFloat func() float64
}

// New creates a Retryer using the provided configuration.
func New(cfg Config) *Retryer {
	cfg = applyDefaults(cfg)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	return &Retryer{
		config:    cfg,
		sleep:     defaultSleep,
		randFloat: rnd.Float64,
	}
}

// Execute runs fn until it succeeds, the context is canceled, or retries are exhausted.
// The provided function should return the HTTP response to inspect for retryable status codes.
//
// Index:
// - Purpose: Retry HTTP operations with backoff and Retry-After support
// - Flow: call fn -> inspect status -> compute delay -> sleep -> retry until max
// - SideEffects: sleeps between retries; closes response bodies on retry
// - FailureModes: context cancellation, function errors, nil response
// - Related: Retryer.nextDelay, parseRetryAfter
// - Keywords: retry, max_attempts, initial_delay, multiplier, retry_after, status_code
func (r *Retryer) Execute(ctx context.Context, fn func() (*http.Response, error)) (*http.Response, error) {
	if fn == nil {
		return nil, errors.New("retry: nil function")
	}

	delay := r.config.InitialDelay
	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		resp, err := fn()
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			return nil, err
		}

		if resp == nil {
			return nil, errors.New("retry: function returned nil response")
		}

		if !isRetryable(resp.StatusCode) || attempt == r.config.MaxAttempts {
			return resp, nil
		}

		wait, fromHeader := r.nextDelay(resp, delay)
		if !fromHeader {
			delay = scaleDelay(delay, r.config.Multiplier, r.config.MaxDelay)
		} else {
			delay = r.config.InitialDelay
		}

		if resp.Body != nil {
			_ = resp.Body.Close()
		}

		if wait <= 0 {
			continue
		}

		if err := r.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func (r *Retryer) nextDelay(resp *http.Response, current time.Duration) (time.Duration, bool) {
	if resp == nil {
		return current, false
	}

	if headerDelay, ok := parseRetryAfter(resp.Header); ok {
		if headerDelay > r.config.MaxDelay {
			headerDelay = r.config.MaxDelay
		}
		return headerDelay, true
	}

	wait := current
	if wait > r.config.MaxDelay {
		wait = r.config.MaxDelay
	}

	if r.jitterEnabled() && wait > 0 {
		factor := 0.5 + r.randFloat()
		wait = time.Duration(float64(wait) * factor)
		if wait > r.config.MaxDelay {
			wait = r.config.MaxDelay
		}
	}

	return wait, false
}

func (r *Retryer) jitterEnabled() bool {
	if r.config.Jitter == nil {
		return true
	}
	return *r.config.Jitter
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(header http.Header) (time.Duration, bool) {
	value := header.Get("Retry-After")
	if value == "" {
		return 0, false
	}

	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds * float64(time.Second)), true
	}

	if t, err := http.ParseTime(value); err == nil {
		delay := time.Until(t)
		if delay < 0 {
			return 0, false
		}
		return delay, true
	}

	return 0, false
}

func scaleDelay(current time.Duration, multiplier float64, limit time.Duration) time.Duration {
	if multiplier < 1 {
		multiplier = defaultMultiplier
	}

	next := time.Duration(math.Round(float64(current) * multiplier))
	if next < 0 {
		next = current
	}
	if next > limit {
		return limit
	}
	return next
}

func isRetryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func applyDefaults(cfg Config) Config {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.InitialDelay <= 0 {
		cfg.InitialDelay = defaultInitialDelay
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = defaultMaxDelay
	}
	if cfg.Multiplier <= 1 {
		cfg.Multiplier = defaultMultiplier
	}
	if cfg.InitialDelay > cfg.MaxDelay {
		cfg.MaxDelay = cfg.InitialDelay
	}
	if cfg.Jitter == nil {
		enabled := true
		cfg.Jitter = &enabled
	}
	return cfg
}
