package retry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestRetryerExecute_SucceedsAfterRetries(t *testing.T) {
	cfg := Config{
		MaxAttempts:  4,
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2,
		Jitter:       boolPtr(false),
	}

	retryer := New(cfg)

	var waits []time.Duration
	retryer.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	attempts := 0
	responses := []*http.Response{
		newResponse(http.StatusTooManyRequests),
		newResponse(http.StatusInternalServerError),
		newResponse(http.StatusOK),
	}

	resp, err := retryer.Execute(context.Background(), func() (*http.Response, error) {
		defer func() { attempts++ }()
		return responses[attempts], nil
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}

	expectedWaits := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	if len(waits) != len(expectedWaits) {
		t.Fatalf("expected %d waits, got %d", len(expectedWaits), len(waits))
	}

	for i, wait := range waits {
		if wait != expectedWaits[i] {
			t.Fatalf("wait %d: expected %v, got %v", i, expectedWaits[i], wait)
		}
	}
}

func TestRetryerExecute_NonRetryable(t *testing.T) {
	retryer := New(Config{Jitter: boolPtr(false)})

	sleepCalled := false
	retryer.sleep = func(_ context.Context, d time.Duration) error {
		sleepCalled = true
		return nil
	}

	resp, err := retryer.Execute(context.Background(), func() (*http.Response, error) {
		return newResponse(http.StatusBadRequest), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if sleepCalled {
		t.Fatalf("sleep should not be called for non-retryable response")
	}
}

func TestRetryerExecute_RespectsRetryAfter(t *testing.T) {
	retryer := New(Config{
		InitialDelay: time.Second,
		MaxDelay:     10 * time.Second,
		Jitter:       boolPtr(false),
	})

	var waits []time.Duration
	retryer.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	attempts := 0
	responses := []*http.Response{
		withRetryAfter(newResponse(http.StatusServiceUnavailable), "3"),
		newResponse(http.StatusOK),
	}

	resp, err := retryer.Execute(context.Background(), func() (*http.Response, error) {
		defer func() { attempts++ }()
		return responses[attempts], nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if len(waits) != 1 || waits[0] != 3*time.Second {
		t.Fatalf("expected single wait of 3s, got %v", waits)
	}
}

func TestRetryerExecute_ContextCanceled(t *testing.T) {
	retryer := New(Config{Jitter: boolPtr(false)})

	ctx, cancel := context.WithCancel(context.Background())
	retryer.sleep = func(c context.Context, _ time.Duration) error {
		cancel()
		<-c.Done()
		return c.Err()
	}

	_, err := retryer.Execute(ctx, func() (*http.Response, error) {
		return withRetryAfter(newResponse(http.StatusServiceUnavailable), "1"), nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRetryerExecute_JitterApplied(t *testing.T) {
	retryer := New(Config{
		InitialDelay: 100 * time.Millisecond,
		Jitter:       boolPtr(true),
	})

	retryer.randFloat = func() float64 { return 0.5 }

	var waits []time.Duration
	retryer.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	attempts := 0
	responses := []*http.Response{
		newResponse(http.StatusServiceUnavailable),
		newResponse(http.StatusOK),
	}

	resp, err := retryer.Execute(context.Background(), func() (*http.Response, error) {
		defer func() { attempts++ }()
		return responses[attempts], nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if len(waits) != 1 {
		t.Fatalf("expected 1 wait, got %d", len(waits))
	}

	expected := time.Duration(float64(100*time.Millisecond) * (0.5 + 0.5))
	if waits[0] != expected {
		t.Fatalf("expected jittered wait %v, got %v", expected, waits[0])
	}
}

func newResponse(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}
}

func withRetryAfter(resp *http.Response, value string) *http.Response {
	resp.Header.Set("Retry-After", value)
	return resp
}

func boolPtr(v bool) *bool {
	return &v
}
