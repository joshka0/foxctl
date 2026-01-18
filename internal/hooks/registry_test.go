package hooks

import (
	"context"
	"testing"
)

// mockRunner is a test double for HookRunner.
type mockRunner struct {
	runFn func(ctx context.Context, hookDef HookDef, input Input) (Output, error)
	name  string
}

func (m *mockRunner) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	if m.runFn != nil {
		return m.runFn(ctx, hookDef, input)
	}
	return NewApprove(m.name+" approved", nil), nil
}

func TestRegistry_GetRunner_ShellHook(t *testing.T) {
	shellRunner := &mockRunner{name: "shell"}
	skillRunner := &mockRunner{name: "skill"}

	cfg := EmptyConfig()
	registry := &Registry{
		config:        cfg,
		shellRunner:   shellRunner,
		skillRunner:   skillRunner,
		defaultRunner: skillRunner,
	}

	// Hook with .sh extension should use shell runner
	hookDef := HookDef{
		ID:      "shell-hook",
		Enabled: true,
		Run: []HookRunEntry{
			{Skill: "/path/to/hook.sh"},
		},
	}

	runner := registry.GetRunner(hookDef)
	if runner != shellRunner {
		t.Errorf("expected shell runner for .sh hook")
	}
}

func TestRegistry_GetRunner_SkillHook(t *testing.T) {
	shellRunner := &mockRunner{name: "shell"}
	skillRunner := &mockRunner{name: "skill"}

	cfg := EmptyConfig()
	registry := &Registry{
		config:        cfg,
		shellRunner:   shellRunner,
		skillRunner:   skillRunner,
		defaultRunner: skillRunner,
	}

	// Hook without .sh extension should use skill runner
	hookDef := HookDef{
		ID:      "skill-hook",
		Enabled: true,
		Run: []HookRunEntry{
			{Skill: "hooks/task_guard"},
		},
	}

	runner := registry.GetRunner(hookDef)
	if runner != skillRunner {
		t.Errorf("expected skill runner for non-.sh hook")
	}
}

func TestRegistry_Run(t *testing.T) {
	cfg := EmptyConfig()

	called := false
	skillRunner := &mockRunner{
		name: "skill",
		runFn: func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
			called = true
			return NewApprove("executed", nil), nil
		},
	}

	registry := &Registry{
		config:        cfg,
		skillRunner:   skillRunner,
		defaultRunner: skillRunner,
	}

	hookDef := HookDef{
		ID:      "test-hook",
		Enabled: true,
		Run: []HookRunEntry{
			{Skill: "test/skill"},
		},
	}
	input := Input{Event: EventPreToolUse}

	output, err := registry.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !called {
		t.Error("expected runner to be called")
	}
	if output.Decision != DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
}

func TestRegistryRunner_DelegatesToRegistry(t *testing.T) {
	cfg := EmptyConfig()

	executed := false
	skillRunner := &mockRunner{
		runFn: func(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
			executed = true
			return NewBlock("blocked by test"), nil
		},
	}

	registry := &Registry{
		config:        cfg,
		skillRunner:   skillRunner,
		defaultRunner: skillRunner,
	}

	registryRunner := &RegistryRunner{Registry: registry}

	hookDef := HookDef{
		ID:      "test",
		Enabled: true,
		Run:     []HookRunEntry{{Skill: "test/skill"}},
	}
	input := Input{Event: EventPreToolUse}

	output, err := registryRunner.Run(context.Background(), hookDef, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !executed {
		t.Error("expected runner to execute")
	}
	if output.Decision != DecisionBlock {
		t.Errorf("expected block, got %s", output.Decision)
	}
}

func TestIsShellHook(t *testing.T) {
	tests := []struct {
		skillName string
		isShell   bool
	}{
		{"/path/to/hook.sh", true},
		{"hook.sh", true},
		{"/absolute/path/script.sh", true},
		{"hooks/task_guard", false},
		{"test/skill", false},
		{"memory/query", false},
		{".sh", true}, // Edge case: just extension
		{"hook.shell", false},
		{"hooks/shell-like", false},
	}

	for _, tt := range tests {
		t.Run(tt.skillName, func(t *testing.T) {
			result := isShellHook(tt.skillName)
			if result != tt.isShell {
				t.Errorf("isShellHook(%q) = %v, want %v", tt.skillName, result, tt.isShell)
			}
		})
	}
}

func TestNewRegistry_Defaults(t *testing.T) {
	cfg := EmptyConfig()
	registry := NewRegistry(cfg, RegistryOptions{
		SkillsDir: "/test/skills",
	})

	if registry.config != cfg {
		t.Error("config not set")
	}
	if registry.shellRunner == nil {
		t.Error("shell runner not set")
	}
	if registry.skillRunner == nil {
		t.Error("skill runner not set")
	}
}

func TestNewRegistry_CustomRunners(t *testing.T) {
	cfg := EmptyConfig()
	customShell := &mockRunner{name: "custom-shell"}
	customSkill := &mockRunner{name: "custom-skill"}

	registry := NewRegistry(cfg, RegistryOptions{
		ShellRunner: customShell,
		SkillRunner: customSkill,
	})

	if registry.shellRunner != customShell {
		t.Error("custom shell runner not used")
	}
	if registry.skillRunner != customSkill {
		t.Error("custom skill runner not used")
	}
}

func TestNewDispatcherWithRegistry(t *testing.T) {
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

	// This should not panic
	d := NewDispatcherWithRegistry(cfg, "/test/skills")
	if d == nil {
		t.Error("expected dispatcher, got nil")
	}
}
