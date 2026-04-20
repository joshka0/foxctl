package components

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// TestSnapshotTabsActiveFocused captures a focused Tabs snapshot with
// visible active indicator (colored underline + Accent label).
func TestSnapshotTabsActiveFocused(t *testing.T) {
	labels := []string{"Agents", "Rooms", "Events"}

	mt := tui.NewMockTerminal(50, 2)
	buf := tui.NewBuffer(50, 2)
	tabs := NewTabs(labels, 50, WithTabsActiveIndex(0), WithTabsFocused(true))
	tabs.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "tabs-active-focused.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Focused snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestSnapshotTabsActiveUnfocused captures an unfocused Tabs snapshot.
// The active tab still has an indicator (dimmer accent underline) but
// less prominent than focused state.
func TestSnapshotTabsActiveUnfocused(t *testing.T) {
	labels := []string{"Agents", "Rooms", "Events"}

	mt := tui.NewMockTerminal(50, 2)
	buf := tui.NewBuffer(50, 2)
	tabs := NewTabs(labels, 50, WithTabsActiveIndex(0), WithTabsFocused(false))
	tabs.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "tabs-active-unfocused.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Unfocused snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestSnapshotTabsMiddleActive captures a snapshot with middle tab active.
func TestSnapshotTabsMiddleActive(t *testing.T) {
	labels := []string{"Agents", "Rooms", "Events"}

	mt := tui.NewMockTerminal(50, 2)
	buf := tui.NewBuffer(50, 2)
	tabs := NewTabs(labels, 50, WithTabsActiveIndex(1), WithTabsFocused(true))
	tabs.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()

	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "tabs-middle-active.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("Middle active snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)
}

// TestFocusedVsUnfocusedTabsDistinguishable verifies that the focused and
// unfocused states produce different raw cell buffer output, confirming
// the active indicator is visually distinct.
func TestFocusedVsUnfocusedTabsDistinguishable(t *testing.T) {
	labels := []string{"Agents", "Rooms", "Events"}

	// Render focused.
	mtF := tui.NewMockTerminal(50, 2)
	bufF := tui.NewBuffer(50, 2)
	tabsF := NewTabs(labels, 50, WithTabsActiveIndex(0), WithTabsFocused(true))
	tabsF.Render(bufF)
	tui.RenderFull(mtF, bufF)

	// Render unfocused.
	mtU := tui.NewMockTerminal(50, 2)
	bufU := tui.NewBuffer(50, 2)
	tabsU := NewTabs(labels, 50, WithTabsActiveIndex(0), WithTabsFocused(false))
	tabsU.Render(bufU)
	tui.RenderFull(mtU, bufU)

	// Find the underline row (y=1) under the active tab.
	// The focused underline should use Accent color; unfocused should use AccentMuted.
	var focusedUnderlineAccent bool
	var unfocusedUnderlineAccentMuted bool

	for x := 0; x < 50; x++ {
		cellF := mtF.CellAt(x, 1)
		cellU := mtU.CellAt(x, 1)

		if cellF.Rune == '━' && cellF.Style.Fg.Equal(theme.Colors.Accent) {
			focusedUnderlineAccent = true
		}
		if cellU.Rune == '━' && cellU.Style.Fg.Equal(theme.Colors.AccentMuted) {
			unfocusedUnderlineAccentMuted = true
		}
	}

	if !focusedUnderlineAccent {
		t.Error("focused tabs: expected underline with Accent fg")
	}
	if !unfocusedUnderlineAccentMuted {
		t.Error("unfocused tabs: expected underline with AccentMuted fg")
	}

	// The underline colors should differ between focused and unfocused.
	for x := 0; x < 50; x++ {
		cellF := mtF.CellAt(x, 1)
		cellU := mtU.CellAt(x, 1)
		if cellF.Rune == '━' && cellU.Rune == '━' {
			if cellF.Style.Fg.Equal(cellU.Style.Fg) {
				t.Errorf("at x=%d: focused and unfocused underline have same fg color", x)
			}
			break
		}
	}
}

// TestActiveVsInactiveTabColorsDiffer verifies that active and inactive
// tab labels have distinct foreground colors in the raw cell buffer.
func TestActiveVsInactiveTabColorsDiffer(t *testing.T) {
	labels := []string{"AAA", "BBB"}

	mt := tui.NewMockTerminal(30, 2)
	buf := tui.NewBuffer(30, 2)
	tabs := NewTabs(labels, 30, WithTabsActiveIndex(0), WithTabsFocused(true))
	tabs.Render(buf)
	tui.RenderFull(mt, buf)

	// Find first 'A' and first 'B' — they should have different fg colors.
	var activeFg, inactiveFg tui.Color
	for x := 0; x < 30; x++ {
		cell := mt.CellAt(x, 0)
		if cell.Rune == 'A' && activeFg.IsDefault() {
			activeFg = cell.Style.Fg
		}
		if cell.Rune == 'B' && inactiveFg.IsDefault() {
			inactiveFg = cell.Style.Fg
		}
	}

	if activeFg.IsDefault() {
		t.Fatal("did not find active tab label cell 'A'")
	}
	if inactiveFg.IsDefault() {
		t.Fatal("did not find inactive tab label cell 'B'")
	}

	if activeFg.Equal(inactiveFg) {
		t.Errorf("active and inactive tab labels have same fg color (%v); they must differ", activeFg)
	}

	// Active should be Accent, inactive should be TextMuted.
	if !activeFg.Equal(theme.Colors.Accent) {
		t.Errorf("active tab fg: want Accent, got %v", activeFg)
	}
	if !inactiveFg.Equal(theme.Colors.TextMuted) {
		t.Errorf("inactive tab fg: want TextMuted, got %v", inactiveFg)
	}
}
