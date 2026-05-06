package obsidian

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBridgeSearchProvider struct {
	hits []DocsBridgeSearchHit
}

func (f fakeBridgeSearchProvider) SearchBridgeCandidates(_ context.Context, _ string, _ int) ([]DocsBridgeSearchHit, error) {
	return f.hits, nil
}

func TestReconcileDocsBridge(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "repo")
	docsRoot := filepath.Join(workspaceRoot, "docs", "architecture")
	vaultRoot := filepath.Join(root, "vault")
	if err := os.MkdirAll(docsRoot, 0o755); err != nil {
		t.Fatalf("mkdir docs root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "repo", "foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir vault notes: %v", err)
	}

	docBody := `---
title: AgentCTL Context Architecture
repo_anchors:
  - anchor:repo:invariant/no-send-without-read
repo_symbols:
  - internal/context/contextplane.WorkspaceStore
---

# AgentCTL Context Architecture

This doc explains the control plane and semantic indexing package wiring.
`
	if err := os.WriteFile(filepath.Join(docsRoot, "context-architecture.md"), []byte(docBody), 0o644); err != nil {
		t.Fatalf("write repo doc: %v", err)
	}

	canonicalBody := `---
title: Context Architecture
type: map
project: foxctl
status: reviewed
trust: canonical
repo_docs:
  - docs/architecture/context-architecture.md
---

# Context Architecture

This note ties together the control plane, semantic indexing, and runtime wiring.
`
	if err := os.WriteFile(filepath.Join(vaultRoot, "notes", "repo", "foxctl", "context-architecture.md"), []byte(canonicalBody), 0o644); err != nil {
		t.Fatalf("write canonical note: %v", err)
	}

	semanticBody := `---
title: Semantic And Memory
type: map
project: foxctl
status: reviewed
trust: canonical
---

# Semantic And Memory

This note covers semantic indexing package behavior and storage memory.
`
	if err := os.WriteFile(filepath.Join(vaultRoot, "notes", "repo", "foxctl", "semantic-and-memory.md"), []byte(semanticBody), 0o644); err != nil {
		t.Fatalf("write semantic note: %v", err)
	}

	writer := NewWriter("", "TestVault", DefaultPolicy())
	writer.VaultPath = vaultRoot
	writer.PostCreateDelay = 0

	result, err := ReconcileDocsBridge(ctx, writer, DocsBridgeReconcileOptions{
		Project:       "foxctl",
		WorkspaceRoot: workspaceRoot,
		MaxMatches:    3,
	})
	if err != nil {
		t.Fatalf("ReconcileDocsBridge: %v", err)
	}
	if result.RootNotePath == "" || len(result.DocNotes) != 1 {
		t.Fatalf("unexpected reconcile result: %#v", result)
	}
	docResult := result.DocNotes[0]
	if got := docResult.DocPath; got != "docs/architecture/context-architecture.md" {
		t.Fatalf("doc path = %q", got)
	}
	if len(docResult.ExistingRepoDocBackrefs) == 0 {
		t.Fatalf("expected existing repo_docs backlink: %#v", docResult)
	}
	if len(docResult.SuggestedVaultRefs) == 0 {
		t.Fatalf("expected suggested vault refs: %#v", docResult)
	}

	draftBody, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(docResult.DraftPath)))
	if err != nil {
		t.Fatalf("read draft note: %v", err)
	}
	text := string(draftBody)
	if !strings.Contains(text, "repo_docs:") || !strings.Contains(text, "vault_refs:") {
		t.Fatalf("expected bridge metadata in draft note:\n%s", text)
	}
	if !strings.Contains(text, "repo_anchors:") || !strings.Contains(text, "anchor:repo:invariant/no-send-without-read") {
		t.Fatalf("expected repo anchor suggestions in draft note:\n%s", text)
	}
	if !strings.Contains(text, "repo_symbols:") || !strings.Contains(text, "internal/context/contextplane.WorkspaceStore") {
		t.Fatalf("expected repo symbol suggestions in draft note:\n%s", text)
	}
	if !strings.Contains(text, "orphaned_anchor:anchor:repo:invariant/no-send-without-read") {
		t.Fatalf("expected orphaned anchor health finding in draft note:\n%s", text)
	}
	if !strings.Contains(text, "docs/architecture/context-architecture.md") {
		t.Fatalf("expected repo doc path in draft note:\n%s", text)
	}
	if !strings.Contains(text, "notes/repo/foxctl/context-architecture.md") {
		t.Fatalf("expected canonical note suggestion in draft note:\n%s", text)
	}

	rootBody, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(result.RootNotePath)))
	if err != nil {
		t.Fatalf("read bridge root note: %v", err)
	}
	if !strings.Contains(string(rootBody), "Docs with links or suggestions") {
		t.Fatalf("expected summary in root note:\n%s", string(rootBody))
	}
}

func TestSuggestVaultNotesPrefersTopicalNotesForArchitectureDocs(t *testing.T) {
	doc := bridgeDoc{
		RelPath: "docs/architecture/context-architecture.md",
		Title:   "AgentCTL Context Architecture",
		Body:    "# AgentCTL Context Architecture\n\nControl plane and runtime wiring.\n",
	}
	notes := []bridgeVaultNote{
		{
			Path:  "notes/repo/foxctl/packages/cmd-foxctl-cmd.md",
			Title: "cmd foxctl cmd",
			Body:  "Main command package.",
		},
		{
			Path:  "notes/repo/foxctl/index.md",
			Title: "foxctl Repo Graph",
			Body:  "Root repo map and architecture entrypoint.",
		},
		{
			Path:  "notes/repo/foxctl/skills-runtime-wiring.md",
			Title: "skills runtime wiring",
			Body:  "Runtime wiring, hooks, orchestration, and control plane behavior.",
		},
	}

	suggestions := suggestVaultNotes(context.Background(), doc, notes, 3, "foxctl", nil)
	if len(suggestions) < 2 {
		t.Fatalf("expected suggestions, got %#v", suggestions)
	}
	if suggestions[0].Path == "notes/repo/foxctl/packages/cmd-foxctl-cmd.md" {
		t.Fatalf("expected architecture doc to prefer topical or root note over package note: %#v", suggestions)
	}
}

func TestSuggestVaultNotesUsesSearchProviderBoosts(t *testing.T) {
	doc := bridgeDoc{
		RelPath: "docs/general/memory.md",
		Title:   "Memory",
		Body:    "# Memory\n\nStorage and retrieval.\n",
	}
	notes := []bridgeVaultNote{
		{
			Path:  "notes/repo/foxctl/platform-and-web.md",
			Title: "platform and web",
			Body:  "Platform configuration and web api.",
		},
		{
			Path:  "notes/repo/foxctl/semantic-and-memory.md",
			Title: "semantic and memory",
			Body:  "Semantic indexing and memory store.",
		},
	}
	provider := fakeBridgeSearchProvider{
		hits: []DocsBridgeSearchHit{
			{Path: "notes/repo/foxctl/semantic-and-memory.md", Title: "semantic and memory", Score: 100},
		},
	}

	suggestions := suggestVaultNotes(context.Background(), doc, notes, 2, "foxctl", provider)
	if len(suggestions) == 0 {
		t.Fatalf("expected suggestions")
	}
	if suggestions[0].Path != "notes/repo/foxctl/semantic-and-memory.md" {
		t.Fatalf("expected provider-backed semantic note to rank first: %#v", suggestions)
	}
}

func TestApplyDocsBridgeDraft(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "repo")
	vaultRoot := filepath.Join(root, "vault")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "docs", "general"), 0o755); err != nil {
		t.Fatalf("mkdir repo docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "repo", "foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir vault notes: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultRoot, "inbox", "drafted-from-foxctl", "docs-bridge", "foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir draft folder: %v", err)
	}

	repoDocPath := filepath.Join(workspaceRoot, "docs", "general", "memory.md")
	repoDocBody := "# Memory\n\nExisting prose.\n"
	if err := os.WriteFile(repoDocPath, []byte(repoDocBody), 0o644); err != nil {
		t.Fatalf("write repo doc: %v", err)
	}

	vaultNotePath := filepath.Join(vaultRoot, "notes", "repo", "foxctl", "semantic-and-memory.md")
	vaultNoteBody := `---
title: semantic and memory
type: map
status: reviewed
trust: canonical
---

# semantic and memory
`
	if err := os.WriteFile(vaultNotePath, []byte(vaultNoteBody), 0o644); err != nil {
		t.Fatalf("write vault note: %v", err)
	}

	draftPath := filepath.Join(vaultRoot, "inbox", "drafted-from-foxctl", "docs-bridge", "foxctl", "docsgeneralmemorymd.md")
	draftBody := `---
title: Memory Bridge
type: map
project: foxctl
status: draft
trust: raw
repo_docs:
  - docs/general/memory.md
repo_anchors:
  - anchor:repo:invariant/no-send-without-read
repo_symbols:
  - internal/context/contextplane.WorkspaceStore
vault_refs:
suggested_vault_refs:
  - notes/repo/foxctl/semantic-and-memory.md
updated: 2026-03-11
---

# Memory Bridge
`
	if err := os.WriteFile(draftPath, []byte(draftBody), 0o644); err != nil {
		t.Fatalf("write draft note: %v", err)
	}

	writer := NewWriter("", "TestVault", DefaultPolicy())
	writer.VaultPath = vaultRoot
	writer.PostCreateDelay = 0

	result, err := ApplyDocsBridgeDraft(ctx, writer, DocsBridgeApplyOptions{
		Project:       "foxctl",
		WorkspaceRoot: workspaceRoot,
		DraftPath:     "inbox/drafted-from-foxctl/docs-bridge/foxctl/docsgeneralmemorymd.md",
	})
	if err != nil {
		t.Fatalf("ApplyDocsBridgeDraft: %v", err)
	}
	if !result.RepoDocUpdated || result.VaultNotesUpdated != 1 {
		t.Fatalf("unexpected apply result: %#v", result)
	}

	updatedRepoDoc, err := os.ReadFile(repoDocPath)
	if err != nil {
		t.Fatalf("read updated repo doc: %v", err)
	}
	if !strings.Contains(string(updatedRepoDoc), "vault_refs:") || !strings.Contains(string(updatedRepoDoc), "notes/repo/foxctl/semantic-and-memory.md") {
		t.Fatalf("expected repo doc vault_refs patch:\n%s", string(updatedRepoDoc))
	}

	updatedVaultNote, err := os.ReadFile(vaultNotePath)
	if err != nil {
		t.Fatalf("read updated vault note: %v", err)
	}
	if !strings.Contains(string(updatedVaultNote), "repo_docs:") || !strings.Contains(string(updatedVaultNote), "docs/general/memory.md") {
		t.Fatalf("expected vault note repo_docs patch:\n%s", string(updatedVaultNote))
	}
	if !strings.Contains(string(updatedVaultNote), "repo_anchors:") || !strings.Contains(string(updatedVaultNote), "anchor:repo:invariant/no-send-without-read") {
		t.Fatalf("expected vault note repo_anchors patch:\n%s", string(updatedVaultNote))
	}
	if !strings.Contains(string(updatedVaultNote), "repo_symbols:") || !strings.Contains(string(updatedVaultNote), "internal/context/contextplane.WorkspaceStore") {
		t.Fatalf("expected vault note repo_symbols patch:\n%s", string(updatedVaultNote))
	}
}

func TestApplyDocsBridgeDraftsReviewedOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "repo")
	vaultRoot := filepath.Join(root, "vault")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "docs", "general"), 0o755); err != nil {
		t.Fatalf("mkdir repo docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "repo", "foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir vault notes: %v", err)
	}
	draftFolder := filepath.Join(vaultRoot, "inbox", "drafted-from-foxctl", "docs-bridge", "foxctl")
	if err := os.MkdirAll(draftFolder, 0o755); err != nil {
		t.Fatalf("mkdir draft folder: %v", err)
	}

	writePair := func(docName, noteName, draftName, status string) {
		t.Helper()
		repoDocPath := filepath.Join(workspaceRoot, "docs", "general", docName)
		if err := os.WriteFile(repoDocPath, []byte("# Doc\n"), 0o644); err != nil {
			t.Fatalf("write repo doc %s: %v", docName, err)
		}
		vaultNotePath := filepath.Join(vaultRoot, "notes", "repo", "foxctl", noteName)
		vaultBody := `---
title: note
type: map
status: reviewed
trust: canonical
---

# note
`
		if err := os.WriteFile(vaultNotePath, []byte(vaultBody), 0o644); err != nil {
			t.Fatalf("write vault note %s: %v", noteName, err)
		}
		draftBody := `---
title: Bridge
type: map
project: foxctl
status: ` + status + `
trust: raw
repo_docs:
  - docs/general/` + docName + `
suggested_vault_refs:
  - notes/repo/foxctl/` + noteName + `
updated: 2026-03-11
---

# Bridge
`
		if err := os.WriteFile(filepath.Join(draftFolder, draftName), []byte(draftBody), 0o644); err != nil {
			t.Fatalf("write draft %s: %v", draftName, err)
		}
	}

	writePair("memory.md", "semantic-and-memory.md", "memory.md", "reviewed")
	writePair("storage.md", "platform-and-web.md", "storage.md", "draft")

	writer := NewWriter("", "TestVault", DefaultPolicy())
	writer.VaultPath = vaultRoot
	writer.PostCreateDelay = 0

	result, err := ApplyDocsBridgeDrafts(ctx, writer, DocsBridgeBatchApplyOptions{
		Project:       "foxctl",
		WorkspaceRoot: workspaceRoot,
		RequireStatus: "reviewed",
	})
	if err != nil {
		t.Fatalf("ApplyDocsBridgeDrafts: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("expected one applied draft, got %#v", result)
	}
	if len(result.Skipped) == 0 {
		t.Fatalf("expected skipped raw draft, got %#v", result)
	}
	if result.Applied[0].RepoDocPath != "docs/general/memory.md" {
		t.Fatalf("unexpected applied doc path: %#v", result.Applied[0])
	}
}

func TestReportDocsBridgeDrafts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "repo")
	vaultRoot := filepath.Join(root, "vault")
	draftFolder := filepath.Join(vaultRoot, "inbox", "drafted-from-foxctl", "docs-bridge", "foxctl")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "docs", "general"), 0o755); err != nil {
		t.Fatalf("mkdir repo docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "repo", "foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir vault notes: %v", err)
	}
	if err := os.MkdirAll(draftFolder, 0o755); err != nil {
		t.Fatalf("mkdir draft folder: %v", err)
	}

	writeDoc := func(name string, refs []string) {
		t.Helper()
		var body strings.Builder
		if len(refs) > 0 {
			body.WriteString("---\n")
			body.WriteString("vault_refs:\n")
			for _, ref := range refs {
				body.WriteString("  - " + ref + "\n")
			}
			body.WriteString("---\n\n")
		}
		body.WriteString("# Doc\n")
		if err := os.WriteFile(filepath.Join(workspaceRoot, "docs", "general", name), []byte(body.String()), 0o644); err != nil {
			t.Fatalf("write repo doc %s: %v", name, err)
		}
	}
	writeVault := func(name string, repoDocs []string) {
		t.Helper()
		var body strings.Builder
		body.WriteString("---\n")
		body.WriteString("title: note\n")
		body.WriteString("type: map\n")
		body.WriteString("status: reviewed\n")
		body.WriteString("trust: canonical\n")
		if len(repoDocs) > 0 {
			body.WriteString("repo_docs:\n")
			for _, ref := range repoDocs {
				body.WriteString("  - " + ref + "\n")
			}
		}
		body.WriteString("---\n\n# note\n")
		if err := os.WriteFile(filepath.Join(vaultRoot, "notes", "repo", "foxctl", name), []byte(body.String()), 0o644); err != nil {
			t.Fatalf("write vault note %s: %v", name, err)
		}
	}
	writeDraft := func(name, status string, suggested []string) {
		t.Helper()
		var body strings.Builder
		body.WriteString("---\n")
		body.WriteString("title: Bridge\n")
		body.WriteString("type: map\n")
		body.WriteString("project: foxctl\n")
		body.WriteString("status: " + status + "\n")
		body.WriteString("trust: raw\n")
		body.WriteString("repo_docs:\n")
		body.WriteString("  - docs/general/" + name + "\n")
		body.WriteString("suggested_vault_refs:\n")
		for _, ref := range suggested {
			body.WriteString("  - " + ref + "\n")
		}
		body.WriteString("updated: 2026-03-11\n")
		body.WriteString("---\n\n# Bridge\n")
		draftName := safeSlug("docs/general/"+name) + ".md"
		if err := os.WriteFile(filepath.Join(draftFolder, draftName), []byte(body.String()), 0o644); err != nil {
			t.Fatalf("write draft %s: %v", name, err)
		}
	}

	writeDoc("draft.md", nil)
	writeDoc("reviewed.md", nil)
	writeDoc("partial.md", []string{"notes/repo/foxctl/partial.md"})
	writeDoc("applied.md", []string{"notes/repo/foxctl/applied.md"})

	writeVault("partial.md", nil)
	writeVault("applied.md", []string{"docs/general/applied.md"})

	writeDraft("draft.md", "draft", []string{"notes/repo/foxctl/partial.md"})
	writeDraft("reviewed.md", "reviewed", []string{"notes/repo/foxctl/partial.md"})
	writeDraft("partial.md", "reviewed", []string{"notes/repo/foxctl/partial.md"})
	writeDraft("applied.md", "reviewed", []string{"notes/repo/foxctl/applied.md"})

	writer := NewWriter("", "TestVault", DefaultPolicy())
	writer.VaultPath = vaultRoot
	writer.PostCreateDelay = 0

	report, err := ReportDocsBridgeDrafts(ctx, writer, DocsBridgeReportOptions{
		Project:       "foxctl",
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("ReportDocsBridgeDrafts: %v", err)
	}
	if report.Total != 4 {
		t.Fatalf("expected 4 report entries, got %#v", report)
	}
	states := map[string]string{}
	for _, entry := range report.Entries {
		states[entry.DocPath] = entry.State
	}
	if states["docs/general/draft.md"] != "draft" {
		t.Fatalf("expected draft state, got %#v", states)
	}
	if states["docs/general/reviewed.md"] != "reviewed" {
		t.Fatalf("expected reviewed state, got %#v", states)
	}
	if states["docs/general/partial.md"] != "partial" {
		t.Fatalf("expected partial state, got %#v", states)
	}
	if states["docs/general/applied.md"] != "applied" {
		t.Fatalf("expected applied state, got %#v", states)
	}
}

func TestTidyDocsBridgeDrafts(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "repo")
	vaultRoot := filepath.Join(root, "vault")
	draftFolder := filepath.Join(vaultRoot, "inbox", "drafted-from-foxctl", "docs-bridge", "foxctl")
	if err := os.MkdirAll(filepath.Join(workspaceRoot, "docs", "general"), 0o755); err != nil {
		t.Fatalf("mkdir repo docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultRoot, "notes", "repo", "foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir vault notes: %v", err)
	}
	if err := os.MkdirAll(draftFolder, 0o755); err != nil {
		t.Fatalf("mkdir draft folder: %v", err)
	}

	repoDocPath := filepath.Join(workspaceRoot, "docs", "general", "applied.md")
	repoDocBody := `---
vault_refs:
  - notes/repo/foxctl/applied.md
---

# Applied
`
	if err := os.WriteFile(repoDocPath, []byte(repoDocBody), 0o644); err != nil {
		t.Fatalf("write repo doc: %v", err)
	}

	vaultNotePath := filepath.Join(vaultRoot, "notes", "repo", "foxctl", "applied.md")
	vaultNoteBody := `---
title: applied
type: map
status: reviewed
trust: canonical
repo_docs:
  - docs/general/applied.md
---

# applied
`
	if err := os.WriteFile(vaultNotePath, []byte(vaultNoteBody), 0o644); err != nil {
		t.Fatalf("write vault note: %v", err)
	}

	draftName := safeSlug("docs/general/applied.md") + ".md"
	draftPath := filepath.Join(draftFolder, draftName)
	draftBody := `---
title: Applied Bridge
type: map
project: foxctl
status: reviewed
trust: reviewed
repo_docs:
  - docs/general/applied.md
suggested_vault_refs:
  - notes/repo/foxctl/applied.md
updated: 2026-03-11
---

# Applied Bridge
`
	if err := os.WriteFile(draftPath, []byte(draftBody), 0o644); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	writer := NewWriter("", "TestVault", DefaultPolicy())
	writer.VaultPath = vaultRoot
	writer.PostCreateDelay = 0

	result, err := TidyDocsBridgeDrafts(ctx, writer, DocsBridgeTidyOptions{
		Project:       "foxctl",
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("TidyDocsBridgeDrafts: %v", err)
	}
	if len(result.Archived) != 1 {
		t.Fatalf("expected one archived draft, got %#v", result)
	}
	archivePath := filepath.Join(vaultRoot, "ops", "docs-bridge-applied", "foxctl", draftName)
	body, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read archive note: %v", err)
	}
	if !strings.Contains(string(body), "status: applied") {
		t.Fatalf("expected archived note status applied:\n%s", string(body))
	}
	if _, err := os.Stat(draftPath); err == nil {
		t.Fatalf("expected draft to be removed from inbox")
	}
}
