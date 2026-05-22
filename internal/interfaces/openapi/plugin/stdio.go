package plugin

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// WriteHandshake writes plugin capability metadata for the --handshake path.
func WriteHandshake(w io.Writer, hs Handshake) error {
	return json.NewEncoder(w).Encode(hs)
}

// ReadRequest decodes one plugin request envelope and its typed data payload.
func ReadRequest(r io.Reader, command string, payload any) error {
	var env envelope.Envelope
	if err := json.NewDecoder(r).Decode(&env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if env.Command != command {
		return fmt.Errorf("unexpected command %s", env.Command)
	}
	if err := DecodeData(env.Data, payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	return nil
}

// DecodeData converts an envelope data field into a typed plugin payload.
func DecodeData(data any, payload any) error {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, payload)
}

// WriteOK writes a successful plugin response envelope.
func WriteOK(w io.Writer, command string, data any) error {
	return envelope.Write(w, envelope.OK(command, data))
}

// WriteError writes a plugin error response envelope.
func WriteError(w io.Writer, command, code, message string, data any) error {
	return envelope.Write(w, envelope.Error(command, code, message, data))
}
