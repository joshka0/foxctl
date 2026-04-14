package fakes

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
)

// ToolExecution captures one Execute invocation.
type ToolExecution struct {
	Name string
	Args json.RawMessage
}

// FakeToolExecutor is a deterministic tool executor for runner tests.
type FakeToolExecutor struct {
	mu            sync.Mutex
	resultsByName map[string]runner.ToolResult
	errByName     map[string]error
	calls         []ToolExecution
	callCountBy   map[string]int
	defaultResult runner.ToolResult
}

func NewFakeToolExecutor() *FakeToolExecutor {
	return &FakeToolExecutor{
		resultsByName: map[string]runner.ToolResult{},
		errByName:     map[string]error{},
		callCountBy:   map[string]int{},
		defaultResult: runner.ToolResult{Status: "ok"},
	}
}

// SetResult configures deterministic result by tool name.
func (f *FakeToolExecutor) SetResult(name string, result runner.ToolResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resultsByName[name] = result
}

// SetError configures deterministic error by tool name.
func (f *FakeToolExecutor) SetError(name string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errByName[name] = err
}

// Calls returns captured tool invocations.
func (f *FakeToolExecutor) Calls() []ToolExecution {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]ToolExecution, len(f.calls))
	for i := range f.calls {
		out[i] = ToolExecution{
			Name: f.calls[i].Name,
			Args: append(json.RawMessage(nil), f.calls[i].Args...),
		}
	}
	return out
}

// CallCount returns total invocations of a tool by name.
func (f *FakeToolExecutor) CallCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCountBy[name]
}

// Execute records invocation and returns configured result/error.
func (f *FakeToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (runner.ToolResult, error) {
	select {
	case <-ctx.Done():
		return runner.ToolResult{}, ctx.Err()
	default:
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, ToolExecution{
		Name: name,
		Args: append(json.RawMessage(nil), args...),
	})
	f.callCountBy[name]++

	if err, ok := f.errByName[name]; ok && err != nil {
		return runner.ToolResult{}, err
	}
	if res, ok := f.resultsByName[name]; ok {
		return res, nil
	}
	return f.defaultResult, nil
}

var _ runner.ToolExecutor = (*FakeToolExecutor)(nil)
