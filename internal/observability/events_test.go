package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// init ensures tests don't pollute each other's state.
func init() {
	// Tests will use SetObsDirForTesting to set the dir
}

func TestHashQuestion(t *testing.T) {
	tests := []struct {
		question string
		wantLen  int
	}{
		{"How does login work?", 8},
		{"", 8},
		{"a", 8},
		{"very long question with many words that should still hash to 8 chars", 8},
	}

	for _, tt := range tests {
		got := HashQuestion(tt.question)
		if len(got) != tt.wantLen {
			t.Errorf("HashQuestion(%q) = %q (len %d), want len %d", tt.question, got, len(got), tt.wantLen)
		}
	}

	// Same input should produce same hash
	h1 := HashQuestion("test question")
	h2 := HashQuestion("test question")
	if h1 != h2 {
		t.Errorf("HashQuestion not deterministic: %q != %q", h1, h2)
	}

	// Different inputs should (very likely) produce different hashes
	h3 := HashQuestion("different question")
	if h1 == h3 {
		t.Errorf("HashQuestion collision: %q == %q", h1, h3)
	}
}

func TestWriteEvent_Disabled(t *testing.T) {
	// Set obsDir to empty to simulate disabled
	SetObsDirForTesting("")

	ctx := context.Background()
	err := WriteEvent(ctx, "test", map[string]string{"foo": "bar"})
	if err != nil {
		t.Errorf("WriteEvent should succeed (no-op) when disabled: %v", err)
	}
}

func TestWriteEvent_Enabled(t *testing.T) {
	// Set up temp observability dir
	tmpDir := t.TempDir()
	SetObsDirForTesting(tmpDir)

	ctx := context.Background()

	// Write first event
	ev1 := map[string]any{"ts": "2025-01-01T00:00:00Z", "msg": "event1"}
	if err := WriteEvent(ctx, "test_events", ev1); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	// Write second event
	ev2 := map[string]any{"ts": "2025-01-01T00:00:01Z", "msg": "event2"}
	if err := WriteEvent(ctx, "test_events", ev2); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	// Verify file exists and contains 2 lines
	filePath := filepath.Join(tmpDir, "events", "test_events.ndjson")
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := 0
	for scanner.Scan() {
		lines++
		var obj map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &obj); err != nil {
			t.Errorf("line %d: invalid JSON: %v", lines, err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan error: %v", err)
	}

	if lines != 2 {
		t.Errorf("expected 2 lines, got %d", lines)
	}
}

func TestWriteEvent_InvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	SetObsDirForTesting(tmpDir)

	ctx := context.Background()

	// These should silently ignore (no file created)
	// Testing invalid paths; errors are expected and ignored.
	_ = WriteEvent(ctx, "../escape", map[string]string{})             //nolint:errcheck
	_ = WriteEvent(ctx, "path/with/slashes", map[string]string{})     //nolint:errcheck
	_ = WriteEvent(ctx, `path\with\backslashes`, map[string]string{}) //nolint:errcheck

	// Verify no files created
	eventsDir := filepath.Join(tmpDir, "events")
	entries, err := os.ReadDir(eventsDir)
	if os.IsNotExist(err) {
		// Directory doesn't exist means no files were created - expected
		return
	}
	if err != nil {
		t.Fatalf("os.ReadDir(%q): %v", eventsDir, err)
	}
	if len(entries) > 0 {
		t.Errorf("expected no files for invalid names, got %d", len(entries))
	}
}

func TestSweGrepEvent(t *testing.T) {
	tmpDir := t.TempDir()
	SetObsDirForTesting(tmpDir)

	ctx := context.Background()

	ev := NewSweGrepEvent(
		"test-workspace",
		"How does login work?",
		3,    // candidates
		3,    // filesConsidered
		2,    // filesRelevant
		5,    // snippetsEmitted
		true, // hasArtifact
		187,  // durationMS
		"run",
	)

	if err := WriteSweGrepEvent(ctx, ev); err != nil {
		t.Fatalf("WriteSweGrepEvent failed: %v", err)
	}

	// Verify file contents
	filePath := filepath.Join(tmpDir, "events", "code_swe_grep.ndjson")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}

	var decoded SweGrepEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	// Verify fields
	if decoded.Command != "code/snippet_extract" {
		t.Errorf("Command = %q, want code/snippet_extract", decoded.Command)
	}
	if decoded.WorkspaceID != "test-workspace" {
		t.Errorf("WorkspaceID = %q, want test-workspace", decoded.WorkspaceID)
	}
	if len(decoded.QuestionHash) != 8 {
		t.Errorf("QuestionHash len = %d, want 8", len(decoded.QuestionHash))
	}
	if decoded.Candidates != 3 {
		t.Errorf("Candidates = %d, want 3", decoded.Candidates)
	}
	if decoded.FilesConsidered != 3 {
		t.Errorf("FilesConsidered = %d, want 3", decoded.FilesConsidered)
	}
	if decoded.FilesRelevant != 2 {
		t.Errorf("FilesRelevant = %d, want 2", decoded.FilesRelevant)
	}
	if decoded.SnippetsEmitted != 5 {
		t.Errorf("SnippetsEmitted = %d, want 5", decoded.SnippetsEmitted)
	}
	if !decoded.HasArtifact {
		t.Error("HasArtifact = false, want true")
	}
	if decoded.DurationMS != 187 {
		t.Errorf("DurationMS = %d, want 187", decoded.DurationMS)
	}
	if decoded.Source != "run" {
		t.Errorf("Source = %q, want run", decoded.Source)
	}
	if decoded.Ts.IsZero() {
		t.Error("Ts is zero")
	}
	if decoded.Ts.After(time.Now().Add(time.Minute)) {
		t.Error("Ts is in the future")
	}
}
