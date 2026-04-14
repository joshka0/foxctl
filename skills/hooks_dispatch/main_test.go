package main

import (
	"errors"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/stretchr/testify/assert"
)

// Tests for buildStepsInfo helper

func TestBuildStepsInfo_Empty(t *testing.T) {
	result := buildStepsInfo(nil)
	assert.Empty(t, result)
}

func TestBuildStepsInfo_SingleResult(t *testing.T) {
	results := []hooks.HookResult{
		{
			HookID:   "hook-1",
			Skill:    "hooks/test_feedback",
			Duration: 100 * time.Millisecond,
			Output: hooks.Output{
				Decision: hooks.DecisionNone,
			},
		},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 1)
	assert.Equal(t, 1, steps[0]["idx"])
	assert.Equal(t, "hook-1", steps[0]["hook_id"])
	assert.Equal(t, "hooks/test_feedback", steps[0]["skill"])
	assert.Equal(t, "none", steps[0]["decision"])
	assert.Equal(t, int64(100), steps[0]["duration_ms"])
}

func TestBuildStepsInfo_MultipleResults(t *testing.T) {
	results := []hooks.HookResult{
		{HookID: "hook-1", Skill: "skill-1", Output: hooks.Output{Decision: hooks.DecisionNone}},
		{HookID: "hook-2", Skill: "skill-2", Output: hooks.Output{Decision: hooks.DecisionApprove}},
		{HookID: "hook-3", Skill: "skill-3", Output: hooks.Output{Decision: hooks.DecisionBlock}},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 3)
	assert.Equal(t, 1, steps[0]["idx"])
	assert.Equal(t, 2, steps[1]["idx"])
	assert.Equal(t, 3, steps[2]["idx"])
}

func TestBuildStepsInfo_WithError(t *testing.T) {
	results := []hooks.HookResult{
		{
			HookID: "hook-1",
			Skill:  "hooks/broken",
			Error:  errors.New("skill failed"),
			Output: hooks.Output{Decision: hooks.DecisionNone},
		},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 1)
	assert.Equal(t, "skill failed", steps[0]["error"])
}

func TestBuildStepsInfo_WithFailOpen(t *testing.T) {
	results := []hooks.HookResult{
		{
			HookID:   "hook-1",
			Skill:    "hooks/test",
			FailOpen: true,
			Output:   hooks.Output{Decision: hooks.DecisionNone},
		},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 1)
	assert.Equal(t, true, steps[0]["fail_open"])
}

func TestBuildStepsInfo_IncludesHookOutput(t *testing.T) {
	output := hooks.Output{
		Decision: hooks.DecisionNone,
		Context:  "test context",
	}
	results := []hooks.HookResult{
		{HookID: "hook-1", Skill: "skill-1", Output: output},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 1)
	hookOutput, ok := steps[0]["hook_output"].(hooks.Output)
	assert.True(t, ok)
	assert.Equal(t, "test context", hookOutput.Context)
}

func TestBuildStepsInfo_BlockDecision(t *testing.T) {
	results := []hooks.HookResult{
		{
			HookID: "blocker",
			Skill:  "hooks/guard",
			Output: hooks.Output{
				Decision: hooks.DecisionBlock,
				Reason:   "Blocked by guard",
			},
		},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 1)
	assert.Equal(t, "block", steps[0]["decision"])
}

func TestBuildStepsInfo_ApproveDecision(t *testing.T) {
	results := []hooks.HookResult{
		{
			HookID: "approver",
			Skill:  "hooks/allow",
			Output: hooks.Output{
				Decision: hooks.DecisionApprove,
			},
		},
	}

	steps := buildStepsInfo(results)

	assert.Len(t, steps, 1)
	assert.Equal(t, "approve", steps[0]["decision"])
}

// Tests for buildMatchedHooksInfo helper

func TestBuildMatchedHooksInfo_EmptyConfig(t *testing.T) {
	cfg := &hooks.Config{}
	in := hooks.Input{Event: hooks.EventPreToolUse}

	result := buildMatchedHooksInfo(cfg, in)

	assert.Empty(t, result)
}

func TestBuildMatchedHooksInfo_WithMatchingHook(t *testing.T) {
	cfg := &hooks.Config{
		Hooks: []hooks.HookDef{
			{
				ID:       "test-hook",
				Enabled:  true,
				Event:    hooks.EventPreToolUse,
				Priority: 100,
				Run:      []hooks.HookRunEntry{{Skill: "hooks/test"}},
			},
		},
	}
	in := hooks.Input{Event: hooks.EventPreToolUse}

	result := buildMatchedHooksInfo(cfg, in)

	assert.Len(t, result, 1)
	assert.Equal(t, "test-hook", result[0]["id"])
	assert.Equal(t, "PreToolUse", result[0]["event"])
	assert.Equal(t, 100, result[0]["priority"])
}

func TestBuildMatchedHooksInfo_WithNonMatchingEvent(t *testing.T) {
	cfg := &hooks.Config{
		Hooks: []hooks.HookDef{
			{
				ID:      "test-hook",
				Enabled: true,
				Event:   hooks.EventPostToolUse,
				Run:     []hooks.HookRunEntry{{Skill: "hooks/test"}},
			},
		},
	}
	in := hooks.Input{Event: hooks.EventPreToolUse}

	result := buildMatchedHooksInfo(cfg, in)

	assert.Empty(t, result)
}

func TestBuildMatchedHooksInfo_WithMatchFilter(t *testing.T) {
	cfg := &hooks.Config{
		Hooks: []hooks.HookDef{
			{
				ID:      "bash-hook",
				Enabled: true,
				Event:   hooks.EventPreToolUse,
				Match:   &hooks.HookMatcher{ToolName: "Bash"},
				Run:     []hooks.HookRunEntry{{Skill: "hooks/bash_guard"}},
			},
		},
	}
	in := hooks.Input{Event: hooks.EventPreToolUse, ToolName: "Bash"}

	result := buildMatchedHooksInfo(cfg, in)

	assert.Len(t, result, 1)
	assert.Equal(t, "bash-hook", result[0]["id"])
	assert.NotNil(t, result[0]["match"])
}

func TestBuildMatchedHooksInfo_FilterByToolName(t *testing.T) {
	cfg := &hooks.Config{
		Hooks: []hooks.HookDef{
			{
				ID:      "read-hook",
				Enabled: true,
				Event:   hooks.EventPreToolUse,
				Match:   &hooks.HookMatcher{ToolName: "Read"},
				Run:     []hooks.HookRunEntry{{Skill: "hooks/read_guard"}},
			},
		},
	}
	// Input has different tool name
	in := hooks.Input{Event: hooks.EventPreToolUse, ToolName: "Bash"}

	result := buildMatchedHooksInfo(cfg, in)

	assert.Empty(t, result)
}

func TestBuildMatchedHooksInfo_IncludesRunConfig(t *testing.T) {
	cfg := &hooks.Config{
		Hooks: []hooks.HookDef{
			{
				ID:      "test-hook",
				Enabled: true,
				Event:   hooks.EventPreToolUse,
				Run:     []hooks.HookRunEntry{{Skill: "hooks/custom", Config: map[string]any{"key": "value"}}},
			},
		},
	}
	in := hooks.Input{Event: hooks.EventPreToolUse}

	result := buildMatchedHooksInfo(cfg, in)

	assert.Len(t, result, 1)
	runCfg, ok := result[0]["run"].([]hooks.HookRunEntry)
	assert.True(t, ok)
	assert.Len(t, runCfg, 1)
	assert.Equal(t, "hooks/custom", runCfg[0].Skill)
}

func TestBuildMatchedHooksInfo_DisabledHook(t *testing.T) {
	cfg := &hooks.Config{
		Hooks: []hooks.HookDef{
			{
				ID:      "disabled-hook",
				Enabled: false,
				Event:   hooks.EventPreToolUse,
				Run:     []hooks.HookRunEntry{{Skill: "hooks/test"}},
			},
		},
	}
	in := hooks.Input{Event: hooks.EventPreToolUse}

	result := buildMatchedHooksInfo(cfg, in)

	// Disabled hooks should not be returned by HooksForEvent
	assert.Empty(t, result)
}

// Tests for skill name constant

func TestSkillName(t *testing.T) {
	assert.Equal(t, "hooks/dispatch", skillName)
}

// Tests for hooks.Decision values used in the skill

func TestDecisionValues(t *testing.T) {
	assert.Equal(t, hooks.Decision("none"), hooks.DecisionNone)
	assert.Equal(t, hooks.Decision("block"), hooks.DecisionBlock)
	assert.Equal(t, hooks.Decision("approve"), hooks.DecisionApprove)
}

// Tests for hooks.Action types used in the skill

func TestActionTypes(t *testing.T) {
	assert.Equal(t, hooks.ActionType("enqueue_context"), hooks.ActionEnqueueContext)
	assert.Equal(t, hooks.ActionType("inject_context"), hooks.ActionInjectContext)
}

// Tests for hooks.Event used in validation

func TestEventValidation(t *testing.T) {
	validEvents := []hooks.Event{
		hooks.EventPreToolUse,
		hooks.EventPostToolUse,
		hooks.EventSessionStart,
		hooks.EventSessionEnd,
		hooks.EventStopRequested,
		hooks.EventUserPromptSubmit,
	}

	for _, event := range validEvents {
		assert.True(t, event.IsValid(), "event %s should be valid", event)
	}

	invalidEvent := hooks.Event("InvalidEvent")
	assert.False(t, invalidEvent.IsValid())
}

// Tests for output structures

func TestOutputStructure(t *testing.T) {
	output := hooks.Output{
		Decision: hooks.DecisionNone,
		Context:  "Injected context",
		Reason:   "Hook completed",
		Actions: []hooks.Action{
			{Type: hooks.ActionInjectContext, Text: "Context text"},
		},
	}

	assert.Equal(t, hooks.DecisionNone, output.Decision)
	assert.Equal(t, "Injected context", output.Context)
	assert.Equal(t, "Hook completed", output.Reason)
	assert.Len(t, output.Actions, 1)
}

func TestActionStructure(t *testing.T) {
	action := hooks.Action{
		Type:       hooks.ActionEnqueueContext,
		Text:       "Context to enqueue",
		Priority:   1,
		Source:     "test-source",
		TTLSeconds: 60,
		Dedupe:     true,
	}

	assert.Equal(t, hooks.ActionEnqueueContext, action.Type)
	assert.Equal(t, "Context to enqueue", action.Text)
	assert.Equal(t, 1, action.Priority)
	assert.Equal(t, "test-source", action.Source)
	assert.Equal(t, 60, action.TTLSeconds)
	assert.True(t, action.Dedupe)
}

// Tests for HookDef structure

func TestHookDefStructure(t *testing.T) {
	hook := hooks.HookDef{
		ID:       "test-hook",
		Enabled:  true,
		Event:    hooks.EventPreToolUse,
		Priority: 50,
		Match:    &hooks.HookMatcher{ToolName: "Edit"},
		Run:      []hooks.HookRunEntry{{Skill: "hooks/file_guard"}},
	}

	assert.Equal(t, "test-hook", hook.ID)
	assert.True(t, hook.Enabled)
	assert.Equal(t, hooks.EventPreToolUse, hook.Event)
	assert.Equal(t, 50, hook.Priority)
	assert.Equal(t, "Edit", hook.Match.ToolName)
	assert.Len(t, hook.Run, 1)
}

// Tests for HookRunEntry structure

func TestHookRunEntryStructure(t *testing.T) {
	ephemeral := true
	entry := hooks.HookRunEntry{
		Skill:     "hooks/custom_skill",
		TimeoutMS: 5000,
		FailOpen:  true,
		Ephemeral: &ephemeral,
		Config:    map[string]any{"option": "value"},
	}

	assert.Equal(t, "hooks/custom_skill", entry.Skill)
	assert.Equal(t, 5000, entry.TimeoutMS)
	assert.True(t, entry.FailOpen)
	assert.True(t, *entry.Ephemeral)
	assert.Equal(t, "value", entry.Config["option"])
}

// Tests for HookResult structure

func TestHookResultStructure(t *testing.T) {
	result := hooks.HookResult{
		HookID:   "hook-123",
		Skill:    "hooks/test",
		Duration: 250 * time.Millisecond,
		Output: hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   "All checks passed",
		},
		FailOpen: false,
	}

	assert.Equal(t, "hook-123", result.HookID)
	assert.Equal(t, "hooks/test", result.Skill)
	assert.Equal(t, 250*time.Millisecond, result.Duration)
	assert.Equal(t, hooks.DecisionApprove, result.Output.Decision)
	assert.False(t, result.FailOpen)
}

func TestHookResultWithError(t *testing.T) {
	result := hooks.HookResult{
		HookID:   "hook-fail",
		Skill:    "hooks/broken",
		Error:    errors.New("execution failed"),
		FailOpen: true,
	}

	assert.NotNil(t, result.Error)
	assert.Equal(t, "execution failed", result.Error.Error())
	assert.True(t, result.FailOpen)
}

// Tests for HookMatcher structure

func TestHookMatcherStructure(t *testing.T) {
	matcher := hooks.HookMatcher{
		ActorID:       "agent-.*",
		ToolName:      "Write",
		ToolCanonical: "fs.write",
		ToolKind:      hooks.ToolKindWrite,
		PathRegex:     `\.go$`,
	}

	assert.Equal(t, "agent-.*", matcher.ActorID)
	assert.Equal(t, "Write", matcher.ToolName)
	assert.Equal(t, "fs.write", matcher.ToolCanonical)
	assert.Equal(t, hooks.ToolKindWrite, matcher.ToolKind)
	assert.Equal(t, `\.go$`, matcher.PathRegex)
}

// Tests for ToolKind values

func TestToolKindValues(t *testing.T) {
	assert.Equal(t, hooks.ToolKind("read"), hooks.ToolKindRead)
	assert.Equal(t, hooks.ToolKind("write"), hooks.ToolKindWrite)
	assert.Equal(t, hooks.ToolKind("exec"), hooks.ToolKindExec)
	assert.Equal(t, hooks.ToolKind("search"), hooks.ToolKindSearch)
}

// Tests for Result structure

func TestResultStructure(t *testing.T) {
	result := hooks.Result{
		Output:      hooks.Output{Decision: hooks.DecisionNone},
		HooksRun:    []string{"hook-1", "hook-2"},
		HookResults: []hooks.HookResult{{HookID: "hook-1"}},
		Blocked:     false,
		BlockedBy:   "",
		Duration:    500 * time.Millisecond,
	}

	assert.Equal(t, hooks.DecisionNone, result.Output.Decision)
	assert.Len(t, result.HooksRun, 2)
	assert.Len(t, result.HookResults, 1)
	assert.False(t, result.Blocked)
	assert.Empty(t, result.BlockedBy)
}

func TestResultWithBlock(t *testing.T) {
	result := hooks.Result{
		Output:    hooks.Output{Decision: hooks.DecisionBlock, Reason: "Policy violation"},
		HooksRun:  []string{"guard-hook"},
		Blocked:   true,
		BlockedBy: "guard-hook",
	}

	assert.True(t, result.Blocked)
	assert.Equal(t, "guard-hook", result.BlockedBy)
	assert.Equal(t, hooks.DecisionBlock, result.Output.Decision)
}
