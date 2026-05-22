package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/observability"
)

func TestConvertEvent(t *testing.T) {
	now := time.Now().UTC()
	src := observability.Event{
		Timestamp: now,
		TraceID:   "trace1",
		SpanID:    "span1",
		ParentID:  "parent1",
		Operation: observability.OpSkillRun,
		Name:      "code/semantic_search",
		Status:    observability.StatusOK,
		Duration:  150 * time.Millisecond,
		Data: map[string]any{
			observability.DataKeyService:     "foxctl",
			observability.DataKeyVersion:     "1.0.0",
			observability.DataKeyComponent:   observability.ComponentSkill,
			observability.DataKeySessionID:   "sess1",
			observability.DataKeyWorkspaceID: "ws1",
			observability.DataKeyJobID:       "job1",
			"result_count":                   5,
		},
	}

	got := ConvertEvent(src)

	if got.ID == "" {
		t.Error("ID should not be empty")
	}
	if got.WorkspaceID != "ws1" {
		t.Errorf("WorkspaceID = %q", got.WorkspaceID)
	}
	if got.Kind != contextengine.EventKindToolEvidenceProduced {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Source != "observability:skill" {
		t.Errorf("Source = %q", got.Source)
	}
	if got.SessionID != "sess1" {
		t.Errorf("SessionID = %q", got.SessionID)
	}
	if got.Data["trace_id"] != "trace1" {
		t.Errorf("Data[trace_id] = %v", got.Data["trace_id"])
	}
	if got.Data["span_id"] != "span1" {
		t.Errorf("Data[span_id] = %v", got.Data["span_id"])
	}
	if got.Data["duration_ms"] != int64(150) {
		t.Errorf("Data[duration_ms] = %v", got.Data["duration_ms"])
	}
	if got.Data["status"] != "ok" {
		t.Errorf("Data[status] = %v", got.Data["status"])
	}
	if got.CreatedAt != now {
		t.Errorf("CreatedAt mismatch")
	}
}

func TestConvertEvent_WithErrors(t *testing.T) {
	src := observability.Event{
		Timestamp:    time.Now().UTC(),
		TraceID:      "trace2",
		SpanID:       "span2",
		Operation:    "skill.run",
		Status:       observability.StatusError,
		ErrorType:    "validation",
		ErrorCode:    "EPARSE",
		ErrorMessage: "bad input",
		Data: map[string]any{
			observability.DataKeyService:   "foxctl",
			observability.DataKeyComponent: observability.ComponentCLI,
			observability.DataKeyRetriable: true,
		},
	}

	got := ConvertEvent(src)

	if got.Data["error_type"] != "validation" {
		t.Errorf("Data[error_type] = %v", got.Data["error_type"])
	}
	if got.Data["error_code"] != "EPARSE" {
		t.Errorf("Data[error_code] = %v", got.Data["error_code"])
	}
	if got.Data["error_message"] != "bad input" {
		t.Errorf("Data[error_message] = %v", got.Data["error_message"])
	}
}

func TestMapEventKind(t *testing.T) {
	tests := []struct {
		operation string
		want      contextengine.ContextEventKind
	}{
		{observability.OpSkillRun, contextengine.EventKindToolEvidenceProduced},
		{observability.OpSkillCache, contextengine.EventKindToolEvidenceProduced},
		{observability.OpHookExecute, contextengine.EventKindToolEvidenceProduced},
		{observability.OpJobSubmit, contextengine.EventKindTaskChanged},
		{observability.OpJobComplete, contextengine.EventKindTaskChanged},
		{observability.OpSessionStart, contextengine.EventKindSessionTurnCaptured},
		{observability.OpSessionEnd, contextengine.EventKindSessionTurnCaptured},
		{observability.OpAgentSpawn, contextengine.EventKindSessionTurnCaptured},
		{observability.OpAgentComplete, contextengine.EventKindSessionTurnCaptured},
		{observability.OpContextSemanticArtifactSearch, contextengine.EventKindRetrievalExecuted},
		{observability.OpContextLayeredBundle, contextengine.EventKindRetrievalExecuted},
		{"unknown_op", contextengine.EventKindToolEvidenceProduced},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			src := observability.Event{Operation: tt.operation}
			got := mapEventKind(src)
			if got != tt.want {
				t.Errorf("mapEventKind(%q) = %q, want %q", tt.operation, got, tt.want)
			}
		})
	}
}
