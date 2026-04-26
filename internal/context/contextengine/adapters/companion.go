package adapters

import (
	"fmt"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/companion"
)

// ConvertConversationEvent converts a companion.ConversationEvent to a contextengine.ContextEvent.
func ConvertConversationEvent(workspaceID string, src companion.ConversationEvent) contextengine.ContextEvent {
	kind := mapEventType(src.EventType)
	return contextengine.ContextEvent{
		ID:          fmt.Sprintf("%d", src.ID),
		WorkspaceID: workspaceID,
		Kind:        kind,
		Source:      src.EventType,
		SessionID:   src.ConversationID,
		Data: map[string]any{
			"turn_id":            src.TurnID,
			"tool_name":          src.ToolName,
			"tool_run_id":        src.ToolRunID,
			"parent_tool_call_id": src.ParentToolCallID,
			"payload_json":       src.PayloadJSON,
			"payload_ref":        src.PayloadRef,
			"token_count":        src.TokenCount,
			"content_hash":       src.ContentHash,
			"content":            src.Content,
			"created_at":         src.CreatedAt,
		},
	}
}

// mapEventType maps companion event types to ContextEventKind.
// Uses explicit switch, not keyword heuristics.
func mapEventType(eventType string) contextengine.ContextEventKind {
	switch eventType {
	case companion.EventTypeToolCall:
		return contextengine.EventKindToolEvidenceProduced
	case companion.EventTypeToolResult:
		return contextengine.EventKindToolEvidenceProduced
	case companion.EventTypeUserMessage:
		return contextengine.EventKindSessionTurnCaptured
	case companion.EventTypeAssistantMessage:
		return contextengine.EventKindSessionTurnCaptured
	default:
		return contextengine.EventKindSessionTurnCaptured
	}
}

// ConvertEvidenceSnippet converts a companion.EvidenceSnippet to a contextengine.EvidenceNode.
func ConvertEvidenceSnippet(workspaceID string, src companion.EvidenceSnippet) contextengine.EvidenceNode {
	ref := contextengine.EvidenceRef{
		Type: contextengine.RefTypeEvent,
		Ref:  fmt.Sprintf("%d", src.SourceEventID),
	}
	return contextengine.EvidenceNode{
		ID:          fmt.Sprintf("%d", src.ID),
		WorkspaceID: workspaceID,
		NodeType:    contextengine.EvidenceNodeTypeMemory,
		Ref:         ref,
		Statement:   src.FactText,
		Confidence:  src.Confidence,
		Grounding:   contextengine.GroundingLoaded,
		Metadata: map[string]any{
			"source_event_id": src.SourceEventID,
			"event_type":      src.EventType,
			"bucket":          src.Bucket,
			"content_hash":    src.ContentHash,
			"can_verify":      src.CanVerify,
		},
	}
}

// ConvertHardStateEntry converts a companion.HardStateEntry to a contextengine.MemoryClaim.
// Returns an error for unknown status values.
func ConvertHardStateEntry(workspaceID string, src companion.HardStateEntry) (contextengine.MemoryClaim, error) {
	status, err := MapHardStateStatus(src.Status)
	if err != nil {
		return contextengine.MemoryClaim{}, err
	}

	sourceEventID := fmt.Sprintf("%d", src.SourceEventID)
	var supersededBy string
	if src.Supersedes != nil {
		supersededBy = fmt.Sprintf("%d", *src.Supersedes)
	}

	return contextengine.MemoryClaim{
		ID:            fmt.Sprintf("%d", src.ID),
		WorkspaceID:   workspaceID,
		ClaimType:     src.EntryType,
		Status:        status,
		Summary:       src.Key,
		Confidence:    src.Confidence,
		SourceEventID: sourceEventID,
		SupersededBy:  supersededBy,
		Reason:        src.ValueJSON,
		Scope: contextengine.ClaimScope{
			SessionID: src.ConversationID,
		},
	}, nil
}

// MapHardStateStatus maps companion hard state statuses to ClaimStatus.
// active → current, superseded → superseded, retracted → rejected.
// Returns error for unknown statuses.
func MapHardStateStatus(status string) (contextengine.ClaimStatus, error) {
	switch status {
	case companion.EntryStatusActive:
		return contextengine.ClaimStatusCurrent, nil
	case companion.EntryStatusSuperseded:
		return contextengine.ClaimStatusSuperseded, nil
	case companion.EntryStatusRetracted:
		return contextengine.ClaimStatusRejected, nil
	default:
		return "", fmt.Errorf("unknown hard state status %q: no mapping to ClaimStatus", status)
	}
}

// ConvertAssumption converts a companion.Assumption to a contextengine.MemoryClaim.
// Returns an error for unknown status values.
func ConvertAssumption(workspaceID string, src companion.Assumption) (contextengine.MemoryClaim, error) {
	status, err := MapAssumptionStatus(src.Status)
	if err != nil {
		return contextengine.MemoryClaim{}, err
	}

	sourceEventID := fmt.Sprintf("%d", src.SourceEventID)
	reason := ""
	if src.Reason != nil {
		reason = *src.Reason
	}
	retractionReason := ""
	if src.RetractionReason != nil {
		retractionReason = *src.RetractionReason
	}
	if retractionReason != "" {
		reason = retractionReason
	}

	return contextengine.MemoryClaim{
		ID:            fmt.Sprintf("%d", src.ID),
		WorkspaceID:   workspaceID,
		ClaimType:     "assumption",
		Status:        status,
		Summary:       src.Assumption,
		Confidence:    src.Confidence,
		SourceEventID: sourceEventID,
		Reason:        reason,
		Scope: contextengine.ClaimScope{
			SessionID: src.ConversationID,
		},
	}, nil
}

// MapAssumptionStatus maps companion assumption statuses to ClaimStatus.
// active → candidate, promoted → current, retracted → rejected.
// Returns error for unknown statuses.
func MapAssumptionStatus(status string) (contextengine.ClaimStatus, error) {
	switch status {
	case companion.AssumptionStatusActive:
		return contextengine.ClaimStatusCandidate, nil
	case companion.AssumptionStatusPromoted:
		return contextengine.ClaimStatusCurrent, nil
	case companion.AssumptionStatusRetracted:
		return contextengine.ClaimStatusRejected, nil
	default:
		return "", fmt.Errorf("unknown assumption status %q: no mapping to ClaimStatus", status)
	}
}
