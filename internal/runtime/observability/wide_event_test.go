package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	event := NewEvent("test.operation")
	built := event.Build()

	if built.Operation != "test.operation" {
		t.Errorf("Operation = %q, want test.operation", built.Operation)
	}
	if built.Service != "foxctl" {
		t.Errorf("Service = %q, want foxctl", built.Service)
	}
	if built.SpanID == "" {
		t.Error("SpanID should be auto-generated")
	}
	if built.TraceID == "" {
		t.Error("TraceID should be auto-generated on Build()")
	}
	if built.Ts.IsZero() {
		t.Error("Ts should be set")
	}
}

func TestEventBuilder_FluentAPI(t *testing.T) {
	event := NewEvent(OpSkillRun).
		WithTraceID("trace-123").
		WithParentID("parent-456").
		WithComponent(ComponentSkill).
		WithCommand("code/snippet_extract").
		WithSubtype("search").
		WithSession("session-abc", "agent-def").
		WithWorkspace("workspace-xyz").
		WithJobID("job-789").
		WithData("files", 10).
		WithData("cached", true).
		Build()

	if event.TraceID != "trace-123" {
		t.Errorf("TraceID = %q, want trace-123", event.TraceID)
	}
	if event.ParentID != "parent-456" {
		t.Errorf("ParentID = %q, want parent-456", event.ParentID)
	}
	if event.Component != ComponentSkill {
		t.Errorf("Component = %q, want %q", event.Component, ComponentSkill)
	}
	if event.Command != "code/snippet_extract" {
		t.Errorf("Command = %q, want code/snippet_extract", event.Command)
	}
	if event.Subtype != "search" {
		t.Errorf("Subtype = %q, want search", event.Subtype)
	}
	if event.SessionID != "session-abc" {
		t.Errorf("SessionID = %q, want session-abc", event.SessionID)
	}
	if event.AgentID != "agent-def" {
		t.Errorf("AgentID = %q, want agent-def", event.AgentID)
	}
	if event.WorkspaceID != "workspace-xyz" {
		t.Errorf("WorkspaceID = %q, want workspace-xyz", event.WorkspaceID)
	}
	if event.JobID != "job-789" {
		t.Errorf("JobID = %q, want job-789", event.JobID)
	}
	if event.Data["files"] != 10 {
		t.Errorf("Data[files] = %v, want 10", event.Data["files"])
	}
	if event.Data["cached"] != true {
		t.Errorf("Data[cached] = %v, want true", event.Data["cached"])
	}
}

func TestEventBuilder_Success(t *testing.T) {
	duration := 500 * time.Millisecond
	event := NewEvent("test.op").
		WithData("count", 42).
		Success(duration)

	if event.Status != StatusOK {
		t.Errorf("Status = %q, want %q", event.Status, StatusOK)
	}
	if event.DurationMS != 500 {
		t.Errorf("DurationMS = %d, want 500", event.DurationMS)
	}
}

func TestEventBuilder_Error(t *testing.T) {
	duration := 100 * time.Millisecond
	err := errors.New("connection refused: dial tcp 127.0.0.1:8080")
	event := NewEvent("test.op").Error(err, duration)

	if event.Status != StatusError {
		t.Errorf("Status = %q, want %q", event.Status, StatusError)
	}
	if event.DurationMS != 100 {
		t.Errorf("DurationMS = %d, want 100", event.DurationMS)
	}
	if event.ErrorMessage != err.Error() {
		t.Errorf("ErrorMessage = %q, want %q", event.ErrorMessage, err.Error())
	}
	if event.ErrorType != "network" {
		t.Errorf("ErrorType = %q, want network", event.ErrorType)
	}
}

func TestEventBuilder_ErrorWithDetails(t *testing.T) {
	duration := 200 * time.Millisecond
	event := NewEvent("test.op").
		ErrorWithDetails("validation", "ERR_INVALID_INPUT", "field 'name' is required", true, duration)

	if event.Status != StatusError {
		t.Errorf("Status = %q, want %q", event.Status, StatusError)
	}
	if event.ErrorType != "validation" {
		t.Errorf("ErrorType = %q, want validation", event.ErrorType)
	}
	if event.ErrorCode != "ERR_INVALID_INPUT" {
		t.Errorf("ErrorCode = %q, want ERR_INVALID_INPUT", event.ErrorCode)
	}
	if event.ErrorMessage != "field 'name' is required" {
		t.Errorf("ErrorMessage = %q, want field 'name' is required", event.ErrorMessage)
	}
	if event.Retriable == nil || !*event.Retriable {
		t.Error("Retriable should be true")
	}
}

func TestEventBuilder_Canceled(t *testing.T) {
	duration := 50 * time.Millisecond
	event := NewEvent("test.op").Canceled(duration)

	if event.Status != StatusCanceled {
		t.Errorf("Status = %q, want %q", event.Status, StatusCanceled)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err      error
		wantType string
	}{
		{errors.New("context canceled"), "timeout"},
		{errors.New("context deadline exceeded"), "timeout"},
		{errors.New("permission denied"), "permission"},
		{errors.New("file not found"), "not_found"},
		{errors.New("no such file or directory"), "not_found"},
		{errors.New("invalid JSON"), "validation"},
		{errors.New("parse error: unexpected token"), "validation"},
		{errors.New("connection refused"), "network"},
		{errors.New("dial tcp: connection refused"), "network"},
		{errors.New("some random error"), "internal"},
		{nil, ""},
	}

	for _, tt := range tests {
		got := classifyError(tt.err)
		if got != tt.wantType {
			errMsg := "<nil>"
			if tt.err != nil {
				errMsg = tt.err.Error()
			}
			t.Errorf("classifyError(%q) = %q, want %q", errMsg, got, tt.wantType)
		}
	}
}

func TestEventBuilder_WithDataMap(t *testing.T) {
	data := map[string]any{
		"files":    10,
		"cached":   true,
		"duration": 100,
	}
	event := NewEvent("test.op").WithDataMap(data).Build()

	if len(event.Data) != 3 {
		t.Errorf("Data has %d keys, want 3", len(event.Data))
	}
	if event.Data["files"] != 10 {
		t.Errorf("Data[files] = %v, want 10", event.Data["files"])
	}
}

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()

	// Initially empty
	if id := TraceIDFromContext(ctx); id != "" {
		t.Errorf("TraceIDFromContext should be empty, got %q", id)
	}

	// Set trace ID
	traceID := NewTraceID()
	ctx = WithTraceID(ctx, traceID)

	if got := TraceIDFromContext(ctx); got != traceID {
		t.Errorf("TraceIDFromContext = %q, want %q", got, traceID)
	}

	// EnsureTraceID with existing
	ctx2, id2 := EnsureTraceID(ctx)
	if id2 != traceID {
		t.Errorf("EnsureTraceID should return existing ID, got %q", id2)
	}
	if ctx2 != ctx {
		// Context should be unchanged when ID already exists
		if TraceIDFromContext(ctx2) != traceID {
			t.Error("Context should still have same trace ID")
		}
	}

	// EnsureTraceID without existing
	ctx3 := context.Background()
	ctx3, id3 := EnsureTraceID(ctx3)
	if id3 == "" {
		t.Error("EnsureTraceID should generate new ID")
	}
	if TraceIDFromContext(ctx3) != id3 {
		t.Error("Context should have the new trace ID")
	}
}

func TestPropagationEnv(t *testing.T) {
	ctx := context.Background()
	traceID := "test-trace-id"
	ctx = WithTraceID(ctx, traceID)

	env := PropagationEnv(ctx)
	if len(env) != 1 {
		t.Fatalf("PropagationEnv should have 1 entry, got %d", len(env))
	}
	if env[0] != "FOXCTL_TRACE_ID=test-trace-id" {
		t.Errorf("env[0] = %q, want FOXCTL_TRACE_ID=test-trace-id", env[0])
	}
}

func TestEnrichFromContext(t *testing.T) {
	ctx := context.Background()
	traceID := "ctx-trace-id"
	ctx = WithTraceID(ctx, traceID)

	event := NewEvent("test.op").
		EnrichFromContext(ctx).
		Build()

	if event.TraceID != traceID {
		t.Errorf("TraceID = %q, want %q", event.TraceID, traceID)
	}
}

func TestTailSampler(t *testing.T) {
	sampler := NewTailSampler(true, 1000, 0.0) // No random sampling

	// Error events should always be sampled
	errEvent := &WideEvent{Status: StatusError, DurationMS: 100}
	if sampler.ShouldSample(errEvent) != AlwaysSample {
		t.Error("Error events should always be sampled")
	}

	// Canceled events should always be sampled
	canceledEvent := &WideEvent{Status: StatusCanceled, DurationMS: 100}
	if sampler.ShouldSample(canceledEvent) != AlwaysSample {
		t.Error("Canceled events should always be sampled")
	}

	// Slow events should always be sampled
	slowEvent := &WideEvent{Status: StatusOK, DurationMS: 1500}
	if sampler.ShouldSample(slowEvent) != AlwaysSample {
		t.Error("Slow events should always be sampled")
	}

	// Fast successful events with 0 rate should be dropped
	fastEvent := &WideEvent{Status: StatusOK, DurationMS: 100}
	if sampler.ShouldSample(fastEvent) != Drop {
		t.Error("Fast successful events with 0 rate should be dropped")
	}
}

func TestTailSampler_DisabledErrorSampling(t *testing.T) {
	sampler := NewTailSampler(false, 1000, 0.0)

	// Even errors should be dropped when error sampling is disabled
	// (except if they're slow)
	errEvent := &WideEvent{Status: StatusError, DurationMS: 100}
	if sampler.ShouldSample(errEvent) != Drop {
		t.Error("Errors should be dropped when error sampling is disabled")
	}
}

func TestTailSampler_RandomSampling(t *testing.T) {
	// With 100% sampling rate, everything should be sampled
	sampler := NewTailSampler(true, 1000, 1.0)

	fastEvent := &WideEvent{Status: StatusOK, DurationMS: 100}
	if sampler.ShouldSample(fastEvent) == Drop {
		t.Error("With 100% rate, fast events should be sampled")
	}
}

func TestTailSampler_AlwaysSampleAllowlist(t *testing.T) {
	sampler := NewTailSampler(true, 1000, 0.0)
	sampler.alwaysSampleSessions = map[string]struct{}{
		"session-1": {},
	}
	sampler.alwaysSampleWorkspaces = map[string]struct{}{
		"workspace-1": {},
	}

	if sampler.ShouldSample(&WideEvent{Status: StatusOK, SessionID: "session-1"}) != AlwaysSample {
		t.Error("session allowlist should always sample")
	}
	if sampler.ShouldSample(&WideEvent{Status: StatusOK, WorkspaceID: "workspace-1"}) != AlwaysSample {
		t.Error("workspace allowlist should always sample")
	}
	if sampler.ShouldSample(&WideEvent{Status: StatusOK, Data: map[string]any{"debug": true}}) != AlwaysSample {
		t.Error("debug flag should always sample")
	}
	if sampler.ShouldSample(&WideEvent{Status: StatusOK, Data: map[string]any{"always_sample": true}}) != AlwaysSample {
		t.Error("always_sample flag should always sample")
	}
	if sampler.ShouldSample(&WideEvent{Status: StatusOK, SessionID: "session-other"}) != Drop {
		t.Error("non-allowlisted session should be dropped with 0 rate")
	}
}

func TestSampleAll(t *testing.T) {
	sampler := SampleAll{}
	event := &WideEvent{Status: StatusOK, DurationMS: 1}
	if sampler.ShouldSample(event) != AlwaysSample {
		t.Error("SampleAll should always return AlwaysSample")
	}
}

func TestSampleNone(t *testing.T) {
	sampler := SampleNone{}
	event := &WideEvent{Status: StatusError, DurationMS: 10000}
	if sampler.ShouldSample(event) != Drop {
		t.Error("SampleNone should always return Drop")
	}
}

func TestEmit_Disabled(t *testing.T) {
	SetObsDirForTesting("")
	SetSamplerForTesting(SampleAll{})

	event := NewEvent("test.op").Success(100 * time.Millisecond)

	// Should not panic, just no-op
	Emit(context.Background(), event)
}

func TestEmit_Enabled(t *testing.T) {
	tmpDir := t.TempDir()
	SetObsDirForTesting(tmpDir)
	SetSamplerForTesting(SampleAll{})

	event := NewEvent(OpSkillRun).
		WithTraceID("test-trace").
		WithComponent(ComponentSkill).
		WithCommand("code/snippet_extract").
		WithWorkspace("test-ws").
		WithData("files", 5).
		Success(250 * time.Millisecond)

	Emit(context.Background(), event)

	// Verify file was written
	filePath := filepath.Join(tmpDir, "events", WideEventFileName+".ndjson")
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open events file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("Expected at least one line in events file")
	}

	var decoded WideEvent
	if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if decoded.TraceID != "test-trace" {
		t.Errorf("TraceID = %q, want test-trace", decoded.TraceID)
	}
	if decoded.Operation != OpSkillRun {
		t.Errorf("Operation = %q, want %q", decoded.Operation, OpSkillRun)
	}
	if decoded.Command != "code/snippet_extract" {
		t.Errorf("Command = %q, want code/snippet_extract", decoded.Command)
	}
	if decoded.Status != StatusOK {
		t.Errorf("Status = %q, want %q", decoded.Status, StatusOK)
	}
	if decoded.DurationMS != 250 {
		t.Errorf("DurationMS = %d, want 250", decoded.DurationMS)
	}
	if decoded.Data["files"] != float64(5) { // JSON numbers are float64
		t.Errorf("Data[files] = %v, want 5", decoded.Data["files"])
	}
}

func TestEmit_Sampled(t *testing.T) {
	tmpDir := t.TempDir()
	SetObsDirForTesting(tmpDir)
	// Use sampler that drops non-error events
	SetSamplerForTesting(NewTailSampler(true, 1000, 0.0))

	// This should be dropped (fast, successful)
	fastEvent := NewEvent("test.op").Success(100 * time.Millisecond)
	Emit(context.Background(), fastEvent)

	// This should be sampled (error)
	errEvent := NewEvent("test.op").Error(errors.New("test error"), 100*time.Millisecond)
	Emit(context.Background(), errEvent)

	// Verify only error event was written
	filePath := filepath.Join(tmpDir, "events", WideEventFileName+".ndjson")
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("Failed to open events file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
		var decoded WideEvent
		if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
			t.Fatalf("Failed to unmarshal event: %v", err)
		}
		if decoded.Status != StatusError {
			t.Errorf("Expected only error events, got status %q", decoded.Status)
		}
	}

	if count != 1 {
		t.Errorf("Expected 1 event (error only), got %d", count)
	}
}
