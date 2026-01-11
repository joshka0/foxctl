package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShellRunner_ValidJSON(t *testing.T) {
	// Create temp directory and script
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test-hook.sh")

	// Script that outputs valid hook output JSON
	script := `#!/bin/sh
cat <<'EOF'
{"decision":"approve","reason":"test approved","context":"additional context"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "test-hook", Enabled: true}
	input := Input{Event: EventPreToolUse, ToolName: "test"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Reason != "test approved" {
		t.Errorf("expected reason 'test approved', got %s", output.Reason)
	}
	if output.Context != "additional context" {
		t.Errorf("expected context 'additional context', got %s", output.Context)
	}
}

func TestShellRunner_LegacyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "legacy-hook.sh")

	// Script with legacy hookSpecificOutput format
	script := `#!/bin/sh
cat <<'EOF'
{"hookSpecificOutput":{"additionalContext":"legacy context data"}}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "legacy-hook", Enabled: true}
	input := Input{Event: EventPreToolUse}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionNone {
		t.Errorf("expected none decision for legacy format, got %s", output.Decision)
	}
	if output.Context != "legacy context data" {
		t.Errorf("expected 'legacy context data', got %s", output.Context)
	}
}

func TestShellRunner_EmptyOutput(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "empty-hook.sh")

	// Script with no output
	script := `#!/bin/sh
# No output
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "empty-hook", Enabled: true}
	input := Input{Event: EventPreToolUse}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionApprove {
		t.Errorf("expected approve for empty output, got %s", output.Decision)
	}
}

func TestShellRunner_PlainTextOutput(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "plain-hook.sh")

	// Script with plain text output
	script := `#!/bin/sh
echo "This is plain text context"
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "plain-hook", Enabled: true}
	input := Input{Event: EventPreToolUse}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionNone {
		t.Errorf("expected none for plain text, got %s", output.Decision)
	}
	if output.Context != "This is plain text context" {
		t.Errorf("expected plain text context, got %q", output.Context)
	}
}

func TestShellRunner_BlockDecision(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "block-hook.sh")

	script := `#!/bin/sh
cat <<'EOF'
{"decision":"block","reason":"operation not allowed"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "block-hook", Enabled: true}
	input := Input{Event: EventPreToolUse}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionBlock {
		t.Errorf("expected block, got %s", output.Decision)
	}
	if output.Reason != "operation not allowed" {
		t.Errorf("expected reason 'operation not allowed', got %s", output.Reason)
	}
}

func TestShellRunner_ScriptFailure(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fail-hook.sh")

	script := `#!/bin/sh
exit 1
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "fail-hook", Enabled: true}
	input := Input{Event: EventPreToolUse}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := runner.Run(ctx, hookDef, input)
	if err == nil {
		t.Fatal("expected error for failing script")
	}
}

func TestShellRunner_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow-hook.sh")

	script := `#!/bin/sh
sleep 10
echo '{"decision":"approve"}'
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "slow-hook", Enabled: true}
	input := Input{Event: EventPreToolUse}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := runner.Run(ctx, hookDef, input)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestShellRunner_ReceivesInput(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "echo-hook.sh")

	// Script that reads input and returns it in context
	// Use grep/sed instead of jq for CI compatibility
	script := `#!/bin/sh
INPUT=$(cat)
TOOL_NAME=$(echo "$INPUT" | grep -o '"tool_name":"[^"]*"' | sed 's/"tool_name":"//;s/"$//')
if [ -z "$TOOL_NAME" ]; then TOOL_NAME="none"; fi
cat <<EOF
{"decision":"approve","context":"received tool: $TOOL_NAME"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "echo-hook", Enabled: true}
	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Context != "received tool: fs.read_file" {
		t.Errorf("expected context to contain tool name, got %q", output.Context)
	}
}

func TestShellRunner_EnvironmentVariables(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "env-hook.sh")

	script := `#!/bin/sh
cat <<EOF
{"decision":"approve","context":"event=$AGENTCTL_HOOK_EVENT tool=$AGENTCTL_TOOL_NAME"}
EOF
`
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	runner := &ShellRunner{ScriptPath: scriptPath}
	hookDef := HookDef{ID: "env-hook", Enabled: true}
	input := Input{
		Event:    EventPreToolUse,
		ToolName: "edit.apply_patch",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	output, err := runner.Run(ctx, hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "event=PreToolUse tool=edit.apply_patch"
	if output.Context != expected {
		t.Errorf("expected %q, got %q", expected, output.Context)
	}
}

func TestParseShellOutput_Formats(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Decision
		wantCtx string
	}{
		{
			name:  "v1 format",
			input: `{"decision":"block","reason":"test"}`,
			want:  DecisionBlock,
		},
		{
			name:    "legacy hookSpecificOutput",
			input:   `{"hookSpecificOutput":{"additionalContext":"legacy"}}`,
			want:    DecisionNone,
			wantCtx: "legacy",
		},
		{
			name:    "simple context",
			input:   `{"context":"simple"}`,
			want:    DecisionNone,
			wantCtx: "simple",
		},
		{
			name:  "empty",
			input: "",
			want:  DecisionApprove,
		},
		{
			name:  "whitespace only",
			input: "   \n\t  ",
			want:  DecisionApprove,
		},
		{
			name:    "plain text",
			input:   "just some text",
			want:    DecisionNone,
			wantCtx: "just some text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := parseShellOutput([]byte(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if output.Decision != tt.want {
				t.Errorf("expected decision %s, got %s", tt.want, output.Decision)
			}
			if tt.wantCtx != "" && output.Context != tt.wantCtx {
				t.Errorf("expected context %q, got %q", tt.wantCtx, output.Context)
			}
		})
	}
}
