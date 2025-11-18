// Package main implements the data/jq skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Query     string `json:"query"`
	Input     string `json:"input"`
	RawOutput bool   `json:"raw_output"`
	Compact   bool   `json:"compact"`
	Slurp     bool   `json:"slurp"`
	SortKeys  bool   `json:"sort_keys"`
	YAMLInput bool   `json:"yaml_input"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("data/jq", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("data/jq", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("data/jq", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("data/jq", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Check if jq is available
	jqPath, err := exec.LookPath("jq")
	if err != nil {
		return fmt.Errorf("jq command not found (install jq to use this skill): %w", err)
	}

	// Build jq command
	args := buildJQArgs(in)
	cmd := exec.CommandContext(ctx, jqPath, args...)

	// Provide input via stdin
	cmd.Stdin = strings.NewReader(in.Input)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Execute jq
	execErr := cmd.Run()

	// Parse result
	result := map[string]any{
		"query":      in.Query,
		"input_size": len(in.Input),
	}

	if execErr != nil {
		result["success"] = false
		result["error"] = execErr.Error()
		if stderr.Len() > 0 {
			result["stderr"] = strings.TrimSpace(stderr.String())
		}
	} else {
		result["success"] = true
		outputStr := stdout.String()

		// Parse output based on raw_output flag
		if in.RawOutput {
			result["output"] = strings.TrimSpace(outputStr)
			result["output_type"] = "string"
		} else {
			// Try to parse as JSON
			var parsed any
			if err := json.Unmarshal([]byte(outputStr), &parsed); err == nil {
				result["output"] = parsed
				result["output_type"] = "json"
			} else {
				// If parsing fails, return as string
				result["output"] = strings.TrimSpace(outputStr)
				result["output_type"] = "string"
			}
		}

		result["output_size"] = len(outputStr)

		// Store as artifact if large (use InlineKB threshold, not MaxPreview)
		inlineThreshold := rc.InlineKB * 1024
		if len(outputStr) > inlineThreshold {
			result["output_preview"] = outputStr[:rc.MaxPreview] + "\n... (truncated)"

			// Determine content type based on output format
			contentType := "application/json"
			if in.RawOutput {
				contentType = "text/plain"
			}

			buf := bytes.NewBufferString(outputStr)
			artifact, err := runner.PersistBuffer(ctx, rc, buf, contentType, "jq_output")
			if err == nil && artifact.Digest != "" {
				result["artifact"] = artifact.Digest
				result["artifact_kind"] = artifact.Kind
				result["artifact_size_bytes"] = artifact.Size
			}
		}
	}

	return rc.Emit("data/jq", result, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}

	if strings.TrimSpace(in.Query) == "" {
		return input{}, fmt.Errorf("query is required")
	}

	if in.Input == "" {
		return input{}, fmt.Errorf("input is required")
	}

	return in, nil
}

func buildJQArgs(in input) []string {
	var args []string

	// Add flags
	if in.RawOutput {
		args = append(args, "-r")
	}

	if in.Compact {
		args = append(args, "-c")
	}

	if in.Slurp {
		args = append(args, "-s")
	}

	if in.SortKeys {
		args = append(args, "-S")
	}

	if in.YAMLInput {
		args = append(args, "--yaml-input")
	}

	// Add the query
	args = append(args, in.Query)

	return args
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit data/jq failure")
	os.Exit(1)
}
