package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
	"github.com/joshka0/foxctl/internal/runtime/memoryblur"
	"github.com/joshka0/foxctl/internal/storage/obsidianindex"
)

func TestDreamSourceRootsUsesExplicitProviders(t *testing.T) {
	flags := dreamCommandFlags{
		Workspace:  "/repo",
		CodexHome:  "~/.codex",
		ClaudeDir:  "~/.claude/projects",
		PiRoot:     "/tmp/pi",
		HermesRoot: "/tmp/hermes",
	}

	got := dreamSourceRoots(flags)
	if len(got) != 4 {
		t.Fatalf("roots=%d want 4", len(got))
	}
	wantProviders := []transcriptpipeline.DreamSourceProvider{
		transcriptpipeline.DreamSourceProviderCodex,
		transcriptpipeline.DreamSourceProviderClaude,
		transcriptpipeline.DreamSourceProviderPi,
		transcriptpipeline.DreamSourceProviderHermes,
	}
	for idx, want := range wantProviders {
		if got[idx].Provider != want {
			t.Fatalf("roots[%d].Provider=%q want %q", idx, got[idx].Provider, want)
		}
		if got[idx].WorkspaceHint != "/repo" {
			t.Fatalf("roots[%d].WorkspaceHint=%q want /repo", idx, got[idx].WorkspaceHint)
		}
	}
}

func TestPreviewDreamSourcesDoesNotConsumeLedgerWork(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex")
	transcriptPath := filepath.Join(codexHome, "sessions", "2026", "05", "25", "dream-session.jsonl")
	writeDreamCommandFile(t, transcriptPath, "{}\n")

	report, err := previewDreamSources(ctx, dreamCommandFlags{
		CodexHome:   codexHome,
		ClaudeDir:   filepath.Join(root, "missing-claude"),
		BatchSize:   5,
		Concurrency: 1,
		MaxAttempts: 2,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("previewDreamSources() error = %v", err)
	}
	if report.Discovered != 2 || report.Queued != 1 || report.Skipped != 1 || report.Processed != 0 || report.Failed != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestDreamScanCommandWritesPreviewEnvelope(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex")
	transcriptPath := filepath.Join(codexHome, "sessions", "2026", "05", "25", "dream-session.jsonl")
	writeDreamCommandFile(t, transcriptPath, "{}\n")

	cmd := newDreamScanCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{
		"--codex-home", codexHome,
		"--claude-dir", filepath.Join(root, "missing-claude"),
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("dream scan execute error = %v", err)
	}
	var envelope struct {
		Status  string `json:"status"`
		Command string `json:"command"`
		Data    struct {
			Discovered float64 `json:"discovered"`
			Queued     float64 `json:"queued"`
			Skipped    float64 `json:"skipped"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope %q: %v", out.String(), err)
	}
	if envelope.Status != "ok" || envelope.Command != "foxctl.dream.scan" {
		t.Fatalf("envelope=%+v", envelope)
	}
	if envelope.Data.Discovered != 2 || envelope.Data.Queued != 1 || envelope.Data.Skipped != 1 {
		t.Fatalf("data=%+v", envelope.Data)
	}
}

func TestDreamBlurAgentForFlagsIsOptIn(t *testing.T) {
	agent, name, err := dreamBlurAgentForFlags(defaultDreamCommandFlags())
	if err != nil {
		t.Fatalf("dreamBlurAgentForFlags() error = %v", err)
	}
	if agent != nil || name != "" {
		t.Fatalf("agent=%T name=%q; dream blur should be disabled by default", agent, name)
	}
}

func TestDreamBlurAgentForFlagsBuildsCommandBackend(t *testing.T) {
	flags := defaultDreamCommandFlags()
	flags.BlurDreams = true
	flags.BlurAgent = memoryblur.BackendCommand
	flags.BlurAgentCommand = `["/bin/cat"]`

	agent, name, err := dreamBlurAgentForFlags(flags)
	if err != nil {
		t.Fatalf("dreamBlurAgentForFlags() error = %v", err)
	}
	if agent == nil || name != memoryblur.BackendCommand {
		t.Fatalf("agent=%T name=%q", agent, name)
	}
}

func TestObsidianDreamNoteIndexerIndexesDreamSearchPath(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	vaultRoot := filepath.Join(root, "vault")
	storageRoot := filepath.Join(root, "storage")
	notePath := "inbox/drafted-from-foxctl/dreams/test.md"
	writeDreamCommandFile(t, filepath.Join(vaultRoot, notePath), `---
title: "Bounded Actor Dream"
type: "transcript_dream"
tags:
  - foxctl/dream
  - foxctl/agent-blurred
---

# Bounded Actor Dream

A bounded actor accepts one scoped work item and publishes a blurred mechanism for later collision.
`)

	store, err := obsidianindex.Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("obsidianindex.Open() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	indexer := &obsidianDreamNoteIndexer{store: store, vaultPath: vaultRoot}
	if err := indexer.IndexDreamNote(ctx, contextplane.TranscriptDreamNote{DraftPath: notePath}); err != nil {
		t.Fatalf("IndexDreamNote() error = %v", err)
	}
	hits, err := store.SearchDreams(ctx, "bounded actor mechanism", obsidianindex.DreamSearchOptions{
		Limit:       5,
		BlurredOnly: true,
	})
	if err != nil {
		t.Fatalf("SearchDreams() error = %v", err)
	}
	if len(hits) != 1 || hits[0].Path != notePath {
		t.Fatalf("hits=%+v", hits)
	}
}

func writeDreamCommandFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
