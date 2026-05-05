package audit_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
	"github.com/joshka0/foxcular/audit"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type captureDrain struct {
	events []*foxcular.Event
}

func (d *captureDrain) Send(_ context.Context, e *foxcular.Event) error {
	d.events = append(d.events, e.Clone())
	return nil
}
func (d *captureDrain) Flush(_ context.Context) error { return nil }
func (d *captureDrain) Close() error                  { return nil }

func makeTestEvent(op string) *foxcular.Event {
	return &foxcular.Event{
		Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		TraceID:   "trace-001",
		SpanID:    "span-001",
		Operation: op,
		Status:    foxcular.StatusOK,
		Duration:  100 * time.Millisecond,
		Data:      map[string]any{"key": "value"},
	}
}

// ---------------------------------------------------------------------------
// VAL-AUDIT-001: Untampered audit chain verifies
// ---------------------------------------------------------------------------

func TestUntamperedAuditChainVerifies(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("test-secret-key")
	ad := audit.NewAuditDrain(key, inner)

	events := []*foxcular.Event{
		makeTestEvent("op.a"),
		makeTestEvent("op.b"),
		makeTestEvent("op.c"),
	}

	for _, e := range events {
		if err := ad.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	// All events should have audit hashes.
	if len(inner.events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(inner.events))
	}

	for i, e := range inner.events {
		hash, _ := e.Data["_audit_hash"].(string)
		if hash == "" {
			t.Errorf("event %d missing audit hash", i)
		}
		seq, _ := e.Data["_audit_seq"].(uint64)
		if seq != uint64(i+1) {
			t.Errorf("event %d seq = %d, want %d", i, seq, i+1)
		}
	}

	// Verify should succeed.
	if err := audit.Verify(key, inner.events); err != nil {
		t.Fatalf("verify should succeed for untampered chain: %v", err)
	}
}

// ---------------------------------------------------------------------------
// VAL-AUDIT-002: Modified event tampering is detected
// ---------------------------------------------------------------------------

func TestModifiedEventTamperingIsDetected(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("test-secret-key")
	ad := audit.NewAuditDrain(key, inner)

	for _, e := range []*foxcular.Event{
		makeTestEvent("op.a"),
		makeTestEvent("op.b"),
		makeTestEvent("op.c"),
	} {
		if err := ad.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	// Modify the second event's operation.
	inner.events[1].Operation = "TAMPERED"

	if err := audit.Verify(key, inner.events); err == nil {
		t.Error("verify should fail for modified event")
	} else {
		if !strings.Contains(err.Error(), "hash mismatch") {
			t.Errorf("expected hash mismatch error, got: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// VAL-AUDIT-003: Removed/reordered events are detected
// ---------------------------------------------------------------------------

func TestRemovedEventsAreDetected(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("test-secret-key")
	ad := audit.NewAuditDrain(key, inner)

	for _, e := range []*foxcular.Event{
		makeTestEvent("op.a"),
		makeTestEvent("op.b"),
		makeTestEvent("op.c"),
		makeTestEvent("op.d"),
	} {
		if err := ad.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	// Remove the second event.
	removed := append(inner.events[:1], inner.events[2:]...)

	if err := audit.Verify(key, removed); err == nil {
		t.Error("verify should fail when event is removed")
	} else {
		if !strings.Contains(err.Error(), "prev_hash mismatch") {
			t.Errorf("expected prev_hash mismatch error, got: %v", err)
		}
	}
}

func TestReorderedEventsAreDetected(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("test-secret-key")
	ad := audit.NewAuditDrain(key, inner)

	for _, e := range []*foxcular.Event{
		makeTestEvent("op.a"),
		makeTestEvent("op.b"),
		makeTestEvent("op.c"),
	} {
		if err := ad.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	// Swap first and second events.
	reordered := []*foxcular.Event{inner.events[1], inner.events[0], inner.events[2]}

	if err := audit.Verify(key, reordered); err == nil {
		t.Error("verify should fail when events are reordered")
	}
}

// ---------------------------------------------------------------------------
// VAL-AUDIT-004: HMAC verification requires correct key
// ---------------------------------------------------------------------------

func TestHMACVerificationRequiresCorrectKey(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("correct-secret-key")
	ad := audit.NewAuditDrain(key, inner)

	for _, e := range []*foxcular.Event{
		makeTestEvent("op.a"),
		makeTestEvent("op.b"),
	} {
		if err := ad.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	// Verify with correct key should succeed.
	if err := audit.VerifyWithKey(key, inner.events); err != nil {
		t.Fatalf("verify with correct key should succeed: %v", err)
	}

	// Verify with wrong key should fail.
	wrongKey := []byte("wrong-secret-key")
	if err := audit.VerifyWithKey(wrongKey, inner.events); err == nil {
		t.Error("verify with wrong key should fail")
	} else {
		if !strings.Contains(err.Error(), "hash mismatch") {
			t.Errorf("expected hash mismatch error, got: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestAuditEmptySequence(t *testing.T) {
	key := []byte("key")
	if err := audit.Verify(key, nil); err != nil {
		t.Errorf("verify empty should succeed: %v", err)
	}
	if err := audit.Verify(key, []*foxcular.Event{}); err != nil {
		t.Errorf("verify empty slice should succeed: %v", err)
	}
}

func TestAuditSingleEvent(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("key")
	ad := audit.NewAuditDrain(key, inner)

	if err := ad.Send(context.Background(), makeTestEvent("solo")); err != nil {
		t.Fatalf("send: %v", err)
	}

	if err := audit.Verify(key, inner.events); err != nil {
		t.Fatalf("verify single event: %v", err)
	}
}

func TestAuditFlushAndClose(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("key")
	ad := audit.NewAuditDrain(key, inner)

	if err := ad.Flush(context.Background()); err != nil {
		t.Errorf("flush: %v", err)
	}
	if err := ad.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestAuditNilEvent(t *testing.T) {
	inner := &captureDrain{}
	key := []byte("key")
	ad := audit.NewAuditDrain(key, inner)

	if err := ad.Send(context.Background(), nil); err != nil {
		t.Errorf("nil event should not error: %v", err)
	}
	if len(inner.events) != 0 {
		t.Errorf("nil event should not be forwarded, got %d events", len(inner.events))
	}
}

func TestAuditMissingHash(t *testing.T) {
	key := []byte("key")
	events := []*foxcular.Event{
		{Operation: "no-hash", Data: map[string]any{}},
	}
	if err := audit.Verify(key, events); err == nil {
		t.Error("verify should fail for event without audit hash")
	}
}
