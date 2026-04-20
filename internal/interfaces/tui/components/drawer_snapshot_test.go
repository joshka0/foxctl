package components

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// TestSnapshotDrawerOpen captures a snapshot of the Drawer in its open state
// with content visible, title in header, and close hint shown.
func TestSnapshotDrawerOpen(t *testing.T) {
	content := []string{
		"Agent ID: abc-123",
		"Role: researcher",
		"Status: running",
		"Workspace: /home/user/project",
		"Last activity: 2m ago",
		"",
		"Raw payload:",
		`{"id":"abc-123","role":"researcher"}`,
	}

	mt := tui.NewMockTerminal(60, 15)
	buf := tui.NewBuffer(60, 15)
	d := NewDrawer("Agent Detail", content, 60, 15,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
		WithDrawerWidth(30),
	)
	d.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "drawer-open.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Open snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestSnapshotDrawerOpenScrolled captures a snapshot of the Drawer with
// content scrolled down.
func TestSnapshotDrawerOpenScrolled(t *testing.T) {
	content := make([]string, 20)
	for i := range content {
		content[i] = "Line of content that is fairly long to test scrolling"
	}

	mt := tui.NewMockTerminal(60, 10)
	buf := tui.NewBuffer(60, 10)
	d := NewDrawer("Evidence", content, 60, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
		WithDrawerWidth(30),
	)

	// Scroll down a few lines.
	for i := 0; i < 3; i++ {
		d.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	}

	d.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "drawer-open-scrolled.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Scrolled snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestDrawerFocusedVsUnfocusedBorder verifies the drawer border color
// differs between focused and unfocused states (beyond font-weight).
func TestDrawerFocusedVsUnfocusedBorder(t *testing.T) {
	content := []string{"Line 1", "Line 2"}

	// Focused drawer.
	mtF := tui.NewMockTerminal(40, 10)
	bufF := tui.NewBuffer(40, 10)
	dF := NewDrawer("Evidence", content, 40, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)
	dF.Render(bufF)
	tui.RenderFull(mtF, bufF)

	// Unfocused drawer.
	mtU := tui.NewMockTerminal(40, 10)
	bufU := tui.NewBuffer(40, 10)
	dU := NewDrawer("Evidence", content, 40, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(false),
	)
	dU.Render(bufU)
	tui.RenderFull(mtU, bufU)

	// The focused border should use BorderFocus; unfocused should use Border.
	var focusedBorderFocus bool
	var unfocusedBorderNotFocus bool
	startX := 40 - 30 // drawerWidth=30

	for y := 0; y < 10; y++ {
		cellF := mtF.CellAt(startX, y)
		cellU := mtU.CellAt(startX, y)

		if cellF.Rune == '│' && cellF.Style.Fg.Equal(theme.Colors.BorderFocus) {
			focusedBorderFocus = true
		}
		if cellU.Rune == '│' && !cellU.Style.Fg.Equal(theme.Colors.BorderFocus) {
			unfocusedBorderNotFocus = true
		}
	}

	if !focusedBorderFocus {
		t.Error("focused drawer should have BorderFocus color on left border")
	}
	if !unfocusedBorderNotFocus {
		t.Error("unfocused drawer should NOT have BorderFocus color on left border")
	}
}
