// Package main implements the data/jq skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "data/jq"

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
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate
	if strings.TrimSpace(in.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if in.Input == "" {
		return fmt.Errorf("input is required")
	}
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
			artifact, err := skillmain.PersistBuffer(ctx, rc, buf, contentType, "jq_output")
			if err == nil && artifact.Digest != "" {
				result["artifact"] = artifact.Digest
			}
		}
	}

	return skillout.Emit(rc, command, result)
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

