package runner

import (
	"context"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
)

func (p *Pipeline) stageEmitEvents(ctx context.Context, st *executionState) *v2errors.V2Error {
	summary := strings.TrimSpace(st.out.Summary)
	st.out.Summary = summary
	if summary == "" {
		if st.out.Degraded {
			summary = "completed with degradation"
		} else {
			summary = "completed"
		}
		st.out.Summary = summary
	}
	return p.appendEvent(ctx, st, StageEmitEvents, events.EventRunCompleted, events.RunCompletedPayload{
		Summary: summary,
	})
}
