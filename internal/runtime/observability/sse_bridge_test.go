package observability

import (
	"testing"
	"time"
)

func TestShouldPublishToSSE_ContextAndV2Prefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   string
		want bool
	}{
		{name: "context", op: "context.layered_bundle", want: true},
		{name: "v2-runtime", op: "v2.runtime.enricher.error", want: true},
		{name: "orchestration", op: "orchestration.dispatch", want: true},
		{name: "v2-unscoped", op: "v2.some.other.event", want: false},
		{name: "agent", op: "agent.spawn", want: true},
		{name: "non-whitelisted", op: "debug.trace", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldPublishToSSE(&Event{Operation: tc.op})
			if got != tc.want {
				t.Fatalf("shouldPublishToSSE(%q)=%v want %v", tc.op, got, tc.want)
			}
		})
	}
}

func TestExtractActivityData_IncludesContextRefs(t *testing.T) {
	t.Parallel()

	event := &Event{
		Data: map[string]any{
			"search_path":        "vector",
			"artifact_hit_count": 3,
			"refs":               []string{"turn/t1", "turn/t2"},
			"turn_refs":          []string{"turn/t1"},
			"slice_refs":         []string{"turn/t1#msg:m1:1-20"},
			"episode_refs":       []string{"episode/e1"},
			"narrative_refs":     []string{"turn/t1/artifact/narrative/v1"},
			"artifact_refs":      []string{"turn/t2/artifact/annotation/v1"},
			"ref_count":          6,
			"ignored_field":      "ignore-me",
		},
	}

	got := extractActivityData(event)
	if got == nil {
		t.Fatal("extractActivityData() returned nil")
	}
	if _, ok := got["refs"]; !ok {
		t.Fatalf("refs missing from extracted data: %+v", got)
	}
	if _, ok := got["turn_refs"]; !ok {
		t.Fatalf("turn_refs missing from extracted data: %+v", got)
	}
	if _, ok := got["artifact_refs"]; !ok {
		t.Fatalf("artifact_refs missing from extracted data: %+v", got)
	}
	if _, ok := got["ignored_field"]; ok {
		t.Fatalf("ignored_field should not be present: %+v", got)
	}
}

func TestExtractActivityData_IncludesOrchestrationFields(t *testing.T) {
	t.Parallel()

	event := &Event{
		Data: map[string]any{
			"request_id":       "req-123",
			"issue_id":         "issue-123",
			"issue_identifier": "ABC-123",
			"lane":             "Running",
			"last_outcome":     "policy_denied",
			"policy_status":    "denied",
			"eligibility":      "ineligible",
			"queued":           true,
			"ignored_field":    "ignore-me",
		},
	}

	got := extractActivityData(event)
	if got == nil {
		t.Fatal("extractActivityData() returned nil")
	}
	required := []string{
		"request_id",
		"issue_id",
		"issue_identifier",
		"lane",
		"last_outcome",
		"policy_status",
		"eligibility",
		"queued",
	}
	for _, key := range required {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in extracted data: %+v", key, got)
		}
	}
	if _, ok := got["ignored_field"]; ok {
		t.Fatalf("ignored_field should not be present: %+v", got)
	}
}

func TestPublishToSSE_EmitsActivityForContextLayeredBundle(t *testing.T) {
	pub := &capturePublisher{}
	SetSSEPublisher(pub)
	t.Cleanup(func() {
		SetSSEPublisher(nil)
	})

	publishToSSE(&Event{
		Operation: OpContextLayeredBundle,
		Status:    StatusOK,
		TraceID:   "trace-ctx-1",
		Timestamp: time.Date(2026, time.February, 27, 10, 0, 0, 0, time.UTC),
		Duration:  12 * time.Millisecond,
		Data: map[string]any{
			"component":  ComponentContextBuilder,
			"session_id": "session-ctx-1",
			"refs":       []string{"turn/t1", "turn/t2"},
			"turn_refs":  []string{"turn/t1"},
			"slice_refs": []string{"turn/t1#msg:m1:1-20"},
		},
	})

	if len(pub.calls) != 1 {
		t.Fatalf("publish calls=%d want 1", len(pub.calls))
	}
	call := pub.calls[0]
	if call.eventType != "activity" {
		t.Fatalf("eventType=%q want activity", call.eventType)
	}
	activity, ok := call.data.(ActivityEvent)
	if !ok {
		t.Fatalf("data type=%T want ActivityEvent", call.data)
	}
	if activity.Operation != OpContextLayeredBundle {
		t.Fatalf("operation=%q want %q", activity.Operation, OpContextLayeredBundle)
	}
	if activity.SessionID != "session-ctx-1" {
		t.Fatalf("session_id=%q want session-ctx-1", activity.SessionID)
	}
	if activity.Data == nil {
		t.Fatal("activity data should not be nil")
	}
	if _, ok := activity.Data["refs"]; !ok {
		t.Fatalf("activity data missing refs: %+v", activity.Data)
	}
}

type publishCall struct {
	eventType string
	data      any
}

type capturePublisher struct {
	calls []publishCall
}

func (c *capturePublisher) Publish(eventType string, data any) {
	c.calls = append(c.calls, publishCall{
		eventType: eventType,
		data:      data,
	})
}
