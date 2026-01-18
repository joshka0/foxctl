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

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
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

	// Check if go is available
	if _, err := executil.RequireTool("go", "install Go from https://go.dev/doc/install"); err != nil {
		return fmt.Errorf("go command not found: %w", err)
	}

	// Build command based on mode
	args, env, err := buildTestArgs(in.Mode, testPath, in)
	if err != nil {
		return err
	}

	// Execute tests
	result := executil.RunWithEnv(ctx, "", "go", env, args...)

	// Parse output
	results := parseTestOutput(string(result.Stdout), in.Mode)

	// Prepare response data
	data := map[string]any{
		"mode":    in.Mode,
		"path":    testPath,
		"results": results,
	}

	// Add summary
	summary := summarizeResults(results)
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

func buildTestArgs(mode, path string, in input) ([]string, []string, error) {
	var args []string

	switch mode {
	case "test":
		args = []string{"test"}
	case "race":
		args = []string{"test", "-race"}
	case "bench":
		args = []string{"test"}
		if in.Pattern != "" {
			args = append(args, "-bench="+in.Pattern)
			args = append(args, "-run=^$") // Don't run tests, only benchmarks
		} else {
			args = append(args, "-bench=.")
		}
	case "coverage":
		args = []string{"test", "-cover", "-covermode=atomic"}
	default:
		return nil, nil, fmt.Errorf("invalid mode: %s", mode)
	}

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
	env := []string{"CGO_ENABLED=0"}
	if mode == "race" {
		env = []string{"CGO_ENABLED=1"}
	}

	return args, env, nil
}

func parseTestOutput(output, mode string) []testResult {
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

func summarizeResults(results []testResult) map[string]any {
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
