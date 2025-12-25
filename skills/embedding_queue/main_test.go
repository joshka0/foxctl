package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func TestEnqueue(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTCTL_HOME", root)

	input := Input{
		Operation:   "enqueue",
		WorkspaceID: "test-ws",
		Symbols: []SymbolInput{
			{
				SymbolID:   "main.go:Handler",
				FilePath:   "main.go",
				SymbolName: "Handler",
				Content:    "func Handler() {}",
			},
		},
		Deduplicate: true,
	}

	inputBytes, _ := json.Marshal(input)
	var output bytes.Buffer

	err := run(context.Background(), bytes.NewReader(inputBytes), &output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(output.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}

	data := env.Data.(map[string]any)
	if data["queued"].(float64) != 1 {
		t.Errorf("expected 1 queued, got %v", data["queued"])
	}
}

func TestStats(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTCTL_HOME", root)

	input := Input{
		Operation: "stats",
	}

	inputBytes, _ := json.Marshal(input)
	var output bytes.Buffer

	err := run(context.Background(), bytes.NewReader(inputBytes), &output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(output.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}

	data := env.Data.(map[string]any)
	if data["stats"] == nil {
		t.Error("expected stats in output")
	}
}

func TestGetNotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTCTL_HOME", root)

	input := Input{
		Operation:   "get",
		WorkspaceID: "test-ws",
		SymbolID:    "nonexistent.go:Foo",
	}

	inputBytes, _ := json.Marshal(input)
	var output bytes.Buffer

	err := run(context.Background(), bytes.NewReader(inputBytes), &output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(output.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}

	data := env.Data.(map[string]any)
	if data["message"] != "Embedding not found" {
		t.Errorf("expected 'Embedding not found' message, got %v", data["message"])
	}
}

func TestCleanup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTCTL_HOME", root)

	input := Input{
		Operation:      "cleanup",
		OlderThanHours: 0, // Clean all
	}

	inputBytes, _ := json.Marshal(input)
	var output bytes.Buffer

	err := run(context.Background(), bytes.NewReader(inputBytes), &output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(output.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal output: %v", err)
	}

	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}
}

func TestUnknownOperation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTCTL_HOME", root)

	input := Input{
		Operation: "unknown",
	}

	inputBytes, _ := json.Marshal(input)
	var output bytes.Buffer

	err := run(context.Background(), bytes.NewReader(inputBytes), &output)
	if err == nil {
		t.Error("expected error for unknown operation")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
