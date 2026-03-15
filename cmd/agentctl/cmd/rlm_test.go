package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
)

func TestRLMRunCommandBootstrapsEnvironment(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	var cfg config.Config
	cfg.Storage.Root = t.TempDir()

	cmd := newRLMRunCommand()
	cmd.SetArgs([]string{
		"--prompt", "inspect auth flow",
		"--workspace", workspace,
	})
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, stderr.String())
	}
	env, err := protocol.DecodeEnvelope(stdout.Bytes())
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "ok" {
		t.Fatalf("status=%q", env.Status)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}
	var payload struct {
		Mode   string `json:"mode"`
		Result struct {
			Answer string `json:"answer"`
		} `json:"result"`
		Environment struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"environment"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Mode != "inspect" {
		t.Fatalf("mode=%q", payload.Mode)
	}
	if payload.Result.Answer == "" {
		t.Fatalf("expected non-empty answer")
	}
	if payload.Result.Answer == "bootstrap complete; recursive execution backend not implemented yet" {
		t.Fatalf("still returning placeholder answer")
	}
	if len(payload.Environment.Tools) == 0 {
		t.Fatalf("expected non-empty tool surface")
	}
}
