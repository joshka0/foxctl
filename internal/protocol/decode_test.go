package protocol

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

type decodeSample struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

func TestDecodeEnvelopeIntoOK(t *testing.T) {
	env := envelope.OK("test/skill", map[string]any{
		"message": "hi",
		"count":   2,
	})
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var got decodeSample
	if err := DecodeEnvelopeInto(raw, &got); err != nil {
		t.Fatalf("DecodeEnvelopeInto failed: %v", err)
	}
	if got.Message != "hi" || got.Count != 2 {
		t.Fatalf("unexpected decode: %+v", got)
	}
}

func TestDecodeEnvelopeIntoStatusError(t *testing.T) {
	env := envelope.Error("test/skill", "EARG", "bad input", map[string]any{
		"hint": "provide a path",
	})
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var got decodeSample
	err = DecodeEnvelopeInto(raw, &got)
	if err == nil {
		t.Fatal("expected error")
	}

	var statusErr EnvelopeStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected EnvelopeStatusError, got %T", err)
	}
	if statusErr.Code != "EARG" || statusErr.Message != "bad input" || statusErr.Hint != "provide a path" {
		t.Fatalf("unexpected status error: %+v", statusErr)
	}
}
