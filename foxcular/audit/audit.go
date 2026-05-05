// Package audit provides tamper-evident audit integrity for event sequences.
//
// Audit uses HMAC-based hash chaining to detect modified, removed, or
// reordered events. It is optional and isolated from the core foxcular package.
package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/joshka0/foxcular"
)

const (
	// auditHashKey is the data key for the current event's audit hash.
	auditHashKey = "_audit_hash"
	// auditPrevHashKey is the data key for the previous event's hash.
	auditPrevHashKey = "_audit_prev_hash"
	// auditSeqKey is the data key for the sequence number.
	auditSeqKey = "_audit_seq"
)

// AuditDrain wraps a Drain and computes an HMAC hash chain over accepted
// events before forwarding them to the underlying drain. Each event receives
// a hash that covers the event content and the previous event's hash.
type AuditDrain struct {
	mu       sync.Mutex
	key      []byte
	drain    foxcular.Drain
	prevHash string
	seq      uint64
}

// NewAuditDrain creates an AuditDrain that signs events with the given HMAC
// key before forwarding to the underlying drain.
func NewAuditDrain(key []byte, drain foxcular.Drain) *AuditDrain {
	return &AuditDrain{
		key:   key,
		drain: drain,
	}
}

// Send computes an audit hash for the event, attaches it, and forwards to
// the underlying drain.
func (a *AuditDrain) Send(ctx context.Context, event *foxcular.Event) error {
	if event == nil {
		return nil
	}
	a.mu.Lock()
	prevHash := a.prevHash
	hash, seq := a.computeHash(event, prevHash)
	a.seq = seq
	a.prevHash = hash
	a.mu.Unlock()

	// Attach audit fields to event data.
	if event.Data == nil {
		event.Data = make(map[string]any)
	}
	event.Data[auditHashKey] = hash
	event.Data[auditPrevHashKey] = prevHash
	event.Data[auditSeqKey] = seq

	return a.drain.Send(ctx, event)
}

// Flush flushes the underlying drain.
func (a *AuditDrain) Flush(ctx context.Context) error {
	return a.drain.Flush(ctx)
}

// Close closes the underlying drain.
func (a *AuditDrain) Close() error {
	return a.drain.Close()
}

// computeHash computes the HMAC hash for the event content chained with
// the previous hash. Must be called with a.mu held.
func (a *AuditDrain) computeHash(event *foxcular.Event, prevHash string) (hash string, seq uint64) {
	seq = a.seq + 1
	payload := auditPayload{
		Timestamp:  event.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		TraceID:    event.TraceID,
		SpanID:     event.SpanID,
		ParentID:   event.ParentID,
		Operation:  event.Operation,
		Name:       event.Name,
		Status:     string(event.Status),
		DurationMS: event.Duration.Milliseconds(),
		Message:    event.Message,
		ErrorType:  event.ErrorType,
		ErrorCode:  event.ErrorCode,
		Data:       event.Data,
		PrevHash:   prevHash,
		Seq:        seq,
	}

	data, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, a.key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), seq
}

// auditPayload is the canonical form used for hash computation.
type auditPayload struct {
	Timestamp  string         `json:"ts"`
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Operation  string         `json:"operation"`
	Name       string         `json:"name,omitempty"`
	Status     string         `json:"status"`
	DurationMS int64          `json:"duration_ms"`
	Message    string         `json:"message,omitempty"`
	ErrorType  string         `json:"error_type,omitempty"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	PrevHash   string         `json:"prev_hash"`
	Seq        uint64         `json:"seq"`
}

// Verify verifies the integrity of an event sequence. It checks that each
// event's hash matches the recomputed hash using the given key and that the
// chain is intact (no modifications, removals, or reordering).
func Verify(key []byte, events []*foxcular.Event) error {
	if len(events) == 0 {
		return nil
	}

	var prevHash string
	for i, event := range events {
		storedHash, _ := event.Data[auditHashKey].(string)
		storedPrevHash, _ := event.Data[auditPrevHashKey].(string)
		storedSeq, _ := event.Data[auditSeqKey].(uint64)

		if storedHash == "" {
			return fmt.Errorf("audit: event %d missing audit hash", i)
		}

		// Verify chain linkage.
		if storedPrevHash != prevHash {
			return fmt.Errorf("audit: event %d prev_hash mismatch: got %q, want %q (removed or reordered event)", i, storedPrevHash, prevHash)
		}

		// Recompute expected hash.
		expectedHash := recomputeHash(key, event, prevHash, uint64(i)+1)
		if !hmac.Equal([]byte(storedHash), []byte(expectedHash)) {
			return fmt.Errorf("audit: event %d hash mismatch: content was modified", i)
		}

		// Verify sequence number.
		expectedSeq := uint64(i) + 1
		if storedSeq != expectedSeq {
			return fmt.Errorf("audit: event %d seq mismatch: got %d, want %d (reordered event)", i, storedSeq, expectedSeq)
		}

		prevHash = storedHash
	}
	return nil
}

// VerifyWithKey verifies the integrity using a specific key and fails if
// a different key was used.
func VerifyWithKey(key []byte, events []*foxcular.Event) error {
	return Verify(key, events)
}

// recomputeHash recomputes the expected hash for an event given the previous
// hash and sequence number.
func recomputeHash(key []byte, event *foxcular.Event, prevHash string, seq uint64) string {
	// Build a clean payload excluding audit fields from data.
	cleanData := make(map[string]any)
	for k, v := range event.Data {
		if k == auditHashKey || k == auditPrevHashKey || k == auditSeqKey {
			continue
		}
		cleanData[k] = v
	}

	payload := auditPayload{
		Timestamp:  event.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		TraceID:    event.TraceID,
		SpanID:     event.SpanID,
		ParentID:   event.ParentID,
		Operation:  event.Operation,
		Name:       event.Name,
		Status:     string(event.Status),
		DurationMS: event.Duration.Milliseconds(),
		Message:    event.Message,
		ErrorType:  event.ErrorType,
		ErrorCode:  event.ErrorCode,
		Data:       cleanData,
		PrevHash:   prevHash,
		Seq:        seq,
	}

	data, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
