package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/observability"
)

// ConvertEvent converts a canonical observability event to a contextengine.ContextEvent.
// The event fields are mapped into the ContextEvent data map for forensics and debugging.
func ConvertEvent(src observability.Event) contextengine.ContextEvent {
	kind := mapEventKind(src)
	component := observability.EventDataString(&src, observability.DataKeyComponent)
	data := map[string]any{
		"trace_id":    src.TraceID,
		"span_id":     src.SpanID,
		"parent_id":   src.ParentID,
		"service":     observability.EventDataString(&src, observability.DataKeyService),
		"version":     observability.EventDataString(&src, observability.DataKeyVersion),
		"component":   component,
		"operation":   src.Operation,
		"command":     src.Name,
		"subtype":     observability.EventDataString(&src, observability.DataKeySubtype),
		"duration_ms": src.Duration.Milliseconds(),
	}
	if src.Status != "" {
		data["status"] = string(src.Status)
	}
	if src.ErrorType != "" || src.ErrorCode != "" || src.ErrorMessage != "" {
		data["error_type"] = src.ErrorType
		data["error_code"] = src.ErrorCode
		data["error_message"] = src.ErrorMessage
		data["retriable"] = observability.EventDataBoolPtr(&src, observability.DataKeyRetriable)
	}
	if src.Data != nil {
		data["event_data"] = src.Data
	}

	return contextengine.ContextEvent{
		ID:          fmt.Sprintf("event_%s_%s", src.TraceID, src.SpanID),
		WorkspaceID: observability.EventDataString(&src, observability.DataKeyWorkspaceID),
		Kind:        kind,
		Source:      fmt.Sprintf("observability:%s", component),
		SessionID:   observability.EventDataString(&src, observability.DataKeySessionID),
		Data:        data,
		CreatedAt:   src.Timestamp,
	}
}

// mapEventKind maps an Event's operation/component to a ContextEventKind.
// Uses explicit mapping, not keyword heuristics.
func mapEventKind(src observability.Event) contextengine.ContextEventKind {
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
