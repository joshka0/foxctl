package obsidian

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureVault_BasicLayout(t *testing.T) {
	root := filepath.Join("testdata", "vaults", "basic")

	requiredPaths := []string{
		filepath.Join(root, ".obsidian"),
		filepath.Join(root, "00-home", "index.md"),
		filepath.Join(root, "atlas", "projects.md"),
		filepath.Join(root, "notes", "adr", "adr-0001-context-architecture.md"),
		filepath.Join(root, "notes", "patterns", "compact-handoff-pattern.md"),
		filepath.Join(root, "notes", "moc", "foxctl-context.md"),
		filepath.Join(root, "inbox", "drafted-from-foxctl", "handoff-draft.md"),
	}

	for _, path := range requiredPaths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected fixture path %s: %v", path, err)
		}
	}
}

func TestFixtureVault_LinkRichNote(t *testing.T) {
	path := filepath.Join("testdata", "vaults", "basic", "notes", "moc", "foxctl-context.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture note: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"[[ACA Memory Layers]]",
		"[[Compact Handoff Pattern]]",
		"[[ADR-0001 Context Architecture]]",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("fixture note missing %q", want)
		}
	}
}

func TestFixtureVault_PolicyBoundedAppendSection(t *testing.T) {
	path := filepath.Join("testdata", "vaults", "basic", "notes", "patterns", "compact-handoff-pattern.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pattern note: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "## Recent Findings") {
		t.Fatalf("expected bounded append section in %s", path)
	}
	if !strings.Contains(text, "provenance_refs:") {
		t.Fatalf("expected provenance frontmatter in %s", path)
	}
}
