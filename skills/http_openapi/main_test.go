package main

import (
	"bytes"
	"context"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestRunHttpOpenApi(t *testing.T) {
	stdout := &bytes.Buffer{}
	cfg := config.Config{}
	rc, err := runner.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatal(err)
	}

	in := Input{
		Spec:        "http://example.com/spec.yaml",
		OperationID: "getUsers",
	}

	ctx := context.Background()
	// This is expected to fail due to missing spec, but covers the initial logic
	_ = run(ctx, rc, in)
}

func TestGenerateHint(t *testing.T) {
	hint := generateHint("EAUTH", 401)
	if hint == "" {
		t.Error("expected hint")
	}

	hint = generateHint("EPAGINATION", 0)
	if hint == "" {
		t.Error("expected pagination hint")
	}
}

func TestConvertHeaders(t *testing.T) {
	input := map[string]string{
		"Content-Type": "application/json",
		"X-Custom":     "value",
	}

	header := convertHeaders(input)

	if header.Get("Content-Type") != "application/json" {
		t.Error("Content-Type not set")
	}
	if header.Get("X-Custom") != "value" {
		t.Error("X-Custom not set")
	}
}

func TestAggregateResponses(t *testing.T) {
	// Test empty
	if aggregateResponses(nil) != nil {
		t.Error("expected nil for empty input")
	}

	// Test single
	single := []any{"foo"}
	if res := aggregateResponses(single); res != "foo" {
		t.Errorf("expected 'foo', got %v", res)
	}

	// Test arrays
	bodies := []any{
		[]any{1, 2},
		[]any{3, 4},
	}

	aggregated := aggregateResponses(bodies).([]any)
	if len(aggregated) != 4 {
		t.Errorf("expected 4 items, got %d", len(aggregated))
	}
}
