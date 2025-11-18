package main

import (
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	output := `worktree /path/to/main
HEAD 1234567890abcdef
branch refs/heads/main

worktree /path/to/feature
HEAD abcdef1234567890
branch refs/heads/feature

`
	results := parseWorktreeList(output)

	if len(results) != 2 {
		t.Errorf("expected 2 worktrees, got %d", len(results))
	}

	if results[0].Path != "/path/to/main" {
		t.Errorf("expected /path/to/main, got %s", results[0].Path)
	}
	if results[0].Branch != "refs/heads/main" {
		t.Errorf("expected refs/heads/main, got %s", results[0].Branch)
	}

	if results[1].Path != "/path/to/feature" {
		t.Errorf("expected /path/to/feature, got %s", results[1].Path)
	}
}
