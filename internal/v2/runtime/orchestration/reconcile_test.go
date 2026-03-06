package orchestration

import (
	"context"
	"errors"
	"testing"
)

func TestReconcileFunc_NilIsNoOp(t *testing.T) {
	t.Parallel()

	var fn ReconcileFunc
	if err := fn.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
}

func TestReconcileFunc_InvokesWrappedFunction(t *testing.T) {
	t.Parallel()

	called := false
	wantErr := errors.New("boom")
	fn := ReconcileFunc(func(context.Context) error {
		called = true
		return wantErr
	})

	err := fn.Reconcile(context.Background())
	if !called {
		t.Fatal("reconcile func was not called")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v want %v", err, wantErr)
	}
}
