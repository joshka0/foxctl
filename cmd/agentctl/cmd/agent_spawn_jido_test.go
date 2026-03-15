package cmd

import (
	"slices"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	runtimetoolnames "github.com/jkatigb/agentctl/internal/v2/runtime/toolnames"
)

func TestV2ProcessProfileForAgentRole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		role string
		want coretool.ProcessProfile
	}{
		{role: "overseer", want: coretool.ProfileOverseer},
		{role: "companion", want: coretool.ProfileCompanion},
		{role: "researcher", want: coretool.ProfileWorker},
		{role: "coder", want: coretool.ProfileWorker},
		{role: "reviewer", want: coretool.ProfileWorker},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.role, func(t *testing.T) {
			t.Parallel()
			if got := v2ProcessProfileForAgentRole(tc.role); got != tc.want {
				t.Fatalf("v2ProcessProfileForAgentRole(%q)=%q want %q", tc.role, got, tc.want)
			}
		})
	}
}

func TestBuildJidoPluginConfig_UsesSharedDefaultToolSpec(t *testing.T) {
	t.Parallel()

	cfg, err := buildJidoPluginConfig("companion", "/tmp/agentctl", "/repo")
	if err != nil {
		t.Fatalf("buildJidoPluginConfig() error = %v", err)
	}

	if got := cfg["binary"]; got != "/tmp/agentctl" {
		t.Fatalf("binary=%v want /tmp/agentctl", got)
	}
	if got := cfg["workspace"]; got != "/repo" {
		t.Fatalf("workspace=%v want /repo", got)
	}
	if got := cfg["transport"]; got != "daemon_rpc" {
		t.Fatalf("transport=%v want daemon_rpc", got)
	}
	if got := cfg["daemon"]; got != true {
		t.Fatalf("daemon=%v want true", got)
	}

	toolCmd, ok := cfg["tool_command"].(map[string]any)
	if !ok {
		t.Fatalf("tool_command=%T want map[string]any", cfg["tool_command"])
	}
	if got := toolCmd["profile"]; got != string(coretool.ProfileCompanion) {
		t.Fatalf("tool_command.profile=%v want %s", got, coretool.ProfileCompanion)
	}
	allowed, ok := toolCmd["allowed_tools"].([]string)
	if !ok {
		t.Fatalf("tool_command.allowed_tools=%T want []string", toolCmd["allowed_tools"])
	}
	canonicalAllowed := make([]string, 0, len(allowed))
	for _, name := range allowed {
		canonicalAllowed = append(canonicalAllowed, runtimetoolnames.Canonical(name))
	}
	for _, want := range []string{"context/show", "context/retrieve", "obsidian/read"} {
		if !slices.Contains(canonicalAllowed, runtimetoolnames.Canonical(want)) {
			t.Fatalf("allowed_tools=%v want %s", allowed, want)
		}
	}
	if slices.Contains(canonicalAllowed, runtimetoolnames.Canonical("heartwood/state")) {
		t.Fatalf("allowed_tools=%v should not include extension tool", allowed)
	}
	if got := toolCmd["default_timeout_ms"]; got != int64(120000) {
		t.Fatalf("tool_command.default_timeout_ms=%v want 120000", got)
	}
}

func TestBuildJidoInitialState_IncludesTaskContinuity(t *testing.T) {
	t.Parallel()

	state := buildJidoInitialState(agent.Agent{
		Prompt:          "Investigate storage",
		MaxIterations:   7,
		MaxAutoTurns:    2,
		ThinkInterval:   30,
		MemoryRetention: "workspace",
		ExecutionLayer:  "jido",
	}, "/repo", map[string]any{
		"task_id":    "T-1",
		"task_title": "Investigate storage",
		"summary":    "Task continuity summary",
	})

	if got := state["prompt"]; got != "Investigate storage" {
		t.Fatalf("prompt=%v", got)
	}
	if got := state["workspace_root"]; got != "/repo" {
		t.Fatalf("workspace_root=%v", got)
	}
	continuity, ok := state["task_continuity"].(map[string]any)
	if !ok {
		t.Fatalf("task_continuity=%T", state["task_continuity"])
	}
	if got := continuity["task_id"]; got != "T-1" {
		t.Fatalf("task_id=%v", got)
	}
}
