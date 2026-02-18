package cli_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/core/ask"
	"github.com/jkatigb/agentctl/internal/v2/core/kill"
	"github.com/jkatigb/agentctl/internal/v2/core/list"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
	"github.com/jkatigb/agentctl/internal/v2/ports/cli"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

type spawnStub struct {
	calls int
	out   spawn.Response
}

func (s *spawnStub) Spawn(context.Context, spawn.Request) (spawn.Response, error) {
	s.calls++
	return s.out, nil
}

type askStub struct {
	calls int
	out   ask.Response
}

func (s *askStub) Ask(context.Context, ask.Request) (ask.Response, error) {
	s.calls++
	return s.out, nil
}

type runStub struct {
	calls int
	out   run.TurnOutput
}

func (s *runStub) Run(context.Context, run.TurnInput) (run.TurnOutput, error) {
	s.calls++
	return s.out, nil
}

type listStub struct {
	calls int
	out   list.Response
}

func (s *listStub) List(context.Context, list.Request) (list.Response, error) {
	s.calls++
	return s.out, nil
}

type killStub struct {
	calls int
	out   kill.Response
}

func (s *killStub) Kill(context.Context, kill.Request) (kill.Response, error) {
	s.calls++
	return s.out, nil
}

func TestCommandSurface_CLIWrappersAlignWithSupportedCommands(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands(strings.Join(portconfig.SupportedCommands(), ","))
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	router := cli.NewRouter(flags, nil)

	t.Run("spawn", func(t *testing.T) {
		v1 := &spawnStub{out: spawn.Response{Status: "v1"}}
		v2 := &spawnStub{out: spawn.Response{Status: "v2"}}
		out, decision, err := cli.Spawn(context.Background(), router, spawn.Request{
			RequestID: "req-spawn",
			Role:      "researcher",
			Prompt:    "test",
		}, v1, v2)
		if err != nil {
			t.Fatalf("Spawn() error = %v", err)
		}
		if out.Status != "v2" || decision != v2ports.DecisionV2 {
			t.Fatalf("Spawn out/decision = %q/%q want v2/v2", out.Status, decision)
		}
		if v1.calls != 0 || v2.calls != 1 {
			t.Fatalf("Spawn calls v1=%d v2=%d want 0/1", v1.calls, v2.calls)
		}
	})

	t.Run("ask", func(t *testing.T) {
		v1 := &askStub{out: ask.Response{Status: "v1"}}
		v2 := &askStub{out: ask.Response{Status: "v2"}}
		out, decision, err := cli.Ask(context.Background(), router, ask.Request{
			RequestID: "req-ask",
			Question:  "test?",
		}, v1, v2)
		if err != nil {
			t.Fatalf("Ask() error = %v", err)
		}
		if out.Status != "v2" || decision != v2ports.DecisionV2 {
			t.Fatalf("Ask out/decision = %q/%q want v2/v2", out.Status, decision)
		}
		if v1.calls != 0 || v2.calls != 1 {
			t.Fatalf("Ask calls v1=%d v2=%d want 0/1", v1.calls, v2.calls)
		}
	})

	t.Run("run", func(t *testing.T) {
		v1 := &runStub{out: run.TurnOutput{Summary: "v1"}}
		v2 := &runStub{out: run.TurnOutput{Summary: "v2"}}
		out, decision, err := cli.Run(context.Background(), router, run.TurnInput{
			RequestID: "req-run",
			RunID:     "run-1",
		}, v1, v2)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if out.Summary != "v2" || decision != v2ports.DecisionV2 {
			t.Fatalf("Run out/decision = %q/%q want v2/v2", out.Summary, decision)
		}
		if v1.calls != 0 || v2.calls != 1 {
			t.Fatalf("Run calls v1=%d v2=%d want 0/1", v1.calls, v2.calls)
		}
	})

	t.Run("list", func(t *testing.T) {
		v1 := &listStub{out: list.Response{Count: 1}}
		v2 := &listStub{out: list.Response{Count: 2}}
		out, decision, err := cli.List(context.Background(), router, list.Request{Limit: 10}, v1, v2)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if out.Count != 2 || decision != v2ports.DecisionV2 {
			t.Fatalf("List out/decision = %d/%q want 2/v2", out.Count, decision)
		}
		if v1.calls != 0 || v2.calls != 1 {
			t.Fatalf("List calls v1=%d v2=%d want 0/1", v1.calls, v2.calls)
		}
	})

	t.Run("kill", func(t *testing.T) {
		v1 := &killStub{out: kill.Response{Status: "v1"}}
		v2 := &killStub{out: kill.Response{Status: "v2"}}
		out, decision, err := cli.Kill(context.Background(), router, kill.Request{
			RequestID: "req-kill",
			RunID:     "run-1",
		}, v1, v2)
		if err != nil {
			t.Fatalf("Kill() error = %v", err)
		}
		if out.Status != "v2" || decision != v2ports.DecisionV2 {
			t.Fatalf("Kill out/decision = %q/%q want v2/v2", out.Status, decision)
		}
		if v1.calls != 0 || v2.calls != 1 {
			t.Fatalf("Kill calls v1=%d v2=%d want 0/1", v1.calls, v2.calls)
		}
	})
}

func TestCommandSurface_EnabledCommandsSortedDeterministically(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands(strings.Join(portconfig.SupportedCommands(), ","))
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}

	commands := flags.Commands()
	want := portconfig.SupportedCommands()
	if !slices.Equal(commands, want) {
		t.Fatalf("Commands() mismatch: got %v want %v", commands, want)
	}
}
