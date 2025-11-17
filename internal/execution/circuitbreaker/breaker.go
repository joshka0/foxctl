// Package circuitbreaker implements the circuit breaker pattern for resilient execution.
package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State represents the current state of a circuit breaker.
type State string

const (
	// StateClosed allows all requests through and tracks failures.
	StateClosed State = "closed"
	// StateOpen rejects all requests immediately to prevent cascading failures.
	StateOpen State = "open"
	// StateHalfOpen allows limited requests to test if the service has recovered.
	StateHalfOpen State = "half-open"
)

// Config defines circuit breaker behavior.
type Config struct {
	// MaxFailures is the number of consecutive failures before opening.
	MaxFailures int
	// ResetTimeout is how long to wait in open state before trying half-open.
	ResetTimeout time.Duration
	// MaxHalfOpenRequests is how many test requests to allow in half-open state.
	MaxHalfOpenRequests int
	// SuccessThreshold is how many successes needed in half-open to close.
	SuccessThreshold int
}

// DefaultConfig returns sensible defaults for circuit breaker configuration.
func DefaultConfig() Config {
	return Config{
		MaxFailures:         5,
		ResetTimeout:        30 * time.Second,
		MaxHalfOpenRequests: 3,
		SuccessThreshold:    2,
	}
}

// Breaker implements the circuit breaker pattern with state machine.
type Breaker struct {
	name   string
	config Config
	mu     sync.RWMutex

	state            State
	failures         int
	successes        int
	consecutiveFails int
	lastFailTime     time.Time
	lastStateChange  time.Time
	halfOpenAllowed  int
}

// New creates a new circuit breaker with the given name and config.
func New(name string, config Config) *Breaker {
	return &Breaker{
		name:            name,
		config:          config,
		state:           StateClosed,
		lastStateChange: time.Now(),
	}
}

// Execute runs the given function through the circuit breaker.
// Returns ErrCircuitOpen if the breaker is open.
func (b *Breaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	if !b.Allow() {
		return ErrCircuitOpen
	}

	err := fn(ctx)

	if err != nil {
		b.RecordFailure()
		return err
	}

	b.RecordSuccess()
	return nil
}

// Allow checks if a request is allowed through the circuit breaker.
func (b *Breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()

	switch b.state {
	case StateClosed:
		return true

	case StateOpen:
		// Check if enough time has passed to try half-open
		if now.Sub(b.lastStateChange) > b.config.ResetTimeout {
			b.toHalfOpen()
			return true
		}
		return false

	case StateHalfOpen:
		// Allow limited requests in half-open state
		if b.halfOpenAllowed < b.config.MaxHalfOpenRequests {
			b.halfOpenAllowed++
			return true
		}
		return false

	default:
		return false
	}
}

// RecordSuccess records a successful execution.
func (b *Breaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.consecutiveFails = 0

	switch b.state {
	case StateHalfOpen:
		b.successes++
		if b.successes >= b.config.SuccessThreshold {
			b.toClosed()
		}
	case StateClosed:
		// Reset failure count on success
		b.failures = 0
	}
}

// RecordFailure records a failed execution.
func (b *Breaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.consecutiveFails++
	b.lastFailTime = time.Now()

	switch b.state {
	case StateClosed:
		if b.consecutiveFails >= b.config.MaxFailures {
			b.toOpen()
		}
	case StateHalfOpen:
		// Any failure in half-open immediately reopens the circuit
		b.toOpen()
	}
}

// State returns the current state of the circuit breaker.
func (b *Breaker) State() State {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

// Stats returns current statistics for the circuit breaker.
func (b *Breaker) Stats() Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return Stats{
		Name:             b.name,
		State:            b.state,
		Failures:         b.failures,
		Successes:        b.successes,
		ConsecutiveFails: b.consecutiveFails,
		LastFailTime:     b.lastFailTime,
		LastStateChange:  b.lastStateChange,
	}
}

// Reset manually resets the circuit breaker to closed state.
func (b *Breaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toClosed()
}

// toClosed transitions to closed state (must hold lock).
func (b *Breaker) toClosed() {
	b.state = StateClosed
	b.failures = 0
	b.successes = 0
	b.consecutiveFails = 0
	b.halfOpenAllowed = 0
	b.lastStateChange = time.Now()
}

// toOpen transitions to open state (must hold lock).
func (b *Breaker) toOpen() {
	b.state = StateOpen
	b.halfOpenAllowed = 0
	b.successes = 0
	b.lastStateChange = time.Now()
}

// toHalfOpen transitions to half-open state (must hold lock).
func (b *Breaker) toHalfOpen() {
	b.state = StateHalfOpen
	b.halfOpenAllowed = 0
	b.successes = 0
	b.lastStateChange = time.Now()
}

// Stats holds circuit breaker statistics.
type Stats struct {
	Name             string    `json:"name"`
	State            State     `json:"state"`
	Failures         int       `json:"failures"`
	Successes        int       `json:"successes"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	LastFailTime     time.Time `json:"last_fail_time,omitempty"`
	LastStateChange  time.Time `json:"last_state_change"`
}

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// IsCircuitOpen checks if an error is ErrCircuitOpen.
func IsCircuitOpen(err error) bool {
	return errors.Is(err, ErrCircuitOpen)
}
