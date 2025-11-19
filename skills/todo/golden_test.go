package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func TestGolden(t *testing.T) {
	tests := []struct {
		name    string
		input   input
		isError bool
		setup   func(*todoTestEnv)
	}{
		{
			name: "ok",
			input: input{
				Operation: "add",
				Add: &addRequest{
					Title:       "Buy milk",
					Description: "2% fat",
				},
			},
		},
		{
			name: "error",
			input: input{
				Operation: "add",
				Add: &addRequest{
					Title: "", // Invalid
				},
			},
			isError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTodoTestEnv(t)
			if tt.setup != nil {
				tt.setup(env)
			}
			tt.input.StorePath = env.storePath

			buf := &bytes.Buffer{}
			env.rc.Stdout = buf
			err := run(env.ctx, env.rc, env.rc.Config, tt.input)

			if err != nil {
				if !tt.isError {
					t.Fatalf("unexpected error: %v", err)
				}
				// Emulate main.go's fail handling
				e := envelope.Error("todo/manage", "ERUNTIME", err.Error(), nil)
				if err := envelope.Write(buf, e); err != nil {
					t.Fatal(err)
				}
			} else if tt.isError {
				t.Fatalf("expected error but got none")
			}

			got := scrub(t, buf.Bytes())

			goldenFile := filepath.Join("testdata", tt.name+".json")
			if _, err := os.Stat(goldenFile); os.IsNotExist(err) {
				if err := os.MkdirAll("testdata", 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenFile, got, 0644); err != nil {
					t.Fatal(err)
				}
				t.Logf("Created golden file: %s", goldenFile)
			}

			want, err := os.ReadFile(goldenFile)
			if err != nil {
				t.Fatal(err)
			}

			if string(got) != string(want) {
				// If mismatch, we can update the golden file if we are confident.
				// For now, just error.
				t.Errorf("Golden file mismatch %s:\nGot:\n%s\nWant:\n%s", goldenFile, got, want)
			}
		})
	}
}

func scrub(t *testing.T, data []byte) []byte {
	var env envelope.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v\nData: %s", err, data)
	}

	// Scrub meta
	env.Meta.TS = "2025-01-01T12:00:00Z"
	env.Meta.DurationMS = 123
	env.Meta.Source = "run"
	env.Meta.Runner = "exec"

	// Scrub data
	if m, ok := env.Data.(map[string]any); ok {
		if task, ok := m["task"].(map[string]any); ok {
			task["id"] = "00000000-0000-0000-0000-000000000000"
			task["created_at"] = "2025-01-01T12:00:00Z"
		}
		if summary, ok := m["summary"].(string); ok {
			if strings.HasPrefix(summary, "added task ") {
				m["summary"] = "added task 00000000-0000-0000-0000-000000000000"
			}
		}
	}

	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	// Append newline to match editor behavior
	return append(out, '\n')
}
