package skillmain

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type mwTestInput struct {
	Timeout int
	Query   string
}

func noopRun(_ context.Context, _ *RunContext, _ mwTestInput) error {
	return nil
}

func failRun(_ context.Context, _ *RunContext, _ mwTestInput) error {
	return errors.New("fail")
}

func testRC(t *testing.T) (*RunContext, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()
	return &RunContext{
		Logger: logger,
		Now:    time.Now,
		Stdout: &bytes.Buffer{},
	}, &buf
}

func TestChain_OrderMatters(t *testing.T) {
	var order []string

	mwA := func(next RunFunc[mwTestInput]) RunFunc[mwTestInput] {
		return func(ctx context.Context, rc *RunContext, in mwTestInput) error {
			order = append(order, "A-enter")
			err := next(ctx, rc, in)
			order = append(order, "A-exit")
			return err
		}
	}
	mwB := func(next RunFunc[mwTestInput]) RunFunc[mwTestInput] {
		return func(ctx context.Context, rc *RunContext, in mwTestInput) error {
			order = append(order, "B-enter")
			err := next(ctx, rc, in)
			order = append(order, "B-exit")
			return err
		}
	}

	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		order = append(order, "run")
		return nil
	}

	chained := Chain(run, mwA, mwB)
	rc, _ := testRC(t)
	_ = chained(context.Background(), rc, mwTestInput{})

	want := "A-enter,B-enter,run,B-exit,A-exit"
	got := strings.Join(order, ",")
	if got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestChain_Empty(t *testing.T) {
	called := false
	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		called = true
		return nil
	}

	chained := Chain(run)
	rc, _ := testRC(t)
	err := chained(context.Background(), rc, mwTestInput{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("run was not called")
	}
}

func TestWithTimeout_Expires(t *testing.T) {
	mw := WithTimeout[mwTestInput](10 * time.Millisecond)
	run := func(ctx context.Context, _ *RunContext, _ mwTestInput) error {
		<-ctx.Done()
		return ctx.Err()
	}

	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestWithTimeout_Completes(t *testing.T) {
	mw := WithTimeout[mwTestInput](5 * time.Second)
	rc, _ := testRC(t)

	err := mw(noopRun)(context.Background(), rc, mwTestInput{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithDynamicTimeout_Zero(t *testing.T) {
	mw := WithDynamicTimeout[mwTestInput](func(_ mwTestInput) time.Duration {
		return 0
	})

	// With zero duration, no timeout should be applied — the context should have no deadline.
	run := func(ctx context.Context, _ *RunContext, _ mwTestInput) error {
		if _, ok := ctx.Deadline(); ok {
			return errors.New("unexpected deadline on context")
		}
		return nil
	}

	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithDynamicTimeout_FromInput(t *testing.T) {
	mw := WithDynamicTimeout[mwTestInput](func(in mwTestInput) time.Duration {
		return time.Duration(in.Timeout) * time.Millisecond
	})

	run := func(ctx context.Context, _ *RunContext, _ mwTestInput) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("expected deadline on context")
		}
		// Deadline should be roughly 100ms from now (allow generous margin).
		remaining := time.Until(deadline)
		if remaining < 50*time.Millisecond || remaining > 200*time.Millisecond {
			return errors.New("deadline not in expected range")
		}
		return nil
	}

	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{Timeout: 100})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithRecover_NoPanic(t *testing.T) {
	mw := WithRecover[mwTestInput]()
	rc, _ := testRC(t)

	err := mw(noopRun)(context.Background(), rc, mwTestInput{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithRecover_CatchesPanic(t *testing.T) {
	mw := WithRecover[mwTestInput]()
	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		panic("kaboom")
	}

	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{})

	if err == nil {
		t.Fatal("expected error from panic")
	}
	if !strings.Contains(err.Error(), "skill panicked: kaboom") {
		t.Errorf("err = %q, want to contain 'skill panicked: kaboom'", err.Error())
	}
}

func TestWithSkillStep_LogsStartAndEnd(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	rc, buf := testRC(t)
	rc.Now = func() time.Time {
		t := now
		now = now.Add(100 * time.Millisecond)
		return t
	}

	mw := WithSkillStep[mwTestInput]("test_step")
	err := mw(noopRun)(context.Background(), rc, mwTestInput{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "test_step") {
		t.Errorf("logs should contain step name, got: %s", logs)
	}
	if !strings.Contains(logs, "step started") {
		t.Errorf("logs should contain 'step started', got: %s", logs)
	}
	if !strings.Contains(logs, "step done") {
		t.Errorf("logs should contain 'step done', got: %s", logs)
	}
}

func TestWithSkillStep_LogsError(t *testing.T) {
	rc, buf := testRC(t)

	mw := WithSkillStep[mwTestInput]("fail_step")
	err := mw(failRun)(context.Background(), rc, mwTestInput{})
	if err == nil {
		t.Fatal("expected error")
	}

	logs := buf.String()
	if !strings.Contains(logs, "step failed") {
		t.Errorf("logs should contain 'step failed', got: %s", logs)
	}
}

func TestWithRetry_SucceedsFirst(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		calls++
		return nil
	}

	mw := WithRetry[mwTestInput](RetryPolicy{MaxAttempts: 3, Backoff: time.Millisecond})
	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWithRetry_RetriesOnError(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		calls++
		if calls < 3 {
			return errors.New("transient")
		}
		return nil
	}

	mw := WithRetry[mwTestInput](RetryPolicy{MaxAttempts: 3, Backoff: time.Millisecond})
	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestWithRetry_NotRetryable(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		calls++
		return errors.New("permanent")
	}

	mw := WithRetry[mwTestInput](RetryPolicy{
		MaxAttempts: 5,
		Backoff:     time.Millisecond,
		Retryable:   func(err error) bool { return false },
	})
	rc, _ := testRC(t)
	err := mw(run)(context.Background(), rc, mwTestInput{})

	if err == nil || err.Error() != "permanent" {
		t.Errorf("err = %v, want 'permanent'", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (should not retry)", calls)
	}
}

func TestWithRetry_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	run := func(_ context.Context, _ *RunContext, _ mwTestInput) error {
		calls++
		cancel() // cancel after first attempt
		return errors.New("fail")
	}

	mw := WithRetry[mwTestInput](RetryPolicy{MaxAttempts: 5, Backoff: time.Millisecond})
	rc, _ := testRC(t)
	err := mw(run)(ctx, rc, mwTestInput{})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWithInputLog_LogsSummary(t *testing.T) {
	mw := WithInputLog[mwTestInput](func(in mwTestInput) map[string]any {
		return map[string]any{
			"query":   in.Query,
			"timeout": in.Timeout,
		}
	})

	rc, buf := testRC(t)
	err := mw(noopRun)(context.Background(), rc, mwTestInput{Query: "test-query", Timeout: 30})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "input received") {
		t.Errorf("logs should contain 'input received', got: %s", logs)
	}
	if !strings.Contains(logs, "test-query") {
		t.Errorf("logs should contain 'test-query', got: %s", logs)
	}
}
