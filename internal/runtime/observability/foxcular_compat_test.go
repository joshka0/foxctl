package observability

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
)

func TestFoxcularBridgeSend(t *testing.T) {
	dir := t.TempDir()
	SetObsDirForTesting(dir)
	defer SetObsDirForTesting("")

	bridge := &foxcularBridge{}

	now := time.Now().UTC()
	event := &foxcular.Event{
		Timestamp: now,
		TraceID:   "test-trace-123",
		SpanID:    "test-span-456",
		ParentID:  "test-parent-789",
		Operation: "skill.run",
		Name:      "code/snippet_extract",
		Status:    foxcular.StatusOK,
		Duration:  150 * time.Millisecond,
		Data: map[string]any{
			"service":      "foxctl",
			"version":      "test",
			"component":    "skill",
			"session_id":   "sess-1",
			"agent_id":     "agent-1",
			"workspace_id": "ws-1",
			"files_count":  42,
		},
	}

	err := bridge.Send(context.Background(), event)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Verify NDJSON file was created
	ndjsonPath := filepath.Join(dir, "events", "wide_events.ndjson")
	data, err := os.ReadFile(ndjsonPath)
	if err != nil {
		t.Fatalf("Failed to read NDJSON: %v", err)
	}

	// Parse the written event
	var we WideEvent
	// Parse NDJSON lines
	lines := splitLines(string(data))
	if len(lines) == 0 {
		t.Fatal("No events written")
	}
	if err := json.Unmarshal([]byte(lines[0]), &we); err != nil {
		t.Fatalf("Failed to parse NDJSON: %v", err)
	}

	// Verify mapping
	if we.TraceID != "test-trace-123" {
		t.Errorf("TraceID = %q, want test-trace-123", we.TraceID)
	}
	if we.SpanID != "test-span-456" {
		t.Errorf("SpanID = %q, want test-span-456", we.SpanID)
	}
	if we.ParentID != "test-parent-789" {
		t.Errorf("ParentID = %q, want test-parent-789", we.ParentID)
	}
	if we.Operation != "skill.run" {
		t.Errorf("Operation = %q, want skill.run", we.Operation)
	}
	if we.Status != StatusOK {
		t.Errorf("Status = %q, want ok", we.Status)
	}
	if we.DurationMS != 150 {
		t.Errorf("DurationMS = %d, want 150", we.DurationMS)
	}
	if we.Service != "foxctl" {
		t.Errorf("Service = %q, want foxctl", we.Service)
	}
	if we.Component != "skill" {
		t.Errorf("Component = %q, want skill", we.Component)
	}
	if we.Command != "code/snippet_extract" {
		t.Errorf("Command = %q, want code/snippet_extract", we.Command)
	}
	if we.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", we.SessionID)
	}
	if we.AgentID != "agent-1" {
		t.Errorf("AgentID = %q, want agent-1", we.AgentID)
	}
	if we.WorkspaceID != "ws-1" {
		t.Errorf("WorkspaceID = %q, want ws-1", we.WorkspaceID)
	}
	if we.Data["files_count"] != float64(42) {
		t.Errorf("Data[files_count] = %v, want 42", we.Data["files_count"])
	}
}

func TestFoxcularBridgeSendError(t *testing.T) {
	dir := t.TempDir()
	SetObsDirForTesting(dir)
	defer SetObsDirForTesting("")

	bridge := &foxcularBridge{}

	event := &foxcular.Event{
		Timestamp:    time.Now().UTC(),
		TraceID:      "err-trace",
		SpanID:       "err-span",
		Operation:    "skill.run",
		Status:       foxcular.StatusError,
		Duration:     50 * time.Millisecond,
		ErrorType:    "timeout",
		ErrorCode:    "ETIMEDOUT",
		ErrorMessage: "connection timed out",
		Data: map[string]any{
			"retriable": true,
		},
	}

	err := bridge.Send(context.Background(), event)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	ndjsonPath := filepath.Join(dir, "events", "wide_events.ndjson")
	data, err := os.ReadFile(ndjsonPath)
	if err != nil {
		t.Fatalf("Failed to read NDJSON: %v", err)
	}

	var we WideEvent
	lines := splitLines(string(data))
	if err := json.Unmarshal([]byte(lines[0]), &we); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if we.Status != StatusError {
		t.Errorf("Status = %q, want error", we.Status)
	}
	if we.ErrorType != "timeout" {
		t.Errorf("ErrorType = %q, want timeout", we.ErrorType)
	}
	if we.ErrorCode != "ETIMEDOUT" {
		t.Errorf("ErrorCode = %q, want ETIMEDOUT", we.ErrorCode)
	}
	if we.ErrorMessage != "connection timed out" {
		t.Errorf("ErrorMessage = %q, want connection timed out", we.ErrorMessage)
	}
	if we.Retriable == nil || !*we.Retriable {
		t.Error("Retriable = nil/false, want true")
	}
}

func TestFoxcularBridgeCanceled(t *testing.T) {
	bridge := &foxcularBridge{}

	event := &foxcular.Event{
		Operation: "job.run",
		Status:    foxcular.StatusCanceled,
	}

	// Should not fail even without FOXCTL_OBS_DIR
	SetObsDirForTesting("")
	err := bridge.Send(context.Background(), event)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

func TestWideEventToFoxcularEventRoundTrip(t *testing.T) {
	retriable := true
	original := &WideEvent{
		Ts:           time.Now().UTC(),
		TraceID:      "trace-rt",
		SpanID:       "span-rt",
		ParentID:     "parent-rt",
		Service:      "foxctl",
		Version:      "1.0",
		Component:    ComponentSkill,
		Operation:    OpSkillRun,
		Command:      "code/snippet_extract",
		Subtype:      "runservice",
		SessionID:    "sess-rt",
		AgentID:      "agent-rt",
		WorkspaceID:  "ws-rt",
		JobID:        "job-rt",
		Status:       StatusOK,
		DurationMS:   200,
		Retriable:    &retriable,
		ErrorMessage: "",
		Data: map[string]any{
			"files_count":  10,
			"custom_field": "custom_value",
		},
	}

	// Convert to foxcular
	fe := wideEventToFoxcularEvent(original)

	// Verify key fields
	if fe.TraceID != "trace-rt" {
		t.Errorf("TraceID = %q, want trace-rt", fe.TraceID)
	}
	if fe.SpanID != "span-rt" {
		t.Errorf("SpanID = %q, want span-rt", fe.SpanID)
	}
	if fe.ParentID != "parent-rt" {
		t.Errorf("ParentID = %q, want parent-rt", fe.ParentID)
	}
	if fe.Operation != "skill.run" {
		t.Errorf("Operation = %q, want skill.run", fe.Operation)
	}
	if fe.Status != foxcular.StatusOK {
		t.Errorf("Status = %q, want ok", fe.Status)
	}
	if fe.Duration.Milliseconds() != 200 {
		t.Errorf("Duration = %v, want 200ms", fe.Duration)
	}

	// Verify foxctl-specific fields preserved in data
	if v, ok := fe.Data["service"].(string); !ok || v != "foxctl" {
		t.Errorf("Data[service] = %v, want foxctl", fe.Data["service"])
	}
	if v, ok := fe.Data["component"].(string); !ok || v != "skill" {
		t.Errorf("Data[component] = %v, want skill", fe.Data["component"])
	}
	if v, ok := fe.Data["command"].(string); !ok || v != "code/snippet_extract" {
		t.Errorf("Data[command] = %v, want code/snippet_extract", fe.Data["command"])
	}
	if v, ok := fe.Data["session_id"].(string); !ok || v != "sess-rt" {
		t.Errorf("Data[session_id] = %v, want sess-rt", fe.Data["session_id"])
	}
	if v, ok := fe.Data["retriable"].(bool); !ok || !v {
		t.Errorf("Data[retriable] = %v, want true", fe.Data["retriable"])
	}
	if _, ok := fe.Data["files_count"]; !ok {
		t.Error("Data[files_count] missing")
	}

	// Now convert back to WideEvent
	we := foxcularEventToWideEvent(fe)

	if we.TraceID != "trace-rt" {
		t.Errorf("Round-trip TraceID = %q, want trace-rt", we.TraceID)
	}
	if we.SpanID != "span-rt" {
		t.Errorf("Round-trip SpanID = %q, want span-rt", we.SpanID)
	}
	if we.Operation != "skill.run" {
		t.Errorf("Round-trip Operation = %q, want skill.run", we.Operation)
	}
	if we.Status != StatusOK {
		t.Errorf("Round-trip Status = %q, want ok", we.Status)
	}
	if we.DurationMS != 200 {
		t.Errorf("Round-trip DurationMS = %d, want 200", we.DurationMS)
	}
}

func TestInitAndCloseFoxcular(t *testing.T) {
	// Should be safe to call multiple times
	InitFoxcular()
	InitFoxcular()

	client := FoxcularClient()
	if client == nil {
		t.Fatal("FoxcularClient() = nil, want non-nil after InitFoxcular")
	}

	if err := CloseFoxcular(); err != nil {
		t.Fatalf("CloseFoxcular failed: %v", err)
	}

	if FoxcularClient() != nil {
		t.Error("FoxcularClient() should be nil after CloseFoxcular")
	}

	// Close again should be safe
	if err := CloseFoxcular(); err != nil {
		t.Fatalf("Second CloseFoxcular failed: %v", err)
	}
}

func TestEmitViaFoxcularDisabled(t *testing.T) {
	// When foxcular is not initialized, emitViaFoxcular should return false
	we := &WideEvent{
		Ts:        time.Now().UTC(),
		TraceID:   "test",
		SpanID:    "test",
		Operation: "test",
		Status:    StatusOK,
		Data:      map[string]any{},
	}
	handled := emitViaFoxcular(context.Background(), we, false)
	if handled {
		t.Error("emitViaFoxcular should return false when not initialized")
	}
}

func TestEmitViaFoxcularEnabled(t *testing.T) {
	dir := t.TempDir()
	SetObsDirForTesting(dir)
	defer SetObsDirForTesting("")

	InitFoxcular()
	defer CloseFoxcular()

	we := &WideEvent{
		Ts:        time.Now().UTC(),
		TraceID:   "foxcular-test-trace",
		SpanID:    "foxcular-test-span",
		Operation: "test.op",
		Status:    StatusOK,
		Data:      map[string]any{},
	}

	handled := emitViaFoxcular(context.Background(), we, true)
	if !handled {
		t.Error("emitViaFoxcular should return true when initialized")
	}

	// Check the event was persisted
	ndjsonPath := filepath.Join(dir, "events", "wide_events.ndjson")
	data, err := os.ReadFile(ndjsonPath)
	if err != nil {
		t.Fatalf("Failed to read NDJSON: %v", err)
	}

	var result WideEvent
	lines := splitLines(string(data))
	if len(lines) == 0 {
		t.Fatal("No events written")
	}
	if err := json.Unmarshal([]byte(lines[0]), &result); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	if result.TraceID != "foxcular-test-trace" {
		t.Errorf("TraceID = %q, want foxcular-test-trace", result.TraceID)
	}
}

// splitLines splits a string into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range splitRawLines(s) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitRawLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
