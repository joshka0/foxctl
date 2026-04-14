package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
)

func TestVersionCommand(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := newVersionCommand()
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if env.Command != "foxctl.version" {
		t.Errorf("expected command foxctl.version, got %s", env.Command)
	}
	if env.Status != "ok" {
		t.Errorf("expected status ok, got %s", env.Status)
	}
	if env.Data == nil {
		t.Errorf("expected data to be present")
	}
}

func TestVersionCommandMetadata(t *testing.T) {
	buf := &bytes.Buffer{}
	cmd := newVersionCommand()
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map[string]any")
	}

	for _, field := range []string{"version", "go_version"} {
		if _, exists := data[field]; !exists {
			t.Errorf("expected field %s to exist in buildinfo data", field)
		}
	}

	if _, exists := data["date"]; exists {
		t.Errorf("unexpected legacy field 'date'; expected 'build_date'")
	}
	if buildinfo.Date != "" {
		if _, exists := data["build_date"]; !exists {
			t.Errorf("expected build_date field when buildinfo.Date is set")
		}
	}
	if buildinfo.Commit != "" {
		if _, exists := data["commit"]; !exists {
			t.Errorf("expected commit field when buildinfo.Commit is set")
		}
	}
}

func TestVersionCommandBuildDateField(t *testing.T) {
	prevDate := buildinfo.Date
	buildinfo.Date = "2025-01-01T00:00:00Z"
	defer func() {
		buildinfo.Date = prevDate
	}()

	buf := &bytes.Buffer{}
	cmd := newVersionCommand()
	cmd.SetOut(buf)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map[string]any")
	}

	if _, exists := data["build_date"]; !exists {
		t.Fatalf("expected build_date field when buildinfo.Date is set")
	}
	if _, exists := data["date"]; exists {
		t.Fatalf("unexpected legacy field 'date'")
	}
}
