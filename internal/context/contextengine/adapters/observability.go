package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// ConvertWideEvent converts an observability.WideEvent to a contextengine.ContextEvent.
// The wide event's fields are mapped into the ContextEvent data map for forensics and debugging.
func ConvertWideEvent(src observability.WideEvent) contextengine.ContextEvent {
	kind := mapWideEventKind(src)
	data := map[string]any{
		"trace_id":    src.TraceID,
		"span_id":     src.SpanID,
		"parent_id":   src.ParentID,
		"service":     src.Service,
		"version":     src.Version,
		"component":   src.Component,
		"operation":   src.Operation,
		"command":     src.Command,
		"subtype":     src.Subtype,
		"duration_ms": src.DurationMS,
	}
	if src.Status != "" {
		data["status"] = string(src.Status)
	}
	if src.ErrorType != "" || src.ErrorCode != "" || src.ErrorMessage != "" {
		data["error_type"] = src.ErrorType
		data["error_code"] = src.ErrorCode
		data["error_message"] = src.ErrorMessage
		data["retriable"] = src.Retriable
	}
	if src.Data != nil {
		data["event_data"] = src.Data
	}

	return contextengine.ContextEvent{
		ID:          fmt.Sprintf("wide_%s_%s", src.TraceID, src.SpanID),
		WorkspaceID: src.WorkspaceID,
		Kind:        kind,
		Source:      fmt.Sprintf("observability:%s", src.Component),
		SessionID:   src.SessionID,
		Data:        data,
		CreatedAt:   src.Ts,
	}
}

// mapWideEventKind maps a WideEvent's operation/component to a ContextEventKind.
// Uses explicit mapping, not keyword heuristics.
func mapWideEventKind(src observability.WideEvent) contextengine.ContextEventKind {
	switch src.Operation {
	case observability.OpSkillRun, observability.OpSkillCache:
		return contextengine.EventKindToolEvidenceProduced
	case observability.OpHookExecute:
		return contextengine.EventKindToolEvidenceProduced
	case observability.OpJobSubmit, observability.OpJobComplete:
		return contextengine.EventKindTaskChanged
	case observability.OpSessionStart, observability.OpSessionEnd:
		return contextengine.EventKindSessionTurnCaptured
	case observability.OpAgentSpawn, observability.OpAgentComplete:
		return contextengine.EventKindSessionTurnCaptured
	case observability.OpContextSemanticArtifactSearch, observability.OpContextLayeredBundle:
		return contextengine.EventKindRetrievalExecuted
	default:
		return contextengine.EventKindToolEvidenceProduced
	}
}
