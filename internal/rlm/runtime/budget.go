package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrBudgetExceeded marks errors where a configured budget limit is exceeded.
	ErrBudgetExceeded = errors.New("rlm runtime: budget exceeded")
	// ErrDepthUnderflow marks attempts to leave depth when already at zero.
	ErrDepthUnderflow = errors.New("rlm runtime: depth underflow")
	// ErrSubcallReleaseUnderflow marks release attempts without an active subcall.
	ErrSubcallReleaseUnderflow = errors.New("rlm runtime: subcall release underflow")
	// ErrConcurrentReleaseUnderflow marks release attempts without an active child slot.
	ErrConcurrentReleaseUnderflow = errors.New("rlm runtime: concurrent release underflow")
	// ErrInvalidBudgetValue marks invalid config values or consume counts.
	ErrInvalidBudgetValue = errors.New("rlm runtime: invalid budget value")
)

// BudgetLimit identifies one logical budget lane.
type BudgetLimit string

const (
	LimitDepth              BudgetLimit = "depth"
	LimitSubcalls           BudgetLimit = "subcalls"
	LimitREPLCalls          BudgetLimit = "repl_calls"
	LimitIterations         BudgetLimit = "iterations"
	LimitParentTokens       BudgetLimit = "parent_tokens"
	LimitChildTokens        BudgetLimit = "child_tokens"
	LimitHelperCalls        BudgetLimit = "helper_calls"
	LimitChildren           BudgetLimit = "children"
	LimitConcurrentChildren BudgetLimit = "concurrent_children"
	LimitTotalNodes         BudgetLimit = "total_nodes"
	LimitWallClock          BudgetLimit = "wall_clock"
)

// LimitExceededError is returned when one specific budget lane exceeds its limit.
type LimitExceededError struct {
	Limit     BudgetLimit `json:"limit"`
	Used      int         `json:"used"`
	Attempted int         `json:"attempted"`
	Max       int         `json:"max"`
}

func (e LimitExceededError) Error() string {
	return fmt.Sprintf(
		"rlm runtime: %s budget exceeded (used=%d attempted=%d max=%d)",
		e.Limit,
		e.Used,
		e.Attempted,
		e.Max,
	)
}

// Unwrap allows limit checks via errors.Is(err, ErrBudgetExceeded).
func (e LimitExceededError) Unwrap() error {
	return ErrBudgetExceeded
}

// DeadlineExceededError indicates wall-clock budget exhaustion.
type DeadlineExceededError struct {
	Now      time.Time `json:"now"`
	Deadline time.Time `json:"deadline"`
}

func (e DeadlineExceededError) Error() string {
	return fmt.Sprintf(
		"rlm runtime: wall-clock budget exceeded (now=%s deadline=%s)",
		e.Now.UTC().Format(time.RFC3339Nano),
		e.Deadline.UTC().Format(time.RFC3339Nano),
	)
}

// Unwrap allows wall-clock checks via errors.Is(err, ErrBudgetExceeded).
func (e DeadlineExceededError) Unwrap() error {
	return ErrBudgetExceeded
}

// BudgetConfig configures hard bounds for one runtime execution.
type BudgetConfig struct {
	MaxDepth        int              `json:"max_depth,omitempty"`
	MaxSubcalls     int              `json:"max_subcalls,omitempty"`
	MaxREPLCalls    int              `json:"max_repl_calls,omitempty"`
	MaxIterations   int              `json:"max_iterations,omitempty"`
	MaxParentTokens int              `json:"max_parent_tokens,omitempty"`
	MaxChildTokens  int              `json:"max_child_tokens,omitempty"`
	MaxHelperCalls  int              `json:"max_helper_calls,omitempty"`
	MaxChildren     int              `json:"max_children,omitempty"`
	MaxConcurrent   int              `json:"max_concurrent,omitempty"`
	MaxTotalNodes   int              `json:"max_total_nodes,omitempty"`
	MaxDuration     time.Duration    `json:"max_duration,omitempty"`
	Deadline        time.Time        `json:"deadline,omitempty"`
	StartTime       time.Time        `json:"start_time,omitempty"`
	Now             func() time.Time `json:"-"`
}

// BudgetSnapshot captures current usage for all tracked lanes.
type BudgetSnapshot struct {
	Depth          int       `json:"depth"`
	SubcallsUsed   int       `json:"subcalls_used"`
	SubcallsActive int       `json:"subcalls_active"`
	REPLCallsUsed  int       `json:"repl_calls_used"`
	IterationsUsed int       `json:"iterations_used"`
	ParentTokens   int       `json:"parent_tokens"`
	ChildTokens    int       `json:"child_tokens"`
	HelperCalls    int       `json:"helper_calls"`
	ChildrenUsed   int       `json:"children_used"`
	ChildrenActive int       `json:"children_active"`
	TotalNodesUsed int       `json:"total_nodes_used"`
	Deadline       time.Time `json:"deadline,omitempty"`
}

// Budget tracks runtime usage and enforces limits.
type Budget struct {
	cfg      BudgetConfig
	now      func() time.Time
	deadline time.Time

	mu    sync.Mutex
	state BudgetSnapshot
}

// NewBudget builds a reusable runtime budget guard.
func NewBudget(cfg BudgetConfig) (*Budget, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	start := cfg.StartTime
	if start.IsZero() {
		start = now()
	}

	deadline := cfg.Deadline
	if cfg.MaxDuration > 0 {
		durationDeadline := start.Add(cfg.MaxDuration)
		if deadline.IsZero() || durationDeadline.Before(deadline) {
			deadline = durationDeadline
		}
	}

	return &Budget{
		cfg:      cfg,
		now:      now,
		deadline: deadline,
		state: BudgetSnapshot{
			Deadline: deadline,
		},
	}, nil
}

// Check runs context and wall-clock checks without mutating counters.
func (b *Budget) Check(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkWallClockLocked()
}

// CheckDepth validates an explicit depth against the configured max depth.
func (b *Budget) CheckDepth(depth int) error {
	if depth < 0 {
		return fmt.Errorf("%w: depth must be >= 0", ErrInvalidBudgetValue)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkDepthLocked(depth)
}

// EnterDepth increments the tracked depth after context and budget checks.
func (b *Budget) EnterDepth(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	next := b.state.Depth + 1
	if err := b.checkDepthLocked(next); err != nil {
		return err
	}
	b.state.Depth = next
	return nil
}

// LeaveDepth decrements tracked depth.
func (b *Budget) LeaveDepth() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Depth == 0 {
		return ErrDepthUnderflow
	}
	b.state.Depth--
	return nil
}

// ReserveSubcall allocates one subcall slot and returns a lease for release.
func (b *Budget) ReserveSubcall(ctx context.Context) (*SubcallLease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return nil, err
	}
	if err := b.consumeLocked(LimitSubcalls, &b.state.SubcallsUsed, b.cfg.MaxSubcalls, 1); err != nil {
		return nil, err
	}
	b.state.SubcallsActive++
	return &SubcallLease{budget: b}, nil
}

// ConsumeSubcall accounts one subcall without requiring lease lifecycle tracking.
func (b *Budget) ConsumeSubcall(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitSubcalls, &b.state.SubcallsUsed, b.cfg.MaxSubcalls, 1)
}

// ConsumeChild accounts one cumulative child node creation.
func (b *Budget) ConsumeChild(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitChildren, &b.state.ChildrenUsed, b.cfg.MaxChildren, 1)
}

// ReserveChild allocates one child slot and one active concurrency slot.
// The lease releases the active slot; child usage remains cumulative.
func (b *Budget) ReserveChild(ctx context.Context) (*ChildLease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return nil, err
	}
	if err := b.consumeLocked(LimitConcurrentChildren, &b.state.ChildrenActive, b.cfg.MaxConcurrent, 1); err != nil {
		return nil, err
	}
	if err := b.consumeLocked(LimitChildren, &b.state.ChildrenUsed, b.cfg.MaxChildren, 1); err != nil {
		if releaseErr := b.releaseConcurrentLocked(); releaseErr != nil {
			return nil, errors.Join(err, releaseErr)
		}
		return nil, err
	}
	return &ChildLease{budget: b}, nil
}

// ReserveConcurrent allocates one active child concurrency slot.
func (b *Budget) ReserveConcurrent(ctx context.Context) (*ConcurrentLease, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return nil, err
	}
	if err := b.consumeLocked(LimitConcurrentChildren, &b.state.ChildrenActive, b.cfg.MaxConcurrent, 1); err != nil {
		return nil, err
	}
	return &ConcurrentLease{budget: b}, nil
}

// ReleaseConcurrent releases one active child concurrency slot.
func (b *Budget) ReleaseConcurrent() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.releaseConcurrentLocked()
}

// ConsumeNode accounts one logical node execution in the recursive tree.
func (b *Budget) ConsumeNode(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitTotalNodes, &b.state.TotalNodesUsed, b.cfg.MaxTotalNodes, 1)
}

// ReleaseSubcall releases one active subcall lease.
func (b *Budget) ReleaseSubcall() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.SubcallsActive == 0 {
		return ErrSubcallReleaseUnderflow
	}
	b.state.SubcallsActive--
	return nil
}

// ConsumeREPLCall accounts one REPL call.
func (b *Budget) ConsumeREPLCall(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitREPLCalls, &b.state.REPLCallsUsed, b.cfg.MaxREPLCalls, 1)
}

// ConsumeIteration accounts one outer loop iteration.
func (b *Budget) ConsumeIteration(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitIterations, &b.state.IterationsUsed, b.cfg.MaxIterations, 1)
}

// ConsumeParentTokens accounts parent-model token usage.
func (b *Budget) ConsumeParentTokens(ctx context.Context, count int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitParentTokens, &b.state.ParentTokens, b.cfg.MaxParentTokens, count)
}

// ConsumeChildTokens accounts child/subcall token usage.
func (b *Budget) ConsumeChildTokens(ctx context.Context, count int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitChildTokens, &b.state.ChildTokens, b.cfg.MaxChildTokens, count)
}

// ConsumeHelperCall accounts one generated-helper/tool-synthesis execution.
func (b *Budget) ConsumeHelperCall(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(ctx); err != nil {
		return err
	}
	return b.consumeLocked(LimitHelperCalls, &b.state.HelperCalls, b.cfg.MaxHelperCalls, 1)
}

// Deadline returns the effective wall-clock deadline if configured.
func (b *Budget) Deadline() (time.Time, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deadline.IsZero() {
		return time.Time{}, false
	}
	return b.deadline, true
}

// Snapshot returns a copy of current budget usage.
func (b *Budget) Snapshot() BudgetSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// SubcallLease holds one reserved subcall slot.
type SubcallLease struct {
	mu       sync.Mutex
	budget   *Budget
	released bool
}

// Release returns the reserved subcall slot. Repeated releases are no-ops.
func (l *SubcallLease) Release() error {
	if l == nil || l.budget == nil {
		return nil
	}
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	budget := l.budget
	l.mu.Unlock()
	return budget.ReleaseSubcall()
}

// ChildLease holds one reserved active child slot from ReserveChild.
type ChildLease struct {
	mu       sync.Mutex
	budget   *Budget
	released bool
}

// Release returns the reserved active child slot. Repeated releases are no-ops.
func (l *ChildLease) Release() error {
	if l == nil || l.budget == nil {
		return nil
	}
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	budget := l.budget
	l.mu.Unlock()
	return budget.ReleaseConcurrent()
}

// ConcurrentLease holds one reserved active child concurrency slot.
type ConcurrentLease struct {
	mu       sync.Mutex
	budget   *Budget
	released bool
}

// Release returns the reserved active child concurrency slot.
// Repeated releases are no-ops.
func (l *ConcurrentLease) Release() error {
	if l == nil || l.budget == nil {
		return nil
	}
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	budget := l.budget
	l.mu.Unlock()
	return budget.ReleaseConcurrent()
}

func validateConfig(cfg BudgetConfig) error {
	limits := []struct {
		name  string
		value int
	}{
		{name: "max_depth", value: cfg.MaxDepth},
		{name: "max_subcalls", value: cfg.MaxSubcalls},
		{name: "max_repl_calls", value: cfg.MaxREPLCalls},
		{name: "max_iterations", value: cfg.MaxIterations},
		{name: "max_parent_tokens", value: cfg.MaxParentTokens},
		{name: "max_child_tokens", value: cfg.MaxChildTokens},
		{name: "max_helper_calls", value: cfg.MaxHelperCalls},
		{name: "max_children", value: cfg.MaxChildren},
		{name: "max_concurrent", value: cfg.MaxConcurrent},
		{name: "max_total_nodes", value: cfg.MaxTotalNodes},
	}

	for _, limit := range limits {
		if limit.value < 0 {
			return fmt.Errorf("%w: %s must be >= 0", ErrInvalidBudgetValue, limit.name)
		}
	}
	if cfg.MaxDuration < 0 {
		return fmt.Errorf("%w: max_duration must be >= 0", ErrInvalidBudgetValue)
	}
	return nil
}

func (b *Budget) releaseConcurrentLocked() error {
	if b.state.ChildrenActive == 0 {
		return ErrConcurrentReleaseUnderflow
	}
	b.state.ChildrenActive--
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (b *Budget) checkLocked(ctx context.Context) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	return b.checkWallClockLocked()
}

func (b *Budget) checkWallClockLocked() error {
	if b.deadline.IsZero() {
		return nil
	}
	now := b.now()
	if now.Before(b.deadline) {
		return nil
	}
	return DeadlineExceededError{
		Now:      now,
		Deadline: b.deadline,
	}
}

func (b *Budget) checkDepthLocked(depth int) error {
	if b.cfg.MaxDepth <= 0 {
		return nil
	}
	if depth <= b.cfg.MaxDepth {
		return nil
	}
	return LimitExceededError{
		Limit:     LimitDepth,
		Used:      b.state.Depth,
		Attempted: depth,
		Max:       b.cfg.MaxDepth,
	}
}

func (b *Budget) consumeLocked(limit BudgetLimit, used *int, max int, delta int) error {
	if delta < 0 {
		return fmt.Errorf("%w: consume delta must be >= 0", ErrInvalidBudgetValue)
	}
	next := *used + delta
	if max > 0 && next > max {
		return LimitExceededError{
			Limit:     limit,
			Used:      *used,
			Attempted: next,
			Max:       max,
		}
	}
	*used = next
	return nil
}
