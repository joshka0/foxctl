// Package envelope defines the ATCP v0.1 wire envelope used by every
// coordination event flowing through the broker.
//
// See docs/atcp/ATCP-v0.1.md §7 for the normative format. This envelope is
// deliberately distinct from internal/domain/envelope.Envelope, which models
// the foxctl command/response skill contract. ATCP envelopes form an
// event stream; skill envelopes form request/response pairs.
package envelope

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

// Version is the only ATCP envelope version v0.1 accepts on the wire.
const Version = "atcp/0.1"

// Envelope is the ATCP v0.1 wire envelope.
//
// Field names and JSON tags mirror docs/atcp/types.go so producers and
// consumers stay in lockstep with the spec.
type Envelope struct {
	Version        string          `json:"v"`
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Timestamp      time.Time       `json:"ts"`
	Source         string          `json:"source,omitempty"`
	Target         string          `json:"target,omitempty"`
	Seq            uint64          `json:"seq,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Body           json.RawMessage `json:"body"`
}

// Validation errors. All validation failures funnel through these sentinels so
// callers (broker HTTP handlers, CLI, test fixtures) can branch deterministically.
var (
	ErrVersionMismatch = errors.New("atcp envelope: version must be " + Version)
	ErrInvalidID       = errors.New("atcp envelope: id must be a ULID")
	ErrMissingKind     = errors.New("atcp envelope: kind is required")
	ErrMissingTS       = errors.New("atcp envelope: ts is required")
	ErrNonUTCTS        = errors.New("atcp envelope: ts must be UTC (offset 0)")
	ErrBodyRequired    = errors.New("atcp envelope: body is required")
	ErrInvalidBody     = errors.New("atcp envelope: body is not valid JSON")
)

// Validate enforces the broker-side invariants listed in the plan (§5.1):
// v == atcp/0.1, id is a ULID, kind is non-empty, ts is set, body is valid JSON.
//
// Registered-kind validation is intentionally delegated to the kinds package
// so envelope has no dependency on the kind registry.
func (e Envelope) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("%w: got %q", ErrVersionMismatch, e.Version)
	}
	if _, err := ulid.Parse(e.ID); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidID, err)
	}
	if strings.TrimSpace(e.Kind) == "" {
		return ErrMissingKind
	}
	if e.Timestamp.IsZero() {
		return ErrMissingTS
	}
	// ATCP canonical ts is UTC. RFC3339 "Z" and "+00:00" both parse into a
	// zero-offset time.Time, so offset-based validation accepts either.
	if _, offset := e.Timestamp.Zone(); offset != 0 {
		return ErrNonUTCTS
	}
	if len(e.Body) == 0 {
		return ErrBodyRequired
	}
	if !json.Valid(e.Body) {
		return ErrInvalidBody
	}
	return nil
}

// Decode unmarshals a JSON byte slice into an Envelope without validating it.
// Callers that require validation should invoke Validate afterwards.
func Decode(data []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("atcp envelope: decode: %w", err)
	}
	return env, nil
}

// DecodeStrict decodes and validates in one call. Use this on the ingress path.
func DecodeStrict(data []byte) (Envelope, error) {
	env, err := Decode(data)
	if err != nil {
		return Envelope{}, err
	}
	if err := env.Validate(); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// Encode marshals an envelope to canonical JSON bytes. Timestamps are emitted
// as RFC3339Nano in UTC via time.Time's default JSON marshaller.
func Encode(e Envelope) ([]byte, error) {
	return json.Marshal(e)
}

// DecodeBody unmarshals the envelope body into the supplied destination.
func (e Envelope) DecodeBody(dst any) error {
	if len(e.Body) == 0 {
		return ErrBodyRequired
	}
	if err := json.Unmarshal(e.Body, dst); err != nil {
		return fmt.Errorf("atcp envelope: body decode: %w", err)
	}
	return nil
}

// WithBody returns a copy of the envelope with body replaced by the JSON-marshalled
// form of the supplied value. Useful for constructing envelopes in test fixtures
// and when producers want to hide json.RawMessage from their call sites.
func (e Envelope) WithBody(body any) (Envelope, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Envelope{}, fmt.Errorf("atcp envelope: body encode: %w", err)
	}
	e.Body = raw
	return e, nil
}

// New constructs a baseline v0.1 envelope with a freshly generated ULID id and
// the provided kind, target, and body. Timestamp defaults to time.Now().UTC().
// Callers may override any field after construction.
func New(kind, target string, body any) (Envelope, error) {
	e := Envelope{
		Version:   Version,
		ID:        ulid.Make().String(),
		Kind:      kind,
		Timestamp: time.Now().UTC(),
		Target:    target,
	}
	return e.WithBody(body)
}
