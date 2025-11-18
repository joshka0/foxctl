// Package main implements the test/run skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
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

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("test/run", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("test/run", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("test/run", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("test/run", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Validate path
	testPath, err := resolveTestPath(rc, in.Path)
	if err != nil {
		return fmt.Errorf("resolve test path: %w", err)
	}

	// Check if go is available
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go command not found: %w", err)
	}

	// Build command based on mode
	cmd, err := buildTestCommand(ctx, in.Mode, testPath, in)
	if err != nil {
		return err
	}

	// Execute tests
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	execErr := cmd.Run()

	// Parse output
	results := parseTestOutput(stdout.String(), in.Mode)

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
	if execErr != nil {
		data["exit_code"] = getExitCode(execErr)
		if stderr.Len() > 0 {
			data["stderr_preview"] = truncate(stderr.String(), 500)
		}
	}

	// Store full output as artifact if substantial
	if stdout.Len() > 5000 {
		artifact, err := runner.PersistBuffer(ctx, rc, &stdout, "text/plain", "test_output")
		if err == nil && artifact.Digest != "" {
			data["artifact"] = artifact.Digest
			data["artifact_kind"] = artifact.Kind
			data["artifact_size_bytes"] = artifact.Size
		}
	} else if stdout.Len() > 0 {
		data["output_preview"] = truncate(stdout.String(), 1000)
	}

	return rc.Emit("test/run", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.Path == "" {
		in.Path = "./..."
	}
	if in.Mode == "" {
		in.Mode = "test"
	}
	if in.Timeout == "" {
		in.Timeout = "10m"
	}
	return in, nil
}

func resolveTestPath(rc *runner.RunnerContext, path string) (string, error) {
	if path == "" || path == "./..." {
		return path, nil
	}
	valid, err := rc.PathValidator.ValidatePath(path)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	return valid, nil
}

func buildTestCommand(ctx context.Context, mode, path string, in input) (*exec.Cmd, error) {
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
		return nil, fmt.Errorf("invalid mode: %s", mode)
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

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = os.Environ()

	// For race mode, we need CGO_ENABLED=1
	if mode == "race" {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=1")
	} else {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	}

	return cmd, nil
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

func getExitCode(err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "... (truncated)"
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit test/run failure")
	os.Exit(1)
}
