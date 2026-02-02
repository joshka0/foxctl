package hook

import "encoding/json"

// Input represents the standardized input from Claude Code hooks.
// This is what the bash wrapper passes to agentctl hook skills.
type Input struct {
	// Event is the hook event type (e.g., "PreToolUse", "PostToolUse", "Stop")
	Event string `json:"event"`

	// WorkspaceRoot is the absolute path to the workspace/project root
	WorkspaceRoot string `json:"workspace_root"`

	// SessionID is the Claude session identifier
	SessionID string `json:"session_id"`

	// TranscriptPath is the path to the conversation transcript file
	TranscriptPath string `json:"transcript_path,omitempty"`

	// ToolName is the name of the tool being used (for PreToolUse/PostToolUse)
	ToolName string `json:"tool_name,omitempty"`

	// ToolInput is the raw JSON input to the tool
	ToolInput json.RawMessage `json:"tool_input,omitempty"`

	// ToolResponse is the raw JSON response from the tool (PostToolUse only)
	ToolResponse json.RawMessage `json:"tool_response,omitempty"`
}

// Decision represents the hook's decision about the operation.
type Decision string

const (
	// DecisionApprove allows the operation to proceed.
	DecisionApprove Decision = "approve"

	// DecisionBlock prevents the operation from proceeding.
	DecisionBlock Decision = "block"

	// DecisionNone indicates no decision (hook is informational only).
	DecisionNone Decision = "none"
)

// Output represents the standardized output from hook skills.
// This is placed in data.hook_output of the envelope.
type Output struct {
	// Decision is the hook's verdict: "approve", "block", or "none"
	Decision Decision `json:"decision"`

	// Reason explains the decision (shown to Claude/user)
	Reason string `json:"reason,omitempty"`

	// Context is supplemental text to inject into Claude's context
	Context string `json:"context,omitempty"`

	// Meta contains additional metadata (e.g., task_id, workspace_id)
	Meta map[string]any `json:"meta,omitempty"`
}

// NewApprove creates an approve output with the given reason and metadata.
func NewApprove(reason string, meta map[string]any) Output {
	return Output{
		Decision: DecisionApprove,
		Reason:   reason,
		Meta:     meta,
	}
}

// NewBlock creates a block output with the given reason.
func NewBlock(reason string) Output {
	return Output{
		Decision: DecisionBlock,
		Reason:   reason,
	}
}

// NewNone creates a no-decision output (informational).
func NewNone() Output {
	return Output{
		Decision: DecisionNone,
	}
}

// IsWriteOperation returns true if the tool name is a write operation
// that should be gated by task_guard.
func IsWriteOperation(toolName string) bool {
	switch toolName {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return true
	default:
		return false
	}
}
