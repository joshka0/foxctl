package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestEvent_WithData(t *testing.T) {
	evt := observability.Event{
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

func TestReconstructedEvent_EmptyArtifacts(t *testing.T) {
	re := reconstructedEvent{
		Event: observability.Event{
			TraceID: "trace-no-artifacts",
		},
	}

	assert.Nil(t, re.Artifacts)
}

// Tests for trajectoryEvent structure

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

func TestEvent_FullJSONRoundTrip(t *testing.T) {
	evt := observability.Event{
		Timestamp: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		TraceID:   "trace-round-trip",
		SpanID:    "span-round-trip",
		Operation: "execute",
		Name:      "code/search",
		Status:    observability.StatusOK,
		Duration:  250 * time.Millisecond,
		Data: map[string]any{
			observability.DataKeyService:     "foxctl",
			observability.DataKeyVersion:     "2.0.0",
			observability.DataKeyComponent:   "skill",
			observability.DataKeyWorkspaceID: "/test/workspace",
			observability.DataKeyJobID:       "job-test",
			"input_artifact":                 "sha256:in",
			"result_artifact":                "sha256:out",
		},
	}

	data, err := json.Marshal(evt)
	assert.NoError(t, err)

	var decoded observability.Event
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, evt.TraceID, decoded.TraceID)
	assert.Equal(t, evt.Duration, decoded.Duration)
	assert.NotNil(t, decoded.Data)
}

func TestEvent_LargeDuration(t *testing.T) {
	evt := observability.Event{
		TraceID:  "trace-long",
		Duration: time.Hour,
		Status:   observability.StatusCanceled,
	}

	data, err := json.Marshal(evt)
	assert.NoError(t, err)

	var decoded observability.Event
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, time.Hour, decoded.Duration)
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
		Event: observability.Event{TraceID: "trace-complex"},
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

func TestEvent_StatusValues(t *testing.T) {
	statuses := []observability.Status{
		observability.StatusOK,
		observability.StatusError,
		observability.StatusCanceled,
	}

	for _, status := range statuses {
		evt := observability.Event{Status: status}
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
