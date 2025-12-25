// Package hook defines types for Claude Code hook integration.
package hook

import (
	"encoding/json"
	"testing"
)

func TestDecision_Constants(t *testing.T) {
	tests := []struct {
		decision Decision
		want     string
	}{
		{DecisionApprove, "approve"},
		{DecisionBlock, "block"},
		{DecisionNone, "none"},
	}

	for _, tt := range tests {
		t.Run(string(tt.decision), func(t *testing.T) {
			if string(tt.decision) != tt.want {
				t.Errorf("Decision = %q, want %q", tt.decision, tt.want)
			}
		})
	}
}

func TestNewApprove(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		meta   map[string]any
	}{
		{
			name:   "simple approve",
			reason: "All checks passed",
			meta:   nil,
		},
		{
			name:   "approve with meta",
			reason: "Approved by policy",
			meta:   map[string]any{"task_id": "task-123", "workspace_id": "ws-456"},
		},
		{
			name:   "empty reason",
			reason: "",
			meta:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewApprove(tt.reason, tt.meta)

			if got.Decision != DecisionApprove {
				t.Errorf("Decision = %v, want %v", got.Decision, DecisionApprove)
			}
			if got.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.reason)
			}
			if tt.meta != nil && got.Meta == nil {
				t.Error("Meta should not be nil")
			}
		})
	}
}

func TestNewBlock(t *testing.T) {
	tests := []struct {
		name   string
		reason string
	}{
		{
			name:   "block with reason",
			reason: "Operation not allowed",
		},
		{
			name:   "block with detailed reason",
			reason: "Write to protected file /etc/config denied by policy",
		},
		{
			name:   "block with empty reason",
			reason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewBlock(tt.reason)

			if got.Decision != DecisionBlock {
				t.Errorf("Decision = %v, want %v", got.Decision, DecisionBlock)
			}
			if got.Reason != tt.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.reason)
			}
			if got.Meta != nil {
				t.Error("Meta should be nil for block")
			}
		})
	}
}

func TestNewNone(t *testing.T) {
	got := NewNone()

	if got.Decision != DecisionNone {
		t.Errorf("Decision = %v, want %v", got.Decision, DecisionNone)
	}
	if got.Reason != "" {
		t.Errorf("Reason should be empty, got %q", got.Reason)
	}
	if got.Meta != nil {
		t.Error("Meta should be nil")
	}
	if got.Context != "" {
		t.Errorf("Context should be empty, got %q", got.Context)
	}
}

func TestIsWriteOperation(t *testing.T) {
	tests := []struct {
		toolName string
		want     bool
	}{
		// Write operations
		{"Edit", true},
		{"Write", true},
		{"MultiEdit", true},
		{"NotebookEdit", true},

		// Non-write operations
		{"Read", false},
		{"View", false},
		{"Search", false},
		{"Bash", false},
		{"ListFiles", false},
		{"", false},
		{"edit", false},  // Case sensitive
		{"write", false}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := IsWriteOperation(tt.toolName)
			if got != tt.want {
				t.Errorf("IsWriteOperation(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestInput_JSONSerialization(t *testing.T) {
	input := Input{
		Event:          "PreToolUse",
		WorkspaceRoot:  "/home/user/project",
		SessionID:      "session-123",
		TranscriptPath: "/tmp/transcript.json",
		ToolName:       "Write",
		ToolInput:      json.RawMessage(`{"path":"/tmp/test.txt","content":"hello"}`),
		ToolResponse:   json.RawMessage(`{"success":true}`),
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Input
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Event != input.Event {
		t.Errorf("Event = %q, want %q", got.Event, input.Event)
	}
	if got.WorkspaceRoot != input.WorkspaceRoot {
		t.Errorf("WorkspaceRoot = %q, want %q", got.WorkspaceRoot, input.WorkspaceRoot)
	}
	if got.SessionID != input.SessionID {
		t.Errorf("SessionID = %q, want %q", got.SessionID, input.SessionID)
	}
	if got.TranscriptPath != input.TranscriptPath {
		t.Errorf("TranscriptPath = %q, want %q", got.TranscriptPath, input.TranscriptPath)
	}
	if got.ToolName != input.ToolName {
		t.Errorf("ToolName = %q, want %q", got.ToolName, input.ToolName)
	}
}

func TestInput_AllEventTypes(t *testing.T) {
	events := []string{"PreToolUse", "PostToolUse", "Stop", "NotificationShown"}

	for _, event := range events {
		t.Run(event, func(t *testing.T) {
			input := Input{
				Event:         event,
				WorkspaceRoot: "/home/user/project",
				SessionID:     "session-test",
			}

			data, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var got Input
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if got.Event != event {
				t.Errorf("Event = %q, want %q", got.Event, event)
			}
		})
	}
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		Decision: DecisionApprove,
		Reason:   "Operation allowed by policy",
		Context:  "Additional context for Claude",
		Meta:     map[string]any{"task_id": "task-123", "policy": "default"},
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Output
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Decision != output.Decision {
		t.Errorf("Decision = %v, want %v", got.Decision, output.Decision)
	}
	if got.Reason != output.Reason {
		t.Errorf("Reason = %q, want %q", got.Reason, output.Reason)
	}
	if got.Context != output.Context {
		t.Errorf("Context = %q, want %q", got.Context, output.Context)
	}
	if got.Meta == nil {
		t.Error("Meta should not be nil")
	}
}

func TestOutput_AllDecisions(t *testing.T) {
	decisions := []Decision{DecisionApprove, DecisionBlock, DecisionNone}

	for _, decision := range decisions {
		t.Run(string(decision), func(t *testing.T) {
			output := Output{
				Decision: decision,
				Reason:   "Test reason",
			}

			data, err := json.Marshal(output)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var got Output
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if got.Decision != decision {
				t.Errorf("Decision = %v, want %v", got.Decision, decision)
			}
		})
	}
}

func TestInput_OptionalFields(t *testing.T) {
	// Test with minimal fields (no optional fields)
	input := Input{
		Event:         "Stop",
		WorkspaceRoot: "/home/user/project",
		SessionID:     "session-123",
	}

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Input
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.TranscriptPath != "" {
		t.Errorf("TranscriptPath should be empty, got %q", got.TranscriptPath)
	}
	if got.ToolName != "" {
		t.Errorf("ToolName should be empty, got %q", got.ToolName)
	}
	if got.ToolInput != nil {
		t.Error("ToolInput should be nil")
	}
	if got.ToolResponse != nil {
		t.Error("ToolResponse should be nil")
	}
}

func TestOutput_OptionalFields(t *testing.T) {
	// Test with minimal fields
	output := Output{
		Decision: DecisionNone,
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Output
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Reason != "" {
		t.Errorf("Reason should be empty, got %q", got.Reason)
	}
	if got.Context != "" {
		t.Errorf("Context should be empty, got %q", got.Context)
	}
	if got.Meta != nil {
		t.Error("Meta should be nil")
	}
}

func TestInput_ToolInputParsing(t *testing.T) {
	input := Input{
		Event:         "PreToolUse",
		WorkspaceRoot: "/home/user/project",
		SessionID:     "session-123",
		ToolName:      "Write",
		ToolInput:     json.RawMessage(`{"path":"/tmp/test.txt","content":"hello world"}`),
	}

	// Verify we can parse the ToolInput
	var toolInput struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	if err := json.Unmarshal(input.ToolInput, &toolInput); err != nil {
		t.Fatalf("Failed to parse ToolInput: %v", err)
	}

	if toolInput.Path != "/tmp/test.txt" {
		t.Errorf("ToolInput.Path = %q, want %q", toolInput.Path, "/tmp/test.txt")
	}
	if toolInput.Content != "hello world" {
		t.Errorf("ToolInput.Content = %q, want %q", toolInput.Content, "hello world")
	}
}
