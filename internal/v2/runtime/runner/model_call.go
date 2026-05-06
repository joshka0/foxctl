package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/toolnames"
)

func (p *Pipeline) stageModelCall(ctx context.Context, st *executionState) *v2errors.V2Error {
	switch st.in.Backend {
	case run.TurnBackendRLMREPL:
		return p.stageModelCallRLMREPL(ctx, st)
	case run.TurnBackendLLMChat:
		return p.stageModelCallLLMChat(ctx, st)
	default:
		return &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: fmt.Sprintf("unsupported backend %q", st.in.Backend),
			Fatal:   true,
		}
	}
}

func (p *Pipeline) stageModelCallLLMChat(ctx context.Context, st *executionState) *v2errors.V2Error {
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

		modelInput := ModelInput{
			Prompt:        st.in.Prompt,
			Iteration:     iter,
			MaxIterations: st.in.MaxIterations,
			Tools:         cloneToolDefs(st.tools),
			Messages:      cloneModelMessages(st.modelMessages),
		}
		modelKey := run.EffectKey{
			RunID:          st.in.RunID,
			RequestID:      st.in.RequestID,
			TurnID:         st.in.TurnID,
			IterationIndex: iter,
		}
		resp, verr := p.modelResponse(ctx, modelKey, modelInput)
		if verr != nil {
			return verr
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

func (p *Pipeline) modelResponse(ctx context.Context, key run.EffectKey, input ModelInput) (ModelResponse, *v2errors.V2Error) {
	var inputJSON json.RawMessage
	if p.cfg.EffectJournal != nil {
		var encodeErr error
		inputJSON, encodeErr = encodeJSON(input)
		if encodeErr != nil {
			return ModelResponse{}, asStageError(StageModelCall, encodeErr, true)
		}
		record, err := p.cfg.EffectJournal.GetModelEffect(ctx, key)
		if err == nil {
			if !modelEffectMatchesInput(record, inputJSON) {
				return ModelResponse{}, effectConflict("model effect input does not match current model input", map[string]any{
					"iteration_index": key.IterationIndex,
				})
			}
			return modelEffectResponse(record)
		}
		if !errors.Is(err, run.ErrEffectNotFound) {
			return ModelResponse{}, asStageError(StageModelCall, err, true)
		}

		record, err = p.cfg.EffectJournal.BeginModelEffect(ctx, run.ModelEffectRecord{
			EffectKey: key,
			InputJSON: inputJSON,
			Status:    run.ModelEffectIntent,
		})
		if err != nil {
			return ModelResponse{}, asStageError(StageModelCall, err, true)
		}
		if record.Status.IsTerminal() {
			return modelEffectResponse(record)
		}
	}

	resp, err := p.cfg.Model.Complete(ctx, input)
	if err != nil {
		if p.cfg.EffectJournal != nil {
			if _, journalErr := p.cfg.EffectJournal.CompleteModelEffect(ctx, run.ModelEffectRecord{
				EffectKey:    key,
				InputJSON:    inputJSON,
				Status:       run.ModelEffectFailed,
				ErrorMessage: err.Error(),
			}); journalErr != nil {
				return ModelResponse{}, asStageError(StageModelCall, fmt.Errorf("record model effect failure: %w", journalErr), true)
			}
		}
		return ModelResponse{}, asStageError(StageModelCall, err, true)
	}
	if p.cfg.EffectJournal == nil {
		return resp, nil
	}

	responseJSON, err := encodeJSON(resp)
	if err != nil {
		return ModelResponse{}, asStageError(StageModelCall, err, true)
	}
	if _, err := p.cfg.EffectJournal.CompleteModelEffect(ctx, run.ModelEffectRecord{
		EffectKey:    key,
		InputJSON:    inputJSON,
		Status:       run.ModelEffectSucceeded,
		ResponseJSON: responseJSON,
	}); err != nil {
		return ModelResponse{}, asStageError(StageModelCall, err, true)
	}
	return resp, nil
}

func modelEffectResponse(record run.ModelEffectRecord) (ModelResponse, *v2errors.V2Error) {
	status := record.Status
	if status == "" && len(record.ResponseJSON) > 0 {
		status = run.ModelEffectSucceeded
	}
	switch status {
	case run.ModelEffectSucceeded:
		var stored ModelResponse
		if err := decodeJSON(record.ResponseJSON, &stored); err != nil {
			return ModelResponse{}, asStageError(StageModelCall, err, true)
		}
		return stored, nil
	case run.ModelEffectFailed:
		message := strings.TrimSpace(record.ErrorMessage)
		if message == "" {
			message = "stored model effect failed"
		}
		return ModelResponse{}, asStageError(StageModelCall, errors.New(message), true)
	default:
		return ModelResponse{}, &v2errors.V2Error{
			Kind:      v2errors.ErrConflict,
			Message:   "model effect has intent without terminal result",
			Cause:     run.ErrEffectIncomplete,
			Fatal:     true,
			Retryable: false,
			Details: map[string]any{
				"iteration_index": record.IterationIndex,
			},
		}
	}
}

func (p *Pipeline) stageModelCallRLMREPL(ctx context.Context, st *executionState) *v2errors.V2Error {
	runnerInstance, err := p.cfg.RLMREPLFactory.New(st.in.RLM)
	if err != nil {
		return asStageError(StageModelCall, err, true)
	}
	result, err := runnerInstance.Run(ctx, taskFromTurnInput(st.in), rlm.Environment{})
	if err != nil {
		return asStageError(StageModelCall, err, true)
	}

	st.out.Summary = strings.TrimSpace(result.Answer)
	st.out.Iterations = result.Iterations
	if st.out.Iterations <= 0 {
		st.out.Iterations = 1
	}
	st.out.ToolCalls = rlmResultToolCalls(result)
	st.out.Metadata = cloneMetadata(result.Metadata)
	st.turn.Iterations = append(st.turn.Iterations, run.IterationRecord{
		TurnID:         st.in.TurnID,
		IterationIndex: 1,
		TraceID:        st.turn.TraceID,
		SpanID:         fmt.Sprintf("%s:iter:%d", st.turn.RootSpanID, 1),
		ParentSpanID:   st.turn.RootSpanID,
		Message: run.MessageRef{
			ID:   "msg-iter-1",
			Role: "assistant",
			Text: st.out.Summary,
		},
	})
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

	replayPolicy := p.toolReplayPolicy(st.tools, call.Name)
	res, toolErr, replayed, key, verr := p.prepareToolEffect(ctx, st, iteration, callID, call, replayPolicy)
	if verr != nil {
		return callID, ToolResult{}, verr
	}

	if err := p.appendEvent(ctx, st, StageModelCall, events.EventToolInvoked, events.ToolInvokedPayload{
		Name:           call.Name,
		IterationIndex: iteration,
	}); err != nil {
		return callID, ToolResult{}, err
	}

	if !replayed {
		res, toolErr = p.cfg.ToolExecutor.Execute(ctx, call.Name, call.Args)
		if p.cfg.EffectJournal != nil {
			if verr := p.completeToolEffect(ctx, key, call, replayPolicy, res, toolErr); verr != nil {
				return callID, res, verr
			}
		}
	}

	var status string
	if toolErr != nil {
		status = "error"
	} else {
		status = strings.TrimSpace(res.Status)
		if status == "" {
			status = "ok"
		}
	}
	resultKind := "tool_result"
	resultText := strings.TrimSpace(res.Output)
	if toolErr != nil {
		resultKind = "tool_error"
		resultText = strings.TrimSpace(toolErr.Error())
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
	if toolErr != nil {
		return callID, res, &v2errors.V2Error{
			Kind:      v2errors.ErrToolFailed,
			Message:   "tool execution failed",
			Cause:     toolErr,
			Fatal:     true,
			Retryable: true,
		}
	}
	return callID, res, nil
}

func (p *Pipeline) prepareToolEffect(ctx context.Context, st *executionState, iteration int, callID string, call run.ToolCall, replayPolicy coretool.EffectReplayPolicy) (ToolResult, error, bool, run.EffectKey, *v2errors.V2Error) {
	key := run.EffectKey{
		RunID:          st.in.RunID,
		RequestID:      st.in.RequestID,
		TurnID:         st.in.TurnID,
		IterationIndex: iteration,
		ToolCallID:     callID,
	}
	if p.cfg.EffectJournal == nil {
		return ToolResult{}, nil, false, key, nil
	}

	record, err := p.cfg.EffectJournal.GetToolEffect(ctx, key)
	if err == nil {
		if !toolEffectMatchesCall(record, call) {
			return ToolResult{}, nil, true, key, effectConflict("tool effect input does not match current tool call", map[string]any{
				"tool_call_id": callID,
				"tool_name":    strings.TrimSpace(call.Name),
			})
		}
		if record.Status.IsTerminal() {
			var stored ToolResult
			if len(record.ResultJSON) > 0 {
				if err := decodeJSON(record.ResultJSON, &stored); err != nil {
					return ToolResult{}, nil, true, key, asStageError(StageModelCall, err, true)
				}
			}
			if record.Status == run.ToolEffectFailed {
				message := strings.TrimSpace(record.ErrorMessage)
				if message == "" {
					message = "stored tool effect failed"
				}
				return stored, errors.New(message), true, key, nil
			}
			return stored, nil, true, key, nil
		}
		if toolEffectReplayPolicy(record, replayPolicy).AllowsIncompleteEffectRetry() {
			return ToolResult{}, nil, false, key, nil
		}
		return ToolResult{}, nil, true, key, &v2errors.V2Error{
			Kind:      v2errors.ErrConflict,
			Message:   "tool effect has intent without terminal result",
			Cause:     run.ErrEffectIncomplete,
			Fatal:     true,
			Retryable: false,
			Details: map[string]any{
				"tool_call_id": callID,
				"tool_name":    strings.TrimSpace(call.Name),
			},
		}
	}
	if !errors.Is(err, run.ErrEffectNotFound) {
		return ToolResult{}, nil, false, key, asStageError(StageModelCall, err, true)
	}

	if _, err := p.cfg.EffectJournal.BeginToolEffect(ctx, run.ToolEffectRecord{
		EffectKey:    key,
		ToolName:     strings.TrimSpace(call.Name),
		ArgsJSON:     append([]byte(nil), call.Args...),
		ReplayPolicy: string(replayPolicy),
		Status:       run.ToolEffectIntent,
	}); err != nil {
		return ToolResult{}, nil, false, key, asStageError(StageModelCall, err, true)
	}
	return ToolResult{}, nil, false, key, nil
}

func (p *Pipeline) completeToolEffect(ctx context.Context, key run.EffectKey, call run.ToolCall, replayPolicy coretool.EffectReplayPolicy, res ToolResult, toolErr error) *v2errors.V2Error {
	status := run.ToolEffectSucceeded
	errorMessage := ""
	if toolErr != nil {
		status = run.ToolEffectFailed
		errorMessage = toolErr.Error()
	}
	resultJSON, err := encodeJSON(res)
	if err != nil {
		return asStageError(StageModelCall, err, true)
	}
	if _, err := p.cfg.EffectJournal.CompleteToolEffect(ctx, run.ToolEffectRecord{
		EffectKey:    key,
		ToolName:     strings.TrimSpace(call.Name),
		ArgsJSON:     append([]byte(nil), call.Args...),
		ReplayPolicy: string(replayPolicy),
		Status:       status,
		ResultJSON:   resultJSON,
		ErrorMessage: errorMessage,
	}); err != nil {
		return asStageError(StageModelCall, err, true)
	}
	return nil
}

func (p *Pipeline) toolReplayPolicy(tools []coretool.ToolDef, name string) coretool.EffectReplayPolicy {
	name = toolnames.Canonical(name)
	if name == "" {
		return coretool.EffectReplayFailClosed
	}
	for _, tool := range tools {
		if toolnames.Canonical(tool.Name) == name {
			return tool.Policy.EffectReplay
		}
	}
	return coretool.EffectReplayFailClosed
}

func toolEffectReplayPolicy(record run.ToolEffectRecord, fallback coretool.EffectReplayPolicy) coretool.EffectReplayPolicy {
	if stored := strings.TrimSpace(record.ReplayPolicy); stored != "" {
		return coretool.EffectReplayPolicy(stored)
	}
	return fallback
}

func modelEffectMatchesInput(record run.ModelEffectRecord, inputJSON json.RawMessage) bool {
	return len(record.InputJSON) == 0 || string(record.InputJSON) == string(inputJSON)
}

func toolEffectMatchesCall(record run.ToolEffectRecord, call run.ToolCall) bool {
	return toolnames.Canonical(record.ToolName) == toolnames.Canonical(call.Name) &&
		string(record.ArgsJSON) == string(call.Args)
}

func effectConflict(message string, details map[string]any) *v2errors.V2Error {
	return &v2errors.V2Error{
		Kind:      v2errors.ErrConflict,
		Message:   message,
		Cause:     run.ErrEffectConflict,
		Fatal:     true,
		Retryable: false,
		Details:   details,
	}
}

func encodeJSON(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func decodeJSON(data json.RawMessage, v any) error {
	if len(data) == 0 {
		return errors.New("empty journal json")
	}
	return json.Unmarshal(data, v)
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
