package foxcular_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
)

// ---------------------------------------------------------------------------
// Sampling Tests: VAL-CORE-021 through VAL-CORE-025
// ---------------------------------------------------------------------------

// newSamplingClient creates a client with the given sampler and disabled
// redaction for pure sampling tests.
func newSamplingClient(sampler foxcular.Sampler) (*foxcular.Client, *captureDrain) {
	drain := &captureDrain{}
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts, ts.Add(50 * time.Millisecond)}}
	ids := &stubIDs{ids: []string{"t1", "s1", "t2", "s2", "t3", "s3"}}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
		foxcular.WithSampler(sampler),
		foxcular.WithRedaction(foxcular.DisabledRedactionPolicy()),
	)
	return client, drain
}

// newRedactionClient creates a client with default sampling and the default
// redaction policy.
func newRedactionClient() (*foxcular.Client, *captureDrain) {
	drain := &captureDrain{}
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts, ts.Add(50 * time.Millisecond)}}
	ids := &stubIDs{ids: []string{"t1", "s1", "t2", "s2", "t3", "s3", "t4", "s4", "t5", "s5"}}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
		foxcular.WithRedaction(foxcular.NewRedactionPolicy()),
	)
	return client, drain
}

// VAL-CORE-021: Always-sample captures all events
func TestAlwaysSampleCapturesAllEvents(t *testing.T) {
	client, drain := newSamplingClient(foxcular.AlwaysSample{})

	for i := range 10 {
		err := client.Emit("test.op").
			WithData("i", i).
			Success(context.Background(), time.Millisecond)
		if err != nil {
			t.Fatalf("emit %d failed: %v", i, err)
		}
	}

	if drain.Len() != 10 {
		t.Errorf("always-sample: expected 10 events, got %d", drain.Len())
	}
}

// VAL-CORE-022: Never-sample drops non-forced events
func TestNeverSampleDropsNonForcedEvents(t *testing.T) {
	client, drain := newSamplingClient(foxcular.NeverSample{})

	for i := range 10 {
		err := client.Emit("test.op").
			WithData("i", i).
			Success(context.Background(), time.Millisecond)
		if err != nil {
			t.Fatalf("emit %d failed: %v", i, err)
		}
	}

	if drain.Len() != 0 {
		t.Errorf("never-sample: expected 0 events, got %d", drain.Len())
	}
}

// VAL-CORE-023: Forced/critical events bypass sampling
func TestForcedEventsBypassSampling(t *testing.T) {
	client, drain := newSamplingClient(foxcular.NeverSample{})

	// Normal event should be dropped.
	_ = client.Emit("normal.op").Success(context.Background(), time.Millisecond)

	// Forced event via builder should pass through never-sample.
	err := client.Emit("critical.op").Forced().
		Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("forced emit failed: %v", err)
	}

	if drain.Len() != 1 {
		t.Fatalf("expected 1 forced event, got %d", drain.Len())
	}
	if drain.Events()[0].Operation != "critical.op" {
		t.Errorf("forced event operation = %q, want critical.op", drain.Events()[0].Operation)
	}

	// Forced via data flags.
	drain.events = nil
	_ = client.Emit("audit.op").
		WithData("forced", true).
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 data-forced event, got %d", drain.Len())
	}

	// Forced via "audit" flag.
	drain.events = nil
	_ = client.Emit("audit2.op").
		WithData("audit", true).
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 audit-forced event, got %d", drain.Len())
	}
}

// VAL-CORE-024: Span sampling decisions are consistent
func TestSpanSamplingDecisionsAreConsistent(t *testing.T) {
	// Use a deterministic sampler that records the first event and drops the rest.
	sampler := foxcular.NewDeterministicSampler(
		foxcular.Record, // first span event: record
		foxcular.Drop,   // second span event: drop
		foxcular.Record, // third span event: record
	)

	client, drain := newSamplingClient(sampler)

	// Span 1: should be recorded.
	_, span1 := client.StartSpan(context.Background(), "span1")
	_ = span1.End(nil)

	// Span 2: should be dropped.
	_, span2 := client.StartSpan(context.Background(), "span2")
	_ = span2.End(nil)

	// Span 3: should be recorded.
	_, span3 := client.StartSpan(context.Background(), "span3")
	_ = span3.End(nil)

	if drain.Len() != 2 {
		t.Errorf("deterministic sampler: expected 2 events, got %d", drain.Len())
	}

	ops := map[string]bool{}
	for _, e := range drain.Events() {
		ops[e.Operation] = true
	}
	if !ops["span1"] || !ops["span3"] {
		t.Errorf("expected span1 and span3 recorded, got %v", ops)
	}
	if ops["span2"] {
		t.Error("span2 should have been dropped")
	}
}

// VAL-CORE-025: Tail sampling retains failed or slow spans
func TestTailSamplingRetainsFailedOrSlowSpans(t *testing.T) {
	// Tail sampler: always sample errors, slow threshold 500ms, 0% random.
	sampler := foxcular.NewTailSampler(
		foxcular.WithSampleErrors(true),
		foxcular.WithSlowThreshold(500),
		foxcular.WithRandomRate(0),
		foxcular.WithRandSource(rand.New(rand.NewSource(42))),
	)

	client, drain := newSamplingClient(sampler)

	tests := []struct {
		name     string
		op       string
		dur      time.Duration
		err      error
		forced   bool
		expected bool
	}{
		{"fast_ok", "fast.ok", 10 * time.Millisecond, nil, false, false},
		{"slow_ok", "slow.ok", 600 * time.Millisecond, nil, false, true},
		{"error", "err.op", 5 * time.Millisecond, fmt.Errorf("fail"), false, true},
		{"canceled", "cancel.op", 5 * time.Millisecond, context.Canceled, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drain.events = nil
			builder := client.Emit(tt.op)
			if tt.forced {
				builder = builder.Forced()
			}
			if tt.err != nil {
				_ = builder.Error(context.Background(), tt.err, tt.dur)
			} else {
				_ = builder.Success(context.Background(), tt.dur)
			}

			got := drain.Len() == 1
			if got != tt.expected {
				t.Errorf("%s: expected recorded=%v, got recorded=%v (drain len=%d)", tt.name, tt.expected, got, drain.Len())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Redaction Tests: VAL-CORE-026 through VAL-CORE-034
// ---------------------------------------------------------------------------

// VAL-CORE-026: Delivered events are redacted regardless of sampling
func TestDeliveredEventsAreRedacted(t *testing.T) {
	drain := &captureDrain{}
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
		foxcular.WithSampler(foxcular.AlwaysSample{}),
		foxcular.WithRedaction(foxcular.NewRedactionPolicy()),
	)

	_ = client.Emit("test.op").Forced().
		WithData("password", "supersecret").
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", drain.Len())
	}
	e := drain.Events()[0]
	if e.Data["password"] != "[REDACTED]" {
		t.Errorf("forced event password not redacted: %v", e.Data["password"])
	}
}

// VAL-CORE-027: Redaction runs before every drain
func TestRedactionRunsBeforeEveryDrain(t *testing.T) {
	drain1 := &captureDrain{}
	drain2 := &captureDrain{}
	fanout := foxcular.NewFanoutDrain(drain1, drain2)

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(fanout,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
		foxcular.WithRedaction(foxcular.NewRedactionPolicy()),
	)

	_ = client.Emit("test.op").
		WithData("token", "bearer abc123secret").
		Success(context.Background(), time.Millisecond)

	for i, d := range []string{"drain1", "drain2"} {
		var events []*foxcular.Event
		if i == 0 {
			events = drain1.Events()
		} else {
			events = drain2.Events()
		}
		if len(events) != 1 {
			t.Fatalf("%s: expected 1 event, got %d", d, len(events))
		}
		if events[0].Data["token"] != "[REDACTED]" {
			t.Errorf("%s: raw token value not redacted: %v", d, events[0].Data["token"])
		}
	}
}

// VAL-CORE-028: Sensitive keys are masked
func TestSensitiveKeysAreMasked(t *testing.T) {
	client, drain := newRedactionClient()

	sensitiveKeys := []string{
		"password", "token", "api_key", "authorization",
		"secret", "credential", "private_key",
	}
	for _, key := range sensitiveKeys {
		_ = client.Emit("test.op").
			WithData(key, "raw-secret-value").
			WithData("safe_key", "preserved").
			Success(context.Background(), time.Millisecond)
	}

	events := drain.Events()
	if len(events) != len(sensitiveKeys) {
		t.Fatalf("expected %d events, got %d", len(sensitiveKeys), len(events))
	}

	for i, key := range sensitiveKeys {
		e := events[i]
		if e.Data[key] != "[REDACTED]" {
			t.Errorf("key %q not redacted: got %v", key, e.Data[key])
		}
		if e.Data["safe_key"] != "preserved" {
			t.Errorf("safe_key was incorrectly modified for event %d: got %v", i, e.Data["safe_key"])
		}
	}
}

// VAL-CORE-029: Sensitive value patterns are masked
func TestSensitiveValuePatternsAreMasked(t *testing.T) {
	client, drain := newRedactionClient()

	patternTests := []struct {
		name     string
		value    string
		contains string // substring that must NOT appear in redacted output
	}{
		{"bearer", "Authorization: bearer sk-abc123def456", "sk-abc123def456"},
		{"jwt", "token is eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", "eyJzdWIiOiIxMjM0NTY3ODkw"},
		{"email", "contact user@example.com for details", "user@example.com"},
		{"credit_card", "card 4111 1111 1111 1111 charged", "4111 1111 1111 1111"},
		{"ipv4", "request from 192.168.1.100", "192.168.1.100"},
		{"phone", "call +1-555-123-4567 now", "555-123-4567"},
		{"iban", "transfer to GB82WEST12345698765432", "GB82WEST12345698765432"},
	}

	for _, tt := range patternTests {
		t.Run(tt.name, func(t *testing.T) {
			drain.events = nil
			_ = client.Emit("test.op").
				WithData("message", tt.value).
				Success(context.Background(), time.Millisecond)

			if drain.Len() != 1 {
				t.Fatalf("expected 1 event, got %d", drain.Len())
			}
			e := drain.Events()[0]
			got, ok := e.Data["message"].(string)
			if !ok {
				t.Fatalf("message is not a string: %T", e.Data["message"])
			}
			if strings.Contains(got, tt.contains) {
				t.Errorf("pattern %q not redacted: got %q", tt.name, got)
			}
			// Redacted output should contain [REDACTED].
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("expected [REDACTED] marker in output: got %q", got)
			}
		})
	}
}

// VAL-CORE-030: Nested maps and slices are redacted
func TestNestedMapsAndSlicesAreRedacted(t *testing.T) {
	client, drain := newRedactionClient()

	_ = client.Emit("test.op").
		WithData("nested", map[string]any{
			"password": "nested-secret",
			"safe":     "preserved",
			"deep": map[string]any{
				"token": "deep-secret",
				"name":  "kept",
			},
		}).
		WithData("items", []any{
			"bearer tok_secret123",
			"normal text",
			map[string]any{
				"api_key": "in-slice-key",
				"label":   "in-slice-safe",
			},
		}).
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", drain.Len())
	}
	e := drain.Events()[0]

	// Check nested map.
	nested, ok := e.Data["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested is not a map")
	}
	if nested["password"] != "[REDACTED]" {
		t.Errorf("nested password not redacted: %v", nested["password"])
	}
	if nested["safe"] != "preserved" {
		t.Errorf("nested safe was modified: %v", nested["safe"])
	}
	deep, ok := nested["deep"].(map[string]any)
	if !ok {
		t.Fatal("nested.deep is not a map")
	}
	if deep["token"] != "[REDACTED]" {
		t.Errorf("deep token not redacted: %v", deep["token"])
	}
	if deep["name"] != "kept" {
		t.Errorf("deep name was modified: %v", deep["name"])
	}

	// Check slice.
	items, ok := e.Data["items"].([]any)
	if !ok {
		t.Fatal("items is not a slice")
	}
	if strings.Contains(items[0].(string), "tok_secret123") {
		t.Errorf("bearer in slice not redacted: %v", items[0])
	}
	if items[1] != "normal text" {
		t.Errorf("safe slice item was modified: %v", items[1])
	}
	sliceMap, ok := items[2].(map[string]any)
	if !ok {
		t.Fatal("items[2] is not a map")
	}
	if sliceMap["api_key"] != "[REDACTED]" {
		t.Errorf("slice map api_key not redacted: %v", sliceMap["api_key"])
	}
	if sliceMap["label"] != "in-slice-safe" {
		t.Errorf("slice map label was modified: %v", sliceMap["label"])
	}
}

// VAL-CORE-031: Redaction preserves safe diagnostic context
func TestRedactionPreservesSafeDiagnosticContext(t *testing.T) {
	client, drain := newRedactionClient()

	_ = client.Emit("test.op").
		WithData("request_id", "req-123").
		WithData("user_count", 42).
		WithData("duration_ms", 150.5).
		WithData("active", true).
		WithData("password", "hunter2").
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", drain.Len())
	}
	e := drain.Events()[0]

	if e.Data["request_id"] != "req-123" {
		t.Errorf("safe request_id was modified: %v", e.Data["request_id"])
	}
	if e.Data["user_count"] != 42 {
		t.Errorf("safe user_count was modified: %v", e.Data["user_count"])
	}
	if e.Data["duration_ms"] != 150.5 {
		t.Errorf("safe duration_ms was modified: %v", e.Data["duration_ms"])
	}
	if e.Data["active"] != true {
		t.Errorf("safe active was modified: %v", e.Data["active"])
	}
	if e.Data["password"] != "[REDACTED]" {
		t.Errorf("password was not redacted: %v", e.Data["password"])
	}
}

// VAL-CORE-032: Custom redaction policy is applied
func TestCustomRedactionPolicyIsApplied(t *testing.T) {
	drain := &captureDrain{}
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}

	customPattern := regexp.MustCompile(`(?i)custom-secret-\w+`)
	policy := foxcular.NewRedactionPolicy(
		foxcular.WithCustomKeys("my_field"),
		foxcular.WithCustomValuePatterns(customPattern),
	)
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
		foxcular.WithRedaction(policy),
	)

	_ = client.Emit("test.op").
		WithData("my_field", "should-be-masked").
		WithData("normal_field", "custom-secret-data-here").
		WithData("untouched", "safe value").
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", drain.Len())
	}
	e := drain.Events()[0]

	if e.Data["my_field"] != "[REDACTED]" {
		t.Errorf("custom key 'my_field' not redacted: %v", e.Data["my_field"])
	}
	if strings.Contains(e.Data["normal_field"].(string), "custom-secret-data") {
		t.Errorf("custom value pattern not redacted: %v", e.Data["normal_field"])
	}
	if e.Data["untouched"] != "safe value" {
		t.Errorf("untouched field was modified: %v", e.Data["untouched"])
	}
}

// VAL-CORE-033: Redaction handles edge values safely
func TestRedactionHandlesEdgeValuesSafely(t *testing.T) {
	client, drain := newRedactionClient()

	ch := make(chan int)
	_ = client.Emit("test.op").
		WithData("nil_val", nil).
		WithData("int_val", 42).
		WithData("bool_val", false).
		WithData("float_val", 0.0).
		WithData("chan_val", ch).
		WithData("empty_string", "").
		WithData("empty_map", map[string]any{}).
		WithData("empty_slice", []any{}).
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", drain.Len())
	}
	e := drain.Events()[0]

	// Verify edge values don't cause panics and safe ones are preserved.
	if e.Data["nil_val"] != nil {
		t.Errorf("nil_val was modified: %v", e.Data["nil_val"])
	}
	if e.Data["int_val"] != 42 {
		t.Errorf("int_val was modified: %v", e.Data["int_val"])
	}
	if e.Data["bool_val"] != false {
		t.Errorf("bool_val was modified: %v", e.Data["bool_val"])
	}
	if e.Data["empty_string"] != "" {
		t.Errorf("empty_string was modified: %v", e.Data["empty_string"])
	}
	emptyMap, ok := e.Data["empty_map"].(map[string]any)
	if !ok || len(emptyMap) != 0 {
		t.Errorf("empty_map was modified: %v", e.Data["empty_map"])
	}
	emptySlice, ok := e.Data["empty_slice"].([]any)
	if !ok || len(emptySlice) != 0 {
		t.Errorf("empty_slice was modified: %v", e.Data["empty_slice"])
	}
	// Chan is an unsupported type; it should pass through without panic.
	if e.Data["chan_val"] == nil {
		t.Error("chan_val should not be nil (unsupported types pass through)")
	}
}

// VAL-CORE-034: Serialized drain input contains no raw secrets
func TestSerializedDrainInputContainsNoRawSecrets(t *testing.T) {
	client, drain := newRedactionClient()

	_ = client.Emit("test.op").
		WithData("password", "hunter2").
		WithData("token", "bearer sk-abc123").
		WithData("email", "admin@company.com").
		WithData("safe", "public data").
		Success(context.Background(), time.Millisecond)

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", drain.Len())
	}
	e := drain.Events()[0]

	// Serialize to JSON and check that raw secrets are absent.
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	jsonStr := string(data)

	rawSecrets := []string{
		"hunter2",
		"sk-abc123",
		"admin@company.com",
	}
	for _, secret := range rawSecrets {
		if strings.Contains(jsonStr, secret) {
			t.Errorf("raw secret %q found in serialized JSON: %s", secret, jsonStr)
		}
	}

	// Safe data should be present.
	if !strings.Contains(jsonStr, "public data") {
		t.Errorf("safe data missing from serialized JSON: %s", jsonStr)
	}

	// [REDACTED] should be present for masked values.
	if strings.Count(jsonStr, "[REDACTED]") < 2 {
		t.Errorf("expected at least 2 [REDACTED] markers in JSON: %s", jsonStr)
	}
}
