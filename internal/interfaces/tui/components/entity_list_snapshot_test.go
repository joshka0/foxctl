package components

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// TestSnapshotEntityListFocused captures a focused EntityList snapshot for
// tuistory evidence. The snapshot is written to the testdata directory.
func TestSnapshotEntityListFocused(t *testing.T) {
	items := []Entity{
		{ID: "a1", Label: "agent-abc12345", SubLabel: "researcher"},
		{ID: "a2", Label: "agent-def67890", SubLabel: "coder"},
		{ID: "a3", Label: "agent-ghi11111", SubLabel: "planner"},
		{ID: "a4", Label: "agent-jkl22222", SubLabel: "reviewer"},
		{ID: "a5", Label: "agent-mno33333", SubLabel: "overseer"},
	}

	mt := renderEntityList(
		items, 50, 10,
		WithSelectedIndex(2),
		WithFocused(true),
	)

	snapshot := mt.StringTrimmed()

	// Write snapshot to testdata.
	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "entitylist-focused.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Focused snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestSnapshotEntityListUnfocused captures an unfocused EntityList snapshot
// for tuistory evidence.
func TestSnapshotEntityListUnfocused(t *testing.T) {
	items := []Entity{
		{ID: "a1", Label: "agent-abc12345", SubLabel: "researcher"},
		{ID: "a2", Label: "agent-def67890", SubLabel: "coder"},
		{ID: "a3", Label: "agent-ghi11111", SubLabel: "planner"},
		{ID: "a4", Label: "agent-jkl22222", SubLabel: "reviewer"},
		{ID: "a5", Label: "agent-mno33333", SubLabel: "overseer"},
	}

	mt := renderEntityList(
		items, 50, 10,
		WithSelectedIndex(2),
		WithFocused(false),
	)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "entitylist-unfocused.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Unfocused snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestSnapshotEntityListEmpty captures an empty EntityList snapshot.
func TestSnapshotEntityListEmpty(t *testing.T) {
	mt := renderEntityList(
		nil, 50, 10,
		WithFocused(true),
		WithEmptyMessage("No agents running."),
	)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "entitylist-empty.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Empty snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestFocusedVsUnfocusedDistinguishable verifies that the focused and
// unfocused states produce different raw cell buffer output, confirming
// the focus indicator is visually distinct.
func TestFocusedVsUnfocusedDistinguishable(t *testing.T) {
	items := []Entity{
		{ID: "a1", Label: "agent-abc12345", SubLabel: "researcher"},
		{ID: "a2", Label: "agent-def67890", SubLabel: "coder"},
		{ID: "a3", Label: "agent-ghi11111", SubLabel: "planner"},
	}

	mtFocused := renderEntityList(
		items, 50, 5,
		WithSelectedIndex(1),
		WithFocused(true),
	)

	mtUnfocused := renderEntityList(
		items, 50, 5,
		WithSelectedIndex(1),
		WithFocused(false),
	)

	// The raw cell buffer must differ between focused and unfocused.
	focusedCell := mtFocused.CellAt(0, 1)     // focus indicator column, selected row
	unfocusedCell := mtUnfocused.CellAt(0, 1) // same position, unfocused

	if focusedCell.Equal(unfocusedCell) {
		t.Errorf("focused and unfocused CellAt(0,1) are identical — focus indicator is not distinct")
	}

	// Specifically, the focused cell should have BorderFocus fg.
	if !focusedCell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("focused CellAt(0,1) fg: want BorderFocus, got %v", focusedCell.Style.Fg)
	}

	// The unfocused cell should NOT have BorderFocus fg.
	if unfocusedCell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("unfocused CellAt(0,1) should NOT have BorderFocus fg")
	}
}
