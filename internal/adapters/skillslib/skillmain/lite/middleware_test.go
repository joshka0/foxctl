package lite

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChainAppliesMiddlewareInOrder(t *testing.T) {
	var calls []string
	run := Chain(
		func(context.Context, *RunContext, struct{}) error {
			calls = append(calls, "run")
			return nil
		},
		func(next RunFunc[struct{}]) RunFunc[struct{}] {
			return func(ctx context.Context, rc *RunContext, in struct{}) error {
				calls = append(calls, "outer-before")
				err := next(ctx, rc, in)
				calls = append(calls, "outer-after")
				return err
			}
		},
		func(next RunFunc[struct{}]) RunFunc[struct{}] {
			return func(ctx context.Context, rc *RunContext, in struct{}) error {
				calls = append(calls, "inner-before")
				err := next(ctx, rc, in)
				calls = append(calls, "inner-after")
				return err
			}
		},
	)

	if err := run(context.Background(), nil, struct{}{}); err != nil {
		t.Fatalf("run: %v", err)
	}

	want := []string{"outer-before", "inner-before", "run", "inner-after", "outer-after"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %#v, want %#v", calls, want)
		}
	}
}

func TestWithRecoverConvertsPanicToError(t *testing.T) {
	run := Chain(func(context.Context, *RunContext, struct{}) error {
		panic("boom")
	}, WithRecover[struct{}]())

	err := run(context.Background(), nil, struct{}{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "skill panicked: boom" {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestWithTimeoutCancelsContext(t *testing.T) {
	run := Chain(func(ctx context.Context, _ *RunContext, _ struct{}) error {
		<-ctx.Done()
		return ctx.Err()
	}, WithTimeout[struct{}](1*time.Nanosecond))

	err := run(context.Background(), nil, struct{}{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestWithDynamicTimeoutSkipsZeroDuration(t *testing.T) {
	run := Chain(func(ctx context.Context, _ *RunContext, _ struct{}) error {
		if _, ok := ctx.Deadline(); ok {
			t.Fatal("did not expect deadline")
		}
		return nil
	}, WithDynamicTimeout(func(struct{}) time.Duration {
		return 0
	}))

	if err := run(context.Background(), nil, struct{}{}); err != nil {
		t.Fatalf("run: %v", err)
	}
}
