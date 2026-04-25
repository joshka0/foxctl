package runner

import (
	"context"
	"fmt"
	"strings"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

func (p *Pipeline) stageModelCall(ctx context.Context, st *executionState) *v2errors.V2Error {
	if len(st.modelMessages) == 0 {
		st.modelMessages = []ModelMessage{{Role: "user", Content: st.in.Prompt}}
	}
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
			Tools:         cloneToolDefs(st.tools),
			Messages:      cloneModelMessages(st.modelMessages),
		})
		if err != nil {
			return asStageError(StageModelCall, err, true)
		}

		st.out.Iterations = iter
		if strings.TrimSpace(resp.Message) != "" {
			st.out.Summary = strings.TrimSpace(resp.Message)
			iterRecord.Message.Text = st.out.Summary
		}

		if len(resp.ToolCalls) > 0 {
			st.modelMessages = append(st.modelMessages, ModelMessage{
				Role:      "assistant",
				Content:   strings.TrimSpace(resp.Message),
				ToolCalls: cloneRunToolCalls(resp.ToolCalls),
			})
		} else if strings.TrimSpace(resp.Message) != "" {
			st.modelMessages = append(st.modelMessages, ModelMessage{
				Role:    "assistant",
				Content: strings.TrimSpace(resp.Message),
			})
		}

		for _, tc := range resp.ToolCalls {
			callID, toolResult, err := p.invokeTool(ctx, st, iter, &iterRecord, tc)
			if err != nil {
				return err
			}
			st.modelMessages = append(st.modelMessages, ModelMessage{
				Role:       "tool",
				Content:    toolResult.Output,
				ToolCallID: callID,
				Name:       strings.TrimSpace(tc.Name),
			})
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

func (p *Pipeline) invokeTool(ctx context.Context, st *executionState, iteration int, iterRecord *run.IterationRecord, call run.ToolCall) (string, ToolResult, *v2errors.V2Error) {
	if strings.TrimSpace(call.Name) == "" {
		return "", ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "tool call name is required",
			Fatal:   true,
		}
	}
	if iterRecord == nil {
		return "", ToolResult{}, &v2errors.V2Error{
			Kind:    v2errors.ErrInternal,
			Message: "missing iteration record",
			Fatal:   true,
		}
	}

	callIndex := len(iterRecord.ToolCalls) + 1
	callID := fmt.Sprintf("tc-%d-%d", iteration, callIndex)
	if id := strings.TrimSpace(call.ID); id != "" {
		callID = id
	}
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
		return callID, ToolResult{}, err
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
		return callID, res, emitErr
	}
	iterRecord.ToolCalls = append(iterRecord.ToolCalls, callRecord)

	st.out.ToolCalls++
	if err != nil {
		return callID, res, &v2errors.V2Error{
			Kind:      v2errors.ErrToolFailed,
			Message:   "tool execution failed",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}
	return callID, res, nil
}

func cloneModelMessages(in []ModelMessage) []ModelMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]ModelMessage, len(in))
	for i, msg := range in {
		out[i] = msg
		out[i].ToolCalls = cloneRunToolCalls(msg.ToolCalls)
	}
	return out
}

func cloneRunToolCalls(in []run.ToolCall) []run.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]run.ToolCall, len(in))
	for i, call := range in {
		out[i] = call
		if len(call.Args) > 0 {
			out[i].Args = append([]byte(nil), call.Args...)
		}
	}
	return out
}
