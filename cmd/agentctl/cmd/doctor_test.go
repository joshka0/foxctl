package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestDoctorOutputsEnvelope(t *testing.T) {
	cfg := config.Config{
		Home:           "/tmp/agentctl",
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
