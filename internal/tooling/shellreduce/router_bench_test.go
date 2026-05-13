package shellreduce

import "testing"

func BenchmarkShellReduceRouteTypicalCommands(b *testing.B) {
	commands := [][]string{
		{"rg", "-n", "Benchmark", "internal"},
		{"git", "status", "--short"},
		{"git", "diff", "--stat"},
		{"find", "internal", "-maxdepth", "2", "-type", "f"},
		{"go", "test", "./internal/tooling/benchmarks"},
		{"sed", "-n", "10,40p", "cmd/foxctl/cmd/root.go"},
	}

	b.ReportAllocs()
	for b.Loop() {
		for _, argv := range commands {
			if _, err := RouteArgv(argv); err != nil {
				b.Fatalf("RouteArgv(%v) error = %v", argv, err)
			}
		}
	}
}

func BenchmarkShellReduceSummarizeGitAndGrep(b *testing.B) {
	cases := []struct {
		route Route
		data  map[string]any
	}{
		{
			route: Route{Intent: "git_status_short"},
			data: map[string]any{
				"files": []any{
					map[string]any{"path": "internal/runtime/engine/tool_runner.go", "status": "M"},
					map[string]any{"path": "internal/rlm/run_spec.go", "status": "M"},
				},
			},
		},
		{
			route: Route{Intent: "grep"},
			data: map[string]any{
				"matches": []any{
					map[string]any{"path": "internal/runtime/engine/tool_runner.go", "line": float64(75), "text": "func (r *ToolRunner) Execute"},
					map[string]any{"path": "internal/rlm/run_spec.go", "line": float64(37), "text": "func ResolveRunSpec"},
				},
			},
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		for _, tc := range cases {
			if got := Summarize(tc.route, tc.data); got == "" {
				b.Fatal("Summarize() returned empty summary")
			}
		}
	}
}
