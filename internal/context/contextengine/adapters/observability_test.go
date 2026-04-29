package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

func TestConvertWideEvent(t *testing.T) {
	now := time.Now().UTC()
	src := observability.WideEvent{
		Ts:          now,
		TraceID:     "trace1",
		SpanID:      "span1",
		ParentID:    "parent1",
		Service:     "foxctl",
		Version:     "1.0.0",
		Component:   observability.ComponentSkill,
		Operation:   observability.OpSkillRun,
		Command:     "code/semantic_search",
		SessionID:   "sess1",
		WorkspaceID: "ws1",
		JobID:       "job1",
		Status:      observability.StatusOK,
		DurationMS:  150,
		Data:        map[string]any{"result_count": 5},
	}

	got := ConvertWideEvent(src)

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

func TestConvertWideEvent_WithErrors(t *testing.T) {
	src := observability.WideEvent{
		Ts:           time.Now().UTC(),
		TraceID:      "trace2",
		SpanID:       "span2",
		Service:      "foxctl",
		Component:    observability.ComponentCLI,
		Operation:    "skill.run",
		Status:       observability.StatusError,
		ErrorType:    "validation",
		ErrorCode:    "EPARSE",
		ErrorMessage: "bad input",
		Retriable:    &[]bool{true}[0],
	}

	got := ConvertWideEvent(src)

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

func TestMapWideEventKind(t *testing.T) {
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
			src := observability.WideEvent{Operation: tt.operation}
			got := mapWideEventKind(src)
			if got != tt.want {
				t.Errorf("mapWideEventKind(%q) = %q, want %q", tt.operation, got, tt.want)
			}
		})
	}
}
