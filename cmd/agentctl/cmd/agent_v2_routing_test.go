package cmd

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
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

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
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

func TestRunAgentList_DefaultEnvRoutesToV2(t *testing.T) {
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
		t.Fatalf("runAgentList() default-env error = %v", err)
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

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
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

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
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

func TestDispatchAgentCLICommand_ShadowRunsForNonMutatingCommand(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	t.Setenv("AGENTCTL_V2_SHADOW_COMMANDS", "list")
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	var v1Calls atomic.Int32
	shadowDone := make(chan struct{}, 1)

	err := dispatchAgentCLICommand(
		cmd,
		"list",
		"corr-shadow-list",
		func(context.Context) error {
			v1Calls.Add(1)
			return nil
		},
		func(context.Context) error {
			select {
			case shadowDone <- struct{}{}:
			default:
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dispatchAgentCLICommand() error = %v", err)
	}
	if v1Calls.Load() != 1 {
		t.Fatalf("v1 calls=%d want 1", v1Calls.Load())
	}

	select {
	case <-shadowDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shadow v2 call")
	}
}

func TestDispatchAgentCLICommand_ShadowMutatingRequiresOptIn(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	// Without opt-in: mutating command shadow should be sanitized out.
	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	t.Setenv("AGENTCTL_V2_SHADOW_COMMANDS", "kill")
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "")

	var blockedShadowCalls atomic.Int32
	err := dispatchAgentCLICommand(
		cmd,
		"kill",
		"corr-shadow-kill-blocked",
		func(context.Context) error { return nil },
		func(context.Context) error {
			blockedShadowCalls.Add(1)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dispatchAgentCLICommand() blocked case error = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if blockedShadowCalls.Load() != 0 {
		t.Fatalf("blocked shadow calls=%d want 0", blockedShadowCalls.Load())
	}

	// With opt-in: mutating command shadow is allowed.
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "true")
	allowedDone := make(chan struct{}, 1)
	err = dispatchAgentCLICommand(
		cmd,
		"kill",
		"corr-shadow-kill-allowed",
		func(context.Context) error { return nil },
		func(context.Context) error {
			select {
			case allowedDone <- struct{}{}:
			default:
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dispatchAgentCLICommand() allowed case error = %v", err)
	}
	select {
	case <-allowedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for allowed mutating shadow call")
	}
}

func TestDispatchAgentCLICommand_FreezeBlocksV1Path(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	t.Setenv("AGENTCTL_V2_FREEZE_V1_COMMANDS", "list")

	var v1Calls, v2Calls atomic.Int32
	err := dispatchAgentCLICommand(
		cmd,
		"list",
		"corr-freeze-list",
		func(context.Context) error { v1Calls.Add(1); return nil },
		func(context.Context) error { v2Calls.Add(1); return nil },
	)
	if err == nil {
		t.Fatal("dispatchAgentCLICommand() error = nil, want freeze error")
	}
	if !strings.Contains(err.Error(), "v1 path frozen for command list") {
		t.Fatalf("unexpected freeze error: %v", err)
	}
	if v1Calls.Load() != 0 || v2Calls.Load() != 0 {
		t.Fatalf("calls v1/v2 = %d/%d want 0/0", v1Calls.Load(), v2Calls.Load())
	}

	t.Setenv("AGENTCTL_V2_COMMANDS", "list")
	err = dispatchAgentCLICommand(
		cmd,
		"list",
		"corr-freeze-list-v2",
		func(context.Context) error { v1Calls.Add(1); return nil },
		func(context.Context) error { v2Calls.Add(1); return nil },
	)
	if err != nil {
		t.Fatalf("dispatchAgentCLICommand() v2 enabled error = %v", err)
	}
	if v2Calls.Load() == 0 {
		t.Fatal("expected v2 call when command is enabled despite freeze flag")
	}
}
