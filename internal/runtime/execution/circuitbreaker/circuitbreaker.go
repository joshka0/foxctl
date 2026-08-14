// Package circuitbreaker re-exports the foxsearch circuitbreaker module.
//
// The implementation moved to github.com/joshka0/foxsearch/circuitbreaker as
// part of the foxsearch v0.2.0 extraction; this shim preserves the foxctl
// import path for existing consumers. New code may import either path.
package circuitbreaker

import "github.com/joshka0/foxsearch/circuitbreaker"

type (
	// State is the circuit breaker state.
	State = circuitbreaker.State
	// Config configures a circuit breaker.
	Config = circuitbreaker.Config
	// Breaker is a single named circuit breaker.
	Breaker = circuitbreaker.Breaker
	// Stats reports breaker counters.
	Stats = circuitbreaker.Stats
	// Manager manages a set of named breakers with shared config.
	Manager = circuitbreaker.Manager
)

const (
	// StateClosed allows all requests through and tracks failures.
	StateClosed = circuitbreaker.StateClosed
	// StateOpen rejects all requests immediately to prevent cascading failures.
	StateOpen = circuitbreaker.StateOpen
	// StateHalfOpen allows limited requests to test if the service has recovered.
	StateHalfOpen = circuitbreaker.StateHalfOpen
)

var (
	// DefaultConfig returns the default breaker configuration.
	DefaultConfig = circuitbreaker.DefaultConfig
	// New creates a single named breaker.
	New = circuitbreaker.New
	// NewManager creates a breaker manager with shared config.
	NewManager = circuitbreaker.NewManager
	// IsCircuitOpen reports whether err is a circuit-open error.
	IsCircuitOpen = circuitbreaker.IsCircuitOpen
)
