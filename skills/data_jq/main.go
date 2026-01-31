// Package main implements the data/jq skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "data/jq"

// input is the expected JSON input for data/jq operations.
type input struct {
	Query     string `json:"query"`
	Input     string `json:"input"`
	RawOutput bool   `json:"raw_output"`
	Compact   bool   `json:"compact"`
	Slurp     bool   `json:"slurp"`
	SortKeys  bool   `json:"sort_keys"`
	YAMLInput bool   `json:"yaml_input"`
}

// main is the skill entry point for data/jq.
func main() {
	skillmain.Main(command, run)
}

// run executes jq queries on JSON/YAML data with configurable output formatting.
//
// Index:
// - Purpose: Execute jq queries on JSON/YAML data with various output options and artifact storage
// - Flow: validate input → check jq availability → build command args → execute jq → parse results → store large outputs
// - SideEffects: subprocess execution; file system reads; artifact storage; content type detection
// - FailureModes: invalid queries, missing jq tool, execution errors, parse errors
// - Observability: emits query results with success status, output type, and optional artifact storage
// - Related: buildJQArgs
// - Keywords: data/jq, JSON, YAML, query, transformation, subprocess
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate
	if strings.TrimSpace(in.Query) == "" {
		return fmt.Errorf("query is required")
	}
	if in.Input == "" {
		return fmt.Errorf("input is required")
	}
	// Check if jq is available
	jqPath, err := executil.RequireTool("jq", "install jq")
	if err != nil {
		return fmt.Errorf("jq command not found (install jq to use this skill): %w", err)
	}

	// Build jq command
	args := buildJQArgs(in)
	cmdResult := executil.RunWithInput(ctx, "", jqPath, []byte(in.Input), args...)

	// Parse result
	payload := map[string]any{
		"query":      in.Query,
		"input_size": len(in.Input),
	}

	if cmdResult.Err != nil {
		payload["success"] = false
		payload["error"] = cmdResult.Err.Error()
		if len(cmdResult.Stderr) > 0 {
			payload["stderr"] = strings.TrimSpace(string(cmdResult.Stderr))
		}
	} else {
		payload["success"] = true
		outputStr := string(cmdResult.Stdout)

		// Parse output based on raw_output flag
		if in.RawOutput {
			payload["output"] = strings.TrimSpace(outputStr)
			payload["output_type"] = "string"
		} else {
			// Try to parse as JSON
			var parsed any
			if err := json.Unmarshal([]byte(outputStr), &parsed); err == nil {
				payload["output"] = parsed
				payload["output_type"] = "json"
			} else {
				// If parsing fails, return as string
				payload["output"] = strings.TrimSpace(outputStr)
				payload["output_type"] = "string"
			}
		}

		payload["output_size"] = len(outputStr)

		// Store as artifact if large (use InlineKB threshold, not MaxPreview)
		inlineThreshold := rc.InlineKB * 1024
		if len(outputStr) > inlineThreshold {
			payload["output_preview"] = skillout.TruncateStringWithSuffix(outputStr, rc.MaxPreview, "\n... (truncated)")

			// Determine content type based on output format
			contentType := "application/json"
			if in.RawOutput {
				contentType = "text/plain"
			}

			buf := bytes.NewBufferString(outputStr)
			artifact, err := skillmain.PersistBuffer(ctx, rc, buf, contentType, "jq_output")
			if err == nil && artifact.Digest != "" {
				payload["artifact"] = artifact.Digest
			}
		}
	}

	return skillout.Emit(rc, command, payload)
}

// buildJQArgs constructs the argument list for the jq command based on input flags.
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
