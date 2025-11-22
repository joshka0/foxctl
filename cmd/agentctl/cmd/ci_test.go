package cmd

import "testing"

func TestNewCICommand(t *testing.T) {
	cmd := newCICommand()
	if cmd.Use != "ci" {
		t.Fatalf("expected use ci, got %s", cmd.Use)
	}
	subs := cmd.Commands()
	expected := []string{"prcomments", "checks"}
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
