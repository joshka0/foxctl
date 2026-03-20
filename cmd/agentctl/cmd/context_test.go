package cmd

import "testing"

func TestNewContextCommand(t *testing.T) {
	cmd := newContextCommand()
	if cmd.Use != "context" {
		t.Fatalf("expected use context, got %s", cmd.Use)
	}
	subs := cmd.Commands()
	expected := []string{"show", "report", "retrieve", "retrieve-inspect", "retrieve-inspect-suite", "retrieve-inspect-runs", "retrieve-inspect-artifact", "repoindex-search-inspect-suite", "repoindex-dag-inspect-suite", "semantic-search-inspect-suite", "task-history", "task-history-summary", "cochange", "next", "next-proposal-merge", "dispatch", "contradictions", "rethink", "handoffs", "observations", "tensions", "proposals", "proposal", "import-evidence", "infer", "promote", "merge-promotion", "hooks"}
	got := map[string]bool{}
	for _, sub := range subs {
		got[sub.Name()] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Fatalf("expected subcommand %s", name)
		}
	}
}
