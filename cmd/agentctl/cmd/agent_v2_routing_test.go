package cmd

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunAgentSpawn_RoutesByFlag(t *testing.T) {
	origV1 := runAgentSpawnV1Fn
	origV2 := runAgentSpawnV2Fn
	defer func() {
		runAgentSpawnV1Fn = origV1
		runAgentSpawnV2Fn = origV2
	}()

	var v1Calls, v2Calls int
	runAgentSpawnV1Fn = func(*cobra.Command, []string) error { v1Calls++; return nil }
	runAgentSpawnV2Fn = func(*cobra.Command, []string) error { v2Calls++; return nil }

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	if err := runAgentSpawn(cmd, nil); err != nil {
		t.Fatalf("runAgentSpawn() v1 error = %v", err)
	}
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}

	v1Calls, v2Calls = 0, 0
	t.Setenv("AGENTCTL_V2_COMMANDS", "spawn")
	if err := runAgentSpawn(cmd, nil); err != nil {
		t.Fatalf("runAgentSpawn() v2 error = %v", err)
	}
	if v1Calls != 0 || v2Calls != 1 {
		t.Fatalf("v1/v2 calls = %d/%d, want 0/1", v1Calls, v2Calls)
	}
}

func TestRunAgentList_RoutesByFlag(t *testing.T) {
	origV1 := runAgentListV1Fn
	origV2 := runAgentListV2Fn
	defer func() {
		runAgentListV1Fn = origV1
		runAgentListV2Fn = origV2
	}()

	var v1Calls, v2Calls int
	runAgentListV1Fn = func(*cobra.Command, []string) error { v1Calls++; return nil }
	runAgentListV2Fn = func(*cobra.Command, []string) error { v2Calls++; return nil }

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	if err := runAgentList(cmd, nil); err != nil {
		t.Fatalf("runAgentList() v1 error = %v", err)
	}
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}

	v1Calls, v2Calls = 0, 0
	t.Setenv("AGENTCTL_V2_COMMANDS", "list")
	if err := runAgentList(cmd, nil); err != nil {
		t.Fatalf("runAgentList() v2 error = %v", err)
	}
	if v1Calls != 0 || v2Calls != 1 {
		t.Fatalf("v1/v2 calls = %d/%d, want 0/1", v1Calls, v2Calls)
	}
}

func TestRunAgentKill_RoutesByFlag(t *testing.T) {
	origV1 := runAgentKillV1Fn
	origV2 := runAgentKillV2Fn
	defer func() {
		runAgentKillV1Fn = origV1
		runAgentKillV2Fn = origV2
	}()

	var v1Calls, v2Calls int
	runAgentKillV1Fn = func(*cobra.Command, []string) error { v1Calls++; return nil }
	runAgentKillV2Fn = func(*cobra.Command, []string) error { v2Calls++; return nil }

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	args := []string{"agent-1"}

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	if err := runAgentKill(cmd, args); err != nil {
		t.Fatalf("runAgentKill() v1 error = %v", err)
	}
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}

	v1Calls, v2Calls = 0, 0
	t.Setenv("AGENTCTL_V2_COMMANDS", "kill")
	if err := runAgentKill(cmd, args); err != nil {
		t.Fatalf("runAgentKill() v2 error = %v", err)
	}
	if v1Calls != 0 || v2Calls != 1 {
		t.Fatalf("v1/v2 calls = %d/%d, want 0/1", v1Calls, v2Calls)
	}
}

func TestRunAgentRun_RoutesByFlag(t *testing.T) {
	origV1 := runAgentRunV1Fn
	origV2 := runAgentRunV2Fn
	defer func() {
		runAgentRunV1Fn = origV1
		runAgentRunV2Fn = origV2
	}()

	var v1Calls, v2Calls int
	runAgentRunV1Fn = func(*cobra.Command, []string) error { v1Calls++; return nil }
	runAgentRunV2Fn = func(*cobra.Command, []string) error { v2Calls++; return nil }

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	args := []string{"agent-1"}

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	if err := runAgentRun(cmd, args); err != nil {
		t.Fatalf("runAgentRun() v1 error = %v", err)
	}
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}

	v1Calls, v2Calls = 0, 0
	t.Setenv("AGENTCTL_V2_COMMANDS", "run")
	if err := runAgentRun(cmd, args); err != nil {
		t.Fatalf("runAgentRun() v2 error = %v", err)
	}
	if v1Calls != 0 || v2Calls != 1 {
		t.Fatalf("v1/v2 calls = %d/%d, want 0/1", v1Calls, v2Calls)
	}
}

func TestDispatchAgentCLICommand_InvalidEnvFallsBackToV1(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "unknown-command")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	var v1Calls, v2Calls int
	err := dispatchAgentCLICommand(
		cmd,
		"list",
		"corr-fallback",
		func(context.Context) error { v1Calls++; return nil },
		func(context.Context) error { v2Calls++; return nil },
	)
	if err != nil {
		t.Fatalf("dispatchAgentCLICommand() error = %v", err)
	}
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}
}
