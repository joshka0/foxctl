package transcriptpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAndParseTranscript_RejectsInvalidProvider(t *testing.T) {
	_, err := ResolveAndParseTranscript("bogus", "", "", "", "actor:system:test")
	if err == nil {
		t.Fatal("expected error for invalid provider")
	}
	if !strings.Contains(err.Error(), "--provider must be one of") {
		t.Fatalf("err=%v want provider validation", err)
	}
}

func TestResolveAndParseTranscript_RequiresResolvableSource(t *testing.T) {
	_, err := ResolveAndParseTranscript("claude", "", "definitely-missing-test-session-id", t.TempDir(), "actor:system:test")
	if err == nil {
		t.Fatal("expected error when source session cannot be resolved")
	}
	if !strings.Contains(err.Error(), "source session JSONL could not be resolved") {
		t.Fatalf("err=%v want unresolved source error", err)
	}
}

func TestResolveAndParseTranscript_BackfillsCodexWorkspaceFromSessionMeta(t *testing.T) {
	t.Setenv("FOXCTL_WORKSPACE", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-2026-03-29T00-00-00-00000000-0000-0000-0000-000000000000.jsonl")
	body := `{"timestamp":"2026-03-29T00:00:00Z","type":"session_meta","payload":{"id":"sess-1","timestamp":"2026-03-29T00:00:00Z","cwd":"/Users/joshka/repos/personal/praze","originator":"codex_cli_rs"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	got, err := ResolveAndParseTranscript("codex", path, "", "", "actor:system:test")
	if err != nil {
		t.Fatalf("ResolveAndParseTranscript() error = %v", err)
	}
	if got.WorkspacePath != "/Users/joshka/repos/personal/praze" {
		t.Fatalf("workspace_path=%q want /Users/joshka/repos/personal/praze", got.WorkspacePath)
	}
}
