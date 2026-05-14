package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
)

type benchmarkToolExecutor struct {
	response string
}

func (e benchmarkToolExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return e.response, nil
}

func (e benchmarkToolExecutor) List() []ToolDef {
	return []ToolDef{{
		Name:        "benchmark.echo",
		Description: "Benchmark echo tool",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}}
}

type benchmarkHookDispatcher struct{}

func (benchmarkHookDispatcher) Dispatch(_ context.Context, input hooks.Input) (hooks.Result, error) {
	if input.Event == hooks.EventPreToolUse {
		return hooks.Result{Output: hooks.NewApprove("bench", nil)}, nil
	}
	return hooks.Result{Output: hooks.NewNone()}, nil
}

func (d benchmarkHookDispatcher) DispatchAsync(ctx context.Context, input hooks.Input) <-chan hooks.Result {
	ch := make(chan hooks.Result, 1)
	result, _ := d.Dispatch(ctx, input)
	ch <- result
	close(ch)
	return ch
}

func BenchmarkToolRunnerExecuteNoHooks(b *testing.B) {
	runner := NewToolRunner(
		benchmarkToolExecutor{response: `{"ok":true,"value":"benchmark"}`},
		nil,
		ToolRunnerConfig{
			Workspace:   b.TempDir(),
			WorkspaceID: "bench-workspace",
			SessionID:   "bench-session",
			ActorID:     "bench-actor",
		},
	)
	call := ToolCall{
		ID:        "call-benchmark",
		Name:      "benchmark.echo",
		Arguments: json.RawMessage(`{"input":"hello"}`),
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := runner.Execute(ctx, call)
		if err != nil {
			b.Fatalf("Execute() error = %v", err)
		}
		if result.IsError {
			b.Fatalf("Execute() returned error result: %s", result.Content)
		}
	}
}

func BenchmarkToolRunnerExecuteWithDispatcher(b *testing.B) {
	runner := NewToolRunner(
		benchmarkToolExecutor{response: `{"ok":true,"value":"benchmark"}`},
		benchmarkHookDispatcher{},
		ToolRunnerConfig{
			Workspace:   b.TempDir(),
			WorkspaceID: "bench-workspace",
			SessionID:   "bench-session",
			ActorID:     "bench-actor",
		},
	)
	call := ToolCall{
		ID:        "call-benchmark",
		Name:      "benchmark.echo",
		Arguments: json.RawMessage(`{"input":"hello"}`),
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := runner.Execute(ctx, call)
		if err != nil {
			b.Fatalf("Execute() error = %v", err)
		}
		if result.IsError {
			b.Fatalf("Execute() returned error result: %s", result.Content)
		}
	}
}

func BenchmarkToolRunnerExecuteWithNormalizer(b *testing.B) {
	runner := NewToolRunner(
		benchmarkToolExecutor{response: `{"ok":true}`},
		nil,
		ToolRunnerConfig{
			WorkspaceID: "bench-workspace",
			SessionID:   "bench-session",
			ActorID:     "bench-actor",
			NormalizeToolName: func(rawName string) (string, bool) {
				if rawName == "echo" {
					return "benchmark.echo", true
				}
				return "", false
			},
		},
	)
	call := ToolCall{
		ID:        "call-normalized",
		Name:      "echo",
		Arguments: json.RawMessage(`{"input":"hello"}`),
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		result, err := runner.Execute(ctx, call)
		if err != nil {
			b.Fatalf("Execute() error = %v", err)
		}
		if result.IsError {
			b.Fatalf("Execute() returned error result: %s", result.Content)
		}
	}
}
