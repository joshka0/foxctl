package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/domain/envelope"
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
			env.rc.SessionID = "test-session"
			if tt.setup != nil {
				tt.setup(env)
			}
			tt.input.WorkspaceID = env.workspaceID

			buf := &bytes.Buffer{}
			env.rc.Stdout = buf
			err := run(env.ctx, env.rc, tt.input)

			if err != nil {
				if !tt.isError {
					t.Fatalf("unexpected error: %v", err)
				}
				// Emulate skillmain error handling.
				writeErrEnvelope(t, buf, err)
			} else if tt.isError {
				t.Fatalf("expected error but got none")
			}

			got := scrub(t, buf.Bytes())

			goldenFile := filepath.Join("testdata", tt.name+".json")
			if _, err := os.Stat(goldenFile); os.IsNotExist(err) {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenFile, got, 0o644); err != nil {
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

func writeErrEnvelope(t *testing.T, buf *bytes.Buffer, err error) {
	t.Helper()

	var skillErr *skillerr.Error
	if errors.As(err, &skillErr) {
		appendUsageHint("todo/manage", skillErr)
		env := envelope.Error("todo/manage", skillErr.Code, skillErr.Message, skillErr.ToEnvelopeData())
		if err := envelope.Write(buf, env); err != nil {
			t.Fatal(err)
		}
		return
	}

	wrapped := skillerr.WrapRuntime("execute", err)
	appendUsageHint("todo/manage", wrapped)
	env := envelope.Error("todo/manage", wrapped.Code, wrapped.Message, wrapped.ToEnvelopeData())
	if err := envelope.Write(buf, env); err != nil {
		t.Fatal(err)
	}
}

func appendUsageHint(command string, err *skillerr.Error) {
	if err == nil {
		return
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	usage := "For examples, run: foxctl run " + command + " --examples"
	if err.Hint == "" {
		err.Hint = usage
		return
	}
	if !strings.Contains(err.Hint, "--examples") {
		err.Hint = strings.TrimSpace(err.Hint + " " + usage)
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
			if _, ok := task["session_id"]; ok {
				task["session_id"] = "test-session"
			}
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
