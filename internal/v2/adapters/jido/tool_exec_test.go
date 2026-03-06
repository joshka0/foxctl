package jido

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestBuildToolCommand_WithInputAndWorkspace(t *testing.T) {
	t.Parallel()

	cmd, err := BuildToolCommand(ToolCommandSpec{
		BinaryPath: "bin/agentctl",
		Workspace:  "/repo",
	}, ToolCommandRequest{
		ToolName: "code/semantic_search",
		Input:    json.RawMessage(`{"query":"orchestration"}`),
		Timeout:  30 * time.Second,
	})
	if err != nil {
		t.Fatalf("BuildToolCommand() error = %v", err)
	}

	if cmd.Path != "bin/agentctl" {
		t.Fatalf("path=%q want bin/agentctl", cmd.Path)
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
