package shellreduce

import (
	"errors"
	"strings"
	"testing"
)

func TestSplitCommandRespectsQuotes(t *testing.T) {
	got, err := SplitCommand(`grep -rn "pub fn" src/`)
	if err != nil {
		t.Fatalf("SplitCommand: %v", err)
	}
	want := []string{"grep", "-rn", "pub fn", "src/"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestRouteArgvLS(t *testing.T) {
	route, err := RouteArgv([]string{"ls", "-la", "src/"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Skill != "fs/ls" {
		t.Fatalf("skill=%q want fs/ls", route.Skill)
	}
	if route.Intent != "ls" {
		t.Fatalf("intent=%q want ls", route.Intent)
	}
	if got := route.Input["path"]; got != "src/" {
		t.Fatalf("path=%v want src/", got)
	}
	if got := route.Input["show_hidden"]; got != true {
		t.Fatalf("show_hidden=%v want true", got)
	}
}

func TestRouteArgvGrep(t *testing.T) {
	route, err := RouteArgv([]string{"grep", "-rn", "pub fn", "src/"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Skill != "text/grep" {
		t.Fatalf("skill=%q want text/grep", route.Skill)
	}
	if got := route.Input["pattern"]; got != "pub fn" {
		t.Fatalf("pattern=%v want pub fn", got)
	}
	if got := route.Input["path"]; got != "src/" {
		t.Fatalf("path=%v want src/", got)
	}
}

func TestRouteArgvSedLineRange(t *testing.T) {
	route, err := RouteArgv([]string{"sed", "-n", "10,20p", "internal/tooling/shellreduce/router.go"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "line_range" || route.Skill != "code/context_grep" {
		t.Fatalf("route=%+v", route)
	}
	if route.Input["file_path"] != "internal/tooling/shellreduce/router.go" {
		t.Fatalf("file_path=%v", route.Input["file_path"])
	}
}

func TestRouteArgvHead(t *testing.T) {
	route, err := RouteArgv([]string{"head", "-n", "5", "go.mod"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "file_head" || route.Native != "file_head" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvTail(t *testing.T) {
	route, err := RouteArgv([]string{"tail", "-20", "go.mod"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "file_tail" || route.Native != "file_tail" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvWC(t *testing.T) {
	route, err := RouteArgv([]string{"wc", "-l", "go.mod"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "file_wc" || route.Native != "file_wc" {
		t.Fatalf("route=%+v", route)
	}
	if route.Input["mode"] != "lines" {
		t.Fatalf("mode=%v want lines", route.Input["mode"])
	}
}

func TestRouteArgvNLPipedToSedLineRange(t *testing.T) {
	route, err := RouteArgv([]string{"nl", "-ba", "internal/tooling/shellreduce/router.go", "|", "sed", "-n", "10,20p"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "line_range" || route.Skill != "code/context_grep" {
		t.Fatalf("route=%+v", route)
	}
	if route.Input["file_path"] != "internal/tooling/shellreduce/router.go" {
		t.Fatalf("file_path=%v", route.Input["file_path"])
	}
}

func TestRouteArgvRGPipedToHead(t *testing.T) {
	route, err := RouteArgv([]string{"rg", "-n", "func", "internal/tooling/shellreduce", "|", "head", "-n", "5"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "pipe_slice" || route.Native != "pipe_line_slice" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvKubectlDescribePipedToSed(t *testing.T) {
	route, err := RouteArgv([]string{"kubectl", "describe", "pods", "-A", "|", "sed", "-n", "1,40p"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "pipe_slice" || route.Native != "pipe_line_slice" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvFind(t *testing.T) {
	route, err := RouteArgv([]string{"find", "internal", "-name", "*.go", "-type", "f", "-maxdepth", "2"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Skill != "fs/find" {
		t.Fatalf("skill=%q want fs/find", route.Skill)
	}
	if route.Intent != "find" {
		t.Fatalf("intent=%q want find", route.Intent)
	}
	if got := route.Input["path"]; got != "internal" {
		t.Fatalf("path=%v want internal", got)
	}
	if got := route.Input["pattern"]; got != "*.go" {
		t.Fatalf("pattern=%v want *.go", got)
	}
	if got := route.Input["type"]; got != "file" {
		t.Fatalf("type=%v want file", got)
	}
}

func TestRouteArgvGitLogIgnoresStat(t *testing.T) {
	route, err := RouteArgv([]string{"git", "log", "--stat", "-10"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Skill != "git/status" {
		t.Fatalf("skill=%q want git/status", route.Skill)
	}
	if got := route.Input["operation"]; got != "log" {
		t.Fatalf("operation=%v want log", got)
	}
	if got := route.Input["limit"]; got != 10 {
		t.Fatalf("limit=%v want 10", got)
	}
	if len(route.Notes) == 0 || !strings.Contains(route.Notes[0], "--stat") {
		t.Fatalf("notes=%v want stat note", route.Notes)
	}
}

func TestRouteArgvGitStatusShort(t *testing.T) {
	route, err := RouteArgv([]string{"git", "status", "--short"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "git_status_short" {
		t.Fatalf("intent=%q want git_status_short", route.Intent)
	}
	if route.Skill != "git/status" {
		t.Fatalf("skill=%q want git/status", route.Skill)
	}
}

func TestRouteArgvGitDiffNameOnly(t *testing.T) {
	route, err := RouteArgv([]string{"git", "diff", "--name-only"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "git_diff_names" {
		t.Fatalf("intent=%q want git_diff_names", route.Intent)
	}
	if got := route.Input["name_only"]; got != true {
		t.Fatalf("name_only=%v want true", got)
	}
}

func TestRouteArgvPytest(t *testing.T) {
	route, err := RouteArgv([]string{"pytest", "-k", "unit", "tests"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "pytest" || route.Skill != "test/run" {
		t.Fatalf("route=%+v", route)
	}
	if got := route.Input["mode"]; got != "pytest" {
		t.Fatalf("mode=%v want pytest", got)
	}
	if got := route.Input["pattern"]; got != "unit" {
		t.Fatalf("pattern=%v want unit", got)
	}
}

func TestRouteArgvNPMTest(t *testing.T) {
	route, err := RouteArgv([]string{"npm", "test", "--prefix", "packages/gui-agent"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "npm_test" || route.Skill != "test/run" {
		t.Fatalf("route=%+v", route)
	}
	if got := route.Input["mode"]; got != "npm" {
		t.Fatalf("mode=%v want npm", got)
	}
	if got := route.Input["path"]; got != "packages/gui-agent" {
		t.Fatalf("path=%v want packages/gui-agent", got)
	}
}

func TestRouteArgvPNPMTest(t *testing.T) {
	route, err := RouteArgv([]string{"pnpm", "test", "--dir", "packages/gui-agent"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "pnpm_test" || route.Skill != "test/run" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvYarnTest(t *testing.T) {
	route, err := RouteArgv([]string{"yarn", "test", "--cwd", "packages/gui-agent"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "yarn_test" || route.Skill != "test/run" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvCargoTest(t *testing.T) {
	route, err := RouteArgv([]string{"cargo", "test", "parser"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "cargo_test" || route.Skill != "test/run" {
		t.Fatalf("route=%+v", route)
	}
	if got := route.Input["pattern"]; got != "parser" {
		t.Fatalf("pattern=%v want parser", got)
	}
}

func TestRouteArgvMixCompile(t *testing.T) {
	route, err := RouteArgv([]string{"mix", "compile"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "mix_compile" || route.Native != "mix_compile" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvEnvWrappedMixTest(t *testing.T) {
	argv, err := SplitCommand("cd apps/praze-api && MIX_ENV=test mix test test/example_test.exs")
	if err != nil {
		t.Fatalf("SplitCommand: %v", err)
	}
	route, err := RouteArgv(argv)
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "mix_test" || route.Native != "mix_test" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvKubectlGet(t *testing.T) {
	route, err := RouteArgv([]string{"kubectl", "get", "pods", "-A"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "kubectl_get" || route.Native != "kubectl_get" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvWrappedKubectlGet(t *testing.T) {
	argv, err := SplitCommand("cd ~/repo && kubectl get pods -A")
	if err != nil {
		t.Fatalf("SplitCommand: %v", err)
	}
	route, err := RouteArgv(argv)
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "kubectl_get" || route.Native != "kubectl_get" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvRuffCheck(t *testing.T) {
	route, err := RouteArgv([]string{"ruff", "check", "."})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "ruff_check" || route.Native != "ruff_check" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvDockerPS(t *testing.T) {
	route, err := RouteArgv([]string{"docker", "ps"})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Intent != "docker_ps" || route.Native != "docker_ps" {
		t.Fatalf("route=%+v", route)
	}
}

func TestRouteArgvGoTestRace(t *testing.T) {
	route, err := RouteArgv([]string{"go", "test", "-race", "./..."})
	if err != nil {
		t.Fatalf("RouteArgv: %v", err)
	}
	if route.Skill != "test/run" {
		t.Fatalf("skill=%q want test/run", route.Skill)
	}
	if got := route.Input["mode"]; got != "race" {
		t.Fatalf("mode=%v want race", got)
	}
	if got := route.Input["path"]; got != "./..." {
		t.Fatalf("path=%v want ./...", got)
	}
}

func TestMeasureSummaryLine(t *testing.T) {
	got := MeasureSummaryLine(map[string]any{
		"raw": map[string]any{
			"combined_tokens":          200.0,
			"combined_bytes":           800.0,
			"duration_ms":              40.0,
			"estimated_input_cost_usd": 0.0002,
		},
		"reduced": map[string]any{
			"tokens":                   20.0,
			"bytes":                    120.0,
			"duration_ms":              10.0,
			"estimated_input_cost_usd": 0.00002,
		},
		"savings": map[string]any{
			"tokens_saved_percent":               90.0,
			"bytes_saved_percent":                85.0,
			"duration_saved_percent":             75.0,
			"estimated_input_cost_saved_percent": 90.0,
		},
	})
	for _, want := range []string{"raw 200 -> reduced 20", "90% saved", "raw 800 -> reduced 120", "ms: raw 40 -> reduced 10", "input cost: $0.000200 -> $0.000020"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q\n%s", want, got)
		}
	}
}

func TestSummarizeGitDiffNames(t *testing.T) {
	got := Summarize(Route{Intent: "git_diff_names"}, map[string]any{
		"files": []any{"a.go", "b.go"},
	})
	if got != "a.go\nb.go" {
		t.Fatalf("got %q want file-only listing", got)
	}
}

func TestSummarizeLineRange(t *testing.T) {
	got := Summarize(Route{Intent: "line_range"}, map[string]any{
		"rendered_context": "```go\n10 | func Foo() {}\n```",
	})
	if !strings.Contains(got, "func Foo") {
		t.Fatalf("summary=%q missing rendered context", got)
	}
}

func TestSummarizeGitStatusShortCompact(t *testing.T) {
	got := Summarize(Route{Intent: "git_status_short"}, map[string]any{
		"files": []any{
			map[string]any{"staging_area": " ", "working_tree": "M", "path": "a.go"},
			map[string]any{"staging_area": " ", "working_tree": "M", "path": "b.go"},
			map[string]any{"staging_area": "?", "working_tree": "?", "path": "c.go"},
			map[string]any{"staging_area": "?", "working_tree": "?", "path": "d.go"},
			map[string]any{"staging_area": " ", "working_tree": "M", "path": "e.go"},
			map[string]any{"staging_area": " ", "working_tree": "M", "path": "f.go"},
			map[string]any{"staging_area": " ", "working_tree": "M", "path": "g.go"},
		},
	})
	for _, want := range []string{"7 paths", "M:5", "??:2", "M a.go"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q\n%s", want, got)
		}
	}
}

func TestRouteArgvUnsupported(t *testing.T) {
	_, err := RouteArgv([]string{"helm", "list"})
	if err == nil {
		t.Fatal("expected unsupported error")
	}
	var unsupported ErrUnsupported
	if !strings.Contains(err.Error(), "supported families") {
		t.Fatalf("err=%v want supported families hint", err)
	}
	if ok := errors.As(err, &unsupported); !ok {
		t.Fatalf("err=%T want ErrUnsupported", err)
	}
}

func TestSummarizeGrep(t *testing.T) {
	summary := Summarize(Route{Intent: "grep"}, map[string]any{
		"match_count":   112,
		"files_touched": 47,
		"top_files": []any{
			[]any{"src/cargo_cmd.rs", float64(3)},
			[]any{"src/config.rs", float64(4)},
		},
		"preview": []any{
			map[string]any{"file": "src/cargo_cmd.rs", "line": float64(17), "snippet": "pub fn run(cmd: CargoCommand, ...) -> Result<()>"},
			map[string]any{"file": "src/cargo_cmd.rs", "line": float64(551), "snippet": "pub fn run_passthrough(args: ...) -> Result<()>"},
			map[string]any{"file": "src/config.rs", "line": float64(73), "snippet": "pub fn load() -> Result<Self>"},
		},
	})
	for _, want := range []string{
		"112 matches in 47 files:",
		"src/cargo_cmd.rs (3):",
		"17: pub fn run",
		"src/config.rs (4):",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q\n%s", want, summary)
		}
	}
}
