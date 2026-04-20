package components

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// TestSnapshotStreamViewerFollowEngaged captures a snapshot of the
// StreamViewer with follow-tail engaged and the viewer at the bottom.
// The follow indicator (↓) should be visible and the last content line
// should be in view.
func TestSnapshotStreamViewerFollowEngaged(t *testing.T) {
	lines := make([]string, 15)
	for i := range lines {
		lines[i] = fmt.Sprintf("Stream line %d: some content here", i)
	}

	sv, mt := renderStreamViewer(lines, 50, 8,
		WithSVFocused(true),
	)

	// Verify follow is engaged and we're at the bottom.
	if !sv.FollowTail() {
		t.Fatal("expected follow-tail engaged")
	}
	wantOffset := 15 - 8 // 7
	if sv.ScrollOffset() != wantOffset {
		t.Fatalf("want scrollOffset=%d, got %d", wantOffset, sv.ScrollOffset())
	}

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "streamviewer-follow-engaged.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Follow-engaged snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)

	// Verify last line is visible.
	if !containsSubstring(snapshot, "Stream line 14") {
		t.Errorf("follow-engaged snapshot should contain last line. Output:\n%s", snapshot)
	}
}

// TestSnapshotStreamViewerScrolledUp captures a snapshot of the
// StreamViewer with follow-tail disengaged and scroll position preserved.
// The user has scrolled up, so the viewport shows earlier content while
// new items have been appended.
func TestSnapshotStreamViewerScrolledUp(t *testing.T) {
	// Start with 10 lines, viewer shows 8.
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = fmt.Sprintf("Original line %d", i)
	}

	sv := NewStreamViewer(lines, 50, 8,
		WithSVFocused(true),
	)

	// Scroll up 3 times to disengage follow and move the anchor.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyUp})

	if sv.FollowTail() {
		t.Fatal("expected follow-tail disengaged after scrolling up")
	}

	anchorBefore := sv.ScrollOffset()

	// Append more lines — anchor should be preserved.
	moreLines := make([]string, 5)
	for i := range moreLines {
		moreLines[i] = fmt.Sprintf("Appended line %d", i)
	}
	sv.SetLines(append(lines, moreLines...))

	if sv.ScrollOffset() != anchorBefore {
		t.Errorf("scroll anchor not preserved: want %d, got %d", anchorBefore, sv.ScrollOffset())
	}

	// Render the viewer.
	mt := tui.NewMockTerminal(50, 8)
	buf := tui.NewBuffer(50, 8)
	sv.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "streamviewer-scrolled-up.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Scrolled-up snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)

	// Verify the anchored content is still visible (not the new bottom).
	if !containsSubstring(snapshot, "Original line") {
		t.Errorf("scrolled-up snapshot should contain original anchored lines. Output:\n%s", snapshot)
	}
}

// TestStreamViewerFocusedVsUnfocusedBorder verifies the StreamViewer border
// color differs between focused and unfocused states (beyond font-weight).
func TestStreamViewerFocusedVsUnfocusedBorder(t *testing.T) {
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = fmt.Sprintf("Line %d", i)
	}

	_, mtF := renderStreamViewer(lines, 40, 5, WithSVFocused(true))
	_, mtU := renderStreamViewer(lines, 40, 5, WithSVFocused(false))

	focusedCell := mtF.CellAt(0, 0)
	unfocusedCell := mtU.CellAt(0, 0)

	if !focusedCell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("focused CellAt(0,0) fg: want BorderFocus, got %v", focusedCell.Style.Fg)
	}

	if unfocusedCell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("unfocused CellAt(0,0) should NOT have BorderFocus fg")
	}
}
