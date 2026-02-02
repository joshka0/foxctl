package skilltest

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

// DecodeEnvelope parses JSON bytes into an Envelope.
// Returns the envelope and any parsing error.
func DecodeEnvelope(data []byte) (envelope.Envelope, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return env, err
	}
	return env, nil
}

// DecodeEnvelopeData parses an envelope and extracts the typed data payload.
// The data field is re-marshaled and unmarshaled into the target type.
func DecodeEnvelopeData[T any](data []byte) (envelope.Envelope, T, error) {
	var result T
	env, err := DecodeEnvelope(data)
	if err != nil {
		return env, result, err
	}
	if env.Data == nil {
		return env, result, nil
	}
	// Re-marshal and unmarshal to get typed data
	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		return env, result, err
	}
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return env, result, err
	}
	return env, result, nil
}

// AssertEnvelopeOK asserts the envelope has status "ok" and expected command.
// Returns the envelope for further inspection.
func AssertEnvelopeOK(t *testing.T, data []byte, expectedCmd string) envelope.Envelope {
	t.Helper()
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Errorf("expected status %q, got %q (error: %s: %s)",
			envelope.StatusOK, env.Status, env.Error.Code, env.Error.Message)
	}
	if env.Command != expectedCmd {
		t.Errorf("expected command %q, got %q", expectedCmd, env.Command)
	}
	return env
}

// AssertEnvelopeError asserts the envelope has status "error" and expected error code.
// Returns the envelope for further inspection.
func AssertEnvelopeError(t *testing.T, data []byte, expectedCmd, expectedCode string) envelope.Envelope {
	t.Helper()
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	if env.Status != envelope.StatusError {
		t.Errorf("expected status %q, got %q", envelope.StatusError, env.Status)
	}
	if env.Command != expectedCmd {
		t.Errorf("expected command %q, got %q", expectedCmd, env.Command)
	}
	if env.Error.Code != expectedCode {
		t.Errorf("expected error code %q, got %q", expectedCode, env.Error.Code)
	}
	return env
}

// AssertEnvelopeDataOK asserts the envelope is OK and extracts typed data.
// Fails the test if the envelope is not OK or data extraction fails.
func AssertEnvelopeDataOK[T any](t *testing.T, data []byte, expectedCmd string) (envelope.Envelope, T) {
	t.Helper()
	var result T
	env := AssertEnvelopeOK(t, data, expectedCmd)
	if env.Data == nil {
		return env, result
	}
	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("failed to marshal envelope data: %v", err)
	}
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal envelope data: %v", err)
	}
	return env, result
}

// CaptureOutput captures skill output written to a bytes.Buffer.
// Returns the buffer for use with envelope assertion helpers.
func CaptureOutput() *bytes.Buffer {
	return new(bytes.Buffer)
}

// EnvelopeValidator provides fluent envelope validation for tests.
type EnvelopeValidator struct {
	t   *testing.T
	env envelope.Envelope
}

// NewEnvelopeValidator creates a validator for the given envelope bytes.
func NewEnvelopeValidator(t *testing.T, data []byte) *EnvelopeValidator {
	t.Helper()
	env, err := DecodeEnvelope(data)
	if err != nil {
		t.Fatalf("failed to decode envelope: %v", err)
	}
	return &EnvelopeValidator{t: t, env: env}
}

// IsOK asserts the envelope has status "ok".
func (v *EnvelopeValidator) IsOK() *EnvelopeValidator {
	v.t.Helper()
	if v.env.Status != envelope.StatusOK {
		v.t.Errorf("expected status %q, got %q (error: %s: %s)",
			envelope.StatusOK, v.env.Status, v.env.Error.Code, v.env.Error.Message)
	}
	return v
}

// IsError asserts the envelope has status "error".
func (v *EnvelopeValidator) IsError() *EnvelopeValidator {
	v.t.Helper()
	if v.env.Status != envelope.StatusError {
		v.t.Errorf("expected status %q, got %q", envelope.StatusError, v.env.Status)
	}
	return v
}

// HasCommand asserts the envelope has the expected command.
func (v *EnvelopeValidator) HasCommand(cmd string) *EnvelopeValidator {
	v.t.Helper()
	if v.env.Command != cmd {
		v.t.Errorf("expected command %q, got %q", cmd, v.env.Command)
	}
	return v
}

// HasErrorCode asserts the envelope has the expected error code.
func (v *EnvelopeValidator) HasErrorCode(code string) *EnvelopeValidator {
	v.t.Helper()
	if v.env.Error.Code != code {
		v.t.Errorf("expected error code %q, got %q", code, v.env.Error.Code)
	}
	return v
}

// HasData asserts the envelope has non-nil data.
func (v *EnvelopeValidator) HasData() *EnvelopeValidator {
	v.t.Helper()
	if v.env.Data == nil {
		v.t.Error("expected envelope to have data, got nil")
	}
	return v
}

// Envelope returns the underlying envelope for further inspection.
func (v *EnvelopeValidator) Envelope() envelope.Envelope {
	return v.env
}

// ExtractData extracts and returns typed data from the envelope.
func ExtractData[T any](t *testing.T, env envelope.Envelope) T {
	t.Helper()
	var result T
	if env.Data == nil {
		return result
	}
	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("failed to marshal envelope data: %v", err)
	}
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		t.Fatalf("failed to unmarshal envelope data: %v", err)
	}
	return result
}
