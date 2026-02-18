package api

import (
	stderrors "errors"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/ports"
)

// BuildOKEnvelope maps successful API results to the canonical envelope contract.
func BuildOKEnvelope(command string, data any, decision ports.Decision) envelope.Envelope {
	profiles := []string{"core/v1"}
	if decision == ports.DecisionV2 {
		profiles = []string{"core/v2"}
	}
	return envelope.OK(command, data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "api"
		m.Profiles = profiles
	}))
}

// BuildErrorEnvelope maps v2 errors to the canonical envelope error shape.
func BuildErrorEnvelope(command string, err error, decision ports.Decision) envelope.Envelope {
	profiles := []string{"core/v1"}
	if decision == ports.DecisionV2 {
		profiles = []string{"core/v2"}
	}
	var verr *v2errors.V2Error
	_ = stderrors.As(err, &verr)
	code := "ERUNTIME"
	msg := "runtime error"
	var data map[string]any
	if verr != nil {
		code = verr.EnvelopeCode()
		msg = verr.Error()
		data = map[string]any{
			"kind": verr.Kind,
		}
	} else if err != nil {
		msg = err.Error()
	}
	env := envelope.Error(command, code, msg, data)
	env.Meta.Source = "api"
	env.Meta.Profiles = profiles
	if env.Meta.TS == "" {
		env.Meta.TS = time.Now().UTC().Format(time.RFC3339)
	}
	return env
}
