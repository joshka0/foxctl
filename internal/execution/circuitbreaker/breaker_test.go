package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreakerClosedToOpen(t *testing.T) {
	config := Config{
		MaxFailures:         3,
		ResetTimeout:        100 * time.Millisecond,
		MaxHalfOpenRequests: 2,
		SuccessThreshold:    2,
	}

	breaker := New("test-service", config)

	// Initial state should be closed
	if breaker.State() != StateClosed {
		t.Errorf("expected initial state closed, got %s", breaker.State())
	}

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		breaker.RecordFailure()
	}

	// Should now be open
	if breaker.State() != StateOpen {
		t.Errorf("expected state open after %d failures, got %s", config.MaxFailures, breaker.State())
	}

	// Requests should be rejected
	if breaker.Allow() {
		t.Error("expected request to be rejected when circuit is open")
	}
}

func TestCircuitBreakerOpenToHalfOpen(t *testing.T) {
	config := Config{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		MaxHalfOpenRequests: 2,
		SuccessThreshold:    2,
	}

	breaker := New("test-service", config)

	// Force open state
	breaker.RecordFailure()
	breaker.RecordFailure()

	if breaker.State() != StateOpen {
		t.Fatalf("expected open state, got %s", breaker.State())
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Next request should transition to half-open
	if !breaker.Allow() {
		t.Error("expected request to be allowed after reset timeout")
	}

	if breaker.State() != StateHalfOpen {
		t.Errorf("expected half-open state after timeout, got %s", breaker.State())
	}
}

func TestCircuitBreakerHalfOpenToClosed(t *testing.T) {
	config := Config{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		MaxHalfOpenRequests: 3,
		SuccessThreshold:    2,
	}

	breaker := New("test-service", config)

	// Force open state
	breaker.RecordFailure()
	breaker.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(60 * time.Millisecond)
	breaker.Allow()

	if breaker.State() != StateHalfOpen {
		t.Fatalf("expected half-open state, got %s", breaker.State())
	}

	// Record enough successes to close
	breaker.RecordSuccess()
	breaker.RecordSuccess()

	if breaker.State() != StateClosed {
		t.Errorf("expected closed state after %d successes, got %s", config.SuccessThreshold, breaker.State())
	}
}

func TestCircuitBreakerHalfOpenToOpen(t *testing.T) {
	config := Config{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		MaxHalfOpenRequests: 3,
		SuccessThreshold:    2,
	}

	breaker := New("test-service", config)

	// Force open state
	breaker.RecordFailure()
	breaker.RecordFailure()

	// Transition to half-open
	time.Sleep(60 * time.Millisecond)
	breaker.Allow()

	if breaker.State() != StateHalfOpen {
		t.Fatalf("expected half-open state, got %s", breaker.State())
	}

	// Single failure in half-open should reopen
	breaker.RecordFailure()

	if breaker.State() != StateOpen {
		t.Errorf("expected open state after failure in half-open, got %s", breaker.State())
	}
}

func TestCircuitBreakerExecuteSuccess(t *testing.T) {
	config := DefaultConfig()
	breaker := New("test-service", config)

	ctx := context.Background()
	executed := false

	err := breaker.Execute(ctx, func(_ context.Context) error {
		executed = true
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !executed {
		t.Error("expected function to be executed")
	}

	stats := breaker.Stats()
	if stats.ConsecutiveFails != 0 {
		t.Errorf("expected 0 consecutive failures, got %d", stats.ConsecutiveFails)
	}
}

func TestCircuitBreakerExecuteFailure(t *testing.T) {
	config := Config{
		MaxFailures:  3,
		ResetTimeout: 1 * time.Second,
	}
	breaker := New("test-service", config)

	ctx := context.Background()
	expectedErr := errors.New("test error")

	// Execute failures up to threshold
	for i := 0; i < 3; i++ {
		err := breaker.Execute(ctx, func(_ context.Context) error {
			return expectedErr
		})

		if !errors.Is(err, expectedErr) {
			t.Errorf("expected test error, got %v", err)
		}
	}

	// Circuit should now be open
	if breaker.State() != StateOpen {
		t.Errorf("expected open state, got %s", breaker.State())
	}

	// Next execution should fail with circuit open error
	err := breaker.Execute(ctx, func(_ context.Context) error {
		t.Error("function should not be executed when circuit is open")
		return nil
	})

	if !IsCircuitOpen(err) {
		t.Errorf("expected circuit open error, got %v", err)
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	config := Config{
		MaxFailures:  2,
		ResetTimeout: 1 * time.Hour, // Long timeout
	}
	breaker := New("test-service", config)

	// Force open state
	breaker.RecordFailure()
	breaker.RecordFailure()

	if breaker.State() != StateOpen {
		t.Fatalf("expected open state, got %s", breaker.State())
	}

	// Manually reset
	breaker.Reset()

	if breaker.State() != StateClosed {
		t.Errorf("expected closed state after reset, got %s", breaker.State())
	}

	// Should allow requests
	if !breaker.Allow() {
		t.Error("expected requests to be allowed after reset")
	}

	stats := breaker.Stats()
	if stats.Failures != 0 {
		t.Errorf("expected 0 failures after reset, got %d", stats.Failures)
	}
	if stats.ConsecutiveFails != 0 {
		t.Errorf("expected 0 consecutive failures after reset, got %d", stats.ConsecutiveFails)
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	config := DefaultConfig()
	breaker := New("test-service", config)

	stats := breaker.Stats()

	if stats.Name != "test-service" {
		t.Errorf("expected name test-service, got %s", stats.Name)
	}

	if stats.State != StateClosed {
		t.Errorf("expected closed state, got %s", stats.State)
	}

	// Record some failures
	breaker.RecordFailure()
	breaker.RecordFailure()

	stats = breaker.Stats()
	if stats.Failures != 2 {
		t.Errorf("expected 2 failures, got %d", stats.Failures)
	}
	if stats.ConsecutiveFails != 2 {
		t.Errorf("expected 2 consecutive failures, got %d", stats.ConsecutiveFails)
	}

	// Record success
	breaker.RecordSuccess()

	stats = breaker.Stats()
	if stats.ConsecutiveFails != 0 {
		t.Errorf("expected consecutive failures to reset, got %d", stats.ConsecutiveFails)
	}
}

func TestCircuitBreakerHalfOpenLimit(t *testing.T) {
	config := Config{
		MaxFailures:         2,
		ResetTimeout:        50 * time.Millisecond,
		MaxHalfOpenRequests: 2,
		SuccessThreshold:    2,
	}

	breaker := New("test-service", config)

	// Force open
	breaker.RecordFailure()
	breaker.RecordFailure()

	// Wait and transition to half-open
	time.Sleep(60 * time.Millisecond)

	// First two requests should be allowed
	if !breaker.Allow() {
		t.Error("expected first request in half-open to be allowed")
	}
	if !breaker.Allow() {
		t.Error("expected second request in half-open to be allowed")
	}

	// Third request should be rejected (limit reached)
	if breaker.Allow() {
		t.Error("expected third request to be rejected in half-open")
	}
}

func TestCircuitBreakerSuccessResetsFailures(t *testing.T) {
	config := Config{
		MaxFailures:  5,
		ResetTimeout: 1 * time.Second,
	}

	breaker := New("test-service", config)

	// Record some failures
	breaker.RecordFailure()
	breaker.RecordFailure()
	breaker.RecordFailure()

	stats := breaker.Stats()
	if stats.ConsecutiveFails != 3 {
		t.Errorf("expected 3 consecutive failures, got %d", stats.ConsecutiveFails)
	}

	// Record success
	breaker.RecordSuccess()

	stats = breaker.Stats()
	if stats.ConsecutiveFails != 0 {
		t.Errorf("expected consecutive failures to be reset, got %d", stats.ConsecutiveFails)
	}

	// Should still be closed
	if breaker.State() != StateClosed {
		t.Errorf("expected closed state, got %s", breaker.State())
	}
}
