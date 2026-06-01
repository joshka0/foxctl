package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

func TestCodeDiffBasic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	oldContent := "line 1\nline 2\nline 3\n"
	newContent := "line 1\nline 2 modified\nline 3\nline 4\n"

	oldFile := filepath.Join(work, "old.txt")
	newFile := filepath.Join(work, "new.txt")

	if err := os.WriteFile(oldFile, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newFile, []byte(newContent), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		OldPath:      oldFile,
		NewPath:      newFile,
		ContextLines: 3,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	// Just verify we have data
	if env["data"] == nil {
		t.Fatalf("expected data in response")
	}
}

func TestCodeDiffIdentical(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := "line 1\nline 2\n"
	file1 := filepath.Join(work, "file1.txt")
	file2 := filepath.Join(work, "file2.txt")

	if err := os.WriteFile(file1, []byte(content), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(content), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		OldPath: file1,
		NewPath: file2,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	// Just verify we have data
	if env["data"] == nil {
		t.Fatalf("expected data in response")
	}
}

func TestComputeStatsSimilarityNeverNegativeForUnbalancedDiff(t *testing.T) {
	oldLines := []string{"old"}
	newLines := []string{
		"new-001",
		"new-002",
		"new-003",
		"new-004",
		"new-005",
		"new-006",
		"new-007",
		"new-008",
		"new-009",
		"new-010",
	}

	stats := computeStats(computeDiff(oldLines, newLines, 0), len(oldLines), len(newLines))
	if stats.Similarity < 0 || stats.Similarity > 100 {
		t.Fatalf("similarity = %f, want bounded 0..100", stats.Similarity)
	}
	if stats.Similarity != 0 {
		t.Fatalf("similarity for fully replaced unbalanced files = %f, want 0", stats.Similarity)
	}
}

func TestComputeStatsGeneratedInvariants(t *testing.T) {
	prop := func(oldRaw []string, newRaw []string, context uint8) bool {
		oldLines := boundedLines(oldRaw)
		newLines := boundedLines(newRaw)
		hunks := computeDiff(oldLines, newLines, int(context%5))
		stats := computeStats(hunks, len(oldLines), len(newLines))
		if stats.LinesAdded < 0 || stats.LinesRemoved < 0 || stats.LinesChanged < 0 || stats.TotalChanges < 0 {
			return false
		}
		if stats.TotalChanges != stats.LinesAdded+stats.LinesRemoved {
			return false
		}
		if stats.LinesChanged != min(stats.LinesAdded, stats.LinesRemoved) {
			return false
		}
		if stats.Similarity < 0 || stats.Similarity > 100 {
			return false
		}
		if equalLines(oldLines, newLines) {
			return stats.TotalChanges == 0 && stats.Similarity == 100
		}
		return true
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeWhitespaceGeneratedIdempotent(t *testing.T) {
	prop := func(raw string) bool {
		once := normalizeWhitespace([]byte(raw))
		twice := normalizeWhitespace(once)
		return bytes.Equal(once, twice)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, _ string) *skillmain.RunContext {
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   filepath.Join(state, "cas"),
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}
	rc, err := skillmain.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func boundedLines(lines []string) []string {
	if len(lines) > 12 {
		lines = lines[:12]
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if len(line) > 64 {
			line = line[:64]
		}
		out[i] = line
	}
	return out
}

func equalLines(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
