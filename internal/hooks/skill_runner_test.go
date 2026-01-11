package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution"
)

// MockSkillExecutor is a test double for SkillExecutor.
type MockSkillExecutor struct {
	ExecuteFn func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error)
}

func (m *MockSkillExecutor) Execute(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
	if m.ExecuteFn != nil {
		return m.ExecuteFn(ctx, opts)
	}
	return &execution.Result{}, nil
}

// MockSkillResolver is a test double for SkillResolver.
type MockSkillResolver struct {
	ResolveFn func(skillName string) (skill.Manifest, string, error)
}

func (m *MockSkillResolver) Resolve(skillName string) (skill.Manifest, string, error) {
	if m.ResolveFn != nil {
		return m.ResolveFn(skillName)
	}
	return skill.Manifest{
		Metadata:     skill.Metadata{Name: skillName, Version: "1.0.0"},
		Distribution: skill.Distribution{Type: "exec"},
	}, "/path/to/bin", nil
}

func TestSkillRunner_EnvelopeExtraction(t *testing.T) {
	// Create envelope output with hook_output
	envelope := map[string]any{
		"version": 1,
		"status":  "ok",
		"command": "test/hook",
		"data": map[string]any{
			"hook_output": map[string]any{
				"decision": "approve",
				"reason":   "skill approved",
				"context":  "skill context",
			},
		},
		"meta": map[string]any{
			"ts": "2024-01-01T00:00:00Z",
		},
	}
	envelopeBytes, _ := json.Marshal(envelope)

	executor := &MockSkillExecutor{
		ExecuteFn: func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
			return &execution.Result{Stdout: envelopeBytes}, nil
		},
	}
	resolver := &MockSkillResolver{}

	runner := &SkillRunner{Executor: executor, Resolver: resolver}
	hookDef := HookDef{
		ID:      "skill-hook",
		Enabled: true,
		Run:     []HookRunEntry{{Skill: "test/skill"}},
	}
	input := Input{Event: EventPreToolUse}

	output, err := runner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Reason != "skill approved" {
		t.Errorf("expected 'skill approved', got %s", output.Reason)
	}
	if output.Context != "skill context" {
		t.Errorf("expected 'skill context', got %s", output.Context)
	}
}

func TestSkillRunner_ErrorStatus(t *testing.T) {
	// Create envelope with error status
	envelope := map[string]any{
		"version": 1,
		"status":  "error",
		"command": "test/hook",
		"error": map[string]any{
			"code":    "SKILL_ERROR",
			"message": "something went wrong",
		},
		"meta": map[string]any{
			"ts": "2024-01-01T00:00:00Z",
		},
	}
	envelopeBytes, _ := json.Marshal(envelope)

	executor := &MockSkillExecutor{
		ExecuteFn: func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
			return &execution.Result{Stdout: envelopeBytes}, nil
		},
	}
	resolver := &MockSkillResolver{}

	runner := &SkillRunner{Executor: executor, Resolver: resolver}
	hookDef := HookDef{
		ID:      "error-hook",
		Enabled: true,
		Run:     []HookRunEntry{{Skill: "test/skill"}},
	}
	input := Input{Event: EventPreToolUse}

	_, err := runner.Run(context.Background(), hookDef, input)
	if err == nil {
		t.Fatal("expected error for error status")
	}
}

func TestSkillRunner_DirectHookOutput(t *testing.T) {
	// Direct hook.Output without envelope wrapper
	hookOutput := Output{
		Decision: DecisionBlock,
		Reason:   "direct output",
	}
	outputBytes, _ := json.Marshal(hookOutput)

	executor := &MockSkillExecutor{
		ExecuteFn: func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
			return &execution.Result{Stdout: outputBytes}, nil
		},
	}
	resolver := &MockSkillResolver{}

	runner := &SkillRunner{Executor: executor, Resolver: resolver}
	hookDef := HookDef{
		ID:      "direct-hook",
		Enabled: true,
		Run:     []HookRunEntry{{Skill: "test/skill"}},
	}
	input := Input{Event: EventPreToolUse}

	output, err := runner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionBlock {
		t.Errorf("expected block, got %s", output.Decision)
	}
}

func TestSkillRunner_PassesHookConfig(t *testing.T) {
	var receivedInput Input

	executor := &MockSkillExecutor{
		ExecuteFn: func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
			if err := json.Unmarshal(opts.Input, &receivedInput); err != nil {
				return nil, err
			}
			return &execution.Result{Stdout: []byte(`{"decision":"approve"}`)}, nil
		},
	}
	resolver := &MockSkillResolver{}

	runner := &SkillRunner{Executor: executor, Resolver: resolver}
	hookDef := HookDef{
		ID:      "config-hook",
		Enabled: true,
		Run: []HookRunEntry{
			{
				Skill:  "test/skill",
				Config: map[string]any{"mode": "strict", "threshold": 10},
			},
		},
	}
	input := Input{Event: EventPreToolUse, ToolName: "test"}

	_, err := runner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedInput.HookConfig["mode"] != "strict" {
		t.Errorf("expected mode=strict, got %v", receivedInput.HookConfig["mode"])
	}
	// Note: JSON numbers decode as float64
	if receivedInput.HookConfig["threshold"] != float64(10) {
		t.Errorf("expected threshold=10, got %v", receivedInput.HookConfig["threshold"])
	}
}

func TestSkillRunner_EmptyOutput(t *testing.T) {
	executor := &MockSkillExecutor{
		ExecuteFn: func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
			return &execution.Result{Stdout: []byte{}}, nil
		},
	}
	resolver := &MockSkillResolver{}

	runner := &SkillRunner{Executor: executor, Resolver: resolver}
	hookDef := HookDef{
		ID:      "empty-hook",
		Enabled: true,
		Run:     []HookRunEntry{{Skill: "test/skill"}},
	}
	input := Input{Event: EventPreToolUse}

	output, err := runner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionApprove {
		t.Errorf("expected approve for empty output, got %s", output.Decision)
	}
}

func TestSkillRunner_NoSkillsToRun(t *testing.T) {
	runner := &SkillRunner{}
	hookDef := HookDef{
		ID:      "no-skills",
		Enabled: true,
		Run:     []HookRunEntry{}, // Empty
	}
	input := Input{Event: EventPreToolUse}

	output, err := runner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.Decision != DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
}

func TestSkillRunner_MultipleSkills(t *testing.T) {
	callCount := 0
	executor := &MockSkillExecutor{
		ExecuteFn: func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
			callCount++
			num := string(rune('0' + callCount))
			output := Output{
				Decision: DecisionApprove,
				Reason:   "reason-" + num,
				Context:  "context-" + num,
			}
			bytes, _ := json.Marshal(output)
			return &execution.Result{Stdout: bytes}, nil
		},
	}
	resolver := &MockSkillResolver{}

	runner := &SkillRunner{Executor: executor, Resolver: resolver}
	hookDef := HookDef{
		ID:      "multi-hook",
		Enabled: true,
		Run: []HookRunEntry{
			{Skill: "skill1"},
			{Skill: "skill2"},
		},
	}
	input := Input{Event: EventPreToolUse}

	output, err := runner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 skill calls, got %d", callCount)
	}

	// Output should use the first approve reason
	if output.Reason != "reason-1" {
		t.Errorf("expected reason-1, got %s", output.Reason)
	}

	if output.Context != "context-1\n\ncontext-2" {
		t.Errorf("expected joined context, got %s", output.Context)
	}

	if len(output.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(output.Actions))
	}
}

func TestParseSkillOutput_Formats(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    Decision
		wantErr bool
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  DecisionApprove,
		},
		{
			name:  "direct hook output",
			input: []byte(`{"decision":"block","reason":"test"}`),
			want:  DecisionBlock,
		},
		{
			name:  "envelope with hook_output",
			input: []byte(`{"version":1,"status":"ok","command":"test","data":{"hook_output":{"decision":"approve"}},"meta":{"ts":"2024-01-01T00:00:00Z"}}`),
			want:  DecisionApprove,
		},
		{
			name:    "envelope with error",
			input:   []byte(`{"version":1,"status":"error","command":"test","error":{"code":"ERR","message":"failed"},"meta":{"ts":"2024-01-01T00:00:00Z"}}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := parseSkillOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if output.Decision != tt.want {
				t.Errorf("expected %s, got %s", tt.want, output.Decision)
			}
		})
	}
}
