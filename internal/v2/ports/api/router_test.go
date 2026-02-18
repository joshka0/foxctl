package api_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
	"github.com/jkatigb/agentctl/internal/v2/ports/api"
	"github.com/jkatigb/agentctl/internal/v2/ports/cli"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

func TestCLI_API_ParityShellTest_Ask(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("ask")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	cliRouter := cli.NewRouter(flags, nil)
	apiRouter := api.NewRouter(flags, nil)

	_, cliDecision, cliErr := cli.Dispatch(context.Background(), cliRouter, "ask", "corr-ask",
		func(context.Context) (string, error) { return "legacy", nil },
		func(context.Context) (string, error) { return "v2", nil },
	)
	if cliErr != nil {
		t.Fatalf("cli Dispatch() error = %v", cliErr)
	}
	_, apiDecision, apiErr := api.Dispatch(context.Background(), apiRouter, "ask", "corr-ask",
		func(context.Context) (string, error) { return "legacy", nil },
		func(context.Context) (string, error) { return "v2", nil },
	)
	if apiErr != nil {
		t.Fatalf("api Dispatch() error = %v", apiErr)
	}
	if cliDecision != v2ports.DecisionV2 || apiDecision != v2ports.DecisionV2 {
		t.Fatalf("decisions cli/api = %q/%q want v2/v2", cliDecision, apiDecision)
	}
}

func TestRouter_EnvelopeContract_ParityV1V2(t *testing.T) {
	t.Parallel()

	okV1 := api.BuildOKEnvelope("agent/ask", map[string]any{"reply": "ok"}, v2ports.DecisionV1)
	okV2 := api.BuildOKEnvelope("agent/ask", map[string]any{"reply": "ok"}, v2ports.DecisionV2)
	errV1 := api.BuildErrorEnvelope("agent/ask", &v2errors.V2Error{
		Kind:    v2errors.ErrValidation,
		Message: "bad input",
		Fatal:   true,
	}, v2ports.DecisionV1)
	errV2 := api.BuildErrorEnvelope("agent/ask", &v2errors.V2Error{
		Kind:    v2errors.ErrValidation,
		Message: "bad input",
		Fatal:   true,
	}, v2ports.DecisionV2)

	for name, env := range map[string]envelope.Envelope{
		"ok-v1":  okV1,
		"ok-v2":  okV2,
		"err-v1": errV1,
		"err-v2": errV2,
	} {
		if validateErr := envelope.Validate(env); validateErr != nil {
			t.Fatalf("%s Validate() error = %v", name, validateErr)
		}
		if env.Version != envelope.Version {
			t.Fatalf("%s version=%d want %d", name, env.Version, envelope.Version)
		}
		if env.Meta.TS == "" {
			t.Fatalf("%s meta.ts is empty", name)
		}
	}

	if okV1.Status != okV2.Status {
		t.Fatalf("ok status mismatch v1=%q v2=%q", okV1.Status, okV2.Status)
	}
	if errV1.Error.Code != errV2.Error.Code {
		t.Fatalf("error code mismatch v1=%q v2=%q", errV1.Error.Code, errV2.Error.Code)
	}
}

func TestBuildErrorEnvelope_UnwrapsWrappedV2Error(t *testing.T) {
	t.Parallel()

	base := &v2errors.V2Error{
		Kind:    v2errors.ErrPolicyViolation,
		Message: "blocked",
		Fatal:   true,
	}
	wrapped := fmt.Errorf("transport wrapper: %w", base)

	env := api.BuildErrorEnvelope("agent/ask", wrapped, v2ports.DecisionV2)
	if env.Error.Code != "EPOLICY" {
		t.Fatalf("error.code=%q want EPOLICY", env.Error.Code)
	}
}

func TestRouter_AllSupportedCommandsCanRouteV2(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands(strings.Join(portconfig.SupportedCommands(), ","))
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	router := api.NewRouter(flags, nil)

	for _, command := range portconfig.SupportedCommands() {
		command := command
		t.Run(command, func(t *testing.T) {
			out, decision, dispatchErr := api.Dispatch(context.Background(), router, command, "corr-"+command,
				func(context.Context) (string, error) { return "v1", nil },
				func(context.Context) (string, error) { return "v2", nil },
			)
			if dispatchErr != nil {
				t.Fatalf("Dispatch(%s) error = %v", command, dispatchErr)
			}
			if out != "v2" || decision != v2ports.DecisionV2 {
				t.Fatalf("Dispatch(%s) out/decision = %q/%q want v2/v2", command, out, decision)
			}
		})
	}
}
