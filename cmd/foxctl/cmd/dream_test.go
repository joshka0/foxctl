package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/context/transcriptpipeline"
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

func writeDreamCommandFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
