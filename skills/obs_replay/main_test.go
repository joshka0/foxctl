package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "obs/replay", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		TraceID:     "trace-abc123",
		SpanID:      "span-xyz789",
		IncludeData: true,
	}

	assert.Equal(t, "trace-abc123", in.TraceID)
	assert.Equal(t, "span-xyz789", in.SpanID)
	assert.True(t, in.IncludeData)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		TraceID:     "trace-test",
		SpanID:      "span-test",
		IncludeData: false,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.TraceID, decoded.TraceID)
	assert.Equal(t, in.SpanID, decoded.SpanID)
	assert.Equal(t, in.IncludeData, decoded.IncludeData)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.TraceID)
	assert.Empty(t, in.SpanID)
	assert.False(t, in.IncludeData)
}

func TestInput_JSONOmitEmpty(t *testing.T) {
	in := input{
		TraceID: "trace-123",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// span_id should be omitted when empty
	assert.NotContains(t, string(data), "span_id")
	// include_data should be omitted when false
	assert.NotContains(t, string(data), "include_data")
}

// Tests for wideEvent structure

func TestWideEvent_AllFields(t *testing.T) {
	evt := wideEvent{
		Timestamp:   "2026-01-15T10:30:00Z",
		TraceID:     "trace-123",
		SpanID:      "span-456",
		Service:     "agentctl",
		Version:     "1.0.0",
		Component:   "skill",
		Operation:   "run",
		Command:     "code/search",
		WorkspaceID: "/workspace/path",
		JobID:       "job-789",
		Status:      "success",
		DurationMS:  150,
		ErrorCode:   "",
		ErrorMsg:    "",
		Data:        map[string]any{"key": "value"},
	}

	assert.Equal(t, "2026-01-15T10:30:00Z", evt.Timestamp)
	assert.Equal(t, "trace-123", evt.TraceID)
	assert.Equal(t, "span-456", evt.SpanID)
	assert.Equal(t, "agentctl", evt.Service)
	assert.Equal(t, "1.0.0", evt.Version)
	assert.Equal(t, "skill", evt.Component)
	assert.Equal(t, "run", evt.Operation)
	assert.Equal(t, "code/search", evt.Command)
	assert.Equal(t, "/workspace/path", evt.WorkspaceID)
	assert.Equal(t, "job-789", evt.JobID)
	assert.Equal(t, "success", evt.Status)
	assert.Equal(t, int64(150), evt.DurationMS)
	assert.NotNil(t, evt.Data)
}

func TestWideEvent_JSONSerialization(t *testing.T) {
	evt := wideEvent{
		Timestamp:  "2026-01-15T10:00:00Z",
		TraceID:    "trace-abc",
		SpanID:     "span-def",
		Service:    "test",
		Operation:  "test-op",
		Status:     "error",
		DurationMS: 500,
		ErrorCode:  "E001",
		ErrorMsg:   "Something went wrong",
	}

	data, err := json.Marshal(evt)
	assert.NoError(t, err)

	var decoded wideEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, evt.TraceID, decoded.TraceID)
	assert.Equal(t, evt.SpanID, decoded.SpanID)
	assert.Equal(t, evt.Status, decoded.Status)
	assert.Equal(t, evt.ErrorCode, decoded.ErrorCode)
	assert.Equal(t, evt.ErrorMsg, decoded.ErrorMsg)
}

func TestWideEvent_EmptyFields(t *testing.T) {
	evt := wideEvent{}

	assert.Empty(t, evt.Timestamp)
	assert.Empty(t, evt.TraceID)
	assert.Empty(t, evt.SpanID)
	assert.Zero(t, evt.DurationMS)
	assert.Nil(t, evt.Data)
}

func TestWideEvent_WithData(t *testing.T) {
	evt := wideEvent{
		TraceID: "trace-test",
		Data: map[string]any{
			"input_artifact":  "sha256:abc123",
			"result_artifact": "sha256:def456",
			"count":           42,
		},
	}

	assert.NotNil(t, evt.Data)
	assert.Equal(t, "sha256:abc123", evt.Data["input_artifact"])
	assert.Equal(t, "sha256:def456", evt.Data["result_artifact"])
	assert.Equal(t, 42, evt.Data["count"])
}

// Tests for reconstructedEvent structure

func TestReconstructedEvent_AllFields(t *testing.T) {
	re := reconstructedEvent{
		Event: wideEvent{
			TraceID:    "trace-123",
			Operation:  "test",
			Status:     "success",
			DurationMS: 100,
		},
		Artifacts: map[string]any{
			"input_artifact": map[string]any{
				"digest":  "sha256:abc",
				"content": "test content",
			},
		},
	}

	assert.Equal(t, "trace-123", re.Event.TraceID)
	assert.NotNil(t, re.Artifacts)
	assert.Contains(t, re.Artifacts, "input_artifact")
}

func TestReconstructedEvent_JSONSerialization(t *testing.T) {
	re := reconstructedEvent{
		Event: wideEvent{
			TraceID: "trace-json",
			SpanID:  "span-json",
			Status:  "success",
		},
		Artifacts: map[string]any{
			"test_artifact": "content",
		},
	}

	data, err := json.Marshal(re)
	assert.NoError(t, err)

	var decoded reconstructedEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, re.Event.TraceID, decoded.Event.TraceID)
	assert.NotNil(t, decoded.Artifacts)
}

func TestReconstructedEvent_EmptyArtifacts(t *testing.T) {
	re := reconstructedEvent{
		Event: wideEvent{
			TraceID: "trace-no-artifacts",
		},
	}

	assert.Nil(t, re.Artifacts)
}

// Tests for trajectoryEvent structure

func TestTrajectoryEvent_AllFields(t *testing.T) {
	te := trajectoryEvent{
		ID:           "evt-123",
		TrajectoryID: "traj-456",
		Timestamp:    "2026-01-15T10:00:00.000Z",
		Kind:         "tool_call",
		Actor:        "claude",
		Command:      "Bash",
		Status:       "success",
		DataInline:   map[string]any{"inline": "data"},
		DataArtifact: "sha256:xyz",
		FullData:     map[string]any{"full": "data"},
	}

	assert.Equal(t, "evt-123", te.ID)
	assert.Equal(t, "traj-456", te.TrajectoryID)
	assert.Equal(t, "2026-01-15T10:00:00.000Z", te.Timestamp)
	assert.Equal(t, "tool_call", te.Kind)
	assert.Equal(t, "claude", te.Actor)
	assert.Equal(t, "Bash", te.Command)
	assert.Equal(t, "success", te.Status)
	assert.NotNil(t, te.DataInline)
	assert.Equal(t, "sha256:xyz", te.DataArtifact)
	assert.NotNil(t, te.FullData)
}

func TestTrajectoryEvent_JSONSerialization(t *testing.T) {
	te := trajectoryEvent{
		ID:           "evt-test",
		TrajectoryID: "traj-test",
		Kind:         "message",
		Actor:        "user",
	}

	data, err := json.Marshal(te)
	assert.NoError(t, err)

	var decoded trajectoryEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, te.ID, decoded.ID)
	assert.Equal(t, te.TrajectoryID, decoded.TrajectoryID)
	assert.Equal(t, te.Kind, decoded.Kind)
	assert.Equal(t, te.Actor, decoded.Actor)
}

func TestTrajectoryEvent_EmptyFields(t *testing.T) {
	te := trajectoryEvent{}

	assert.Empty(t, te.ID)
	assert.Empty(t, te.TrajectoryID)
	assert.Empty(t, te.Timestamp)
	assert.Empty(t, te.Kind)
	assert.Nil(t, te.DataInline)
	assert.Nil(t, te.FullData)
}

// Tests for truncateID helper

func TestTruncateID_ShortID(t *testing.T) {
	result := truncateID("short")
	assert.Equal(t, "short", result)
}

func TestTruncateID_ExactLength(t *testing.T) {
	// 16 characters
	id := "1234567890123456"
	result := truncateID(id)
	assert.Equal(t, id, result)
}

func TestTruncateID_LongID(t *testing.T) {
	// 20 characters
	id := "12345678901234567890"
	result := truncateID(id)
	assert.Equal(t, "1234567890123456...", result)
	assert.Len(t, result, 19) // 16 + 3 for "..."
}

func TestTruncateID_Empty(t *testing.T) {
	result := truncateID("")
	assert.Equal(t, "", result)
}

func TestTruncateID_UUID(t *testing.T) {
	// Typical UUID format (36 characters)
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	result := truncateID(uuid)
	assert.Equal(t, "550e8400-e29b-41...", result)
}

func TestTruncateID_TraceID(t *testing.T) {
	// Typical trace ID (32 hex characters)
	traceID := "abc123def456789012345678abcdef01"
	result := truncateID(traceID)
	assert.Equal(t, "abc123def4567890...", result)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		TraceID:     "trace-full-test",
		SpanID:      "span-full-test",
		IncludeData: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.TraceID, decoded.TraceID)
	assert.Equal(t, in.SpanID, decoded.SpanID)
	assert.Equal(t, in.IncludeData, decoded.IncludeData)
}

func TestWideEvent_FullJSONRoundTrip(t *testing.T) {
	evt := wideEvent{
		Timestamp:   "2026-01-15T12:00:00Z",
		TraceID:     "trace-round-trip",
		SpanID:      "span-round-trip",
		Service:     "agentctl",
		Version:     "2.0.0",
		Component:   "skill",
		Operation:   "execute",
		Command:     "code/search",
		WorkspaceID: "/test/workspace",
		JobID:       "job-test",
		Status:      "success",
		DurationMS:  250,
		ErrorCode:   "",
		ErrorMsg:    "",
		Data: map[string]any{
			"input_artifact":  "sha256:in",
			"result_artifact": "sha256:out",
		},
	}

	data, err := json.Marshal(evt)
	assert.NoError(t, err)

	var decoded wideEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, evt.TraceID, decoded.TraceID)
	assert.Equal(t, evt.DurationMS, decoded.DurationMS)
	assert.NotNil(t, decoded.Data)
}

func TestWideEvent_LargeDuration(t *testing.T) {
	evt := wideEvent{
		TraceID:    "trace-long",
		DurationMS: 3600000, // 1 hour in milliseconds
		Status:     "timeout",
	}

	data, err := json.Marshal(evt)
	assert.NoError(t, err)

	var decoded wideEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, int64(3600000), decoded.DurationMS)
}

func TestTrajectoryEvent_KindValues(t *testing.T) {
	kinds := []string{"tool_call", "tool_result", "message", "thinking", "user_input"}

	for _, kind := range kinds {
		te := trajectoryEvent{Kind: kind}
		assert.Equal(t, kind, te.Kind)
	}
}

func TestReconstructedEvent_ComplexArtifacts(t *testing.T) {
	re := reconstructedEvent{
		Event: wideEvent{TraceID: "trace-complex"},
		Artifacts: map[string]any{
			"input_artifact": map[string]any{
				"digest":  "sha256:input123",
				"content": map[string]any{"query": "test query"},
			},
			"result_artifact": map[string]any{
				"digest": "sha256:result456",
				"content": []any{
					map[string]any{"file": "a.go"},
					map[string]any{"file": "b.go"},
				},
			},
		},
	}

	data, err := json.Marshal(re)
	assert.NoError(t, err)

	var decoded reconstructedEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.NotNil(t, decoded.Artifacts)
	assert.Contains(t, decoded.Artifacts, "input_artifact")
	assert.Contains(t, decoded.Artifacts, "result_artifact")
}

func TestWideEvent_StatusValues(t *testing.T) {
	statuses := []string{"success", "error", "timeout", "cancelled", "skipped"}

	for _, status := range statuses {
		evt := wideEvent{Status: status}
		assert.Equal(t, status, evt.Status)
	}
}

func TestInput_TraceIDOnly(t *testing.T) {
	in := input{
		TraceID: "trace-only-id-12345678901234567890",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "trace_id")
	assert.Contains(t, string(data), "trace-only-id-12345678901234567890")
}
