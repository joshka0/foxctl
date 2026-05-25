package transcriptpipeline

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildDreamSourceCandidatesReturnsStableSortedCandidates(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	root := DreamSourceRoot{
		Provider:      DreamSourceProviderCodex,
		RootPath:      "/home/dev/.codex",
		WorkspacePath: "/repo/foxctl",
	}
	files := []DreamSourceFile{
		{
			Provider:   DreamSourceProviderCodex,
			RootPath:   "/home/dev/.codex",
			SourcePath: "/home/dev/.codex/sessions/2026/05/24/rollout-bbb.jsonl",
			Size:       20,
			ModTime:    now.Add(-2 * time.Hour),
		},
		{
			Provider:   DreamSourceProviderCodex,
			RootPath:   "/home/dev/.codex",
			SourcePath: "/home/dev/.codex/sessions/2026/05/24/rollout-aaa.jsonl",
			Size:       10,
			ModTime:    now.Add(-3 * time.Hour),
		},
	}

	got, err := BuildDreamSourceCandidates([]DreamSourceRoot{root}, files, now, time.Hour)
	if err != nil {
		t.Fatalf("BuildDreamSourceCandidates() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates=%d want 2", len(got))
	}
	if got[0].SessionID != "rollout-aaa" || got[1].SessionID != "rollout-bbb" {
		t.Fatalf("session order=%v want [rollout-aaa rollout-bbb]", []string{got[0].SessionID, got[1].SessionID})
	}
	for _, candidate := range got {
		if !candidate.Stable() {
			t.Fatalf("candidate %s stability=%q want stable", candidate.SourcePath, candidate.Stability)
		}
		if candidate.WorkspacePath != "/repo/foxctl" {
			t.Fatalf("workspace=%q want root workspace", candidate.WorkspacePath)
		}
		if !strings.HasPrefix(candidate.Fingerprint, "sha256:") {
			t.Fatalf("fingerprint=%q want sha256 prefix", candidate.Fingerprint)
		}
	}
}

func TestBuildDreamSourceCandidatesMarksUnstableFutureInvalidAndOutsideRoot(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	root := DreamSourceRoot{Provider: DreamSourceProviderClaude, RootPath: "/home/dev/.claude", WorkspacePath: "/repo/foxctl"}
	files := []DreamSourceFile{
		{
			Provider:   DreamSourceProviderClaude,
			RootPath:   root.RootPath,
			SourcePath: "/home/dev/.claude/projects/ws/changing.jsonl",
			Size:       10,
			ModTime:    now.Add(-10 * time.Second),
		},
		{
			Provider:   DreamSourceProviderClaude,
			RootPath:   root.RootPath,
			SourcePath: "/home/dev/.claude/projects/ws/future.jsonl",
			Size:       10,
			ModTime:    now.Add(time.Minute),
		},
		{
			Provider:   DreamSourceProviderClaude,
			RootPath:   root.RootPath,
			SourcePath: "/home/dev/.claude/projects/ws/invalid.jsonl",
			Size:       -1,
			ModTime:    now.Add(-time.Hour),
		},
		{
			Provider:   DreamSourceProviderClaude,
			RootPath:   root.RootPath,
			SourcePath: "/home/dev/elsewhere/outside.jsonl",
			Size:       10,
			ModTime:    now.Add(-time.Hour),
		},
	}

	got, err := BuildDreamSourceCandidates([]DreamSourceRoot{root}, files, now, time.Minute)
	if err != nil {
		t.Fatalf("BuildDreamSourceCandidates() error = %v", err)
	}
	stabilityBySession := map[string]DreamSourceStability{}
	for _, candidate := range got {
		stabilityBySession[candidate.SessionID] = candidate.Stability
	}
	want := map[string]DreamSourceStability{
		"changing": DreamSourceChanging,
		"future":   DreamSourceFutureMTime,
		"invalid":  DreamSourceInvalidStat,
		"outside":  DreamSourceOutsideRoot,
	}
	if !reflect.DeepEqual(stabilityBySession, want) {
		t.Fatalf("stability=%v want %v", stabilityBySession, want)
	}
}

func TestBuildDreamSourceCandidatesDedupeAndFingerprintReflectStatChanges(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	root := DreamSourceRoot{Provider: DreamSourceProviderPi, RootPath: "/home/dev/.pi/transcripts"}
	file := DreamSourceFile{
		Provider:   DreamSourceProviderPi,
		RootPath:   root.RootPath,
		SourcePath: "/home/dev/.pi/transcripts/session-1.jsonl",
		SessionID:  "session-1",
		Size:       10,
		ModTime:    now.Add(-time.Hour),
	}

	first, err := BuildDreamSourceCandidates([]DreamSourceRoot{root}, []DreamSourceFile{file, file}, now, time.Minute)
	if err != nil {
		t.Fatalf("BuildDreamSourceCandidates() error = %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("deduped candidates=%d want 1", len(first))
	}

	changed := file
	changed.Size = 11
	second, err := BuildDreamSourceCandidates([]DreamSourceRoot{root}, []DreamSourceFile{changed}, now, time.Minute)
	if err != nil {
		t.Fatalf("BuildDreamSourceCandidates() changed error = %v", err)
	}
	if first[0].Fingerprint == second[0].Fingerprint {
		t.Fatalf("fingerprint did not change after stat change: %s", first[0].Fingerprint)
	}
}

func TestBuildDreamSourceCandidatesRejectsInvalidRootsAndUnconfiguredFiles(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	_, err := BuildDreamSourceCandidates([]DreamSourceRoot{{Provider: "auto", RootPath: "/tmp"}}, nil, now, time.Minute)
	if !errors.Is(err, ErrDreamSourceInvalidRoot) {
		t.Fatalf("invalid root error=%v want ErrDreamSourceInvalidRoot", err)
	}

	_, err = BuildDreamSourceCandidates(
		[]DreamSourceRoot{{Provider: DreamSourceProviderHermes, RootPath: "/tmp/hermes"}},
		[]DreamSourceFile{{
			Provider:   DreamSourceProviderHermes,
			RootPath:   "/tmp/other",
			SourcePath: "/tmp/other/session.jsonl",
			Size:       1,
			ModTime:    now,
		}},
		now,
		time.Minute,
	)
	if !errors.Is(err, ErrDreamSourceInvalidFile) {
		t.Fatalf("unconfigured file error=%v want ErrDreamSourceInvalidFile", err)
	}
}

func TestBuildDreamSourceCandidatesSupportsConfiguredPiAndHermesRoots(t *testing.T) {
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	roots := []DreamSourceRoot{
		{Provider: DreamSourceProviderPi, RootPath: "/home/dev/.pi/transcripts"},
		{Provider: DreamSourceProviderHermes, RootPath: "/home/dev/.hermes/transcripts"},
	}
	files := []DreamSourceFile{
		{
			Provider:   DreamSourceProviderHermes,
			RootPath:   "/home/dev/.hermes/transcripts",
			SourcePath: "/home/dev/.hermes/transcripts/hermes-1.jsonl",
			Size:       1,
			ModTime:    now.Add(-time.Hour),
		},
		{
			Provider:   DreamSourceProviderPi,
			RootPath:   "/home/dev/.pi/transcripts",
			SourcePath: "/home/dev/.pi/transcripts/pi-1.jsonl",
			Size:       1,
			ModTime:    now.Add(-time.Hour),
		},
	}

	got, err := BuildDreamSourceCandidates(roots, files, now, time.Minute)
	if err != nil {
		t.Fatalf("BuildDreamSourceCandidates() error = %v", err)
	}
	if providers := []DreamSourceProvider{got[0].Provider, got[1].Provider}; !reflect.DeepEqual(providers, []DreamSourceProvider{DreamSourceProviderHermes, DreamSourceProviderPi}) {
		t.Fatalf("providers=%v want [hermes pi]", providers)
	}
	if got[0].SessionID != "hermes-1" || got[1].SessionID != "pi-1" {
		t.Fatalf("session IDs=%q/%q want hermes-1/pi-1", got[0].SessionID, got[1].SessionID)
	}
}

func TestCodexDreamSourceFilesUsesExistingSessionLocatorShape(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, ".codex")
	sessionDir := filepath.Join(root, "sessions", "2026", "05", "25")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(sessionDir, "rollout-codex-1.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write codex source: %v", err)
	}

	files, err := CodexDreamSourceFiles(DreamSourceRoot{
		Provider:      DreamSourceProviderCodex,
		RootPath:      root,
		WorkspacePath: "/repo/foxctl",
	})
	if err != nil {
		t.Fatalf("CodexDreamSourceFiles() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files=%d want 1", len(files))
	}
	if files[0].SessionID != "rollout-codex-1" || files[0].SourcePath != path {
		t.Fatalf("file=%+v want session/source from codex locator", files[0])
	}
}
