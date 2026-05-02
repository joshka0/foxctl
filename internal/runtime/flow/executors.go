package flow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// ---------------------------------------------------------------------------
// NodeExecutor interface
// ---------------------------------------------------------------------------

// NodeExecutor executes a flow node, producing output from input.
// Implementations must be safe for concurrent use.
type NodeExecutor interface {
	// Execute runs the node with the given input and returns the output.
	// Source nodes receive nil input.
	// Implementations should respect context cancellation.
	Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error)
}

// ---------------------------------------------------------------------------
// SkillExecutor
// ---------------------------------------------------------------------------

// SkillExecutor executes a foxctl skill subprocess.
type SkillExecutor struct {
	// Workspace is the working directory for skill execution.
	Workspace string
}

// Execute runs the configured skill via executil.RunFoxctlSkill.
// For source nodes (nil input), it passes no input.
// For non-source nodes, it serializes the input data and passes it as --input.
func (e *SkillExecutor) Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
	start := time.Now()

	// Parse skill config.
	var cfg SkillConfig
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return NodeOutput{}, fmt.Errorf("flow: skill executor: parse config: %w", err)
	}

	workspace := cfg.Workspace
	if workspace == "" {
		workspace = e.Workspace
	}

	// Serialize input.
	var inputBytes []byte
	if input != nil {
		var err error
		inputBytes, err = json.Marshal(input)
		if err != nil {
			return NodeOutput{}, fmt.Errorf("flow: skill executor: marshal input: %w", err)
		}
	}

	// Run skill.
	result, err := executil.RunFoxctlSkillWithArgs(ctx, workspace, cfg.Skill, inputBytes, cfg.ExtraArgs)
	duration := time.Since(start)

	if err != nil {
		// If we have a partial result with an envelope, use it.
		if result.Envelope.Version > 0 {
			return NodeOutput{
				Envelope: result.Envelope,
				Duration: duration,
				NodeID:   node.ID,
			}, nil
		}
		// Otherwise, produce an error envelope.
		return NodeOutput{
			Envelope: envelope.Error("flow/skill", "ERUNTIME",
				fmt.Sprintf("skill %q execution failed: %v", cfg.Skill, err), nil),
			Duration: duration,
			NodeID:   node.ID,
		}, nil
	}

	return NodeOutput{
		Envelope: result.Envelope,
		Duration: duration,
		NodeID:   node.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// TransformExecutor
// ---------------------------------------------------------------------------

// TransformExecutor applies a configured transform to input data.
// It uses the transform registry to look up and execute transforms.
type TransformExecutor struct{}

// Execute runs the configured transform on the input data.
func (e *TransformExecutor) Execute(ctx context.Context, node FlowNode, input any) (NodeOutput, error) {
	start := time.Now()

	// Parse transform config.
	var cfg struct {
		Transform string `json:"transform"`
		Config    string `json:"config"`
	}
	if err := json.Unmarshal(node.Config, &cfg); err != nil {
		return NodeOutput{}, fmt.Errorf("flow: transform executor: parse config: %w", err)
	}

	kind := TransformKind(cfg.Transform)
	if !kind.IsValid() {
		return NodeOutput{}, fmt.Errorf("flow: transform executor: invalid transform kind %q", kind)
	}

	// Apply the transform.
	result, err := ApplyTransform(ctx, kind, cfg.Config, input)
	duration := time.Since(start)

	if err != nil {
		return NodeOutput{
			Envelope: envelope.Error("flow/transform", "EPARSE", err.Error(), nil),
			Duration: duration,
			NodeID:   node.ID,
		}, nil
	}

	return NodeOutput{
		Envelope: envelope.OK("flow/transform", result),
		Duration: duration,
		NodeID:   node.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// parseOutputEnvelope helper
// ---------------------------------------------------------------------------

// parseOutputEnvelope parses raw bytes into an envelope.
// Returns an error if the bytes are not valid JSON or not a valid envelope.
func parseOutputEnvelope(data []byte) (envelope.Envelope, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope.Envelope{}, fmt.Errorf("flow: parse output: %w", err)
	}
	return env, nil
}
