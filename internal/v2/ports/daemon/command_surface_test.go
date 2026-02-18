package daemon_test

import (
	"slices"
	"testing"

	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
	"github.com/jkatigb/agentctl/internal/v2/ports/daemon"
)

func TestCommandSurface_AlignsWithSupportedV2Commands(t *testing.T) {
	t.Parallel()

	methodToCommand := map[string]string{
		"agent.spawn": "spawn",
		"agent.ask":   "ask",
		"agent.run":   "run",
		"agent.list":  "list",
		"agent.kill":  "kill",
	}

	mapped := make([]string, 0, len(methodToCommand))
	for method, wantCommand := range methodToCommand {
		gotCommand, ok := daemon.CommandForMethod(method)
		if !ok {
			t.Fatalf("CommandForMethod(%q) returned not ok", method)
		}
		if gotCommand != wantCommand {
			t.Fatalf("CommandForMethod(%q)=%q want %q", method, gotCommand, wantCommand)
		}
		mapped = append(mapped, gotCommand)
	}
	slices.Sort(mapped)

	if !slices.Equal(mapped, portconfig.SupportedCommands()) {
		t.Fatalf("mapped command surface mismatch: got %v, want %v", mapped, portconfig.SupportedCommands())
	}

	if got, ok := daemon.CommandForMethod("agent.resume"); ok || got != "" {
		t.Fatalf("CommandForMethod(agent.resume)=%q,%v want \"\",false", got, ok)
	}
}
