package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestDoctorOutputsEnvelope(t *testing.T) {
	cfg := config.Config{
		Home:           "/tmp/foxctl",
		InlineOutputKB: 256,
		MaxCaptureKB:   1024,
	}

	command := newDoctorCommand()
	buf := &bytes.Buffer{}
	command.SetOut(buf)
	command.SetErr(&bytes.Buffer{})
	command.SetContext(config.WithContext(context.Background(), cfg))

	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("doctor run: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if payload["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", payload["status"])
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", payload["data"])
	}
	if data["config"] == nil {
		t.Fatalf("expected config field in data")
	}
}

func TestDoctorRedactsConfigSecrets(t *testing.T) {
	cfg := config.Config{
		Home:           "/tmp/foxctl",
		InlineOutputKB: 256,
		MaxCaptureKB:   1024,
	}
	cfg.Embedding.APIKey = "embedding-private-value-1234567890"
	cfg.Embedding.VoyageAPIKey = "voyage-private-value-1234567890"
	cfg.Search.ExaAPIKey = "exa-private-value-1234567890"
	cfg.LLM.OpenRouterAPIKey = "openrouter-private-value-1234567890"
	cfg.LLM.OpenAIAPIKey = "openai-private-value-1234567890"

	command := newDoctorCommand()
	buf := &bytes.Buffer{}
	command.SetOut(buf)
	command.SetErr(&bytes.Buffer{})
	command.SetContext(config.WithContext(context.Background(), cfg))

	if err := command.RunE(command, nil); err != nil {
		t.Fatalf("doctor run: %v", err)
	}

	out := buf.String()
	for _, leaked := range []string{
		cfg.Embedding.APIKey,
		cfg.Embedding.VoyageAPIKey,
		cfg.Search.ExaAPIKey,
		cfg.LLM.OpenRouterAPIKey,
		cfg.LLM.OpenAIAPIKey,
	} {
		if strings.Contains(out, leaked) {
			t.Fatalf("doctor output leaked secret value %q", leaked)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("doctor output did not contain redaction marker: %s", out)
	}
}
