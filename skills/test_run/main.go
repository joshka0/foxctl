// Package main implements the test/run skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

type input struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"`
	Short   bool   `json:"short"`
	Verbose bool   `json:"verbose"`
	Pattern string `json:"pattern"`
	Timeout string `json:"timeout"`
}

type testResult struct {
	Package  string  `json:"package"`
	Status   string  `json:"status"` // pass, fail, skip
	Duration float64 `json:"duration_seconds"`
	Coverage float64 `json:"coverage_percent,omitempty"`
}

const command = "test/run"

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Set defaults
	if in.Path == "" {
		in.Path = "./..."
	}
	if in.Mode == "" {
		in.Mode = "test"
	}
	if in.Timeout == "" {
		in.Timeout = "10m"
	}
	// Validate path
	testPath, err := resolveTestPath(rc, in.Path)
	if err != nil {
		return err
	}

	// Build command based on mode
	runner, args, env, runDir, err := buildTestCommand(in.Mode, testPath, in)
	if err != nil {
		return err
	}

	// Execute tests
	result := executil.RunWithEnv(ctx, runDir, runner, env, args...)

	// Parse output
	results := parseGoTestOutput(string(result.Stdout), in.Mode)

	// Prepare response data
	data := map[string]any{
		"mode":   in.Mode,
		"runner": runner,
		"path":   testPath,
	}
	if len(results) > 0 {
		data["results"] = results
	}

	// Add summary
	summary := summarizeResults(in.Mode, string(result.Stdout), string(result.Stderr), result.ExitCode, results)
	for k, v := range summary {
		data[k] = v
	}

	// Include stderr if there were errors
	if result.Err != nil {
		data["exit_code"] = result.ExitCode
		if len(result.Stderr) > 0 {
			data["stderr_preview"] = skillout.TruncateStringWithSuffix(string(result.Stderr), 500, "... (truncated)")
		}
	}

	// Store full output as artifact if substantial
	if len(result.Stdout) > 5000 {
		stdout := bytes.NewBuffer(result.Stdout)
		artifact, err := skillout.PersistBuffer(ctx, rc, stdout, "text/plain", "test_output")
		if err == nil {
			skillout.AddArtifact(data, &artifact)
		}
	} else if len(result.Stdout) > 0 {
		data["output_preview"] = skillout.TruncateStringWithSuffix(string(result.Stdout), 1000, "... (truncated)")
	}

	return skillout.Emit(rc, command, data)
}

func resolveTestPath(rc *skillmain.RunContext, path string) (string, error) {
	if path == "" || path == "./..." {
		return path, nil
	}
	valid, err := skillmain.ValidatePath(rc, path, skillmain.WithPathMessage("resolve test path"))
	if err != nil {
		return "", err
	}
	return valid, nil
}

func buildTestCommand(mode, path string, in input) (string, []string, []string, string, error) {
	var (
		args   []string
		env    []string
		runner string
		runDir string
	)

	switch mode {
	case "test":
		if _, err := executil.RequireTool("go", "install Go from https://go.dev/doc/install"); err != nil {
			return "", nil, nil, "", fmt.Errorf("go command not found: %w", err)
		}
		runner = "go"
		args = []string{"test"}
	case "race":
		if _, err := executil.RequireTool("go", "install Go from https://go.dev/doc/install"); err != nil {
			return "", nil, nil, "", fmt.Errorf("go command not found: %w", err)
		}
		runner = "go"
		args = []string{"test", "-race"}
	case "bench":
		if _, err := executil.RequireTool("go", "install Go from https://go.dev/doc/install"); err != nil {
			return "", nil, nil, "", fmt.Errorf("go command not found: %w", err)
		}
		runner = "go"
		args = []string{"test"}
		if in.Pattern != "" {
			args = append(args, "-bench="+in.Pattern)
			args = append(args, "-run=^$") // Don't run tests, only benchmarks
		} else {
			args = append(args, "-bench=.")
		}
	case "coverage":
		if _, err := executil.RequireTool("go", "install Go from https://go.dev/doc/install"); err != nil {
			return "", nil, nil, "", fmt.Errorf("go command not found: %w", err)
		}
		runner = "go"
		args = []string{"test", "-cover", "-covermode=atomic"}
	case "cargo":
		if _, err := executil.RequireTool("cargo", "install Rust/Cargo"); err != nil {
			return "", nil, nil, "", fmt.Errorf("cargo command not found: %w", err)
		}
		runner = "cargo"
		runDir = testCommandDir(path)
		args = []string{"test"}
		if in.Pattern != "" {
			args = append(args, in.Pattern)
		}
	case "pytest":
		if _, err := executil.RequireTool("pytest", "install pytest in the active environment"); err != nil {
			return "", nil, nil, "", fmt.Errorf("pytest command not found: %w", err)
		}
		runner = "pytest"
		runDir = testCommandDir(path)
		if in.Verbose {
			args = append(args, "-v")
		}
		if in.Pattern != "" {
			args = append(args, "-k", in.Pattern)
		}
		if path != "" && path != "." {
			args = append(args, path)
		}
	case "npm":
		if _, err := executil.RequireTool("npm", "install npm or Node.js"); err != nil {
			return "", nil, nil, "", fmt.Errorf("npm command not found: %w", err)
		}
		runner = "npm"
		runDir = testCommandDir(path)
		args = []string{"test"}
	case "pnpm":
		if _, err := executil.RequireTool("pnpm", "install pnpm"); err != nil {
			return "", nil, nil, "", fmt.Errorf("pnpm command not found: %w", err)
		}
		runner = "pnpm"
		runDir = testCommandDir(path)
		args = []string{"test"}
	case "yarn":
		if _, err := executil.RequireTool("yarn", "install yarn"); err != nil {
			return "", nil, nil, "", fmt.Errorf("yarn command not found: %w", err)
		}
		runner = "yarn"
		runDir = testCommandDir(path)
		args = []string{"test"}
	default:
		return "", nil, nil, "", fmt.Errorf("invalid mode: %s", mode)
	}

	if runner == "go" {
		// Add common flags
		if in.Short {
			args = append(args, "-short")
		}
		if in.Verbose {
			args = append(args, "-v")
		}
		if in.Pattern != "" && mode != "bench" {
			// For bench mode, pattern is already handled above with -bench flag
			args = append(args, "-run="+in.Pattern)
		}
		if in.Timeout != "" {
			args = append(args, "-timeout="+in.Timeout)
		}

		// Add JSON output for parsing
		args = append(args, "-json")

		// Add path
		args = append(args, path)

		// For race mode, we need CGO_ENABLED=1.
		env = []string{"CGO_ENABLED=0"}
		if mode == "race" {
			env = []string{"CGO_ENABLED=1"}
		}
	}

	return runner, args, env, runDir, nil
}

func testCommandDir(path string) string {
	if path == "" || path == "." || path == "./..." {
		return ""
	}
	return path
}

func parseGoTestOutput(output, mode string) []testResult {
	var results []testResult
	packageMap := make(map[string]*testResult)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		var event struct {
			Action  string  `json:"Action"`
			Package string  `json:"Package"`
			Elapsed float64 `json:"Elapsed"`
			Output  string  `json:"Output"`
		}

		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.Action == "pass" || event.Action == "fail" || event.Action == "skip" {
			if event.Package != "" {
				result := testResult{
					Package:  event.Package,
					Status:   event.Action,
					Duration: event.Elapsed,
				}

				// Try to extract coverage
				if mode == "coverage" && event.Output != "" {
					if cov := extractCoverage(event.Output); cov > 0 {
						result.Coverage = cov
					}
				}

				packageMap[event.Package] = &result
			}
		}
	}

	// Convert map to slice
	for _, result := range packageMap {
		results = append(results, *result)
	}

	return results
}

func extractCoverage(output string) float64 {
	// Match patterns like "coverage: 85.3% of statements"
	re := regexp.MustCompile(`coverage:\s+(\d+\.?\d*)%`)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		if cov, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return cov
		}
	}
	return 0
}

func summarizeResults(mode, stdout, stderr string, exitCode int, results []testResult) map[string]any {
	if mode == "pytest" {
		return summarizePytest(stdout, stderr, exitCode)
	}
	if mode == "npm" || mode == "pnpm" || mode == "yarn" {
		return summarizeNPM(stdout, stderr, exitCode)
	}
	if mode == "cargo" {
		return summarizeCargo(stdout, stderr, exitCode)
	}
	summary := map[string]any{
		"total_packages": len(results),
		"passed":         0,
		"failed":         0,
		"skipped":        0,
		"total_duration": 0.0,
	}

	var totalCoverage float64
	var coveredPackages int

	for _, r := range results {
		switch r.Status {
		case "pass":
			summary["passed"] = summary["passed"].(int) + 1
		case "fail":
			summary["failed"] = summary["failed"].(int) + 1
		case "skip":
			summary["skipped"] = summary["skipped"].(int) + 1
		}
		summary["total_duration"] = summary["total_duration"].(float64) + r.Duration

		if r.Coverage > 0 {
			totalCoverage += r.Coverage
			coveredPackages++
		}
	}

	if coveredPackages > 0 {
		summary["average_coverage"] = totalCoverage / float64(coveredPackages)
	}

	summary["success"] = summary["failed"].(int) == 0

	return summary
}

func summarizePytest(stdout, stderr string, exitCode int) map[string]any {
	summary := map[string]any{
		"passed":        0,
		"failed":        0,
		"skipped":       0,
		"runner_status": "unknown",
	}
	text := strings.TrimSpace(stdout + "\n" + stderr)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " in ") {
			continue
		}
		if strings.Contains(line, " passed") || strings.Contains(line, " failed") || strings.Contains(line, " skipped") {
			counts := parseNamedCounts(line, []string{"passed", "failed", "skipped", "xfailed", "xpassed", "error", "errors"})
			summary["passed"] = counts["passed"]
			summary["failed"] = counts["failed"] + counts["error"] + counts["errors"]
			summary["skipped"] = counts["skipped"] + counts["xfailed"] + counts["xpassed"]
			break
		}
	}
	if exitCode == 0 {
		summary["runner_status"] = "pass"
	} else {
		summary["runner_status"] = "fail"
	}
	return summary
}

func summarizeNPM(stdout, stderr string, exitCode int) map[string]any {
	summary := map[string]any{
		"passed":        0,
		"failed":        0,
		"skipped":       0,
		"total_suites":  0,
		"passed_suites": 0,
		"failed_suites": 0,
		"runner_status": "unknown",
	}
	text := strings.TrimSpace(stdout + "\n" + stderr)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Tests:"):
			counts := parseNamedCounts(line, []string{"passed", "failed", "skipped", "todo", "total"})
			summary["passed"] = counts["passed"]
			summary["failed"] = counts["failed"]
			summary["skipped"] = counts["skipped"] + counts["todo"]
		case strings.HasPrefix(line, "Test Suites:"):
			counts := parseNamedCounts(line, []string{"passed", "failed", "total"})
			summary["passed_suites"] = counts["passed"]
			summary["failed_suites"] = counts["failed"]
			summary["total_suites"] = counts["total"]
		}
	}
	if exitCode == 0 {
		summary["runner_status"] = "pass"
	} else {
		summary["runner_status"] = "fail"
	}
	return summary
}

func summarizeCargo(stdout, stderr string, exitCode int) map[string]any {
	summary := map[string]any{
		"passed":        0,
		"failed":        0,
		"skipped":       0,
		"runner_status": "unknown",
	}
	text := strings.TrimSpace(stdout + "\n" + stderr)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "test result:") {
			continue
		}
		counts := parseNamedCounts(line, []string{"passed", "failed", "ignored"})
		summary["passed"] = counts["passed"]
		summary["failed"] = counts["failed"]
		summary["skipped"] = counts["ignored"]
		break
	}
	if exitCode == 0 {
		summary["runner_status"] = "pass"
	} else {
		summary["runner_status"] = "fail"
	}
	return summary
}

func parseNamedCounts(line string, names []string) map[string]int {
	counts := make(map[string]int, len(names))
	for _, name := range names {
		re := regexp.MustCompile(`(\d+)\s+` + regexp.QuoteMeta(name) + `\b`)
		matches := re.FindStringSubmatch(strings.ToLower(line))
		if len(matches) != 2 {
			continue
		}
		value, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}
		counts[name] = value
	}
	return counts
}
