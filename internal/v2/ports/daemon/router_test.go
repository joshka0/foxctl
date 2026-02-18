package daemon_test

import (
	"context"
	"testing"

	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
	"github.com/jkatigb/agentctl/internal/v2/ports/daemon"
)

func TestDaemonDispatch_SpawnAskKillRouting(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("spawn,ask,kill")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	router := daemon.NewRouter(flags, nil)

	check := func(method string) {
		t.Helper()
		out, decision, err := daemon.DispatchMethod(context.Background(), router, method, "corr-daemon",
			func(context.Context) (string, error) { return "v1", nil },
			func(context.Context) (string, error) { return "v2", nil },
		)
		if err != nil {
			t.Fatalf("%s DispatchMethod() error = %v", method, err)
		}
		if out != "v2" || decision != v2ports.DecisionV2 {
			t.Fatalf("%s out/decision = %q/%q want v2/v2", method, out, decision)
		}
	}

	check("agent.spawn")
	check("agent.ask")
	check("agent.kill")
}

func TestDaemonDispatch_UnknownMethodObservedAndFallsBackToV1(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("spawn,ask,kill")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	var observedCommand string
	var observedDecision v2ports.Decision
	router := daemon.NewRouter(flags, func(command string, decision v2ports.Decision, _ string) {
		observedCommand = command
		observedDecision = decision
	})

	out, decision, err := daemon.DispatchMethod(context.Background(), router, "agent.resume", "corr-daemon",
		func(context.Context) (string, error) { return "v1", nil },
		func(context.Context) (string, error) { return "v2", nil },
	)
	if err != nil {
		t.Fatalf("DispatchMethod() error = %v", err)
	}
	if out != "v1" || decision != v2ports.DecisionV1 {
		t.Fatalf("out/decision = %q/%q want v1/v1", out, decision)
	}
	if observedCommand != "agent.resume" {
		t.Fatalf("observed command=%q want agent.resume", observedCommand)
	}
	if observedDecision != v2ports.DecisionV1 {
		t.Fatalf("observed decision=%q want v1", observedDecision)
	}
}
