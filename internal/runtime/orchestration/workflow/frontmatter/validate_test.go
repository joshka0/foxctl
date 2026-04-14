package frontmatter

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Polling.IntervalMS != 30000 {
		t.Fatalf("polling.interval_ms = %d, want 30000", cfg.Polling.IntervalMS)
	}
	if cfg.Codex.Command == "" {
		t.Fatal("codex.command empty")
	}
	if cfg.Workspace.Root == "" {
		t.Fatal("workspace.root empty")
	}
}

func TestDecodeConfig_ResolvesEnvAndDefaults(t *testing.T) {
	front := map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"api_key":      "$MY_LINEAR_KEY",
			"project_slug": "AC-123",
		},
		"polling": map[string]any{
			"interval_ms": "45000",
		},
		"workspace": map[string]any{
			"root": "~/demo",
		},
		"agent": map[string]any{
			"max_concurrent_agents_by_state": map[string]any{
				"In Progress": 2,
				"Todo":        "3",
				"Bad":         0,
			},
		},
	}
	getenv := func(key string) string {
		if key == "MY_LINEAR_KEY" {
			return "lin-key"
		}
		return ""
	}

	cfg, err := DecodeConfig(front, DecodeOptions{
		Getenv:  getenv,
		BaseDir: "/tmp/workflow-base",
	})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if cfg.Tracker.APIKey != "lin-key" {
		t.Fatalf("tracker.api_key = %q, want lin-key", cfg.Tracker.APIKey)
	}
	if cfg.Polling.IntervalMS != 45000 {
		t.Fatalf("polling.interval_ms = %d, want 45000", cfg.Polling.IntervalMS)
	}
	if cfg.Workspace.Root == "~/demo" {
		t.Fatalf("workspace.root was not expanded: %q", cfg.Workspace.Root)
	}
	if got := cfg.Agent.MaxConcurrentAgentsByState["in progress"]; got != 2 {
		t.Fatalf("state map in progress = %d, want 2", got)
	}
	if got := cfg.Agent.MaxConcurrentAgentsByState["todo"]; got != 3 {
		t.Fatalf("state map todo = %d, want 3", got)
	}
}

func TestDecodeConfig_LinearAPIKeyFallback(t *testing.T) {
	front := map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"project_slug": "AC-55",
		},
	}
	getenv := func(key string) string {
		if key == "LINEAR_API_KEY" {
			return "fallback-key"
		}
		return ""
	}
	cfg, err := DecodeConfig(front, DecodeOptions{Getenv: getenv})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if cfg.Tracker.APIKey != "fallback-key" {
		t.Fatalf("tracker.api_key = %q, want fallback-key", cfg.Tracker.APIKey)
	}
}

func TestDecodeConfig_ExactEnvTokenOnly(t *testing.T) {
	front := map[string]any{
		"workspace": map[string]any{
			"root": "$HOME/demo",
		},
	}
	getenv := func(key string) string {
		if key == "HOME" {
			return "/home/demo"
		}
		return ""
	}
	cfg, err := DecodeConfig(front, DecodeOptions{
		Getenv:  getenv,
		BaseDir: "/base",
	})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if cfg.Workspace.Root == "" {
		t.Fatal("workspace.root empty")
	}
	// Inline env expansion should still work via expandPath.
	if got, want := cfg.Workspace.Root, filepath.Clean("/home/demo/demo"); got != want {
		t.Fatalf("workspace.root=%q want %q", got, want)
	}
}

func TestDecodeConfig_UnresolvedInlinePathEnvErrors(t *testing.T) {
	front := map[string]any{
		"workspace": map[string]any{
			"root": "$MISSING/demo",
		},
	}
	getenv := func(string) string { return "" }
	if _, err := DecodeConfig(front, DecodeOptions{
		Getenv:  getenv,
		BaseDir: "/base",
	}); err == nil {
		t.Fatal("expected error for unresolved inline env")
	}
}

func TestDecodeConfig_RelativeWorkspaceRootUsesBaseDir(t *testing.T) {
	front := map[string]any{
		"workspace": map[string]any{
			"root": "relative/work",
		},
	}
	cfg, err := DecodeConfig(front, DecodeOptions{
		BaseDir: "/tmp/base",
	})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	want := filepath.Clean("/tmp/base/relative/work")
	if cfg.Workspace.Root != want {
		t.Fatalf("workspace.root=%q want %q", cfg.Workspace.Root, want)
	}
}

func TestDecodeConfig_RelativeWorkspaceRootRequiresBaseDir(t *testing.T) {
	front := map[string]any{
		"workspace": map[string]any{
			"root": "relative/work",
		},
	}
	if _, err := DecodeConfig(front, DecodeOptions{}); err == nil {
		t.Fatal("expected error for relative root without base dir")
	}
}

func TestDecodeConfig_StringStateLists(t *testing.T) {
	front := map[string]any{
		"tracker": map[string]any{
			"active_states":   "Todo, In Progress",
			"terminal_states": []any{"Done", "Closed"},
		},
	}
	cfg, err := DecodeConfig(front, DecodeOptions{})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if len(cfg.Tracker.ActiveStates) != 2 {
		t.Fatalf("active_states len=%d, want 2", len(cfg.Tracker.ActiveStates))
	}
	if len(cfg.Tracker.TerminalStates) != 2 {
		t.Fatalf("terminal_states len=%d, want 2", len(cfg.Tracker.TerminalStates))
	}
}

func TestDecodeConfig_StallTimeoutAllowsDisable(t *testing.T) {
	front := map[string]any{
		"codex": map[string]any{
			"stall_timeout_ms": 0,
		},
	}
	cfg, err := DecodeConfig(front, DecodeOptions{})
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if cfg.Codex.StallTimeoutMS != 0 {
		t.Fatalf("stall_timeout_ms = %d, want 0", cfg.Codex.StallTimeoutMS)
	}
}

func TestDecodeConfig_InvalidIntegerErrors(t *testing.T) {
	front := map[string]any{
		"polling": map[string]any{
			"interval_ms": "abc",
		},
	}
	if _, err := DecodeConfig(front, DecodeOptions{}); err == nil {
		t.Fatal("expected decode error for invalid integer")
	}

	front = map[string]any{
		"polling": map[string]any{
			"interval_ms": 1.5,
		},
	}
	if _, err := DecodeConfig(front, DecodeOptions{}); err == nil {
		t.Fatal("expected decode error for float integer field")
	}
}

func TestValidateDispatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tracker.Kind = "linear"
	cfg.Tracker.APIKey = "k"
	cfg.Tracker.ProjectSlug = "ABC"
	cfg.Codex.Command = "codex app-server"
	if err := ValidateDispatch(cfg); err != nil {
		t.Fatalf("ValidateDispatch() error = %v", err)
	}
}

func TestValidateDispatch_Errors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tracker.Kind = ""
	if err := ValidateDispatch(cfg); err == nil {
		t.Fatal("expected missing kind error")
	}

	cfg.Tracker.Kind = "unknown"
	if err := ValidateDispatch(cfg); err == nil {
		t.Fatal("expected unsupported kind error")
	}

	cfg.Tracker.Kind = "linear"
	cfg.Tracker.APIKey = ""
	if err := ValidateDispatch(cfg); !errors.Is(err, errMissingTrackerAPIKey) {
		t.Fatalf("error = %v, want errMissingTrackerAPIKey", err)
	}

	cfg.Tracker.APIKey = "x"
	cfg.Tracker.ProjectSlug = ""
	if err := ValidateDispatch(cfg); !errors.Is(err, errMissingProjectSlug) {
		t.Fatalf("error = %v, want errMissingProjectSlug", err)
	}
}

func TestExpandPath(t *testing.T) {
	got, err := expandPath("~/tmp-workflow", "", nil)
	if err != nil {
		t.Fatalf("expandPath() error = %v", err)
	}
	if runtime.GOOS == "windows" {
		if filepath.VolumeName(got) == "" {
			t.Fatalf("expanded path appears invalid on windows: %q", got)
		}
		return
	}
	if got == "~/tmp-workflow" {
		t.Fatalf("path was not expanded: %q", got)
	}
}
