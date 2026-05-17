package observability

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartSpan_Success(t *testing.T) {
	// Use test sampler to capture events
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	ctx := context.Background()
	ctx, done, builder := StartSpan(
		ctx, OpSkillRun,
		WithSpanComponent(ComponentSkill),
		WithSpanCommand("test/skill"),
		WithSpanSubtype("test"),
	)

	// Verify trace ID was set
	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		t.Error("expected trace ID to be set in context")
	}
	spanID := SpanIDFromContext(ctx)
	if spanID == "" {
		t.Error("expected span ID to be set in context")
	}
	if spanID != builder.event.SpanID {
		t.Errorf("expected context span ID %q, got %q", builder.event.SpanID, spanID)
	}

	// Verify builder was populated
	if builder.event.Operation != OpSkillRun {
		t.Errorf("expected operation %q, got %q", OpSkillRun, builder.event.Operation)
	}
	if eventDataString(builder.event, "component") != ComponentSkill {
		t.Errorf("expected component %q, got %q", ComponentSkill, eventDataString(builder.event, "component"))
	}
	if builder.event.Name != "test/skill" {
		t.Errorf("expected command %q, got %q", "test/skill", builder.event.Name)
	}
	if eventDataString(builder.event, "subtype") != "test" {
		t.Errorf("expected subtype %q, got %q", "test", eventDataString(builder.event, "subtype"))
	}

	// Call done with nil error (success)
	done(nil)
}

func TestStartSpan_Error(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	ctx := context.Background()
	_, done, _ := StartSpan(ctx, OpSkillRun)

	// Call done with error
	done(errors.New("test error"))
}

func TestStartSpan_Canceled(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	ctx := context.Background()
	_, done, _ := StartSpan(ctx, OpSkillRun)

	// Call done with context.Canceled
	done(context.Canceled)
}

func TestStartSpan_WithExistingTraceID(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	existingTraceID := "01EXISTING123456789"
	ctx := context.Background()

	// Start span with explicit trace ID
	ctx, done, builder := StartSpan(
		ctx, OpSkillRun,
		WithSpanTraceID(existingTraceID),
	)
	defer done(nil)

	// Verify trace ID was used
	if builder.event.TraceID != existingTraceID {
		t.Errorf("expected trace ID %q, got %q", existingTraceID, builder.event.TraceID)
	}

	// Verify context also has the trace ID
	ctxTraceID := TraceIDFromContext(ctx)
	if ctxTraceID != existingTraceID {
		t.Errorf("expected context trace ID %q, got %q", existingTraceID, ctxTraceID)
	}
}

func TestStartSpan_AllOptions(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	ctx := context.Background()
	_, done, builder := StartSpan(
		ctx, OpSkillRun,
		WithSpanTraceID("trace123"),
		WithSpanParentID("parent456"),
		WithSpanComponent(ComponentHook),
		WithSpanCommand("test/command"),
		WithSpanSubtype("mysubtype"),
		WithSpanSession("session789", "agent000"),
		WithSpanWorkspace("/path/to/workspace"),
		WithSpanJobID("job111"),
		WithSpanData("key1", "value1"),
		WithSpanDataMap(map[string]any{"key2": 42, "key3": true}),
	)
	defer done(nil)

	ev := builder.event
	if ev.TraceID != "trace123" {
		t.Errorf("expected trace ID %q, got %q", "trace123", ev.TraceID)
	}
	if ev.ParentID != "parent456" {
		t.Errorf("expected parent ID %q, got %q", "parent456", ev.ParentID)
	}
	if eventDataString(ev, "component") != ComponentHook {
		t.Errorf("expected component %q, got %q", ComponentHook, eventDataString(ev, "component"))
	}
	if ev.Name != "test/command" {
		t.Errorf("expected command %q, got %q", "test/command", ev.Name)
	}
	if eventDataString(ev, "subtype") != "mysubtype" {
		t.Errorf("expected subtype %q, got %q", "mysubtype", eventDataString(ev, "subtype"))
	}
	if eventDataString(ev, "session_id") != "session789" {
		t.Errorf("expected session ID %q, got %q", "session789", eventDataString(ev, "session_id"))
	}
	if eventDataString(ev, "agent_id") != "agent000" {
		t.Errorf("expected agent ID %q, got %q", "agent000", eventDataString(ev, "agent_id"))
	}
	if eventDataString(ev, "workspace_id") != "/path/to/workspace" {
		t.Errorf("expected workspace %q, got %q", "/path/to/workspace", eventDataString(ev, "workspace_id"))
	}
	if eventDataString(ev, "job_id") != "job111" {
		t.Errorf("expected job ID %q, got %q", "job111", eventDataString(ev, "job_id"))
	}
	if ev.Data["key1"] != "value1" {
		t.Errorf("expected data key1=%q, got %v", "value1", ev.Data["key1"])
	}
	if ev.Data["key2"] != 42 {
		t.Errorf("expected data key2=%v, got %v", 42, ev.Data["key2"])
	}
	if ev.Data["key3"] != true {
		t.Errorf("expected data key3=%v, got %v", true, ev.Data["key3"])
	}
}

func TestStartSpanAt_CustomStartTime(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	// Start time 100ms ago
	startTime := time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	_, done, _ := StartSpanAt(ctx, startTime, OpSkillRun)

	// done() will compute duration from startTime
	done(nil)
}

func TestStartSpan_NilOptions(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	ctx := context.Background()
	// Should not panic with nil options
	_, done, builder := StartSpan(
		ctx, OpSkillRun,
		nil, // nil option should be ignored
		WithSpanCommand("test"),
		nil, // another nil
	)
	defer done(nil)

	if builder.event.Name != "test" {
		t.Errorf("expected command %q, got %q", "test", builder.event.Name)
	}
}

func TestWithSpanDataMap_Empty(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	ctx := context.Background()
	_, done, builder := StartSpan(
		ctx, OpSkillRun,
		WithSpanDataMap(nil),              // nil map
		WithSpanDataMap(map[string]any{}), // empty map
	)
	defer done(nil)

	// Should not panic and data should be initialized
	if builder.event.Data == nil {
		t.Error("expected data map to be initialized")
	}
}

func TestStartSpan_InheritsFromContext(t *testing.T) {
	SetSamplerForTesting(SampleNone{})
	defer SetSamplerForTesting(nil)

	// Pre-set trace ID in context
	existingTraceID := "01CONTEXTRACE123456"
	ctx := WithTraceID(context.Background(), existingTraceID)

	// Start span without explicit trace ID - should inherit from context
	ctx2, done, builder := StartSpan(ctx, OpSkillRun)
	defer done(nil)

	if builder.event.TraceID != existingTraceID {
		t.Errorf("expected inherited trace ID %q, got %q", existingTraceID, builder.event.TraceID)
	}

	// Context should still have the same trace ID
	if TraceIDFromContext(ctx2) != existingTraceID {
		t.Errorf("expected context trace ID %q, got %q", existingTraceID, TraceIDFromContext(ctx2))
	}
	if SpanIDFromContext(ctx2) != builder.event.SpanID {
		t.Errorf("expected context span ID %q, got %q", builder.event.SpanID, SpanIDFromContext(ctx2))
	}
}
