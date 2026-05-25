package main

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/quick"
)

func stubRequireTool(t *testing.T) {
	t.Helper()

	original := requireTool
	requireTool = func(name, _ string) (string, error) {
		return "/usr/bin/" + name, nil
	}
	t.Cleanup(func() {
		requireTool = original
	})
}

func TestBuildGoTestCommandContract(t *testing.T) {
	stubRequireTool(t)

	tests := []struct {
		name    string
		mode    string
		path    string
		in      input
		wantRun string
		wantArg []string
		wantEnv []string
	}{
		{
			name:    "test mode keeps flags as arguments",
			mode:    "test",
			path:    "./pkg",
			in:      input{Short: true, Verbose: true, Pattern: "TestThing", Timeout: "30s"},
			wantRun: "go",
			wantArg: []string{"test", "-short", "-v", "-run=TestThing", "-timeout=30s", "-json", "./pkg"},
			wantEnv: []string{"CGO_ENABLED=0"},
		},
		{
			name:    "bench pattern uses benchmark selector only",
			mode:    "bench",
			path:    "./pkg",
			in:      input{Pattern: "BenchmarkThing", Timeout: "1m"},
			wantRun: "go",
			wantArg: []string{"test", "-bench=BenchmarkThing", "-run=^$", "-timeout=1m", "-json", "./pkg"},
			wantEnv: []string{"CGO_ENABLED=0"},
		},
		{
			name:    "race enables cgo",
			mode:    "race",
			path:    "./...",
			in:      input{Timeout: "10m"},
			wantRun: "go",
			wantArg: []string{"test", "-race", "-timeout=10m", "-json", "./..."},
			wantEnv: []string{"CGO_ENABLED=1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, args, env, runDir, err := buildTestCommand(tt.mode, tt.path, tt.in)
			if err != nil {
				t.Fatalf("buildTestCommand returned error: %v", err)
			}
			if runner != tt.wantRun {
				t.Fatalf("runner=%q want %q", runner, tt.wantRun)
			}
			if !reflect.DeepEqual(args, tt.wantArg) {
				t.Fatalf("args=%v want %v", args, tt.wantArg)
			}
			if !reflect.DeepEqual(env, tt.wantEnv) {
				t.Fatalf("env=%v want %v", env, tt.wantEnv)
			}
			if runDir != "" {
				t.Fatalf("runDir=%q want empty for Go mode", runDir)
			}
		})
	}
}

func TestBuildTestCommandRejectsInvalidMode(t *testing.T) {
	stubRequireTool(t)

	_, _, _, _, err := buildTestCommand("shell", "./...", input{Timeout: "10m"})
	if err == nil {
		t.Fatal("buildTestCommand accepted invalid mode")
	}
}

func TestBuildPytestCommandTreatsGoDefaultAsCurrentDirectory(t *testing.T) {
	stubRequireTool(t)

	runner, args, _, runDir, err := buildTestCommand("pytest", "./...", input{
		Verbose: true,
		Pattern: "unit",
	})
	if err != nil {
		t.Fatalf("buildTestCommand returned error: %v", err)
	}
	if runner != "pytest" {
		t.Fatalf("runner=%q want pytest", runner)
	}
	if !reflect.DeepEqual(args, []string{"-v", "-k", "unit"}) {
		t.Fatalf("args=%v want pytest flags without Go ./... path", args)
	}
	if runDir != "" {
		t.Fatalf("runDir=%q want empty for pytest default path", runDir)
	}
}

func TestParseGoTestOutputReturnsPackagesSortedByName(t *testing.T) {
	stdout := strings.Join([]string{
		`{"Action":"pass","Package":"example.com/fox/zeta","Elapsed":0.12}`,
		`{"Action":"pass","Package":"example.com/fox/alpha","Elapsed":0.34}`,
		`{"Action":"pass","Package":"example.com/fox/mid","Elapsed":0.56}`,
		`{"Action":"pass","Package":"example.com/fox/beta","Elapsed":0.78}`,
	}, "\n")

	for i := 0; i < 25; i++ {
		got := parseGoTestOutput(stdout, "test")
		packages := make([]string, 0, len(got))
		for _, result := range got {
			packages = append(packages, result.Package)
		}
		if !sort.StringsAreSorted(packages) {
			t.Fatalf("packages=%v want stable sorted output order", packages)
		}
	}
}

func TestParseGoTestOutputCapturesCoverageFromOutputEvents(t *testing.T) {
	stdout := strings.Join([]string{
		`{"Action":"run","Package":"example.com/fox/pkg"}`,
		`{"Action":"output","Package":"example.com/fox/pkg","Output":"coverage: 83.3% of statements\n"}`,
		`{"Action":"pass","Package":"example.com/fox/pkg","Elapsed":0.12}`,
	}, "\n")

	got := parseGoTestOutput(stdout, "coverage")
	if len(got) != 1 {
		t.Fatalf("result count=%d want 1: %v", len(got), got)
	}
	if got[0].Coverage != 83.3 {
		t.Fatalf("coverage=%v want 83.3", got[0].Coverage)
	}
}

func TestParseGoTestOutputKeepsCoverageWhenStatusArrivesFirst(t *testing.T) {
	stdout := strings.Join([]string{
		`{"Action":"pass","Package":"example.com/fox/pkg","Elapsed":0.12}`,
		`{"Action":"output","Package":"example.com/fox/pkg","Output":"coverage: 77.7% of statements\n"}`,
	}, "\n")

	got := parseGoTestOutput(stdout, "coverage")
	if len(got) != 1 {
		t.Fatalf("result count=%d want 1: %v", len(got), got)
	}
	if got[0].Coverage != 77.7 {
		t.Fatalf("coverage=%v want 77.7", got[0].Coverage)
	}
}

func TestParseGoTestOutputCapturesZeroCoverage(t *testing.T) {
	stdout := strings.Join([]string{
		`{"Action":"output","Package":"example.com/fox/pkg","Output":"coverage: 0.0% of statements\n"}`,
		`{"Action":"pass","Package":"example.com/fox/pkg","Elapsed":0.12}`,
	}, "\n")

	got := parseGoTestOutput(stdout, "coverage")
	if len(got) != 1 {
		t.Fatalf("result count=%d want 1: %v", len(got), got)
	}
	if !got[0].HasCoverage || got[0].Coverage != 0 {
		t.Fatalf("coverage=(%v, present=%v), want zero coverage present", got[0].Coverage, got[0].HasCoverage)
	}

	summary := summarizeResults("coverage", "", "", 0, got)
	average, ok := summary["average_coverage"].(float64)
	if !ok || average != 0 {
		t.Fatalf("average_coverage=%v (present=%v), want 0.0 present", summary["average_coverage"], ok)
	}
}

func TestParseGoTestOutputPropertyCoverageSurvivesEventOrdering(t *testing.T) {
	property := func(rawCoverage uint16, statusFirst bool) bool {
		coverage := float64(rawCoverage%1001) / 10
		coverageLine := fmt.Sprintf(
			`{"Action":"output","Package":"example.com/fox/pkg","Output":"coverage: %.1f%% of statements\n"}`,
			coverage,
		)
		statusLine := `{"Action":"pass","Package":"example.com/fox/pkg","Elapsed":0.12}`
		lines := []string{coverageLine, statusLine}
		if statusFirst {
			lines = []string{statusLine, coverageLine}
		}

		got := parseGoTestOutput(strings.Join(lines, "\n"), "coverage")
		if len(got) != 1 || got[0].Status != "pass" || !got[0].HasCoverage {
			t.Logf("unexpected coverage result: %+v", got)
			return false
		}
		if math.Abs(got[0].Coverage-coverage) > 0.000001 {
			t.Logf("coverage=%v want %v", got[0].Coverage, coverage)
			return false
		}

		summary := summarizeResults("coverage", "", "", 0, got)
		average, ok := summary["average_coverage"].(float64)
		if !ok {
			t.Logf("summary missing average coverage for result %+v: %v", got[0], summary)
			return false
		}
		return math.Abs(average-coverage) <= 0.000001
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 250}); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizePytest(t *testing.T) {
	got := summarizePytest("=== 2 passed, 1 skipped in 0.12s ===", "", 0)
	if got["passed"] != 2 || got["skipped"] != 1 || got["failed"] != 0 {
		t.Fatalf("unexpected pytest summary: %v", got)
	}
	if got["runner_status"] != "pass" {
		t.Fatalf("runner_status=%v want pass", got["runner_status"])
	}
}

func TestSummarizeNPM(t *testing.T) {
	stdout := strings.Join([]string{
		"Test Suites: 1 failed, 2 passed, 3 total",
		"Tests:       4 failed, 10 passed, 14 total",
	}, "\n")
	got := summarizeNPM(stdout, "", 1)
	if got["failed"] != 4 || got["passed"] != 10 {
		t.Fatalf("unexpected npm test counts: %v", got)
	}
	if got["failed_suites"] != 1 || got["total_suites"] != 3 {
		t.Fatalf("unexpected npm suite counts: %v", got)
	}
	if got["runner_status"] != "fail" {
		t.Fatalf("runner_status=%v want fail", got["runner_status"])
	}
}

func TestParseNamedCounts(t *testing.T) {
	got := parseNamedCounts("2 passed, 1 skipped, 3 total", []string{"passed", "skipped", "total"})
	if got["passed"] != 2 || got["skipped"] != 1 || got["total"] != 3 {
		t.Fatalf("unexpected counts: %v", got)
	}
}

func TestSummarizeCargo(t *testing.T) {
	got := summarizeCargo("test result: ok. 3 passed; 0 failed; 1 ignored; 0 measured; 0 filtered out", "", 0)
	if got["passed"] != 3 || got["failed"] != 0 || got["skipped"] != 1 {
		t.Fatalf("unexpected cargo summary: %v", got)
	}
	if got["runner_status"] != "pass" {
		t.Fatalf("runner_status=%v want pass", got["runner_status"])
	}
}

func TestSummarizeGoResultsSuccessRequiresCleanExit(t *testing.T) {
	got := summarizeResults("test", "", "setup failed", 2, nil)
	if got["success"] != false {
		t.Fatalf("success=%v want false for nonzero exit without package results", got["success"])
	}
}

func TestSummarizeGoResultsGeneratedNonzeroExitNeverSucceeds(t *testing.T) {
	err := quick.Check(func(exit uint8) bool {
		if exit == 0 {
			return true
		}
		got := summarizeResults("coverage", "", "load failed", int(exit), nil)
		return got["success"] == false
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}
