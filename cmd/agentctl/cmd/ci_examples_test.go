package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestCICommandExamples(t *testing.T) {
	cfg := newTestConfig(t)
	cmd := newCICommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--examples"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("ci examples: %v (stderr=%s)", err, stderr.String())
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode ci examples envelope: %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("expected ok status, got %s", env.Status)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map response, got %T", env.Data)
	}
	examples, ok := data["examples"].([]any)
	if !ok || len(examples) == 0 {
		t.Fatalf("expected examples list, got %T", data["examples"])
	}
}
