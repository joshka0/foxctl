package cmd

import "testing"

func TestNewHooksCommand(t *testing.T) {
	cmd := newHooksCommand()
	if cmd.Use != "hooks" {
		t.Fatalf("expected use hooks, got %s", cmd.Use)
	}
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	if !got["session-start"] {
		t.Fatalf("expected session-start subcommand")
	}
	if !got["session-end"] {
		t.Fatalf("expected session-end subcommand")
	}
	if !got["subagent-stop"] {
		t.Fatalf("expected subagent-stop subcommand")
	}
	if !got["todo-sync"] {
		t.Fatalf("expected todo-sync subcommand")
	}
	if !got["todo-continuation"] {
		t.Fatalf("expected todo-continuation subcommand")
	}
	if !got["task-file-link"] {
		t.Fatalf("expected task-file-link subcommand")
	}
	if !got["context-updater-drain"] {
		t.Fatalf("expected context-updater-drain subcommand")
	}
	if !got["session-restore-postcompact"] {
		t.Fatalf("expected session-restore-postcompact subcommand")
	}
	if !got["overseer-inbox"] {
		t.Fatalf("expected overseer-inbox subcommand")
	}
	if !got["overseer-inbox-post"] {
		t.Fatalf("expected overseer-inbox-post subcommand")
	}
	if !got["anchor-detect"] {
		t.Fatalf("expected anchor-detect subcommand")
	}
	if !got["memory-detector"] {
		t.Fatalf("expected memory-detector subcommand")
	}
	if !got["memory-recall"] {
		t.Fatalf("expected memory-recall subcommand")
	}
	if !got["memory-lifecycle"] {
		t.Fatalf("expected memory-lifecycle subcommand")
	}
	if !got["code-analysis"] {
		t.Fatalf("expected code-analysis subcommand")
	}
	if !got["live-index"] {
		t.Fatalf("expected live-index subcommand")
	}
	if !got["lsp-diagnostics"] {
		t.Fatalf("expected lsp-diagnostics subcommand")
	}
	if !got["embedding-flush"] {
		t.Fatalf("expected embedding-flush subcommand")
	}
	if !got["plan-sync"] {
		t.Fatalf("expected plan-sync subcommand")
	}
	if !got["graph-maintenance"] {
		t.Fatalf("expected graph-maintenance subcommand")
	}
}
