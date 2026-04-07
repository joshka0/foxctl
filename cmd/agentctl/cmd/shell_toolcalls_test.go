package cmd

import (
	"encoding/json"
	"testing"
)

func TestExtractCodexCommand(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"cmd": "git log --stat -5",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := extractCodexCommand(raw)
	if got != "git log --stat -5" {
		t.Fatalf("got %q want git log --stat -5", got)
	}
}

func TestExtractCodexCommandJSONString(t *testing.T) {
	raw, err := json.Marshal(`{"cmd":"rg -n foo ."}`)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := extractCodexCommand(raw)
	if got != "rg -n foo ." {
		t.Fatalf("got %q want rg -n foo .", got)
	}
}

func TestTopCounts(t *testing.T) {
	target := map[string]*transcriptCount{
		"git log": {Name: "git log", Total: 5},
		"rg":      {Name: "rg", Total: 9},
		"ls":      {Name: "ls", Total: 3},
	}
	got := topCounts(target, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got[0].Name != "rg" || got[1].Name != "git log" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestNewShellCommandHasToolcallsSubcommand(t *testing.T) {
	cmd := newShellCommand()
	found := false
	for _, child := range cmd.Commands() {
		if child.Name() == "toolcalls" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("toolcalls subcommand missing")
	}
}
