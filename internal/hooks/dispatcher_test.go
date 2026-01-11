package hooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDispatcher_NoHooks(t *testing.T) {
	cfg := EmptyConfig()
	d := NewDispatcher(cfg, NoopRunner{})

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, err := d.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output.Decision != DecisionApprove {
		t.Errorf("expected approve, got %s", result.Output.Decision)
	}
	if len(result.HooksRun) != 0 {
		t.Errorf("expected no hooks run, got %d", len(result.HooksRun))
	}
}

func TestDispatcher_SingleHook(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "test-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/skill", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		return NewApprove("test approved", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, err := d.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.HooksRun) != 1 {
		t.Errorf("expected 1 hook run, got %d", len(result.HooksRun))
	}
	if result.HooksRun[0] != "test-hook" {
		t.Errorf("expected hook 'test-hook', got %s", result.HooksRun[0])
	}
}

func TestDispatcher_HookBlocks(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "blocker",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/blocker", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		return NewBlock("blocked by test"), nil
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "edit.apply_patch"}
	result, err := d.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Blocked {
		t.Error("expected result to be blocked")
	}
	if result.BlockedBy != "blocker" {
		t.Errorf("expected blocked by 'blocker', got %s", result.BlockedBy)
	}
	if result.Output.Decision != DecisionBlock {
		t.Errorf("expected block decision, got %s", result.Output.Decision)
	}
}

func TestDispatcher_MultipleHooks_MergeOutput(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "hook1",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "hook1/skill", TimeoutMS: 1000, FailOpen: true},
			},
		},
		{
			ID:      "hook2",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "hook2/skill", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return Output{
			Decision: DecisionApprove,
			Reason:   hookDef.ID,
			Context:  "ctx-" + hookDef.ID,
		}, nil
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, err := d.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 hook calls, got %d", callCount)
	}

	// Reasons should use the first approve reason
	if result.Output.Reason != "hook1" {
		t.Errorf("expected reason hook1, got %s", result.Output.Reason)
	}

	if result.Output.Context != "ctx-hook1\n\nctx-hook2" {
		t.Errorf("expected joined context, got %s", result.Output.Context)
	}

	if len(result.Output.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(result.Output.Actions))
	}
}

func TestDispatcher_FailOpen(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "failing-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/failing", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		return Output{}, errors.New("hook execution failed")
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, err := d.Dispatch(context.Background(), input)

	// Should NOT return error because fail_open=true
	if err != nil {
		t.Fatalf("expected no error with fail_open, got: %v", err)
	}

	// Should have recorded the error in HookResults
	if len(result.HookResults) != 1 {
		t.Fatalf("expected 1 hook result, got %d", len(result.HookResults))
	}
	if result.HookResults[0].Error == nil {
		t.Error("expected error in hook result")
	}
	if !result.HookResults[0].FailOpen {
		t.Error("expected fail_open=true in hook result")
	}
}

func TestDispatcher_FailClosed(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "critical-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/critical", TimeoutMS: 1000, FailOpen: false},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		return Output{}, errors.New("critical hook failed")
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "edit.apply_patch"}
	result, err := d.Dispatch(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error with fail_open=false: %v", err)
	}

	if !result.Blocked {
		t.Fatal("expected result to be blocked for fail_open=false")
	}
	if result.Output.Decision != DecisionBlock {
		t.Fatalf("expected block decision, got %s", result.Output.Decision)
	}
	expectedReason := "hook_failed:test/critical:critical hook failed"
	if result.Output.Reason != expectedReason {
		t.Fatalf("expected reason %q, got %q", expectedReason, result.Output.Reason)
	}
}

func TestDispatcher_Matcher_ToolName(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "edit-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Match:   &HookMatcher{ToolName: "^edit\\."},
			Run: []HookRunEntry{
				{Skill: "test/edit", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return NewApprove("matched", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	// Test non-matching tool
	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, _ := d.Dispatch(context.Background(), input)
	if len(result.HooksRun) != 0 {
		t.Error("expected no hooks to run for fs.read_file")
	}

	// TODO: Add proper regex support and test edit.* matching
	// For now, the matcher stub returns true for all patterns
}

func TestDispatcher_DisabledHook(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "disabled-hook",
			Enabled: false, // Disabled
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/disabled", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return NewApprove("should not run", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, _ := d.Dispatch(context.Background(), input)

	if callCount != 0 {
		t.Errorf("expected no hook calls for disabled hook, got %d", callCount)
	}
	if len(result.HooksRun) != 0 {
		t.Errorf("expected no hooks run, got %d", len(result.HooksRun))
	}
}

func TestDispatcher_DifferentEvents(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "pre-tool",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/pre", TimeoutMS: 1000, FailOpen: true},
			},
		},
		{
			ID:      "post-tool",
			Enabled: true,
			Event:   EventPostToolUse,
			Run: []HookRunEntry{
				{Skill: "test/post", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		return NewApprove(hookDef.ID, nil), nil
	})

	d := NewDispatcher(cfg, runner)

	// Test PreToolUse
	preInput := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	preResult, _ := d.Dispatch(context.Background(), preInput)
	if len(preResult.HooksRun) != 1 || preResult.HooksRun[0] != "pre-tool" {
		t.Errorf("expected pre-tool hook, got %v", preResult.HooksRun)
	}

	// Test PostToolUse
	postInput := Input{Event: EventPostToolUse, ToolName: "fs.read_file"}
	postResult, _ := d.Dispatch(context.Background(), postInput)
	if len(postResult.HooksRun) != 1 || postResult.HooksRun[0] != "post-tool" {
		t.Errorf("expected post-tool hook, got %v", postResult.HooksRun)
	}
}

func TestDispatcher_HookConfig(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "configurable",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{
					Skill:     "test/configurable",
					TimeoutMS: 1000,
					FailOpen:  true,
					Config:    map[string]any{"mode": "strict", "threshold": 10},
				},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	var receivedConfig map[string]any
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		receivedConfig = input.HookConfig
		return NewApprove("ok", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	if _, err := d.Dispatch(context.Background(), input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedConfig["mode"] != "strict" {
		t.Errorf("expected mode=strict, got %v", receivedConfig["mode"])
	}
	if receivedConfig["threshold"] != 10 {
		t.Errorf("expected threshold=10, got %v", receivedConfig["threshold"])
	}
}

func TestDispatcher_Timeout(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "slow-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/slow", TimeoutMS: 100, FailOpen: true}, // 100ms timeout
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		select {
		case <-ctx.Done():
			return Output{}, ctx.Err()
		case <-time.After(1 * time.Second): // Would take 1 second
			return NewApprove("slow", nil), nil
		}
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	result, err := d.Dispatch(context.Background(), input)

	// Should not error because fail_open=true
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// But should have recorded the timeout
	if len(result.HookResults) != 1 {
		t.Fatalf("expected 1 hook result, got %d", len(result.HookResults))
	}
	if result.HookResults[0].Error == nil {
		t.Error("expected timeout error in hook result")
	}
}

func TestDispatcher_DispatchAsync(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "async-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/async", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		return NewApprove("async", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	input := Input{Event: EventPreToolUse, ToolName: "fs.read_file"}
	ch := d.DispatchAsync(context.Background(), input)

	select {
	case result := <-ch:
		if result.Output.Decision != DecisionApprove {
			t.Errorf("expected approve, got %s", result.Output.Decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch async timed out")
	}
}

func TestDispatcher_Matcher_ToolCanonical(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "edit-canonical-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Match:   &HookMatcher{ToolCanonical: "^edit\\."},
			Run: []HookRunEntry{
				{Skill: "test/edit", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return NewApprove("matched", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	// Test matching canonical tool
	input := Input{Event: EventPreToolUse, ToolCanonical: "edit.apply_patch"}
	result, _ := d.Dispatch(context.Background(), input)
	if len(result.HooksRun) != 1 {
		t.Error("expected hook to run for edit.apply_patch")
	}

	// Reset and test non-matching
	callCount = 0
	input2 := Input{Event: EventPreToolUse, ToolCanonical: "fs.read_file"}
	result2, _ := d.Dispatch(context.Background(), input2)
	if len(result2.HooksRun) != 0 {
		t.Error("expected no hooks to run for fs.read_file")
	}
}

func TestDispatcher_Matcher_ToolKind(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "write-kind-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Match:   &HookMatcher{ToolKind: ToolKindWrite},
			Run: []HookRunEntry{
				{Skill: "test/write", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return NewApprove("matched", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	// Test with explicit tool_kind
	input := Input{Event: EventPreToolUse, ToolKind: ToolKindWrite}
	result, _ := d.Dispatch(context.Background(), input)
	if len(result.HooksRun) != 1 {
		t.Error("expected hook to run for write kind")
	}

	// Reset and test with CC tool name (should auto-classify)
	callCount = 0
	input2 := Input{Event: EventPreToolUse, ToolName: "Edit"}
	result2, _ := d.Dispatch(context.Background(), input2)
	if len(result2.HooksRun) != 1 {
		t.Error("expected hook to run for Edit (auto-classified as write)")
	}

	// Reset and test non-matching kind
	callCount = 0
	input3 := Input{Event: EventPreToolUse, ToolKind: ToolKindRead}
	result3, _ := d.Dispatch(context.Background(), input3)
	if len(result3.HooksRun) != 0 {
		t.Error("expected no hooks to run for read kind")
	}
}

func TestDispatcher_Matcher_PromptRegex(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "refactor-prompt-hook",
			Enabled: true,
			Event:   EventUserPromptSubmit,
			Match:   &HookMatcher{PromptRegex: "(?i)refactor"},
			Run: []HookRunEntry{
				{Skill: "test/refactor", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return NewApprove("matched", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	// Test matching prompt
	input := Input{Event: EventUserPromptSubmit, Prompt: "Please refactor this code"}
	result, _ := d.Dispatch(context.Background(), input)
	if len(result.HooksRun) != 1 {
		t.Error("expected hook to run for 'refactor' prompt")
	}

	// Reset and test non-matching prompt
	callCount = 0
	input2 := Input{Event: EventUserPromptSubmit, Prompt: "Fix the bug"}
	result2, _ := d.Dispatch(context.Background(), input2)
	if len(result2.HooksRun) != 0 {
		t.Error("expected no hooks to run for 'Fix the bug' prompt")
	}
}

func TestDispatcher_Matcher_PathRegex(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "test-path-hook",
			Enabled: true,
			Event:   EventPreToolUse,
			Match:   &HookMatcher{PathRegex: "_test\\.go$"},
			Run: []HookRunEntry{
				{Skill: "test/path", TimeoutMS: 1000, FailOpen: true},
			},
		},
	}
	cfg := ConfigFromHooks(hooks)

	callCount := 0
	runner := FuncRunner(func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
		callCount++
		return NewApprove("matched", nil), nil
	})

	d := NewDispatcher(cfg, runner)

	// Test matching path (using file_path)
	input := Input{
		Event:     EventPreToolUse,
		ToolInput: []byte(`{"file_path": "/src/foo_test.go"}`),
	}
	result, _ := d.Dispatch(context.Background(), input)
	if len(result.HooksRun) != 1 {
		t.Error("expected hook to run for test file path")
	}

	// Reset and test with "path" field
	callCount = 0
	input2 := Input{
		Event:     EventPreToolUse,
		ToolInput: []byte(`{"path": "/src/bar_test.go"}`),
	}
	result2, _ := d.Dispatch(context.Background(), input2)
	if len(result2.HooksRun) != 1 {
		t.Error("expected hook to run for test file (path field)")
	}

	// Reset and test non-matching path
	callCount = 0
	input3 := Input{
		Event:     EventPreToolUse,
		ToolInput: []byte(`{"file_path": "/src/main.go"}`),
	}
	result3, _ := d.Dispatch(context.Background(), input3)
	if len(result3.HooksRun) != 0 {
		t.Error("expected no hooks to run for non-test file")
	}
}

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "file_path field",
			input:    []byte(`{"file_path": "/src/main.go"}`),
			expected: "/src/main.go",
		},
		{
			name:     "path field",
			input:    []byte(`{"path": "/src/other.go"}`),
			expected: "/src/other.go",
		},
		{
			name:     "file field",
			input:    []byte(`{"file": "/src/another.go"}`),
			expected: "/src/another.go",
		},
		{
			name:     "current_path field",
			input:    []byte(`{"current_path": "/src/current.go"}`),
			expected: "/src/current.go",
		},
		{
			name:     "file_path takes precedence",
			input:    []byte(`{"file_path": "/first.go", "path": "/second.go"}`),
			expected: "/first.go",
		},
		{
			name:     "empty input",
			input:    nil,
			expected: "",
		},
		{
			name:     "no path fields",
			input:    []byte(`{"other": "value"}`),
			expected: "",
		},
		{
			name:     "invalid json",
			input:    []byte(`not json`),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFilePath(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestClassifyToolKind(t *testing.T) {
	tests := []struct {
		name          string
		toolName      string
		toolCanonical string
		expected      ToolKind
	}{
		// CC tools
		{name: "CC Edit", toolName: "Edit", expected: ToolKindWrite},
		{name: "CC Write", toolName: "Write", expected: ToolKindWrite},
		{name: "CC MultiEdit", toolName: "MultiEdit", expected: ToolKindWrite},
		{name: "CC NotebookEdit", toolName: "NotebookEdit", expected: ToolKindWrite},
		{name: "CC Read", toolName: "Read", expected: ToolKindRead},
		{name: "CC Grep", toolName: "Grep", expected: ToolKindSearch},
		{name: "CC Glob", toolName: "Glob", expected: ToolKindSearch},
		{name: "CC Bash", toolName: "Bash", expected: ToolKindExec},
		{name: "CC Task", toolName: "Task", expected: ToolKindExec},
		{name: "CC TodoWrite", toolName: "TodoWrite", expected: ToolKindWrite},
		// Canonical tools
		{name: "canonical edit", toolCanonical: "edit.apply_patch", expected: ToolKindWrite},
		{name: "canonical fs.read", toolCanonical: "fs.read_file", expected: ToolKindRead},
		{name: "canonical fs.write", toolCanonical: "fs.write_file", expected: ToolKindWrite},
		{name: "canonical code.search", toolCanonical: "code.search", expected: ToolKindSearch},
		{name: "canonical text.grep", toolCanonical: "text.grep", expected: ToolKindSearch},
		{name: "canonical tests", toolCanonical: "tests.run", expected: ToolKindExec},
		{name: "canonical todo", toolCanonical: "todo.add", expected: ToolKindWrite},
		// Unknown
		{name: "unknown tool", toolName: "Unknown", expected: ToolKindAny},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyToolKind(tt.toolName, tt.toolCanonical)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
