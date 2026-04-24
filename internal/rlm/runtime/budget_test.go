package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestBudgetExhaustion(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{
		MaxSubcalls:   1,
		MaxREPLCalls:  1,
		MaxIterations: 1,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	lease, err := budget.ReserveSubcall(context.Background())
	if err != nil {
		t.Fatalf("ReserveSubcall() error = %v", err)
	}
	_, err = budget.ReserveSubcall(context.Background())
	requireLimitExceeded(t, err, LimitSubcalls)

	if err := lease.Release(); err != nil {
		t.Fatalf("lease.Release() error = %v", err)
	}

	if err := budget.ConsumeREPLCall(context.Background()); err != nil {
		t.Fatalf("ConsumeREPLCall() first error = %v", err)
	}
	err = budget.ConsumeREPLCall(context.Background())
	requireLimitExceeded(t, err, LimitREPLCalls)

	if err := budget.ConsumeIteration(context.Background()); err != nil {
		t.Fatalf("ConsumeIteration() first error = %v", err)
	}
	err = budget.ConsumeIteration(context.Background())
	requireLimitExceeded(t, err, LimitIterations)
}

func TestBudgetDepthChecks(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{MaxDepth: 2})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	if err := budget.CheckDepth(2); err != nil {
		t.Fatalf("CheckDepth(2) error = %v", err)
	}
	requireLimitExceeded(t, budget.CheckDepth(3), LimitDepth)

	if err := budget.EnterDepth(context.Background()); err != nil {
		t.Fatalf("EnterDepth() #1 error = %v", err)
	}
	if err := budget.EnterDepth(context.Background()); err != nil {
		t.Fatalf("EnterDepth() #2 error = %v", err)
	}
	requireLimitExceeded(t, budget.EnterDepth(context.Background()), LimitDepth)

	if err := budget.LeaveDepth(); err != nil {
		t.Fatalf("LeaveDepth() #1 error = %v", err)
	}
	if err := budget.LeaveDepth(); err != nil {
		t.Fatalf("LeaveDepth() #2 error = %v", err)
	}
	if err := budget.LeaveDepth(); !errors.Is(err, ErrDepthUnderflow) {
		t.Fatalf("LeaveDepth() underflow error = %v, want %v", err, ErrDepthUnderflow)
	}
}

func TestBudgetTokenAccounting(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{
		MaxParentTokens: 10,
		MaxChildTokens:  6,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	ctx := context.Background()
	if err := budget.ConsumeParentTokens(ctx, 4); err != nil {
		t.Fatalf("ConsumeParentTokens(4) error = %v", err)
	}
	if err := budget.ConsumeParentTokens(ctx, 6); err != nil {
		t.Fatalf("ConsumeParentTokens(6) error = %v", err)
	}
	requireLimitExceeded(t, budget.ConsumeParentTokens(ctx, 1), LimitParentTokens)

	if err := budget.ConsumeChildTokens(ctx, 3); err != nil {
		t.Fatalf("ConsumeChildTokens(3) error = %v", err)
	}
	if err := budget.ConsumeChildTokens(ctx, 3); err != nil {
		t.Fatalf("ConsumeChildTokens(3) second error = %v", err)
	}
	requireLimitExceeded(t, budget.ConsumeChildTokens(ctx, 1), LimitChildTokens)

	if err := budget.ConsumeParentTokens(ctx, -1); !errors.Is(err, ErrInvalidBudgetValue) {
		t.Fatalf("ConsumeParentTokens(-1) error = %v, want %v", err, ErrInvalidBudgetValue)
	}

	snapshot := budget.Snapshot()
	if snapshot.ParentTokens != 10 {
		t.Fatalf("snapshot.ParentTokens = %d, want 10", snapshot.ParentTokens)
	}
	if snapshot.ChildTokens != 6 {
		t.Fatalf("snapshot.ChildTokens = %d, want 6", snapshot.ChildTokens)
	}
}

func TestBudgetRejectsMaxChildren(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{
		MaxChildren:   1,
		MaxConcurrent: 2,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	lease, err := budget.ReserveChild(context.Background())
	if err != nil {
		t.Fatalf("ReserveChild() first error = %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("lease.Release() error = %v", err)
	}

	_, err = budget.ReserveChild(context.Background())
	requireLimitExceeded(t, err, LimitChildren)
}

func TestBudgetRejectsMaxConcurrent(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{
		MaxChildren:   3,
		MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	lease, err := budget.ReserveChild(context.Background())
	if err != nil {
		t.Fatalf("ReserveChild() first error = %v", err)
	}
	_, err = budget.ReserveChild(context.Background())
	requireLimitExceeded(t, err, LimitConcurrentChildren)

	if err := lease.Release(); err != nil {
		t.Fatalf("lease.Release() error = %v", err)
	}
}

func TestBudgetRejectsMaxTotalNodes(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{MaxTotalNodes: 1})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	if err := budget.ConsumeNode(context.Background()); err != nil {
		t.Fatalf("ConsumeNode() first error = %v", err)
	}
	requireLimitExceeded(t, budget.ConsumeNode(context.Background()), LimitTotalNodes)
}

func TestBudgetReleasesConcurrentSlotOnFailure(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{
		MaxChildren:   1,
		MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	first, err := budget.ReserveChild(context.Background())
	if err != nil {
		t.Fatalf("ReserveChild() first error = %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("first.Release() error = %v", err)
	}

	_, err = budget.ReserveChild(context.Background())
	requireLimitExceeded(t, err, LimitChildren)

	if snapshot := budget.Snapshot(); snapshot.ChildrenActive != 0 {
		t.Fatalf("snapshot.ChildrenActive = %d, want 0", snapshot.ChildrenActive)
	}

	lease, err := budget.ReserveConcurrent(context.Background())
	if err != nil {
		t.Fatalf("ReserveConcurrent() error after failed ReserveChild() = %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("lease.Release() error = %v", err)
	}
}

func TestBudgetContextAndDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	budget, err := NewBudget(BudgetConfig{
		MaxDuration: time.Second,
		StartTime:   now,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := budget.ConsumeIteration(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ConsumeIteration(canceled) error = %v, want context.Canceled", err)
	}

	now = now.Add(2 * time.Second)
	err = budget.Check(context.Background())
	var deadlineErr DeadlineExceededError
	if !errors.As(err, &deadlineErr) {
		t.Fatalf("Check() deadline error = %v, want DeadlineExceededError", err)
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("Check() deadline error should unwrap ErrBudgetExceeded, got %v", err)
	}
}

func TestBudgetSnapshotJSONStable(t *testing.T) {
	t.Parallel()

	budget, err := NewBudget(BudgetConfig{
		MaxSubcalls:     3,
		MaxREPLCalls:    4,
		MaxIterations:   5,
		MaxParentTokens: 20,
		MaxChildTokens:  10,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	if err := budget.ConsumeIteration(context.Background()); err != nil {
		t.Fatalf("ConsumeIteration() error = %v", err)
	}
	if err := budget.ConsumeParentTokens(context.Background(), 7); err != nil {
		t.Fatalf("ConsumeParentTokens() error = %v", err)
	}

	snapshot := budget.Snapshot()
	first, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot) error = %v", err)
	}
	second, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot) second error = %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("snapshot JSON not stable:\nfirst:  %s\nsecond: %s", first, second)
	}
}

func requireLimitExceeded(t *testing.T, err error, want BudgetLimit) {
	t.Helper()
	var limitErr LimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want LimitExceededError", err)
	}
	if limitErr.Limit != want {
		t.Fatalf("limit = %q, want %q", limitErr.Limit, want)
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("error = %v, should unwrap ErrBudgetExceeded", err)
	}
}
