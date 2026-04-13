package execution

import (
	"context"
	"sync"
)

// MockExecutor is a test double for SkillExecutor.
// It records all calls and allows custom behavior to be injected.
type MockExecutor struct {
	// ExecuteFunc is called by Execute if set. If nil, returns default success.
	ExecuteFunc func(ctx context.Context, opts ExecuteOptions) (*Result, error)

	// Calls records all Execute invocations.
	Calls []ExecuteOptions

	// mu protects Calls for concurrent access.
	mu sync.Mutex
}

// NewMockExecutor creates a new mock executor.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		Calls: make([]ExecuteOptions, 0),
	}
}

// Execute implements SkillExecutor, recording the call and optionally
// invoking ExecuteFunc.
func (m *MockExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, opts)
	m.mu.Unlock()

	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, opts)
	}

	// Default success response
	return &Result{
		Stdout:   []byte(`{"ok": true}`),
		Stderr:   []byte{},
		ExitCode: 0,
		Error:    nil,
	}, nil
}

// CallCount returns the number of times Execute was called.
func (m *MockExecutor) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Calls)
}

// LastCall returns the options from the most recent Execute call.
// Returns nil if Execute has not been called.
func (m *MockExecutor) LastCall() *ExecuteOptions {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.Calls) == 0 {
		return nil
	}
	lastCall := m.Calls[len(m.Calls)-1]
	return &lastCall
}

// Reset clears all recorded calls.
func (m *MockExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = make([]ExecuteOptions, 0)
}
