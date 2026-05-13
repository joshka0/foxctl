// Package helperpipeline defines compact helper-pipeline records shared by
// RLM helper tools and eval reporting.
package helperpipeline

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

// Capability describes one helper-pipeline step responsibility.
type Capability string

const (
	CapabilityParseProblem Capability = "parse_problem"
	CapabilitySolve        Capability = "solve"
	CapabilityVerify       Capability = "verify"
	CapabilityFormatAnswer Capability = "format_answer"
)

// TaskSignature identifies the concrete task shape a helper pipeline ran for.
type TaskSignature struct {
	Domain      string         `json:"domain,omitempty"`
	Template    string         `json:"template,omitempty"`
	Shape       string         `json:"shape,omitempty"`
	InputDigest string         `json:"input_digest,omitempty"`
	VerifierID  string         `json:"verifier_id,omitempty"`
	InputKeys   []string       `json:"input_keys,omitempty"`
	Constraints map[string]any `json:"constraints,omitempty"`
}

// StepRun is the compact, parent-visible status for one pipeline step.
type StepRun struct {
	StepID        string         `json:"step_id"`
	Capability    Capability     `json:"capability"`
	Status        string         `json:"status"`
	Error         string         `json:"error,omitempty"`
	SourceHash    string         `json:"source_hash,omitempty"`
	PresetName    string         `json:"preset_name,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	InputSummary  map[string]any `json:"input_summary,omitempty"`
	OutputSummary map[string]any `json:"output_summary,omitempty"`
}

// PipelineRun is the compact trace returned to parent RLM phases. Full helper
// source and large outputs belong in artifacts, not in model prompt context.
type PipelineRun struct {
	PipelineID            string        `json:"pipeline_id"`
	Scaffolded            bool          `json:"scaffolded"`
	LeaderboardComparable bool          `json:"leaderboard_comparable"`
	Status                string        `json:"status"`
	Signature             TaskSignature `json:"signature,omitempty"`
	Steps                 []StepRun     `json:"steps,omitempty"`
	Answer                string        `json:"answer,omitempty"`
	Error                 string        `json:"error,omitempty"`
}

// RunInput is the compact construction contract for one helper-pipeline run.
type RunInput struct {
	ToolName    string
	PresetName  string
	TaskDigest  string
	InputDigest string
	SourceHash  string
	VerifierID  string
	InputKeys   []string
	OK          bool
	Answer      string
	Error       string
	Steps       []StepRun
}

// NewRun builds the canonical parent-visible helper-pipeline trace.
func NewRun(input RunInput) PipelineRun {
	verifierID := strings.TrimSpace(input.VerifierID)
	if verifierID == "" {
		verifierID = strings.TrimSpace(input.PresetName)
	}
	return PipelineRun{
		PipelineID: StablePipelineID(
			input.ToolName,
			input.PresetName,
			input.TaskDigest,
			input.InputDigest,
			input.SourceHash,
			input.Error,
		),
		Scaffolded:            true,
		LeaderboardComparable: false,
		Status:                statusForOK(input.OK),
		Signature: TaskSignature{
			InputDigest: strings.TrimSpace(input.InputDigest),
			VerifierID:  verifierID,
			InputKeys:   append([]string(nil), input.InputKeys...),
		},
		Steps:  append([]StepRun(nil), input.Steps...),
		Answer: strings.TrimSpace(input.Answer),
		Error:  strings.TrimSpace(input.Error),
	}
}

func statusForOK(ok bool) string {
	if ok {
		return "completed"
	}
	return "failed"
}

// StablePipelineID derives a short deterministic ID from the helper contract.
func StablePipelineID(parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("helper-pipeline:%x", sum[:8])
}
