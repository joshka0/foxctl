package main

import (
	"context"
	"strings"
	"testing"
)

func TestParseInput_Default(t *testing.T) {
	r := strings.NewReader(`{}`)
	in, err := parseInput(r)
	if err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}
	if in.Path != "./..." {
		t.Errorf("expected default path './...', got %q", in.Path)
	}
	if in.Mode != "test" {
		t.Errorf("expected default mode 'test', got %q", in.Mode)
	}
	if in.Timeout != "10m" {
		t.Errorf("expected default timeout '10m', got %q", in.Timeout)
	}
}

func TestParseInput_WithValues(t *testing.T) {
	r := strings.NewReader(`{"path":"./pkg/...","mode":"coverage","short":true,"verbose":true,"pattern":"TestFoo","timeout":"5m"}`)
	in, err := parseInput(r)
	if err != nil {
		t.Fatalf("parseInput failed: %v", err)
	}
	if in.Path != "./pkg/..." {
		t.Errorf("expected path './pkg/...', got %q", in.Path)
	}
	if in.Mode != "coverage" {
		t.Errorf("expected mode 'coverage', got %q", in.Mode)
	}
	if !in.Short {
		t.Error("expected short=true")
	}
	if !in.Verbose {
		t.Error("expected verbose=true")
	}
	if in.Pattern != "TestFoo" {
		t.Errorf("expected pattern 'TestFoo', got %q", in.Pattern)
	}
	if in.Timeout != "5m" {
		t.Errorf("expected timeout '5m', got %q", in.Timeout)
	}
}

func TestParseInput_InvalidJSON(t *testing.T) {
	r := strings.NewReader(`{invalid}`)
	_, err := parseInput(r)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		limit int
		want  string
	}{
		{"hello world", 5, "hello... (truncated)"},
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"", 5, ""},
		{"ab", 10, "ab"},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.limit)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.want)
		}
	}
}

func TestGetExitCode(t *testing.T) {
	// nil error returns default 1
	code := getExitCode(nil)
	if code != 1 {
		t.Errorf("expected 1 for nil error, got %d", code)
	}
}

func TestExtractCoverage(t *testing.T) {
	tests := []struct {
		output string
		want   float64
	}{
		{"coverage: 85.3% of statements", 85.3},
		{"coverage: 100% of statements", 100.0},
		{"coverage: 0% of statements", 0.0},
		{"coverage: 12.5% of statements in ./...", 12.5},
		{"no coverage info", 0.0},
		{"", 0.0},
		{"coverage: invalid%", 0.0},
	}

	for _, tt := range tests {
		got := extractCoverage(tt.output)
		if got != tt.want {
			t.Errorf("extractCoverage(%q) = %f, want %f", tt.output, got, tt.want)
		}
	}
}

func TestParseTestOutput_Empty(t *testing.T) {
	results := parseTestOutput("", "test")
	if len(results) != 0 {
		t.Errorf("expected empty results for empty output, got %d", len(results))
	}
}

func TestParseTestOutput_JSON(t *testing.T) {
	output := `{"Action":"pass","Package":"foo/bar","Elapsed":1.5}
{"Action":"fail","Package":"foo/baz","Elapsed":0.5}
{"Action":"skip","Package":"foo/qux","Elapsed":0.1}`

	results := parseTestOutput(output, "test")
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	statusMap := make(map[string]string)
	for _, r := range results {
		statusMap[r.Package] = r.Status
	}

	if statusMap["foo/bar"] != "pass" {
		t.Errorf("expected foo/bar to pass, got %s", statusMap["foo/bar"])
	}
	if statusMap["foo/baz"] != "fail" {
		t.Errorf("expected foo/baz to fail, got %s", statusMap["foo/baz"])
	}
	if statusMap["foo/qux"] != "skip" {
		t.Errorf("expected foo/qux to skip, got %s", statusMap["foo/qux"])
	}
}

func TestParseTestOutput_InvalidJSON(t *testing.T) {
	output := `not json
also not json
{"Action":"pass","Package":"valid/pkg","Elapsed":1.0}`

	results := parseTestOutput(output, "test")
	if len(results) != 1 {
		t.Errorf("expected 1 valid result, got %d", len(results))
	}
	if results[0].Package != "valid/pkg" {
		t.Errorf("expected valid/pkg, got %s", results[0].Package)
	}
}

func TestSummarizeResults(t *testing.T) {
	results := []testResult{
		{Package: "pkg1", Status: "pass", Duration: 1.0, Coverage: 80.0},
		{Package: "pkg2", Status: "pass", Duration: 2.0, Coverage: 60.0},
		{Package: "pkg3", Status: "fail", Duration: 0.5},
		{Package: "pkg4", Status: "skip", Duration: 0.1},
	}

	summary := summarizeResults(results)

	if summary["total_packages"] != 4 {
		t.Errorf("expected total_packages=4, got %v", summary["total_packages"])
	}
	if summary["passed"] != 2 {
		t.Errorf("expected passed=2, got %v", summary["passed"])
	}
	if summary["failed"] != 1 {
		t.Errorf("expected failed=1, got %v", summary["failed"])
	}
	if summary["skipped"] != 1 {
		t.Errorf("expected skipped=1, got %v", summary["skipped"])
	}
	if summary["success"] != false {
		t.Errorf("expected success=false, got %v", summary["success"])
	}
	if summary["average_coverage"] != 70.0 {
		t.Errorf("expected average_coverage=70.0, got %v", summary["average_coverage"])
	}
}

func TestSummarizeResults_AllPass(t *testing.T) {
	results := []testResult{
		{Package: "pkg1", Status: "pass", Duration: 1.0},
		{Package: "pkg2", Status: "pass", Duration: 2.0},
	}

	summary := summarizeResults(results)
	if summary["success"] != true {
		t.Errorf("expected success=true when no failures, got %v", summary["success"])
	}
}

func TestSummarizeResults_Empty(t *testing.T) {
	results := []testResult{}
	summary := summarizeResults(results)

	if summary["total_packages"] != 0 {
		t.Errorf("expected total_packages=0, got %v", summary["total_packages"])
	}
	if summary["success"] != true {
		t.Errorf("expected success=true for empty results, got %v", summary["success"])
	}
}

func TestBuildTestCommand_Modes(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		mode     string
		wantArgs []string
		wantErr  bool
	}{
		{"test", []string{"test"}, false},
		{"race", []string{"test", "-race"}, false},
		{"coverage", []string{"test", "-cover", "-covermode=atomic"}, false},
		{"bench", []string{"test", "-bench=."}, false},
		{"invalid", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			cmd, err := buildTestCommand(ctx, tt.mode, "./...", input{Timeout: "10m"})
			if tt.wantErr {
				if err == nil {
					t.Error("expected error for invalid mode")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildTestCommand failed: %v", err)
			}

			args := cmd.Args
			for _, want := range tt.wantArgs {
				found := false
				for _, arg := range args {
					if arg == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected arg %q in command, got %v", want, args)
				}
			}
		})
	}
}

func TestBuildTestCommand_Flags(t *testing.T) {
	ctx := context.Background()
	in := input{
		Short:   true,
		Verbose: true,
		Pattern: "TestFoo",
		Timeout: "5m",
	}

	cmd, err := buildTestCommand(ctx, "test", "./...", in)
	if err != nil {
		t.Fatalf("buildTestCommand failed: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "-short") {
		t.Error("expected -short flag")
	}
	if !strings.Contains(args, "-v") {
		t.Error("expected -v flag")
	}
	if !strings.Contains(args, "-run=TestFoo") {
		t.Error("expected -run=TestFoo flag")
	}
	if !strings.Contains(args, "-timeout=5m") {
		t.Error("expected -timeout=5m flag")
	}
}

func TestBuildTestCommand_BenchPattern(t *testing.T) {
	ctx := context.Background()
	in := input{
		Pattern: "BenchmarkFoo",
		Timeout: "10m",
	}

	cmd, err := buildTestCommand(ctx, "bench", "./...", in)
	if err != nil {
		t.Fatalf("buildTestCommand failed: %v", err)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "-bench=BenchmarkFoo") {
		t.Error("expected -bench=BenchmarkFoo flag")
	}
	// Should NOT have -run=BenchmarkFoo for bench mode
	if strings.Contains(args, "-run=BenchmarkFoo") {
		t.Error("should not have -run flag for bench mode pattern")
	}
}
