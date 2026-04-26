package adapters

import (
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/companion"
)

func ptrInt64(v int64) *int64 { return &v }

func TestConvertConversationEvent(t *testing.T) {
	src := companion.ConversationEvent{
		ID:             42,
		ConversationID: "conv1",
		EventType:      companion.EventTypeToolCall,
		TurnID:         "turn1",
		ToolName:       "code_search",
		ToolRunID:      "run1",
		TokenCount:     100,
		ContentHash:    "abc123",
		CreatedAt:      "2025-01-15T12:00:00Z",
	}

	got := ConvertConversationEvent("ws1", src)

	if got.ID != "42" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Kind != contextengine.EventKindToolEvidenceProduced {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.SessionID != "conv1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.Data["tool_name"] != "code_search" {
		t.Errorf("Data[tool_name] = %v", got.Data["tool_name"])
	}
	if got.Data["tool_run_id"] != "run1" {
		t.Errorf("Data[tool_run_id] = %v", got.Data["tool_run_id"])
	}
	if got.Data["token_count"] != 100 {
		t.Errorf("Data[token_count] = %v", got.Data["token_count"])
	}
	if got.Data["content_hash"] != "abc123" {
		t.Errorf("Data[content_hash] = %v", got.Data["content_hash"])
	}
}

func TestConvertEvidenceSnippet(t *testing.T) {
	src := companion.EvidenceSnippet{
		ID:            10,
		ConversationID: "conv1",
		SourceEventID: 42,
		EventType:     "tool_result",
		FactText:      "File uses typed enums",
		ContentHash:   "hash1",
		Confidence:    0.9,
		Bucket:        "code_style",
	}

	got := ConvertEvidenceSnippet("ws1", src)

	if got.ID != "10" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.NodeType != contextengine.EvidenceNodeTypeMemory {
		t.Errorf("NodeType = %q", got.NodeType)
	}
	if got.Statement != "File uses typed enums" {
		t.Errorf("Statement = %q", got.Statement)
	}
	if got.Confidence != 0.9 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
	if got.Grounding != contextengine.GroundingLoaded {
		t.Errorf("Grounding = %q", got.Grounding)
	}
	if got.Metadata["bucket"] != "code_style" {
		t.Errorf("metadata[bucket] = %v", got.Metadata["bucket"])
	}
	if got.Metadata["content_hash"] != "hash1" {
		t.Errorf("metadata[content_hash] = %v", got.Metadata["content_hash"])
	}
}

func TestMapHardStateStatus(t *testing.T) {
	tests := []struct {
		status  string
		want    contextengine.ClaimStatus
		wantErr bool
	}{
		{companion.EntryStatusActive, contextengine.ClaimStatusCurrent, false},
		{companion.EntryStatusSuperseded, contextengine.ClaimStatusSuperseded, false},
		{companion.EntryStatusRetracted, contextengine.ClaimStatusRejected, false},
		{"unknown_status", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got, err := MapHardStateStatus(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapHardStateStatus(%q) error = %v, wantErr %v", tt.status, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("MapHardStateStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestConvertHardStateEntry(t *testing.T) {
	supercedes := int64(5)
	src := companion.HardStateEntry{
		ID:             1,
		ConversationID: "conv1",
		EntryType:      "preference",
		Key:            "pref:test",
		ValueJSON:      `{"value": "typed"}`,
		Status:         companion.EntryStatusActive,
		SourceEventID:  42,
		Confidence:     0.85,
		Supersedes:     &supercedes,
	}

	got, err := ConvertHardStateEntry("ws1", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ClaimType != "preference" {
		t.Errorf("ClaimType = %q", got.ClaimType)
	}
	if got.Status != contextengine.ClaimStatusCurrent {
		t.Errorf("Status = %q, want current", got.Status)
	}
	if got.Summary != "pref:test" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if got.Confidence != 0.85 {
		t.Errorf("Confidence = %f", got.Confidence)
	}
	if got.SourceEventID != "42" {
		t.Errorf("SourceEventID = %q", got.SourceEventID)
	}
	if got.SupersededBy != "5" {
		t.Errorf("SupersededBy = %q", got.SupersededBy)
	}
	if got.Scope.SessionID != "conv1" {
		t.Errorf("Scope.SessionID = %q", got.Scope.SessionID)
	}
}

func TestConvertHardStateEntry_UnknownStatus(t *testing.T) {
	src := companion.HardStateEntry{
		ID:     1,
		Status: "garbage",
	}
	_, err := ConvertHardStateEntry("ws1", src)
	if err == nil {
		t.Error("expected error for unknown status")
	}
}

func TestMapAssumptionStatus(t *testing.T) {
	tests := []struct {
		status  string
		want    contextengine.ClaimStatus
		wantErr bool
	}{
		{companion.AssumptionStatusActive, contextengine.ClaimStatusCandidate, false},
		{companion.AssumptionStatusPromoted, contextengine.ClaimStatusCurrent, false},
		{companion.AssumptionStatusRetracted, contextengine.ClaimStatusRejected, false},
		{"unknown_status", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got, err := MapAssumptionStatus(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("MapAssumptionStatus(%q) error = %v, wantErr %v", tt.status, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("MapAssumptionStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestConvertAssumption_Active(t *testing.T) {
	src := companion.Assumption{
		ID:             1,
		ConversationID: "conv1",
		Assumption:     "User prefers dark mode",
		Status:         companion.AssumptionStatusActive,
		SourceEventID:  10,
		Confidence:     0.7,
	}
	got, err := ConvertAssumption("ws1", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != contextengine.ClaimStatusCandidate {
		t.Errorf("active → %q, want candidate", got.Status)
	}
	if got.ClaimType != "assumption" {
		t.Errorf("ClaimType = %q", got.ClaimType)
	}
	if got.Summary != "User prefers dark mode" {
		t.Errorf("Summary = %q", got.Summary)
	}
}

func TestConvertAssumption_Promoted(t *testing.T) {
	src := companion.Assumption{
		ID:             2,
		ConversationID: "conv1",
		Assumption:     "Confirmed preference",
		Status:         companion.AssumptionStatusPromoted,
		SourceEventID:  20,
		Confidence:     0.95,
	}
	got, err := ConvertAssumption("ws1", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != contextengine.ClaimStatusCurrent {
		t.Errorf("promoted → %q, want current", got.Status)
	}
}

func TestConvertAssumption_Retracted(t *testing.T) {
	reason := "user corrected"
	src := companion.Assumption{
		ID:                3,
		ConversationID:    "conv1",
		Assumption:        "Wrong assumption",
		Status:            companion.AssumptionStatusRetracted,
		SourceEventID:     30,
		Confidence:        0.3,
		RetractionReason:  &reason,
		RetractedByEventID: ptrInt64(40),
	}
	got, err := ConvertAssumption("ws1", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != contextengine.ClaimStatusRejected {
		t.Errorf("retracted → %q, want rejected", got.Status)
	}
	if got.Reason != "user corrected" {
		t.Errorf("Reason = %q, want retraction reason", got.Reason)
	}
}

func TestConvertAssumption_UnknownStatus(t *testing.T) {
	src := companion.Assumption{
		ID:     4,
		Status: "garbage",
	}
	_, err := ConvertAssumption("ws1", src)
	if err == nil {
		t.Error("expected error for unknown status")
	}
}

func TestMapEventType(t *testing.T) {
	tests := []struct {
		eventType string
		want      contextengine.ContextEventKind
	}{
		{companion.EventTypeToolCall, contextengine.EventKindToolEvidenceProduced},
		{companion.EventTypeToolResult, contextengine.EventKindToolEvidenceProduced},
		{companion.EventTypeUserMessage, contextengine.EventKindSessionTurnCaptured},
		{companion.EventTypeAssistantMessage, contextengine.EventKindSessionTurnCaptured},
		{"unknown_event", contextengine.EventKindSessionTurnCaptured},
	}
	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := mapEventType(tt.eventType)
			if got != tt.want {
				t.Errorf("mapEventType(%q) = %q, want %q", tt.eventType, got, tt.want)
			}
		})
	}
}
