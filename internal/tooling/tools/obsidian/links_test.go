package obsidian

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNoteLinks(t *testing.T) {
	content := []byte(`---
aliases:
  - ACA Layers
---

# ACA Memory Layers

See [[ACA MOC]] and [[ACA L1 Top of Mind|Top of Mind]].
`)

	result := ParseNoteLinks("ACA Memory Layers.md", content)
	if result.Title != "ACA Memory Layers" {
		t.Fatalf("title=%q", result.Title)
	}
	if len(result.Aliases) != 1 || result.Aliases[0] != "ACA Layers" {
		t.Fatalf("aliases=%v", result.Aliases)
	}
	if len(result.Headings) != 1 || result.Headings[0].Anchor != "aca-memory-layers" {
		t.Fatalf("headings=%v", result.Headings)
	}
	if len(result.Outgoing) != 2 {
		t.Fatalf("outgoing=%d", len(result.Outgoing))
	}
	if result.Outgoing[1].Alias != "Top of Mind" {
		t.Fatalf("alias=%q", result.Outgoing[1].Alias)
	}
}

func TestRelatedNotes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "ACA Memory Layers.md"), `---
aliases:
  - ACA Layers
---

# ACA Memory Layers

See [[ACA MOC]] and [[ACA L1 Top of Mind]].
`)
	mustWrite(t, filepath.Join(root, "ACA MOC.md"), `# ACA MOC

- [[ACA Memory Layers]]
`)
	mustWrite(t, filepath.Join(root, "ACA L1 Top of Mind.md"), `# ACA L1 Top of Mind

Related to [[ACA Memory Layers]]
`)
	mustWrite(t, filepath.Join(root, "Unrelated.md"), `# Unrelated`)

	results, err := RelatedNotes(root, filepath.Join(root, "ACA Memory Layers.md"), LinkQueryOptions{
		IncludeDirect: true,
		IncludeBack:   true,
		IncludeAlias:  true,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("RelatedNotes: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("results=%v", results)
	}
	if results[0].Title == "Unrelated" {
		t.Fatalf("unexpected unrelated first result: %+v", results[0])
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
