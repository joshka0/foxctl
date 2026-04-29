package runner

import (
	"context"
	"fmt"
	"strings"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func (p *Pipeline) stageInitContext(ctx context.Context, st *executionState) *v2errors.V2Error {
	if st.in.MaxIterations <= 0 {
		st.in.MaxIterations = DefaultMaxIterations
	}
	st.in.Backend = run.NormalizeTurnBackend(st.in.Backend)
	if !run.IsSupportedTurnBackend(st.in.Backend) {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: fmt.Sprintf("unsupported backend %q", st.in.Backend),
			Fatal:   true,
			Details: map[string]any{
				"field":   "backend",
				"backend": string(st.in.Backend),
			},
		}
	}
	if strings.TrimSpace(st.in.Command) == "" {
		st.in.Command = "run"
	}
	if strings.TrimSpace(st.in.Mode) == "" {
		st.in.Mode = "reactive"
	}
	if strings.TrimSpace(st.in.RunID) == "" {
		st.in.RunID = fmt.Sprintf("run-%s", p.cfg.NewID())
	}
	if strings.TrimSpace(st.in.TurnID) == "" {
		st.in.TurnID = fmt.Sprintf("turn-%s", p.cfg.NewID())
	}
	if strings.TrimSpace(st.in.RequestID) == "" {
		st.in.RequestID = p.cfg.NewID()
	}
	if strings.TrimSpace(st.in.CorrelationID) == "" {
		st.in.CorrelationID = st.in.RequestID
	}
	if strings.TrimSpace(st.in.CausationID) == "" {
		st.in.CausationID = st.in.RequestID
	}
	st.out.TurnID = st.in.TurnID
	turnIndex := p.nextTurnIndex(ctx, st.in.RunID)
	st.turn = run.TurnRecord{
		ID:            st.in.TurnID,
		SessionID:     st.in.RunID,
		TurnIndex:     turnIndex,
		TraceID:       st.in.CorrelationID,
		RootSpanID:    fmt.Sprintf("span:%s:%s:root", st.in.RunID, st.in.TurnID),
		CorrelationID: st.in.CorrelationID,
		CausationID:   st.in.CausationID,
		RequestID:     st.in.RequestID,
		ActorID:       st.in.ActorID,
		Command:       st.in.Command,
		Prompt:        st.in.Prompt,
	}

	return p.appendEvent(ctx, st, StageInitContext, events.EventRunStarted, events.RunStartedPayload{
		Mode:   st.in.Mode,
		Prompt: st.in.Prompt,
	})
}
