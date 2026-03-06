package jido

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SignalAckToCallback converts a synchronous runtime.signal response into a
// normalized reconciliation callback payload.
func SignalAckToCallback(req SignalRequest, resp SignalResponse) SignalCallback {
	askID := chooseNonEmpty(
		req.Signal.CorrelationID,
		askIDFromSignalData(req.Signal.Data),
		req.Signal.ID,
	)

	status, summary, errMsg, metadata := callbackFieldsFromResponse(resp)

	cb := SignalCallback{
		AskID:         askID,
		RequestID:     strings.TrimSpace(req.RequestID),
		AgentID:       chooseNonEmpty(resp.AgentID, req.AgentID),
		MessageID:     chooseNonEmpty(resp.MessageID, resp.SignalID, askID),
		Status:        normalizeCallbackStatus(status),
		CorrelationID: chooseNonEmpty(req.Signal.CorrelationID, askID),
		CausationID:   chooseNonEmpty(req.Signal.CausationID, req.RequestID),
		Summary:       strings.TrimSpace(summary),
		Error:         strings.TrimSpace(errMsg),
		Metadata:      metadata,
	}

	if cb.Status == "" {
		cb.Status = "completed"
	}
	if cb.Error != "" && cb.Status != "failed" {
		cb.Status = "failed"
	}
	return cb
}

func callbackFieldsFromResponse(resp SignalResponse) (status, summary, errMsg string, metadata map[string]any) {
	metadata = map[string]any{
		"response_status": strings.TrimSpace(resp.Status),
	}
	status = normalizeCallbackStatus(resp.Status)
	if status == "sent" || status == "processed" {
		status = "completed"
	}

	if len(resp.Data) == 0 {
		return status, "", "", metadata
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		metadata["decode_error"] = err.Error()
		return status, "", "", metadata
	}

	state := mapAt(payload, "state")
	target := mapAt(state, "agentctl")
	if len(target) == 0 {
		target = state
	}

	if v := stringValue(target["status"]); v != "" {
		status = normalizeCallbackStatus(v)
	}

	lastResult := mapAt(target, "last_result")
	tool := strings.TrimSpace(stringValue(lastResult["tool"]))
	if tool != "" {
		metadata["tool"] = tool
		summary = "tool=" + tool
	}

	companionCtx := mapAt(lastResult, "companion_context")
	if len(companionCtx) > 0 {
		if refs, ok := companionCtx["refs"].([]any); ok {
			metadata["companion_ref_count"] = len(refs)
			if summary == "" {
				summary = fmt.Sprintf("companion_refs=%d", len(refs))
			}
		}
		if meta, ok := companionCtx["meta"].(map[string]any); ok && len(meta) > 0 {
			metadata["companion_meta"] = meta
		}
	}

	envelope := mapAt(lastResult, "envelope")
	if envStatus := strings.TrimSpace(stringValue(envelope["status"])); envStatus != "" {
		metadata["envelope_status"] = envStatus
		if summary == "" {
			summary = "envelope_status=" + envStatus
		} else {
			summary = summary + " envelope_status=" + envStatus
		}
		if strings.EqualFold(envStatus, "error") {
			status = "failed"
			if errMsg == "" {
				errMsg = extractEnvelopeError(envelope)
			}
		}
	}

	if errMsg == "" {
		errMsg = errorValue(target["last_error"])
	}
	if errMsg == "" {
		errMsg = errorValue(lastResult["error"])
	}

	if errMsg != "" {
		metadata["error"] = errMsg
		status = "failed"
	}

	return status, strings.TrimSpace(summary), strings.TrimSpace(errMsg), metadata
}

func askIDFromSignalData(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(stringValue(payload["ask_id"]))
}

func normalizeCallbackStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "ok", "success":
		return "completed"
	case "failed", "error":
		return "failed"
	case "sent", "processed":
		return "completed"
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func mapAt(input map[string]any, key string) map[string]any {
	if len(input) == 0 {
		return nil
	}
	raw, ok := input[key]
	if !ok || raw == nil {
		return nil
	}
	out, _ := raw.(map[string]any)
	return out
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.RawMessage:
		return string(value)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("%v", value)
	}
}

func errorValue(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return normalizedErrorText(value)
	case map[string]any:
		msg := normalizedErrorText(stringValue(value["message"]))
		if msg != "" {
			return msg
		}
		errText := normalizedErrorText(stringValue(value["error"]))
		if errText != "" {
			return errText
		}
		reason := normalizedErrorText(stringValue(value["reason"]))
		if reason != "" {
			return reason
		}
		if nested := errorValue(value["transport_error"]); nested != "" {
			return nested
		}
		if nested := errorValue(value["cli_error"]); nested != "" {
			return nested
		}
		return normalizedErrorText(fmt.Sprintf("%v", value))
	default:
		return normalizedErrorText(fmt.Sprintf("%v", value))
	}
}

func extractEnvelopeError(envelope map[string]any) string {
	errObj := mapAt(envelope, "error")
	if len(errObj) == 0 {
		return ""
	}
	msg := strings.TrimSpace(stringValue(errObj["message"]))
	if msg != "" {
		return msg
	}
	return strings.TrimSpace(stringValue(errObj["code"]))
}

func normalizedErrorText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	trimmed := strings.Trim(text, "\"")
	switch strings.ToLower(trimmed) {
	case "", "nil", "<nil>", "null":
		return ""
	default:
		return trimmed
	}
}
