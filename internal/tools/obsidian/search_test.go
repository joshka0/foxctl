package obsidian

import (
	"context"
	"testing"
)

func TestSearchParsesJSONMatches(t *testing.T) {
	bin := writeFakeCLI(t, `#!/bin/sh
case "$1" in
  search)
    printf '%s\n' '[{"path":"notes/a.md","line":12,"text":"alpha"},{"path":"notes/b.md","line":4,"text":"beta"}]'
    ;;
  *)
    exit 1
    ;;
esac
`)

	result, err := Search(context.Background(), SearchOptions{
		BinaryPath: bin,
		VaultName:  "Obsidian Vault",
		Query:      "alpha",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("matches=%d want 2", len(result.Matches))
	}
	if result.Matches[0].Path != "notes/a.md" {
		t.Fatalf("path=%q", result.Matches[0].Path)
	}
}

func TestSearchFallsBackToRawOutput(t *testing.T) {
	bin := writeFakeCLI(t, `#!/bin/sh
case "$1" in
  search)
    printf '%s\n' 'notes/a.md:12:alpha'
    ;;
  *)
    exit 1
    ;;
esac
`)

	result, err := Search(context.Background(), SearchOptions{
		BinaryPath: bin,
		VaultName:  "Obsidian Vault",
		Query:      "alpha",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.Raw != "notes/a.md:12:alpha" {
		t.Fatalf("raw=%q", result.Raw)
	}
	if len(result.Matches) != 0 {
		t.Fatalf("matches=%d want 0", len(result.Matches))
	}
}
