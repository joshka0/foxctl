package runner

import (
	"context"
	"fmt"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func (p *Pipeline) stageInitContext(ctx context.Context, st *executionState) *v2errors.V2Error {
	if st.in.MaxIterations <= 0 {
		st.in.MaxIterations = DefaultMaxIterations
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
	st.turn = run.TurnRecord{
		ID:            st.in.TurnID,
		SessionID:     st.in.RunID,
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
		Mode: st.in.Mode,
	})
}
