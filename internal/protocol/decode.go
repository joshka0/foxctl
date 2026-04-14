package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/maputil"
)

// EnvelopeStatusError captures a skill envelope error status.
type EnvelopeStatusError struct {
	Code    string
	Message string
	Hint    string
}

func (e EnvelopeStatusError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		if e.Hint != "" {
			return fmt.Sprintf("skill error [%s]: %s (hint: %s)", e.Code, e.Message, e.Hint)
		}
		return fmt.Sprintf("skill error [%s]: %s", e.Code, e.Message)
	case e.Message != "":
		if e.Hint != "" {
			return fmt.Sprintf("skill error: %s (hint: %s)", e.Message, e.Hint)
		}
		return fmt.Sprintf("skill error: %s", e.Message)
	default:
		return "skill error"
	}
}

// DecodeEnvelope unmarshals JSON bytes into an envelope.
func DecodeEnvelope(data []byte) (envelope.Envelope, error) {
	var env envelope.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return env, fmt.Errorf("parse envelope: %w", err)
	}
	return env, nil
}

// DecodeEnvelopeData extracts the data map from an envelope, returning an error
// if the envelope reports a skill error.
func DecodeEnvelopeData(data []byte) (map[string]any, error) {
	env, err := DecodeEnvelope(data)
	if err != nil {
		return nil, err
	}
	if env.Status == envelope.StatusError {
		return nil, EnvelopeStatusErrorFromEnvelope(env)
	}
	return maputil.MapOrEmpty(env.Data), nil
}

// DecodeEnvelopeDataInto decodes the envelope data payload into dst.
func DecodeEnvelopeDataInto(env envelope.Envelope, dst any) error {
	if dst == nil {
		return fmt.Errorf("destination is required")
	}
	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		return fmt.Errorf("marshal envelope data: %w", err)
	}
	if err := json.Unmarshal(dataBytes, dst); err != nil {
		return fmt.Errorf("decode envelope data: %w", err)
	}
	return nil
}

// DecodeEnvelopeInto decodes envelope bytes and unmarshals the data payload into dst.
func DecodeEnvelopeInto(data []byte, dst any) error {
	env, err := DecodeEnvelope(data)
	if err != nil {
		return err
	}
	if env.Status == envelope.StatusError {
		return EnvelopeStatusErrorFromEnvelope(env)
	}
	return DecodeEnvelopeDataInto(env, dst)
}

// EnvelopeStatusErrorFromEnvelope builds a status error with any available hint.
func EnvelopeStatusErrorFromEnvelope(env envelope.Envelope) EnvelopeStatusError {
	return EnvelopeStatusError{
		Code:    env.Error.Code,
		Message: env.Error.Message,
		Hint:    extractHint(env.Data),
	}
}

func extractHint(data any) string {
	if payload, ok := maputil.AsStringMap(data); ok {
		if hint, ok := payload["hint"].(string); ok {
			return hint
		}
	}
	return ""
}
