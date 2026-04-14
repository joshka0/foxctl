package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"

	tooling "github.com/joshka0/foxctl/internal/tooling"
)

// registerTestTools registers test execution tools.
func (r *Registry) registerTestTools() error {
	// tests.run - run tests in the workspace
	runTool := tooling.NewFuncTool(
		"tests.run",
		"Run tests in the workspace. Supports Go, Python (pytest), and JavaScript (jest/npm test).",
		models.InputSchema{
			Type: "object",
			Properties: map[string]models.ParameterSchema{
				"path": {
					Type:        "string",
					Description: "Path to run tests in (relative to workspace, defaults to current directory)",
				},
				"pattern": {
					Type:        "string",
					Description: "Test name pattern to filter (e.g., 'TestFoo', 'test_bar')",
				},
				"verbose": {
					Type:        "boolean",
					Description: "Enable verbose output",
				},
				"timeout": {
					Type:        "integer",
					Description: "Timeout in seconds (default 300)",
				},
			},
		},
		r.wrapWithTelemetry("tests.run", r.runTests),
	)
	if err := r.tools.Register(runTool); err != nil {
		return fmt.Errorf("register tests.run: %w", err)
	}

	return nil
}

// TestResult represents the result of a test run.
type TestResult struct {
	Passed   bool     `json:"passed"`
	Output   string   `json:"output"`
	Failures []string `json:"failures,omitempty"`
	Duration string   `json:"duration"`
}

// runTests implements the tests.run tool.
func (r *Registry) runTests(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
	// Determine test path
	testPath := r.config.WorkspaceRoot
	if p, ok := args["path"].(string); ok && p != "" {
		resolved, err := r.resolvePath(p)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		testPath = resolved
	}

	// Determine timeout
	timeout := 300 * time.Second
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Detect project type and run appropriate tests
	var cmd *exec.Cmd
	var testType string

	// Check for Go project
	if fileExists(filepath.Join(testPath, "go.mod")) {
		testType = "go"
		goArgs := []string{"test"}
		if v, ok := args["verbose"].(bool); ok && v {
			goArgs = append(goArgs, "-v")
		}
		if pattern, ok := args["pattern"].(string); ok && pattern != "" {
			goArgs = append(goArgs, "-run", pattern)
		}
		goArgs = append(goArgs, "./...")
		cmd = exec.CommandContext(ctx, "go", goArgs...)
	} else if fileExists(filepath.Join(testPath, "pytest.ini")) || fileExists(filepath.Join(testPath, "setup.py")) || fileExists(filepath.Join(testPath, "pyproject.toml")) {
		testType = "pytest"
		pytestArgs := []string{}
		if v, ok := args["verbose"].(bool); ok && v {
			pytestArgs = append(pytestArgs, "-v")
		}
		if pattern, ok := args["pattern"].(string); ok && pattern != "" {
			pytestArgs = append(pytestArgs, "-k", pattern)
		}
		cmd = exec.CommandContext(ctx, "pytest", pytestArgs...)
	} else if fileExists(filepath.Join(testPath, "package.json")) {
		testType = "npm"
		cmd = exec.CommandContext(ctx, "npm", "test")
	} else {
		// Default to go test
		testType = "go"
		cmd = exec.CommandContext(ctx, "go", "test", "./...")
	}

	cmd.Dir = testPath

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	// Combine output
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}

	// Parse failures based on test type
	var failures []string
	if err != nil {
		failures = parseTestFailures(testType, output)
	}

	result := TestResult{
		Passed:   err == nil,
		Output:   truncateOutput(output, 10000), // Limit output size
		Failures: failures,
		Duration: duration.String(),
	}

	return successResult(map[string]any{
		"test_type": testType,
		"result":    result,
	}), nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// parseTestFailures extracts failure messages from test output.
func parseTestFailures(testType, output string) []string {
	var failures []string
	lines := strings.Split(output, "\n")

	switch testType {
	case "go":
		for _, line := range lines {
			if strings.Contains(line, "--- FAIL:") || strings.Contains(line, "FAIL\t") {
				failures = append(failures, strings.TrimSpace(line))
			}
		}
	case "pytest":
		inFailure := false
		for _, line := range lines {
			if strings.HasPrefix(line, "FAILED ") {
				failures = append(failures, strings.TrimSpace(line))
			} else if strings.HasPrefix(line, "=") && strings.Contains(line, "FAILURES") {
				inFailure = true
			} else if inFailure && strings.HasPrefix(line, "_") {
				failures = append(failures, strings.TrimSpace(line))
			}
		}
	case "npm":
		for _, line := range lines {
			if strings.Contains(line, "FAIL ") || strings.Contains(line, "✕") {
				failures = append(failures, strings.TrimSpace(line))
			}
		}
	}

	return failures
}

// truncateOutput limits output to maxLen characters.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}
