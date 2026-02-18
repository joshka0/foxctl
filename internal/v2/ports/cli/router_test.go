package cli_test

import (
	"context"
	"testing"

	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
	"github.com/jkatigb/agentctl/internal/v2/ports/cli"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

func TestRouter_DefaultV1WhenFlagUnset(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	router := cli.NewRouter(flags, nil)

	v1Called := 0
	v2Called := 0
	out, decision, err := cli.Dispatch(context.Background(), router, "spawn", "corr-1",
		func(context.Context) (string, error) {
			v1Called++
			return "v1", nil
		},
		func(context.Context) (string, error) {
			v2Called++
			return "v2", nil
		},
	)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if out != "v1" {
		t.Fatalf("out=%q want v1", out)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision=%q want v1", decision)
	}
	if v1Called != 1 || v2Called != 0 {
		t.Fatalf("calls v1=%d v2=%d want 1/0", v1Called, v2Called)
	}
}

func TestRouter_SingleCommandOptInToV2(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("ask")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	router := cli.NewRouter(flags, nil)

	askOut, askDecision, askErr := cli.Dispatch(context.Background(), router, "ask", "corr-2",
		func(context.Context) (string, error) { return "v1", nil },
		func(context.Context) (string, error) { return "v2", nil },
	)
	if askErr != nil {
		t.Fatalf("ask Dispatch() error = %v", askErr)
	}
	if askOut != "v2" || askDecision != v2ports.DecisionV2 {
		t.Fatalf("ask out/decision = %q/%q want v2/v2", askOut, askDecision)
	}

	spawnOut, spawnDecision, spawnErr := cli.Dispatch(context.Background(), router, "spawn", "corr-3",
		func(context.Context) (string, error) { return "v1", nil },
		func(context.Context) (string, error) { return "v2", nil },
	)
	if spawnErr != nil {
		t.Fatalf("spawn Dispatch() error = %v", spawnErr)
	}
	if spawnOut != "v1" || spawnDecision != v2ports.DecisionV1 {
		t.Fatalf("spawn out/decision = %q/%q want v1/v1", spawnOut, spawnDecision)
	}
}

func TestKillCommandRollbackToV1WhenNotEnabled(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("spawn,ask")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	router := cli.NewRouter(flags, nil)

	v1Called := 0
	v2Called := 0
	out, decision, err := cli.Dispatch(context.Background(), router, "kill", "corr-kill",
		func(context.Context) (string, error) {
			v1Called++
			return "legacy-kill", nil
		},
		func(context.Context) (string, error) {
			v2Called++
			return "v2-kill", nil
		},
	)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision=%q want v1", decision)
	}
	if out != "legacy-kill" {
		t.Fatalf("out=%q want legacy-kill", out)
	}
	if v1Called != 1 || v2Called != 0 {
		t.Fatalf("calls v1=%d v2=%d want 1/0", v1Called, v2Called)
	}
}
