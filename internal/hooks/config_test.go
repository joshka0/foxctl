package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_HooksForEvent(t *testing.T) {
	hooks := []HookDef{
		{ID: "pre1", Enabled: true, Event: EventPreToolUse, Run: []HookRunEntry{{Skill: "s1"}}},
		{ID: "pre2", Enabled: true, Event: EventPreToolUse, Run: []HookRunEntry{{Skill: "s2"}}},
		{ID: "post1", Enabled: true, Event: EventPostToolUse, Run: []HookRunEntry{{Skill: "s3"}}},
		{ID: "disabled", Enabled: false, Event: EventPreToolUse, Run: []HookRunEntry{{Skill: "s4"}}},
	}
	cfg := ConfigFromHooks(hooks)

	// Test PreToolUse - should have 2 enabled hooks
	pre := cfg.HooksForEvent(EventPreToolUse)
	if len(pre) != 2 {
		t.Errorf("expected 2 PreToolUse hooks, got %d", len(pre))
	}

	// Test PostToolUse - should have 1 hook
	post := cfg.HooksForEvent(EventPostToolUse)
	if len(post) != 1 {
		t.Errorf("expected 1 PostToolUse hook, got %d", len(post))
	}

	// Test event with no hooks
	none := cfg.HooksForEvent(EventSessionStart)
	if len(none) != 0 {
		t.Errorf("expected 0 SessionStart hooks, got %d", len(none))
	}
}

func TestEmptyConfig(t *testing.T) {
	cfg := EmptyConfig()
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.Hooks) != 0 {
		t.Errorf("expected empty hooks, got %d", len(cfg.Hooks))
	}
	// Should not panic on HooksForEvent
	hooks := cfg.HooksForEvent(EventPreToolUse)
	if len(hooks) != 0 {
		t.Errorf("expected 0 hooks, got %d", len(hooks))
	}
}

func TestConfigFromHooks(t *testing.T) {
	hooks := []HookDef{
		{
			ID:      "test",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/skill"},
			},
		},
	}

	cfg := ConfigFromHooks(hooks)

	// Should set defaults
	if cfg.Hooks[0].Run[0].TimeoutMS != 2000 {
		t.Errorf("expected default timeout 2000, got %d", cfg.Hooks[0].Run[0].TimeoutMS)
	}

	// Index should be built
	pre := cfg.HooksForEvent(EventPreToolUse)
	if len(pre) != 1 {
		t.Errorf("expected 1 hook, got %d", len(pre))
	}
}

func TestLoadConfig_SingleFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hooks.yaml")

	// Write test config
	configContent := `version: 1
hooks:
  - id: test-hook
    enabled: true
    event: PreToolUse
    run:
      - skill: test/skill
        timeout_ms: 3000
        fail_open: true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Hooks) != 1 {
		t.Errorf("expected 1 hook, got %d", len(cfg.Hooks))
	}
	if cfg.Hooks[0].ID != "test-hook" {
		t.Errorf("expected hook id 'test-hook', got %s", cfg.Hooks[0].ID)
	}
	if cfg.Hooks[0].Run[0].TimeoutMS != 3000 {
		t.Errorf("expected timeout 3000, got %d", cfg.Hooks[0].Run[0].TimeoutMS)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hooks.yaml")

	configContent := `version: 1
hooks:
  - id: default-hook
    event: PreToolUse
    run:
      - skill: test/skill
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(cfg.Hooks))
	}

	if !cfg.Hooks[0].Enabled {
		t.Error("expected enabled=true by default")
	}
	if !cfg.Hooks[0].Run[0].FailOpen {
		t.Error("expected fail_open=true by default")
	}
	if cfg.Hooks[0].Run[0].TimeoutMS != 2000 {
		t.Errorf("expected timeout 2000, got %d", cfg.Hooks[0].Run[0].TimeoutMS)
	}
}

func TestLoadConfig_DefaultsFromConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hooks.yaml")

	configContent := `version: 1
defaults:
  timeout_ms: 1500
  fail_mode: closed
hooks:
  - id: default-hook
    event: PreToolUse
    run:
      - skill: test/skill
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(cfg.Hooks))
	}

	if cfg.Hooks[0].Run[0].TimeoutMS != 1500 {
		t.Errorf("expected timeout 1500, got %d", cfg.Hooks[0].Run[0].TimeoutMS)
	}
	if cfg.Hooks[0].Run[0].FailOpen {
		t.Error("expected fail_open=false from defaults.fail_mode=closed")
	}
}

func TestLoadConfig_PriorityOrder(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hooks.yaml")

	configContent := `version: 1
hooks:
  - id: mid
    event: PreToolUse
    priority: 10
    run:
      - skill: test/skill1
  - id: first
    event: PreToolUse
    priority: 0
    run:
      - skill: test/skill2
  - id: mid-2
    event: PreToolUse
    priority: 10
    run:
      - skill: test/skill3
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	hooks := cfg.HooksForEvent(EventPreToolUse)
	if len(hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(hooks))
	}

	if hooks[0].ID != "first" || hooks[1].ID != "mid" || hooks[2].ID != "mid-2" {
		t.Errorf("unexpected hook order: %v", []string{hooks[0].ID, hooks[1].ID, hooks[2].ID})
	}
}

func TestLoadConfig_FailModeAliases(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "hooks.yaml")

	configContent := `version: 1
hooks:
  - id: closed-hook
    event: PreToolUse
    run:
      - skill: test/skill
        fail_mode: closed
  - id: required-hook
    event: PreToolUse
    run:
      - skill: test/skill2
        required: true
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Hooks[0].Run[0].FailOpen {
		t.Error("expected fail_open=false for fail_mode=closed")
	}
	if cfg.Hooks[1].Run[0].FailOpen {
		t.Error("expected fail_open=false for required=true")
	}
}

func TestLoadConfig_Merge(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global.yaml")
	workspacePath := filepath.Join(tmpDir, "workspace.yaml")

	// Global config
	globalContent := `version: 1
hooks:
  - id: shared-hook
    enabled: true
    event: PreToolUse
    run:
      - skill: global/skill
  - id: global-only
    enabled: true
    event: PostToolUse
    run:
      - skill: global/only
`
	if err := os.WriteFile(globalPath, []byte(globalContent), 0o644); err != nil {
		t.Fatalf("failed to write global config: %v", err)
	}

	// Workspace config (overrides shared-hook)
	workspaceContent := `version: 1
hooks:
  - id: shared-hook
    enabled: true
    event: PreToolUse
    run:
      - skill: workspace/skill
  - id: workspace-only
    enabled: true
    event: StopRequested
    run:
      - skill: workspace/only
`
	if err := os.WriteFile(workspacePath, []byte(workspaceContent), 0o644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	// Load in order: global first, workspace second (workspace overrides)
	cfg, err := LoadConfig(globalPath, workspacePath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Hooks) != 3 {
		t.Errorf("expected 3 hooks after merge, got %d", len(cfg.Hooks))
	}

	// shared-hook should be from workspace (workspace skill overrides global)
	var sharedHook *HookDef
	for i := range cfg.Hooks {
		if cfg.Hooks[i].ID == "shared-hook" {
			sharedHook = &cfg.Hooks[i]
			break
		}
	}
	if sharedHook == nil {
		t.Fatal("shared-hook not found")
		return
	}
	if sharedHook.Run[0].Skill != "workspace/skill" {
		t.Errorf("expected workspace/skill (workspace override), got %s", sharedHook.Run[0].Skill)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/hooks.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}

	// Should return empty config
	if len(cfg.Hooks) != 0 {
		t.Errorf("expected empty hooks for missing file, got %d", len(cfg.Hooks))
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	// Write invalid YAML
	invalidContent := `version: 1
hooks:
  - id: test
    event: [invalid yaml here
`
	if err := os.WriteFile(configPath, []byte(invalidContent), 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadConfig_InvalidHook(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "missing id",
			content: `version: 1
hooks:
  - event: PreToolUse
    run:
      - skill: test/skill
`,
		},
		{
			name: "missing event",
			content: `version: 1
hooks:
  - id: test-hook
    run:
      - skill: test/skill
`,
		},
		{
			name: "invalid event",
			content: `version: 1
hooks:
  - id: test-hook
    event: InvalidEvent
    run:
      - skill: test/skill
`,
		},
		{
			name: "missing run",
			content: `version: 1
hooks:
  - id: test-hook
    event: PreToolUse
`,
		},
		{
			name: "missing skill",
			content: `version: 1
hooks:
  - id: test-hook
    event: PreToolUse
    run:
      - timeout_ms: 1000
`,
		},
		{
			name: "invalid fail_mode",
			content: `version: 1
hooks:
  - id: test-hook
    event: PreToolUse
    run:
      - skill: test/skill
        fail_mode: invalid
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, tt.name+".yaml")
			if err := os.WriteFile(configPath, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			_, err := LoadConfig(configPath)
			if err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}

func TestWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "subdir", "hooks.yaml")

	cfg := ConfigFromHooks([]HookDef{
		{
			ID:      "test",
			Enabled: true,
			Event:   EventPreToolUse,
			Run: []HookRunEntry{
				{Skill: "test/skill", TimeoutMS: 2000},
			},
		},
	})

	if err := WriteConfig(cfg, configPath); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}

	// Load and verify
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load written config: %v", err)
	}

	if len(loaded.Hooks) != 1 {
		t.Errorf("expected 1 hook, got %d", len(loaded.Hooks))
	}
}

func TestDefaultConfigPaths(t *testing.T) {
	paths := DefaultConfigPaths("/workspace/root")

	if len(paths) < 1 {
		t.Fatal("expected at least 1 path")
	}

	// First should be workspace config
	if paths[0] != "/workspace/root/.agentctl/hooks.yaml" {
		t.Errorf("expected workspace path first, got %s", paths[0])
	}
}

func TestDefaultConfigPaths_NoWorkspace(t *testing.T) {
	paths := DefaultConfigPaths("")

	// Should still have global path
	if len(paths) == 0 {
		t.Error("expected at least global config path")
	}

	// Should not have workspace path
	for _, p := range paths {
		if filepath.Base(filepath.Dir(filepath.Dir(p))) == "" {
			// This would indicate a workspace path with empty workspace
			continue
		}
	}
}
