package jido

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
)

func TestBuildToolCommand_WithInputAndWorkspace(t *testing.T) {
	t.Parallel()

	cmd, err := BuildToolCommand(ToolCommandSpec{
		BinaryPath: "bin/foxctl",
		Workspace:  "/repo",
	}, ToolCommandRequest{
		ToolName: "code/semantic_search",
		Input:    json.RawMessage(`{"query":"orchestration"}`),
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatalf("BuildToolCommand() error = %v", err)
	}

	if cmd.Path != "bin/foxctl" {
		t.Fatalf("path=%q want bin/foxctl", cmd.Path)
	}
	wantArgs := []string{
		"run",
		"code/semantic_search",
		"--workspace",
		"/repo",
		"--input-file",
		"-",
	}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("args=%v want %v", cmd.Args, wantArgs)
	}
	if got := string(cmd.Stdin); got != `{"query":"orchestration"}` {
		t.Fatalf("stdin=%q want JSON input", got)
	}
	if cmd.Timeout != 30*time.Second {
		t.Fatalf("timeout=%v want 30s", cmd.Timeout)
	}
}

func TestBuildToolCommand_Allowlist(t *testing.T) {
	t.Parallel()

	_, err := BuildToolCommand(ToolCommandSpec{
		AllowedTools: []string{"code/semantic_search"},
	}, ToolCommandRequest{
		ToolName: "memory/search",
	})
	if err == nil {
		t.Fatal("expected allowlist error")
	}
}

func TestBuildToolCommand_Defaults(t *testing.T) {
	t.Parallel()

	cmd, err := BuildToolCommand(ToolCommandSpec{}, ToolCommandRequest{
		ToolName: "memory/search",
	})
	if err != nil {
		t.Fatalf("BuildToolCommand() error = %v", err)
	}

	if cmd.Path != defaultAgentctlBinary {
		t.Fatalf("path=%q want %q", cmd.Path, defaultAgentctlBinary)
	}
	if cmd.Timeout != defaultToolCallTimeout {
		t.Fatalf("timeout=%v want %v", cmd.Timeout, defaultToolCallTimeout)
	}
}

func TestBuildToolCommand_AllowlistCanonicalizesAliases(t *testing.T) {
	t.Parallel()

	_, err := BuildToolCommand(ToolCommandSpec{
		AllowedTools: []string{"context/show"},
	}, ToolCommandRequest{
		ToolName: "context_show",
	})
	if err != nil {
		t.Fatalf("BuildToolCommand() alias error = %v", err)
	}
}

func TestNewDefaultToolCommandSpec_CompanionIncludesACAAndObsidian(t *testing.T) {
	t.Parallel()

	spec, err := NewDefaultToolCommandSpec(coretool.ProfileCompanion, "/repo", "bin/foxctl", nil, false)
	if err != nil {
		t.Fatalf("NewDefaultToolCommandSpec() error = %v", err)
	}
	for _, want := range []string{"context_show", "context_retrieve", "obsidian_index_search", "obsidian_read", "obsidian_related"} {
		if !slices.Contains(spec.AllowedTools, want) {
			t.Fatalf("allowed tools=%v want %s", spec.AllowedTools, want)
		}
	}
}

func TestNewDefaultToolCommandSpec_HeartwoodRequiresExtensions(t *testing.T) {
	t.Parallel()

	withoutExt, err := NewDefaultToolCommandSpec(coretool.ProfileOverseer, "/repo", "", nil, false)
	if err != nil {
		t.Fatalf("NewDefaultToolCommandSpec() error = %v", err)
	}
	if slices.Contains(withoutExt.AllowedTools, "heartwood_state") {
		t.Fatalf("unexpected heartwood tool in default spec: %v", withoutExt.AllowedTools)
	}

	specs := profiles.WithAllowedTools(nil, map[coretool.ProcessProfile][]string{
		coretool.ProfileOverseer: {"heartwood/state", "heartwood/action"},
	})
	withExt, err := NewDefaultToolCommandSpec(coretool.ProfileOverseer, "/repo", "", specs, true)
	if err != nil {
		t.Fatalf("NewDefaultToolCommandSpec() with extensions error = %v", err)
	}
	for _, want := range []string{"heartwood_state", "heartwood_action"} {
		if !slices.Contains(withExt.AllowedTools, want) {
			t.Fatalf("allowed tools=%v want %s", withExt.AllowedTools, want)
		}
	}
}
