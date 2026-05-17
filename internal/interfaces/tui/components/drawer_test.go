package components

import (
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- Drawer test helpers ---

// renderDrawer is a test helper that creates a Drawer, renders it to
// a MockTerminal, and returns both for inspection.
func renderDrawer(title string, content []string, width, height int, opts ...DrawerOption) (*Drawer, *tui.MockTerminal) {
	mt := tui.NewMockTerminal(width, height)
	buf := tui.NewBuffer(width, height)
	d := NewDrawer(title, content, width, height, opts...)
	d.Render(buf)
	tui.RenderFull(mt, buf)
	return d, mt
}

// --- Tests: Open ---

func TestDrawerOpen(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1", "Line 2", "Line 3"}
	d, mt := renderDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// Verify drawer is open.
	if !d.IsOpen() {
		t.Fatal("drawer should be open")
	}

	// Verify content is rendered — check for "Line 1" somewhere in the buffer.
	found := false
	s := mt.StringTrimmed()
	for _, line := range content {
		if containsSubstring(s, line) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("open drawer should render content; got:\n%s", s)
	}

	// Verify title is rendered.
	if !containsSubstring(s, "Evidence") {
		t.Errorf("open drawer should render title; got:\n%s", s)
	}
}

func TestDrawerClosedRendersNothing(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1", "Line 2"}
	d, mt := renderDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(false),
	)

	if d.IsOpen() {
		t.Fatal("drawer should be closed")
	}

	// Closed drawer renders nothing — the entire terminal should be empty or
	// minimal (no content lines visible).
	s := mt.StringTrimmed()
	for _, line := range content {
		if containsSubstring(s, line) {
			t.Errorf("closed drawer should not render content; got:\n%s", s)
		}
	}
}

// --- Tests: Close via ESC ---

func TestDrawerCloseViaESC(t *testing.T) {
	t.Parallel()

	var closeCount int
	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
		WithDrawerOnClose(func() { closeCount++ }),
	)

	// Press ESC — should close the drawer.
	consumed := d.HandleKey(tui.KeyEvent{Key: tui.KeyEscape})
	if !consumed {
		t.Fatal("ESC on open drawer should be consumed")
	}
	if d.IsOpen() {
		t.Fatal("ESC should close the drawer")
	}
	if closeCount != 1 {
		t.Errorf("OnClose should fire exactly once; got %d fires", closeCount)
	}
}

func TestDrawerESCIgnoredWhenClosed(t *testing.T) {
	t.Parallel()

	var closeCount int
	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(false),
		WithDrawerOnClose(func() { closeCount++ }),
	)

	// Press ESC on a closed drawer — should not be consumed.
	consumed := d.HandleKey(tui.KeyEvent{Key: tui.KeyEscape})
	if consumed {
		t.Error("ESC on closed drawer should not be consumed")
	}
	if closeCount != 0 {
		t.Errorf("OnClose should not fire on closed drawer; got %d fires", closeCount)
	}
}

// --- Tests: Focus trap (Tab cycles inside, does not escape) ---

func TestDrawerFocusTrapTab(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1", "Line 2", "Line 3"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// Tab on an open, focused drawer should be consumed (focus trapped inside).
	consumed := d.HandleKey(tui.KeyEvent{Key: tui.KeyTab})
	if !consumed {
		t.Error("Tab on open drawer should be consumed (focus trap)")
	}

	// The drawer should still be open and focused.
	if !d.IsOpen() {
		t.Error("Tab should not close the drawer")
	}
	if !d.Focused() {
		t.Error("Tab should not unfocus the drawer")
	}
}

func TestDrawerFocusTrapShiftTab(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// Shift+Tab should also be consumed.
	consumed := d.HandleKey(tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift})
	if !consumed {
		t.Error("Shift+Tab on open drawer should be consumed (focus trap)")
	}
}

func TestDrawerKeysIgnoredWhenNotFocused(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(false),
	)

	// Tab on unfocused open drawer should not be consumed.
	if d.HandleKey(tui.KeyEvent{Key: tui.KeyTab}) {
		t.Error("Tab on unfocused drawer should not be consumed")
	}
	if d.HandleKey(tui.KeyEvent{Key: tui.KeyEscape}) {
		t.Error("ESC on unfocused drawer should not be consumed")
	}
	if d.HandleKey(tui.KeyEvent{Key: tui.KeyDown}) {
		t.Error("Down on unfocused drawer should not be consumed")
	}
}

// --- Tests: Previously-focused element restored on close ---

func TestDrawerFocusRestoreOnClose(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1"}
	var restoredRef string
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
		WithDrawerPreviouslyFocused("entity-list"),
		WithDrawerOnRestoreFocus(func(ref string) {
			restoredRef = ref
		}),
	)

	// Close via ESC.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyEscape})

	if restoredRef != "entity-list" {
		t.Errorf("focus should be restored to 'entity-list'; got %q", restoredRef)
	}
}

func TestDrawerFocusRestoreNotCalledWhenAlreadyClosed(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1"}
	var restoreCount int
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(false),
		WithDrawerPreviouslyFocused("entity-list"),
		WithDrawerOnRestoreFocus(func(ref string) {
			restoreCount++
		}),
	)

	// Close an already-closed drawer (should be no-op).
	d.Close()

	if restoreCount != 0 {
		t.Errorf("OnRestoreFocus should not fire when closing already-closed drawer; got %d fires", restoreCount)
	}
}

// --- Tests: Double-close safety ---

func TestDrawerDoubleCloseSafe(t *testing.T) {
	t.Parallel()

	var closeCount int
	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
		WithDrawerOnClose(func() { closeCount++ }),
	)

	// Close once.
	d.Close()
	if closeCount != 1 {
		t.Fatalf("first close: want closeCount=1, got %d", closeCount)
	}
	if d.IsOpen() {
		t.Fatal("drawer should be closed after first Close()")
	}

	// Close again — should be no-op.
	d.Close()
	if closeCount != 1 {
		t.Errorf("double close: want closeCount still 1, got %d", closeCount)
	}
}

func TestDrawerDoubleCloseViaESCSafe(t *testing.T) {
	t.Parallel()

	var closeCount int
	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
		WithDrawerOnClose(func() { closeCount++ }),
	)

	// ESC closes.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyEscape})
	if closeCount != 1 {
		t.Fatalf("first ESC: want closeCount=1, got %d", closeCount)
	}

	// Second ESC on closed drawer — not consumed, no extra fire.
	consumed := d.HandleKey(tui.KeyEvent{Key: tui.KeyEscape})
	if consumed {
		t.Error("second ESC on closed drawer should not be consumed")
	}
	if closeCount != 1 {
		t.Errorf("second ESC: want closeCount still 1, got %d", closeCount)
	}
}

// --- Tests: Scroll content ---

func TestDrawerScrollContent(t *testing.T) {
	t.Parallel()

	// Content taller than viewport.
	content := []string{
		"Line 1", "Line 2", "Line 3", "Line 4", "Line 5",
		"Line 6", "Line 7", "Line 8", "Line 9", "Line 10",
	}
	d := NewDrawer(
		"Evidence", content, 30, 6,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// Header takes 1 row, so body height = 5. With 10 lines, content overflows.
	// Scroll down.
	consumed := d.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if !consumed {
		t.Fatal("↓ should be consumed when content overflows")
	}
	if d.ScrollOffset() != 1 {
		t.Errorf("after ↓: want scrollOffset=1, got %d", d.ScrollOffset())
	}

	// Scroll back up.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if d.ScrollOffset() != 0 {
		t.Errorf("after ↑: want scrollOffset=0, got %d", d.ScrollOffset())
	}
}

func TestDrawerPageDownPageUp(t *testing.T) {
	t.Parallel()

	content := make([]string, 20)
	for i := range content {
		content[i] = "Line"
	}
	d := NewDrawer(
		"Evidence", content, 30, 6,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// PageDown.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	if d.ScrollOffset() == 0 {
		t.Error("PageDown should scroll the content")
	}

	// PageUp.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	// Just verify no panic and scroll offset is valid.
	if d.ScrollOffset() < 0 {
		t.Errorf("scrollOffset should be >= 0, got %d", d.ScrollOffset())
	}
}

func TestDrawerHomeEnd(t *testing.T) {
	t.Parallel()

	content := make([]string, 20)
	for i := range content {
		content[i] = "Line"
	}
	d := NewDrawer(
		"Evidence", content, 30, 6,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// End → scroll to bottom.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyEnd})
	if d.ScrollOffset() == 0 {
		t.Error("End should scroll to bottom")
	}

	// Home → scroll to top.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	if d.ScrollOffset() != 0 {
		t.Errorf("Home should scroll to top; got scrollOffset=%d", d.ScrollOffset())
	}
}

// --- Tests: Visible focus indicator ---

func TestDrawerFocusIndicatorWhenOpen(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1", "Line 2"}
	_, mt := renderDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// The left border of the drawer should use BorderFocus color when focused.
	foundFocusBorder := false
	for y := 0; y < 10; y++ {
		cell := mt.CellAt(0, y)
		if cell.Rune != 0 && cell.Rune != ' ' && cell.Style.Fg.Equal(theme.Colors.BorderFocus) {
			foundFocusBorder = true
			break
		}
	}
	if !foundFocusBorder {
		t.Error("focused drawer should have at least one cell with BorderFocus fg color")
	}
}

func TestDrawerNoFocusIndicatorWhenUnfocused(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1", "Line 2"}
	_, mt := renderDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(false),
	)

	// When unfocused, the border should NOT use BorderFocus.
	for y := 0; y < 10; y++ {
		cell := mt.CellAt(0, y)
		if cell.Rune != 0 && cell.Rune != ' ' && cell.Style.Fg.Equal(theme.Colors.BorderFocus) {
			t.Error("unfocused drawer should not have BorderFocus color on border")
			break
		}
	}
}

// --- Tests: Open method (programmatic) ---

func TestDrawerOpenMethod(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1"}
	d := NewDrawer("Evidence", content, 30, 10)

	if d.IsOpen() {
		t.Fatal("new drawer should be closed by default")
	}

	d.Open()
	if !d.IsOpen() {
		t.Fatal("Open() should open the drawer")
	}
}

func TestDrawerOpenMethodIdempotent(t *testing.T) {
	t.Parallel()

	var closeCount int
	content := []string{"Line 1"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOnClose(func() { closeCount++ }),
	)

	// Open twice.
	d.Open()
	d.Open()

	if !d.IsOpen() {
		t.Fatal("drawer should be open")
	}

	// Close once — should fire exactly once.
	d.Close()
	if closeCount != 1 {
		t.Errorf("want closeCount=1, got %d", closeCount)
	}
}

// --- Tests: SetContent ---

func TestDrawerSetContent(t *testing.T) {
	t.Parallel()

	d := NewDrawer("Evidence", []string{"Old"}, 30, 10)

	d.SetContent([]string{"New Line 1", "New Line 2"})

	// Verify content updated by rendering.
	mt := tui.NewMockTerminal(30, 10)
	buf := tui.NewBuffer(30, 10)
	d.Open()
	d.Render(buf)
	tui.RenderFull(mt, buf)

	s := mt.StringTrimmed()
	if !containsSubstring(s, "New Line 1") {
		t.Errorf("after SetContent, should render new content; got:\n%s", s)
	}
}

// --- Tests: Close hint ---

func TestDrawerCloseHintRendered(t *testing.T) {
	t.Parallel()

	content := []string{"Line 1"}
	_, mt := renderDrawer(
		"Evidence", content, 40, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	s := mt.StringTrimmed()
	// The header should show ESC close hint.
	if !containsSubstring(s, "ESC") {
		t.Errorf("open drawer header should show ESC close hint; got:\n%s", s)
	}
}

// --- Tests: Empty content ---

func TestDrawerEmptyContent(t *testing.T) {
	t.Parallel()

	d, mt := renderDrawer(
		"Evidence", nil, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	s := mt.StringTrimmed()
	_ = s // Just verify no panic.

	// Should render without panic even with nil content.
	if !d.IsOpen() {
		t.Error("drawer should still be open with empty content")
	}
}

// --- Tests: Scroll offset clamping ---

func TestDrawerScrollClamped(t *testing.T) {
	t.Parallel()

	content := []string{"Only one line"}
	d := NewDrawer(
		"Evidence", content, 30, 10,
		WithDrawerOpen(true),
		WithDrawerFocused(true),
	)

	// Down with content fitting viewport should not scroll past.
	d.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if d.ScrollOffset() != 0 {
		t.Errorf("content fits viewport; scrollOffset should stay 0, got %d", d.ScrollOffset())
	}
}

// --- Helper ---

// containsSubstring reports whether s contains sub.
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
