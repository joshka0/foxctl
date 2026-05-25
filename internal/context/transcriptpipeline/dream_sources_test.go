package transcriptpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDiscoverDreamSourceCandidates_CodexStableFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "2026", "05", "25", "test-session.jsonl")
	writeDreamTestFile(t, path, `{"type":"session_meta","payload":{"id":"test-session","cwd":"/repo"}}`+"\n")
	mtime := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	got := DiscoverDreamSourceCandidates([]DreamSourceRoot{{
		Provider:      DreamSourceProviderCodex,
		RootPath:      root,
		WorkspaceHint: "/repo",
	}})

	if len(got) != 1 {
		t.Fatalf("len(candidates)=%d want 1: %#v", len(got), got)
	}
	candidate := got[0]
	if candidate.Provider != DreamSourceProviderCodex {
		t.Fatalf("provider=%q want codex", candidate.Provider)
	}
	if candidate.Path != path {
		t.Fatalf("path=%q want %q", candidate.Path, path)
	}
	if candidate.SessionID != "test-session" {
		t.Fatalf("session_id=%q want test-session", candidate.SessionID)
	}
	if candidate.WorkspaceHint != "/repo" || candidate.WorkspacePath != "/repo" {
		t.Fatalf("workspace hint/path=%q/%q want /repo", candidate.WorkspaceHint, candidate.WorkspacePath)
	}
	if candidate.StabilityStatus != DreamSourceStable {
		t.Fatalf("stability=%q want stable", candidate.StabilityStatus)
	}
	if candidate.Size <= 0 {
		t.Fatalf("size=%d want positive", candidate.Size)
	}
	if !candidate.ModTime.Equal(mtime) {
		t.Fatalf("mtime=%s want %s", candidate.ModTime, mtime)
	}
	wantDigest := "sha256:" + sha256Hex([]byte(`{"type":"session_meta","payload":{"id":"test-session","cwd":"/repo"}}`+"\n"))
	if candidate.Digest != wantDigest {
		t.Fatalf("digest=%q want %q", candidate.Digest, wantDigest)
	}
	if candidate.Fingerprint == "" || candidate.Fingerprint == candidate.Digest {
		t.Fatalf("fingerprint=%q should be populated independently from digest %q", candidate.Fingerprint, candidate.Digest)
	}
}

func TestDiscoverDreamSourceCandidates_InvalidRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	got := DiscoverDreamSourceCandidates([]DreamSourceRoot{{
		Provider:      DreamSourceProviderClaude,
		RootPath:      missing,
		WorkspaceHint: "/repo",
	}})

	if len(got) != 1 {
		t.Fatalf("len(candidates)=%d want 1", len(got))
	}
	if got[0].StabilityStatus != DreamSourceInvalidRoot {
		t.Fatalf("stability=%q want invalid_root", got[0].StabilityStatus)
	}
	if got[0].Root.Provider != DreamSourceProviderClaude || got[0].Root.RootPath != missing {
		t.Fatalf("root=%#v does not preserve configured root", got[0].Root)
	}
	if got[0].Path != "" || got[0].Digest != "" || got[0].Fingerprint != "" {
		t.Fatalf("invalid root should not have file identity: %#v", got[0])
	}
}

func TestDiscoverDreamSourceCandidates_DeterministicOrdering(t *testing.T) {
	codexRoot := t.TempDir()
	claudeRoot := t.TempDir()
	writeDreamTestFile(t, filepath.Join(codexRoot, "sessions", "2026", "05", "25", "b-session.jsonl"), "{}\n")
	writeDreamTestFile(t, filepath.Join(codexRoot, "sessions", "2026", "05", "25", "a-session.jsonl"), "{}\n")
	writeDreamTestFile(t, filepath.Join(claudeRoot, "z", "claude-z.jsonl"), "{}\n")
	writeDreamTestFile(t, filepath.Join(claudeRoot, "a", "claude-a.jsonl"), "{}\n")

	got := DiscoverDreamSourceCandidates([]DreamSourceRoot{
		{Provider: DreamSourceProviderCodex, RootPath: codexRoot},
		{Provider: DreamSourceProviderClaude, RootPath: claudeRoot},
	})

	var order []string
	for _, candidate := range got {
		order = append(order, string(candidate.Provider)+":"+candidate.SessionID)
	}
	want := []string{
		"claude:claude-a",
		"claude:claude-z",
		"codex:a-session",
		"codex:b-session",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order=%v want %v", order, want)
	}
}

func TestDiscoverDreamSourceCandidates_RootProviderShape(t *testing.T) {
	piRoot := t.TempDir()
	hermesRoot := t.TempDir()

	got := DiscoverDreamSourceCandidates([]DreamSourceRoot{
		{Provider: DreamSourceProviderPi, RootPath: piRoot, WorkspaceHint: "/pi/ws"},
		{Provider: DreamSourceProviderHermes, RootPath: hermesRoot, WorkspaceHint: "/hermes/ws"},
	})

	if len(got) != 2 {
		t.Fatalf("len(candidates)=%d want 2", len(got))
	}
	byProvider := map[DreamSourceProvider]DreamSourceCandidate{}
	for _, candidate := range got {
		byProvider[candidate.Provider] = candidate
	}
	for _, provider := range []DreamSourceProvider{DreamSourceProviderHermes, DreamSourceProviderPi} {
		candidate, ok := byProvider[provider]
		if !ok {
			t.Fatalf("missing provider %q in %#v", provider, got)
		}
		if candidate.StabilityStatus != DreamSourceRootOnly {
			t.Fatalf("%s stability=%q want root_only", provider, candidate.StabilityStatus)
		}
		if candidate.Root.Provider != provider || candidate.Root.RootPath == "" {
			t.Fatalf("%s root shape lost provider/path: %#v", provider, candidate.Root)
		}
		if candidate.Path != "" || candidate.SessionID != "" || candidate.Digest != "" || candidate.Fingerprint != "" {
			t.Fatalf("%s root-only source should not invent parser file fields: %#v", provider, candidate)
		}
	}
}

func TestDiscoverDreamSourceCandidates_FingerprintChangesWithContent(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "sessions", "2026", "05", "25", "rollout-first.jsonl")
	second := filepath.Join(root, "sessions", "2026", "05", "25", "rollout-second.jsonl")
	writeDreamTestFile(t, first, "first\n")
	writeDreamTestFile(t, second, "second\n")
	mtime := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	for _, path := range []string{first, second} {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	got := DiscoverDreamSourceCandidates([]DreamSourceRoot{{Provider: DreamSourceProviderCodex, RootPath: root}})

	if len(got) != 2 {
		t.Fatalf("len(candidates)=%d want 2", len(got))
	}
	if got[0].Digest == got[1].Digest {
		t.Fatalf("digests should differ for different content: %#v", got)
	}
	if got[0].Fingerprint == got[1].Fingerprint {
		t.Fatalf("fingerprints should differ for different content: %#v", got)
	}
}

func writeDreamTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
