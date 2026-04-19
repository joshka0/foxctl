package envelope

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

func newValidEnvelope(t *testing.T) Envelope {
	t.Helper()
	env, err := New("session.create", "session:", map[string]string{"cmd": "echo"})
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}
	return env
}

func TestNewAndValidate(t *testing.T) {
	env := newValidEnvelope(t)
	if err := env.Validate(); err != nil {
		t.Fatalf("newly constructed envelope failed Validate: %v", err)
	}
	if env.Version != Version {
		t.Errorf("Version = %q, want %q", env.Version, Version)
	}
	if _, err := ulid.Parse(env.ID); err != nil {
		t.Errorf("ID is not a valid ULID: %v", err)
	}
	if env.Timestamp.IsZero() {
		t.Error("Timestamp was not populated")
	}
}

func TestValidate_VersionMismatch(t *testing.T) {
	env := newValidEnvelope(t)
	env.Version = "atcp/0.0"
	if err := env.Validate(); !errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("want ErrVersionMismatch, got %v", err)
	}
}

func TestValidate_InvalidID(t *testing.T) {
	env := newValidEnvelope(t)
	env.ID = "not-a-ulid"
	if err := env.Validate(); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("want ErrInvalidID, got %v", err)
	}
}

func TestValidate_MissingKind(t *testing.T) {
	env := newValidEnvelope(t)
	env.Kind = "   "
	if err := env.Validate(); !errors.Is(err, ErrMissingKind) {
		t.Fatalf("want ErrMissingKind, got %v", err)
	}
}

func TestValidate_MissingTimestamp(t *testing.T) {
	env := newValidEnvelope(t)
	env.Timestamp = time.Time{}
	if err := env.Validate(); !errors.Is(err, ErrMissingTS) {
		t.Fatalf("want ErrMissingTS, got %v", err)
	}
}

func TestValidate_MissingBody(t *testing.T) {
	env := newValidEnvelope(t)
	env.Body = nil
	if err := env.Validate(); !errors.Is(err, ErrBodyRequired) {
		t.Fatalf("want ErrBodyRequired, got %v", err)
	}
}

func TestValidate_InvalidBodyJSON(t *testing.T) {
	env := newValidEnvelope(t)
	env.Body = json.RawMessage([]byte("{not json"))
	if err := env.Validate(); !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("want ErrInvalidBody, got %v", err)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	original := newValidEnvelope(t)
	original.Source = "agent:alice"
	original.Seq = 42
	original.CorrelationID = "corr-xyz"
	original.IdempotencyKey = "idem-1"

	raw, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	decoded, err := DecodeStrict(raw)
	if err != nil {
		t.Fatalf("DecodeStrict returned error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID round-trip mismatch: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Kind != original.Kind {
		t.Errorf("Kind round-trip mismatch: got %q, want %q", decoded.Kind, original.Kind)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source round-trip mismatch: got %q, want %q", decoded.Source, original.Source)
	}
	if decoded.Seq != original.Seq {
		t.Errorf("Seq round-trip mismatch: got %d, want %d", decoded.Seq, original.Seq)
	}
	if decoded.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID round-trip mismatch")
	}
	if decoded.IdempotencyKey != original.IdempotencyKey {
		t.Errorf("IdempotencyKey round-trip mismatch")
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp round-trip mismatch: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
}

func TestDecodeStrict_RejectsInvalidEnvelope(t *testing.T) {
	// Missing version.
	bad := []byte(`{"id":"01HX","kind":"session.create","ts":"2025-01-01T00:00:00Z","body":{}}`)
	if _, err := DecodeStrict(bad); err == nil {
		t.Fatal("expected DecodeStrict to reject envelope missing version")
	}
}

func TestDecode_MalformedJSON(t *testing.T) {
	if _, err := Decode([]byte("{not json")); err == nil {
		t.Fatal("expected Decode to fail on malformed JSON")
	} else if !strings.Contains(err.Error(), "atcp envelope: decode") {
		t.Errorf("error message should mention atcp envelope: %v", err)
	}
}

func TestDecodeBody(t *testing.T) {
	type payload struct {
		Cmd string `json:"cmd"`
	}
	env, err := New("session.create", "session:", payload{Cmd: "bash"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	var got payload
	if err := env.DecodeBody(&got); err != nil {
		t.Fatalf("DecodeBody error: %v", err)
	}
	if got.Cmd != "bash" {
		t.Errorf("got Cmd=%q, want %q", got.Cmd, "bash")
	}
}

func TestDecodeBody_EmptyBodyIsError(t *testing.T) {
	var e Envelope
	var sink map[string]any
	if err := e.DecodeBody(&sink); !errors.Is(err, ErrBodyRequired) {
		t.Fatalf("want ErrBodyRequired, got %v", err)
	}
}

func TestWithBody_InvalidValue(t *testing.T) {
	env := newValidEnvelope(t)
	// channels cannot be JSON-marshalled
	_, err := env.WithBody(make(chan int))
	if err == nil {
		t.Fatal("expected WithBody to reject unmarshallable value")
	}
}
