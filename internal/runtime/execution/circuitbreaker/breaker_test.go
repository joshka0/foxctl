package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"testing/quick"
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

	forceResetTimeoutElapsed(breaker)

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

	forceResetTimeoutElapsed(breaker)
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

	forceResetTimeoutElapsed(breaker)
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

func TestCircuitBreakerDisabledResetTimeoutNeverAutoTransitions(t *testing.T) {
	breaker := New("manual-recovery", Config{
		MaxFailures:         1,
		ResetTimeout:        0,
		MaxHalfOpenRequests: 1,
		SuccessThreshold:    1,
	})

	breaker.RecordFailure()
	if breaker.State() != StateOpen {
		t.Fatalf("state after threshold failure=%s want open", breaker.State())
	}

	forceElapsedSinceStateChange(breaker, time.Hour)
	if breaker.Allow() {
		t.Fatal("Allow()=true with disabled reset timeout; want open circuit to require manual reset")
	}
	if breaker.State() != StateOpen {
		t.Fatalf("state after disabled-timeout Allow()=%s want open", breaker.State())
	}

	breaker.Reset()
	if !breaker.Allow() {
		t.Fatal("Allow()=false after manual reset; want closed circuit to allow requests")
	}
	if breaker.State() != StateClosed {
		t.Fatalf("state after manual reset=%s want closed", breaker.State())
	}
}

func TestCircuitBreakerNegativeResetTimeoutUsesDefaultRecoveryWindow(t *testing.T) {
	defaults := DefaultConfig()
	breaker := New("negative-timeout", Config{
		MaxFailures:         1,
		ResetTimeout:        -time.Second,
		MaxHalfOpenRequests: 1,
		SuccessThreshold:    1,
	})

	breaker.RecordFailure()
	if breaker.State() != StateOpen {
		t.Fatalf("state after threshold failure=%s want open", breaker.State())
	}

	forceElapsedSinceStateChange(breaker, defaults.ResetTimeout/2)
	if breaker.Allow() {
		t.Fatal("Allow()=true before default recovery window elapsed; want open circuit")
	}
	if breaker.State() != StateOpen {
		t.Fatalf("state before default recovery window=%s want open", breaker.State())
	}

	forceResetTimeoutElapsed(breaker)
	if !breaker.Allow() {
		t.Fatal("Allow()=false after default recovery window elapsed; want half-open probe")
	}
	if breaker.State() != StateHalfOpen {
		t.Fatalf("state after default recovery window=%s want half-open", breaker.State())
	}
}

func TestCircuitBreakerNonPositiveCountConfigUsesSafeDefaults(t *testing.T) {
	defaults := DefaultConfig()
	breaker := New("partial-config", Config{
		ResetTimeout: time.Second,
	})

	for i := 0; i < defaults.MaxFailures-1; i++ {
		breaker.RecordFailure()
		if breaker.State() != StateClosed {
			t.Fatalf("failure %d opened breaker with partial config; want closed until default threshold %d", i+1, defaults.MaxFailures)
		}
	}
	breaker.RecordFailure()
	if breaker.State() != StateOpen {
		t.Fatalf("state after default failure threshold=%s want open", breaker.State())
	}

	forceResetTimeoutElapsed(breaker)
	for i := 0; i < defaults.MaxHalfOpenRequests; i++ {
		if !breaker.Allow() {
			t.Fatalf("half-open probe %d rejected; want default allowance %d", i+1, defaults.MaxHalfOpenRequests)
		}
	}
	if breaker.Allow() {
		t.Fatalf("half-open probe beyond default allowance was allowed")
	}

	breaker.RecordFailure()
	if breaker.State() != StateOpen {
		t.Fatalf("state after half-open failure=%s want open", breaker.State())
	}

	forceResetTimeoutElapsed(breaker)
	if !breaker.Allow() {
		t.Fatal("expected first recovery probe to be allowed")
	}
	breaker.RecordSuccess()
	if breaker.State() != StateHalfOpen {
		t.Fatalf("state after one default-threshold success=%s want half-open", breaker.State())
	}
	breaker.RecordSuccess()
	if breaker.State() != StateClosed {
		t.Fatalf("state after default success threshold=%s want closed", breaker.State())
	}
}

func TestCircuitBreakerHalfOpenAllowanceCannotMakeRecoveryImpossible(t *testing.T) {
	breaker := New("impossible-recovery", Config{
		MaxFailures:         1,
		ResetTimeout:        time.Second,
		MaxHalfOpenRequests: 1,
		SuccessThreshold:    2,
	})
	ctx := context.Background()
	probeErr := errors.New("probe failed")

	if err := breaker.Execute(ctx, func(_ context.Context) error {
		return probeErr
	}); !errors.Is(err, probeErr) {
		t.Fatalf("initial Execute() error=%v want probeErr", err)
	}
	if breaker.State() != StateOpen {
		t.Fatalf("state after initial failure=%s want open", breaker.State())
	}

	forceResetTimeoutElapsed(breaker)
	for i := 0; i < 2; i++ {
		if err := breaker.Execute(ctx, func(_ context.Context) error { return nil }); err != nil {
			t.Fatalf("recovery success %d returned %v; want enough half-open allowance to satisfy success threshold", i+1, err)
		}
	}
	if breaker.State() != StateClosed {
		t.Fatalf("state after success threshold=%s want closed", breaker.State())
	}
}

func TestCircuitBreakerExecuteHalfOpenFailureReopensAndBlocksNextCall(t *testing.T) {
	config := Config{
		MaxFailures:         1,
		ResetTimeout:        10 * time.Millisecond,
		MaxHalfOpenRequests: 2,
		SuccessThreshold:    2,
	}
	breaker := New("test-service", config)
	ctx := context.Background()
	probeErr := errors.New("probe failed")

	if err := breaker.Execute(ctx, func(_ context.Context) error {
		return probeErr
	}); !errors.Is(err, probeErr) {
		t.Fatalf("initial Execute() error=%v want probeErr", err)
	}
	if breaker.State() != StateOpen {
		t.Fatalf("state after initial failure=%s want open", breaker.State())
	}

	forceResetTimeoutElapsed(breaker)
	if err := breaker.Execute(ctx, func(_ context.Context) error {
		return probeErr
	}); !errors.Is(err, probeErr) {
		t.Fatalf("half-open probe Execute() error=%v want probeErr", err)
	}
	if breaker.State() != StateOpen {
		t.Fatalf("state after half-open probe failure=%s want open", breaker.State())
	}

	executed := false
	err := breaker.Execute(ctx, func(_ context.Context) error {
		executed = true
		return nil
	})
	if !IsCircuitOpen(err) {
		t.Fatalf("next Execute() error=%v want ErrCircuitOpen", err)
	}
	if executed {
		t.Fatal("next call executed while breaker should be reopened")
	}
}

func TestCircuitBreakerExecuteSequenceOpensAtConsecutiveFailureThreshold(t *testing.T) {
	ctx := context.Background()
	operationErr := errors.New("operation failed")

	property := func(rawThreshold uint8, outcomes []bool) bool {
		maxFailures := int(rawThreshold%5) + 1
		if len(outcomes) > 50 {
			outcomes = outcomes[:50]
		}
		breaker := New("generated-sequence", Config{
			MaxFailures:         maxFailures,
			ResetTimeout:        0,
			MaxHalfOpenRequests: 1,
			SuccessThreshold:    1,
		})

		consecutiveFailures := 0
		opened := false

		for _, succeeds := range outcomes {
			executed := false
			err := breaker.Execute(ctx, func(_ context.Context) error {
				executed = true
				if succeeds {
					return nil
				}
				return operationErr
			})

			if opened {
				if executed || !IsCircuitOpen(err) || breaker.State() != StateOpen {
					return false
				}
				continue
			}

			if !executed {
				return false
			}
			if succeeds {
				if err != nil {
					return false
				}
				consecutiveFailures = 0
				if breaker.State() != StateClosed {
					return false
				}
				continue
			}

			if !errors.Is(err, operationErr) {
				return false
			}
			consecutiveFailures++
			if consecutiveFailures >= maxFailures {
				opened = true
				if breaker.State() != StateOpen {
					return false
				}
				continue
			}
			if breaker.State() != StateClosed {
				return false
			}
		}

		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated execute sequence violated circuit breaker threshold invariant: %v", err)
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

	forceResetTimeoutElapsed(breaker)

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

func forceResetTimeoutElapsed(breaker *Breaker) {
	forceElapsedSinceStateChange(breaker, breaker.config.ResetTimeout+time.Millisecond)
}

func forceElapsedSinceStateChange(breaker *Breaker, elapsed time.Duration) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	breaker.lastStateChange = time.Now().Add(-elapsed)
}
