package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/spf13/cobra"
)

func TestNewCICommand(t *testing.T) {
	cmd := newCICommand()
	if cmd.Use != "ci" {
		t.Fatalf("expected use ci, got %s", cmd.Use)
	}
	subs := cmd.Commands()
	expected := []string{"status", "prcomments", "checks", "todos", "comments", "results"}
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

func TestCreateTodoFromCITask_BuildsPayloadAndCallsTodoSkill(t *testing.T) {
	prev := runTodoSkillFunc
	t.Cleanup(func() { runTodoSkillFunc = prev })

	var gotCmd *cobra.Command
	var gotPayload map[string]any
	runTodoSkillFunc = func(c *cobra.Command, payload map[string]any) error {
		gotCmd = c
		gotPayload = payload
		return nil
	}

	cmd := &cobra.Command{Use: "test"}
	tm := map[string]any{
		"summary":        "Fix `thing` formatting",
		"kind":           "review_comment",
		"source":         "coderabbit",
		"severity":       "minor",
		"file":           "cmd/agentctl/cmd/ci.go",
		"line":           float64(42),
		"comment_author": "coderabbitai[bot]",
		"comment_body":   "Original body with `backticks` inside.",
	}

	storePath := "/tmp/todo-store.json"
	if err := createTodoFromCITask(cmd, tm, storePath); err != nil {
		t.Fatalf("createTodoFromCITask returned error: %v", err)
	}
	if gotCmd == nil {
		t.Fatalf("expected runTodoSkillFunc to be called with command")
	}
	if gotPayload == nil {
		t.Fatalf("expected payload to be captured")
	}

	op, ok := gotPayload["operation"].(string)
	if !ok || op != "add" {
		t.Fatalf("expected operation add, got %v", gotPayload["operation"])
	}
	add, ok := gotPayload["add"].(map[string]any)
	if !ok {
		t.Fatalf("expected add map, got %T", gotPayload["add"])
	}

	title, _ := add["title"].(string)
	if title != "[review_comment] Fix 'thing' formatting" {
		t.Fatalf("unexpected title: %q", title)
	}
	desc, _ := add["description"].(string)
	if desc == "" {
		t.Fatalf("expected non-empty description")
	}
	if strings.Contains(desc, "`") {
		t.Fatalf("description should not contain backticks: %q", desc)
	}
	if !strings.Contains(desc, "Source: coderabbit") {
		t.Fatalf("description missing source: %q", desc)
	}
	if !strings.Contains(desc, "Severity: minor") {
		t.Fatalf("description missing severity: %q", desc)
	}
	if !strings.Contains(desc, "Location: cmd/agentctl/cmd/ci.go:42") {
		t.Fatalf("description missing location: %q", desc)
	}
	if !strings.Contains(desc, "Reviewer: coderabbitai[bot]") {
		t.Fatalf("description missing reviewer: %q", desc)
	}
	if !strings.Contains(desc, "Original body with 'backticks' inside.") {
		t.Fatalf("description missing sanitized comment body: %q", desc)
	}

	if sp, ok := gotPayload["store_path"].(string); !ok || sp != storePath {
		t.Fatalf("expected store_path %q, got %v", storePath, gotPayload["store_path"])
	}
}

func TestCreateTodoFromCITask_SkipsEmptySummary(t *testing.T) {
	prev := runTodoSkillFunc
	t.Cleanup(func() { runTodoSkillFunc = prev })

	called := 0
	runTodoSkillFunc = func(_ *cobra.Command, _ map[string]any) error {
		called++
		return nil
	}

	cmd := &cobra.Command{Use: "test"}
	tm := map[string]any{"summary": "   "}
	if err := createTodoFromCITask(cmd, tm, ""); err != nil {
		t.Fatalf("expected no error for empty summary, got %v", err)
	}
	if called != 0 {
		t.Fatalf("expected runTodoSkillFunc not to be called for empty summary, called %d times", called)
	}
}
