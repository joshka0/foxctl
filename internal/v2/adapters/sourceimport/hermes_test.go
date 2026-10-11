package sourceimport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseHermesFile_ToCanonicalTurns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session_20260513_230931_12cc11.json")
	body := `{
  "session_id": "20260513_230931_12cc11",
  "model": "glm-4.5",
  "base_url": "http://127.0.0.1:4000/v4",
  "platform": "telegram",
  "session_start": "2026-05-13T23:09:31.343426",
  "last_updated": "2026-05-13T23:09:37.343426",
  "messages": [
    {"role": "user", "content": "Summarize the local memory plan"},
    {"role": "assistant", "content": "I will inspect the current settings.", "tool_calls": [
      {"call_id": "response_call_abc", "id": "call_abc", "type": "function", "function": {"name": "shell", "arguments": {"cmd": "pwd"}}}
    ]},
    {"role": "tool", "tool_call_id": "call_abc", "content": "workspace=/tmp/project"},
    {"role": "assistant", "content": [{"type": "text", "text": "The memory plan is ready."}]},
    {"role": "user", "content": "Persist it"},
    {"role": "assistant", "content": "Persisted."}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	detected, err := DetectProviderFromFile(path)
	if err != nil {
		t.Fatalf("DetectProviderFromFile() error = %v", err)
	}
	if detected != ProviderHermes {
		t.Fatalf("detected provider=%q want %q", detected, ProviderHermes)
	}

	parsed, err := ParseHermesFile(path, "", "/tmp/workspace", "actor:test")
	if err != nil {
		t.Fatalf("ParseHermesFile() error = %v", err)
	}
	if parsed.Provider != ProviderHermes {
		t.Fatalf("provider=%q want %q", parsed.Provider, ProviderHermes)
	}
	if parsed.SessionID != "20260513_230931_12cc11" {
		t.Fatalf("session_id=%q want fixture id", parsed.SessionID)
	}
	if parsed.SourcePath != path {
		t.Fatalf("source_path=%q want %q", parsed.SourcePath, path)
	}
	if parsed.WorkspacePath != "/tmp/workspace" {
		t.Fatalf("workspace_path=%q want /tmp/workspace", parsed.WorkspacePath)
	}
	if len(parsed.Turns) != 2 {
		t.Fatalf("turn count=%d want 2", len(parsed.Turns))
	}

	first := parsed.Turns[0]
	if first.SessionID != "source:hermes:20260513_230931_12cc11" {
		t.Fatalf("turn session_id=%q want source hermes id", first.SessionID)
	}
	if first.Command != "source.import.hermes" {
		t.Fatalf("command=%q want source.import.hermes", first.Command)
	}
	if !strings.Contains(first.Prompt, "local memory plan") {
		t.Fatalf("prompt=%q want memory prompt", first.Prompt)
	}
	if first.FinalOutput.Text != "The memory plan is ready." {
		t.Fatalf("final_output=%q want final assistant text", first.FinalOutput.Text)
	}
	wantCreated := time.Date(2026, 5, 13, 23, 9, 31, 343426000, time.UTC)
	if !first.CreatedAt.Equal(wantCreated) {
		t.Fatalf("created_at=%s want %s", first.CreatedAt, wantCreated)
	}
	if len(first.Iterations) != 2 {
		t.Fatalf("first turn iterations=%d want 2", len(first.Iterations))
	}
	if len(first.Iterations[0].ToolCalls) != 1 {
		t.Fatalf("tool calls=%d want 1", len(first.Iterations[0].ToolCalls))
	}
	call := first.Iterations[0].ToolCalls[0]
	if call.CallID != "response_call_abc" || call.Name != "shell" {
		t.Fatalf("tool call id/name=%q/%q want response_call_abc/shell", call.CallID, call.Name)
	}
	if call.Status != "ok" {
		t.Fatalf("tool status=%q want ok", call.Status)
	}
	if call.ResultRef.Text != "workspace=/tmp/project" {
		t.Fatalf("tool result=%q want workspace result", call.ResultRef.Text)
	}
	var args map[string]string
	if err := json.Unmarshal(call.ArgsJSON, &args); err != nil {
		t.Fatalf("unmarshal tool args: %v", err)
	}
	if args["cmd"] != "pwd" {
		t.Fatalf("tool args cmd=%q want pwd", args["cmd"])
	}

	second := parsed.Turns[1]
	if second.Prompt != "Persist it" || second.FinalOutput.Text != "Persisted." {
		t.Fatalf("second turn prompt/final=%q/%q want persist turn", second.Prompt, second.FinalOutput.Text)
	}
}

func TestLocateHermesSessionJSON_UsesHermesHome(t *testing.T) {
	home := t.TempDir()
	sessionsDir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	path := filepath.Join(sessionsDir, "session_20260513_230931_12cc11.json")
	if err := os.WriteFile(path, []byte(`{"session_id":"20260513_230931_12cc11","model":"glm","messages":[{"role":"user","content":"hi"}]}`), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	t.Setenv("HERMES_HOME", home)

	if got := LocateHermesSessionJSON("20260513_230931_12cc11"); got != path {
		t.Fatalf("LocateHermesSessionJSON()=%q want %q", got, path)
	}
	gotPath, gotID := LocateMostRecentHermesSessionJSON()
	if gotPath != path {
		t.Fatalf("LocateMostRecentHermesSessionJSON() path=%q want %q", gotPath, path)
	}
	if gotID != "20260513_230931_12cc11" {
		t.Fatalf("LocateMostRecentHermesSessionJSON() id=%q want fixture id", gotID)
	}
}
