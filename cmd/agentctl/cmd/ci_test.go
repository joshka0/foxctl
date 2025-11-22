package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func TestNewCICommand(t *testing.T) {
	cmd := newCICommand()
	if cmd.Use != "ci" {
		t.Fatalf("expected use ci, got %s", cmd.Use)
	}
	subs := cmd.Commands()
	expected := []string{"prcomments", "checks", "todos"}
	if len(subs) != len(expected) {
		t.Fatalf("expected %d subcommands, got %d", len(expected), len(subs))
	}
	got := map[string]bool{}
	for _, sub := range subs {
		got[sub.Use] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Fatalf("expected subcommand %s to exist", name)
		}
	}
}

func TestCITodosFlags(t *testing.T) {
	cmd := newCITodosCommand()
	if cmd.Use != "todos" {
		t.Fatalf("expected todos command, got %s", cmd.Use)
	}
	for _, flag := range []string{"pr", "owner", "repo", "store", "skip-cache"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestCIPRCommentsFlags(t *testing.T) {
	cmd := newCIPRCommentsCommand()
	if cmd.Use != "prcomments" {
		t.Fatalf("expected prcomments command, got %s", cmd.Use)
	}
	for _, flag := range []string{"pr", "owner", "repo", "format", "output-path", "with-context", "errors-only", "skip-cache", "data-only", "no-comments"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestCIChecksFlags(t *testing.T) {
	cmd := newCIChecksCommand()
	if cmd.Use != "checks" {
		t.Fatalf("expected checks command, got %s", cmd.Use)
	}
	for _, flag := range []string{"pr", "owner", "repo", "mode", "errors-only", "skip-cache", "data-only"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestCIPRCommentsHelpJSON(t *testing.T) {
	cmd := newCIPRCommentsCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--help-json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help-json execution failed: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode help envelope: %v", err)
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("expected ok status, got %s", env.Status)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	if _, ok := data["flags"]; !ok {
		t.Fatalf("expected flags field in help data")
	}
}
