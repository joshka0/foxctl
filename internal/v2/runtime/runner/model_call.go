package runner

import (
	"context"
	"fmt"
	"strings"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	"github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

func (p *Pipeline) stageModelCall(ctx context.Context, st *executionState) *v2errors.V2Error {
	for iter := 1; iter <= st.in.MaxIterations; iter++ {
		if err := ctx.Err(); err != nil {
			return contextError(StageModelCall, err)
		}

		iterRecord := run.IterationRecord{
			TurnID:         st.in.TurnID,
			IterationIndex: iter,
			TraceID:        st.turn.TraceID,
			SpanID:         fmt.Sprintf("%s:iter:%d", st.turn.RootSpanID, iter),
			ParentSpanID:   st.turn.RootSpanID,
			Message: run.MessageRef{
				ID:   fmt.Sprintf("msg-iter-%d", iter),
				Role: "assistant",
			},
		}

		resp, err := p.cfg.Model.Complete(ctx, ModelInput{
			Prompt:        st.in.Prompt,
			Iteration:     iter,
			MaxIterations: st.in.MaxIterations,
		})
		if err != nil {
			return asStageError(StageModelCall, err, true)
		}

		st.out.Iterations = iter
		if strings.TrimSpace(resp.Message) != "" {
			st.out.Summary = strings.TrimSpace(resp.Message)
			iterRecord.Message.Text = st.out.Summary
		}

		for _, tc := range resp.ToolCalls {
			if err := p.invokeTool(ctx, st, iter, &iterRecord, tc); err != nil {
				return err
			}
		}

		st.turn.Iterations = append(st.turn.Iterations, iterRecord)

		if resp.Done {
			return nil
		}
	}

	// Max iteration budget is a degraded, non-fatal outcome.
	verr := &v2errors.V2Error{
		Kind:    v2errors.ErrStageFailed,
		Message: "max iterations reached",
		Fatal:   false,
	}
	p.recordStageFailure(st, StageModelCall, verr)
	if emitErr := p.emitStageFailed(ctx, st, StageModelCall, verr); emitErr != nil {
		return emitErr
	}
	return nil
}

func (p *Pipeline) invokeTool(ctx context.Context, st *executionState, iteration int, iterRecord *run.IterationRecord, call run.ToolCall) *v2errors.V2Error {
	if strings.TrimSpace(call.Name) == "" {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "tool call name is required",
			Fatal:   true,
		}
	}
	if iterRecord == nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrInternal,
			Message: "missing iteration record",
			Fatal:   true,
		}
	}

	callIndex := len(iterRecord.ToolCalls) + 1
	callID := fmt.Sprintf("tc-%d-%d", iteration, callIndex)
	callRecord := run.ToolCallRecord{
		CallID:         callID,
		IterationIndex: iteration,
		TraceID:        st.turn.TraceID,
		SpanID:         fmt.Sprintf("%s:tool:%d", iterRecord.SpanID, callIndex),
		ParentSpanID:   iterRecord.SpanID,
		Name:           strings.TrimSpace(call.Name),
		ArgsJSON:       append([]byte(nil), call.Args...),
	}

	if err := p.appendEvent(ctx, st, StageModelCall, events.EventToolInvoked, events.ToolInvokedPayload{
		Name:           call.Name,
		IterationIndex: iteration,
	}); err != nil {
		return err
	}

	res, err := p.cfg.ToolExecutor.Execute(ctx, call.Name, call.Args)
	var status string
	if err != nil {
		status = "error"
	} else {
		status = strings.TrimSpace(res.Status)
		if status == "" {
			status = "ok"
		}
	}
	resultKind := "tool_result"
	resultText := strings.TrimSpace(res.Output)
	if err != nil {
		resultKind = "tool_error"
		resultText = strings.TrimSpace(err.Error())
	}
	callRecord.Status = status
	callRecord.ResultRef = run.ArtifactRef{
		ID:   fmt.Sprintf("artifact-%s", callID),
		Kind: resultKind,
		Text: resultText,
	}
	if emitErr := p.appendEvent(ctx, st, StageModelCall, events.EventToolResponded, events.ToolRespondedPayload{
		Name:   call.Name,
		Status: status,
	}); emitErr != nil {
		return emitErr
	}
	iterRecord.ToolCalls = append(iterRecord.ToolCalls, callRecord)

	st.out.ToolCalls++
	if err != nil {
		return &v2errors.V2Error{
			Kind:      v2errors.ErrToolFailed,
			Message:   "tool execution failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}
	return nil
}
