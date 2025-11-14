package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/config"
	runner "github.com/jkatigb/agentctl/internal/skillslib/runner"
)

func TestPlanGeneration(t *testing.T) {
	temp := t.TempDir()
	cfg := config.Config{
		InlineOutputKB: 32,
		Paths:          config.Paths{CAS: filepath.Join(temp, "cas")},
	}
	buf := &bytes.Buffer{}
	rc, err := runner.NewRunnerContext(cfg, buf)
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{BaseURL: "https://api.example.com", Path: "/users", Method: "get", Query: map[string]string{"page": "1"}}
	if err := run(rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data := env["data"].(map[string]any)
	plan := data["request_plan"].(map[string]any)
	if plan["method"].(string) != "GET" {
		t.Fatalf("expected GET")
	}
}
