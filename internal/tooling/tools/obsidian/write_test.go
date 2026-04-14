package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendMarkdownUnderHeading(t *testing.T) {
	input := "# Title\n\n## Recent Findings\n\n- a\n\n## Next\n\n- b\n"
	got := appendMarkdownUnderHeading(input, "Recent Findings", "- c")
	want := "# Title\n\n## Recent Findings\n\n- a\n\n- c\n## Next\n\n- b\n"
	if got != want {
		t.Fatalf("unexpected markdown:\n%s", got)
	}
}

func TestWriterCreateAndRead(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "obsidian")
	vaultRoot := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	content := `#!/bin/sh
cmd="$1"; shift
vault=""
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    vault=*) vault="${arg#vault=}" ;;
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
mkdir -p "$(dirname "$full")"
case "$cmd" in
  create) printf "%s" "$payload" > "$full" ;;
  read) cat "$full" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	w := NewWriter(script, "TestVault", DefaultPolicy())
	w.PostCreateDelay = 0
	notePath := "inbox/drafted-from-foxctl/test.md"
	if err := w.CreateNote(context.Background(), notePath, "# Test\n", true); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	got, err := w.Read(context.Background(), notePath)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "# Test" {
		t.Fatalf("content=%q", got)
	}
}

func TestWriterMergeReviewedDraftContent(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "obsidian")
	vaultRoot := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "patterns"), 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	content := `#!/bin/sh
cmd="$1"; shift
vault=""
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    vault=*) vault="${arg#vault=}" ;;
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
case "$cmd" in
  create)
    mkdir -p "$(dirname "$full")"
    printf "%s" "$payload" > "$full"
    ;;
  read)
    if [ ! -f "$full" ]; then
      echo "File not found." 1>&2
      exit 1
    fi
    cat "$full"
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	w := NewWriter(script, "TestVault", DefaultPolicy())
	w.PostCreateDelay = 0
	draft := `---
title: Compact Handoff Pattern
type: pattern
status: draft
trust: raw
---

# Compact Handoff Pattern

## Statement

Compact handoffs work better.
`
	createResult, err := w.MergeReviewedDraftContent(context.Background(), "notes/patterns/compact-handoff-pattern.md", "", draft, "draft.md")
	if err != nil {
		t.Fatalf("MergeReviewedDraftContent create: %v", err)
	}
	if createResult.MergedAs != "create" {
		t.Fatalf("mergedAs=%q want create", createResult.MergedAs)
	}
	body, err := os.ReadFile(filepath.Join(vaultRoot, "notes", "patterns", "compact-handoff-pattern.md"))
	if err != nil {
		t.Fatalf("read created note: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "status: reviewed") || !strings.Contains(text, "trust: canonical") {
		t.Fatalf("expected reviewed canonical frontmatter, got:\n%s", text)
	}

	appendResult, err := w.MergeReviewedDraftContent(context.Background(), "notes/patterns/compact-handoff-pattern.md", "Review", draft, "draft.md")
	if err != nil {
		t.Fatalf("MergeReviewedDraftContent append: %v", err)
	}
	if appendResult.MergedAs != "append" {
		t.Fatalf("mergedAs=%q want append", appendResult.MergedAs)
	}
	body, err = os.ReadFile(filepath.Join(vaultRoot, "notes", "patterns", "compact-handoff-pattern.md"))
	if err != nil {
		t.Fatalf("read appended note: %v", err)
	}
	text = string(body)
	if !strings.Contains(text, "### Reviewed Merge") || !strings.Contains(text, "Source draft: `draft.md`") {
		t.Fatalf("expected reviewed merge block, got:\n%s", text)
	}
}

func TestWriterReviewMergeDraftCreatesCanonicalNote(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "obsidian")
	vaultRoot := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	content := `#!/bin/sh
cmd="$1"; shift
vault=""
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    vault=*) vault="${arg#vault=}" ;;
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
case "$cmd" in
  create)
    mkdir -p "$(dirname "$full")"
    printf "%s" "$payload" > "$full"
    ;;
  read)
    if [ ! -f "$full" ]; then
      echo "File not found." >&2
      exit 1
    fi
    cat "$full"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	w := NewWriter(script, "TestVault", DefaultPolicy())
	w.PostCreateDelay = 0
	draftPath := "inbox/drafted-from-foxctl/promotion.md"
	draft := "---\ntitle: Compact Handoff Pattern\ntype: pattern\nstatus: draft\ntrust: reviewed\nprimary_anchor_path: internal/context/contextplane/store.go\nimpl_anchor_paths:\n  - internal/context/contextplane/store.go\nsupport_anchor_paths:\n  - internal/context/contextplane/retrieval.go\nprovenance_refs:\n  - observation:O-887\n---\n\n# Compact Handoff Pattern\n\nCompact handoffs work better.\n"
	if err := w.CreateNote(context.Background(), draftPath, draft, true); err != nil {
		t.Fatalf("CreateNote draft: %v", err)
	}
	result, err := w.ReviewMergeDraft(context.Background(), draftPath, "notes/patterns/compact-handoff-pattern.md", "")
	if err != nil {
		t.Fatalf("ReviewMergeDraft: %v", err)
	}
	if result.MergedAs != "create" {
		t.Fatalf("MergedAs=%q want create", result.MergedAs)
	}
	got, err := w.Read(context.Background(), "notes/patterns/compact-handoff-pattern.md")
	if err != nil {
		t.Fatalf("Read canonical: %v", err)
	}
	if !strings.Contains(got, "status: reviewed") || !strings.Contains(got, "trust: canonical") {
		t.Fatalf("canonical frontmatter not updated:\n%s", got)
	}
	if !strings.Contains(got, "primary_anchor_path: internal/context/contextplane/store.go") {
		t.Fatalf("primary anchor path not preserved:\n%s", got)
	}
	if !strings.Contains(got, "impl_anchor_paths:") || !strings.Contains(got, "support_anchor_paths:") {
		t.Fatalf("anchor role lists not preserved:\n%s", got)
	}
}

func TestWriterReviewMergeDraftAppendsToCanonicalHeading(t *testing.T) {
	tmp := t.TempDir()
	script := filepath.Join(tmp, "obsidian")
	vaultRoot := filepath.Join(tmp, "vault")
	if err := os.MkdirAll(vaultRoot, 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	content := `#!/bin/sh
cmd="$1"; shift
vault=""
path=""
payload=""
for arg in "$@"; do
  case "$arg" in
    vault=*) vault="${arg#vault=}" ;;
    path=*) path="${arg#path=}" ;;
    content=*) payload="${arg#content=}" ;;
  esac
done
root="` + vaultRoot + `"
full="$root/$path"
case "$cmd" in
  create)
    mkdir -p "$(dirname "$full")"
    printf "%s" "$payload" > "$full"
    ;;
  read)
    if [ ! -f "$full" ]; then
      echo "File not found." >&2
      exit 1
    fi
    cat "$full"
    ;;
  *)
    exit 1
    ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	w := NewWriter(script, "TestVault", DefaultPolicy())
	w.PostCreateDelay = 0
	if _, err := w.run(context.Background(), "create", "vault=TestVault", "path=notes/patterns/compact-handoff-pattern.md", "content=---\ntitle: Compact Handoff Pattern\ntype: pattern\nstatus: reviewed\ntrust: canonical\n---\n\n# Compact Handoff Pattern\n\n## Review\n\nExisting review.\n", "overwrite"); err != nil {
		t.Fatalf("seed canonical: %v", err)
	}
	draftPath := "inbox/drafted-from-foxctl/promotion.md"
	draft := "---\ntitle: Compact Handoff Pattern\ntype: pattern\nstatus: draft\ntrust: reviewed\n---\n\n# Compact Handoff Pattern\n\nNew reviewed content.\n"
	if err := w.CreateNote(context.Background(), draftPath, draft, true); err != nil {
		t.Fatalf("CreateNote draft: %v", err)
	}
	result, err := w.ReviewMergeDraft(context.Background(), draftPath, "notes/patterns/compact-handoff-pattern.md", "Review")
	if err != nil {
		t.Fatalf("ReviewMergeDraft: %v", err)
	}
	if result.MergedAs != "append" {
		t.Fatalf("MergedAs=%q want append", result.MergedAs)
	}
	got, err := w.Read(context.Background(), "notes/patterns/compact-handoff-pattern.md")
	if err != nil {
		t.Fatalf("Read canonical: %v", err)
	}
	if !strings.Contains(got, "### Reviewed Merge") || !strings.Contains(got, "New reviewed content.") {
		t.Fatalf("expected reviewed merge block:\n%s", got)
	}
}
