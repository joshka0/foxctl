package engine

import (
	"context"
	"encoding/json"
	"testing"
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
