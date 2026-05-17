package components

import (
	"fmt"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- StreamViewer test helpers ---

// makeLines creates n test content lines with sequential labels.
func makeLines(n int) []string {
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = fmt.Sprintf("Line %d", i)
	}
	return lines
}

// renderStreamViewer is a test helper that creates a StreamViewer, renders it
// to a MockTerminal, and returns both the widget and the terminal.
func renderStreamViewer(lines []string, width, height int, opts ...StreamViewerOption) (*StreamViewer, *tui.MockTerminal) {
	mt := tui.NewMockTerminal(width, height)
	buf := tui.NewBuffer(width, height)
	sv := NewStreamViewer(lines, width, height, opts...)
	sv.Render(buf)
	tui.RenderFull(mt, buf)
	return sv, mt
}

// --- Tests: Follow-tail engaged when at bottom ---

// TestFollowTailEngaged verifies: (i) follow-tail engaged when at bottom →
// new items scroll into view. When followTail is true (default) and the
// viewer is at the bottom, appending new lines causes the viewport to scroll
// so the last line is visible.
func TestFollowTailEngaged(t *testing.T) {
	t.Parallel()

	height := 5
	// Start with 5 lines that fill the viewport.
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = fmt.Sprintf("initial-%d", i)
	}

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	if !sv.FollowTail() {
		t.Fatal("expected FollowTail=true by default")
	}

	// At bottom: viewport shows lines 0-4, content is 5 lines.

	// Append 10 new lines with distinct labels.
	newLines := make([]string, 10)
	for i := range newLines {
		newLines[i] = fmt.Sprintf("appended-%d", i)
	}
	sv.SetLines(append(lines, newLines...))

	// The viewer should now show the last `height` lines.
	// Total lines: 15, scrollOffset should be 15-5=10.
	wantOffset := 15 - height
	if sv.ScrollOffset() != wantOffset {
		t.Errorf("after append with follow-tail: want scrollOffset=%d, got %d", wantOffset, sv.ScrollOffset())
	}

	// Verify the last appended line is visible in the rendered output.
	_, mt := renderStreamViewer(
		sv.lines, 40, height,
		WithSVFocused(true),
		WithSVScrollOffset(sv.ScrollOffset()),
		WithSVFollowTail(sv.FollowTail()),
	)
	output := mt.StringTrimmed()
	// The last line should be "appended-9".
	if !containsSubstring(output, "appended-9") {
		t.Errorf("follow-tail engaged: last line 'appended-9' should be visible. Output:\n%s", output)
	}
}

// --- Tests: Scroll up disengages follow and preserves anchor ---

// TestScrollUpDisengagesFollow verifies: (ii) user scrolls up by ≥1 line →
// follow disengages and absolute scroll anchor is preserved across new items.
func TestScrollUpDisengagesFollow(t *testing.T) {
	t.Parallel()

	height := 5
	lines := makeLines(20) // more than viewport

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	// Start at bottom.
	if !sv.FollowTail() {
		t.Fatal("expected FollowTail=true initially")
	}

	// Scroll up by 1 line → follow disengages.
	consumed := sv.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if !consumed {
		t.Fatal("↑ should be consumed when focused")
	}

	if sv.FollowTail() {
		t.Fatal("scrolling up should disengage follow-tail")
	}

	// After scrolling up once from bottom, scrollOffset should be
	// (20 - 5) - 1 = 14.
	wantOffset := (20 - height) - 1
	if sv.ScrollOffset() != wantOffset {
		t.Errorf("after scroll up: want scrollOffset=%d, got %d", wantOffset, sv.ScrollOffset())
	}
}

// TestScrollAnchorPreservedAcrossAppends verifies that when follow is
// disengaged and new items are appended, the absolute scroll anchor (line
// index at the top of the viewport) is preserved.
func TestScrollAnchorPreservedAcrossAppends(t *testing.T) {
	t.Parallel()

	height := 5
	lines := makeLines(20)

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	// Scroll to top.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	// Now scroll down to line 3.
	for i := 0; i < 3; i++ {
		sv.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	}

	anchorBefore := sv.ScrollOffset()
	if anchorBefore != 3 {
		t.Fatalf("want anchor=3, got %d", anchorBefore)
	}

	// Append 100 new lines.
	sv.SetLines(append(lines, makeLines(100)...))

	// The scroll anchor should be preserved (still showing line 3 at top).
	if sv.ScrollOffset() != anchorBefore {
		t.Errorf("scroll anchor not preserved: want %d, got %d", anchorBefore, sv.ScrollOffset())
	}
}

// --- Tests: PageUp/PageDown bindings ---

// TestPageUpPageDown verifies: (iii) PageUp/PageDown bindings documented and
// work. PageDown scrolls down by Height lines, PageUp scrolls up by Height
// lines.
func TestPageUpPageDown(t *testing.T) {
	t.Parallel()

	height := 5
	lines := makeLines(30) // well more than viewport

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	// Scroll to top first.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	if sv.ScrollOffset() != 0 {
		t.Fatalf("after Home: want offset=0, got %d", sv.ScrollOffset())
	}

	// PageDown should advance by `height` lines.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	if sv.ScrollOffset() != height {
		t.Errorf("after PageDown from top: want offset=%d, got %d", height, sv.ScrollOffset())
	}

	// Another PageDown.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	if sv.ScrollOffset() != 2*height {
		t.Errorf("after second PageDown: want offset=%d, got %d", 2*height, sv.ScrollOffset())
	}

	// PageUp should go back by `height` lines.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyPageUp})
	if sv.ScrollOffset() != height {
		t.Errorf("after PageUp: want offset=%d, got %d", height, sv.ScrollOffset())
	}

	// PageUp from offset=5 should clamp to 0.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyPageUp})
	if sv.ScrollOffset() != 0 {
		t.Errorf("after PageUp from 5: want offset=0, got %d", sv.ScrollOffset())
	}
}

// TestPageDownCtrlBindings verifies Ctrl+D / Ctrl+U also work for
// PageDown/PageUp.
func TestPageDownCtrlBindings(t *testing.T) {
	t.Parallel()

	height := 5
	lines := makeLines(20)

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	// Start at top.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyHome})

	// Ctrl+D for PageDown.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'd', Mod: tui.ModCtrl})
	if sv.ScrollOffset() != height {
		t.Errorf("after Ctrl+D: want offset=%d, got %d", height, sv.ScrollOffset())
	}

	// Ctrl+U for PageUp.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'u', Mod: tui.ModCtrl})
	if sv.ScrollOffset() != 0 {
		t.Errorf("after Ctrl+U: want offset=0, got %d", sv.ScrollOffset())
	}
}

// TestPageDownDisengagesFollow verifies PageUp disengages follow-tail.
func TestPageUpDisengagesFollow(t *testing.T) {
	t.Parallel()

	height := 5
	lines := makeLines(20)

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	if !sv.FollowTail() {
		t.Fatal("expected follow=true initially")
	}

	// PageUp disengages follow.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyPageUp})
	if sv.FollowTail() {
		t.Fatal("PageUp should disengage follow-tail")
	}
}

// --- Tests: Burst of 1000 items ---

// TestBurstNoDrops verifies: (iv) burst of 1000 appended items in a tight
// loop does NOT drop intermediate items (final buffer has all items in order).
func TestBurstNoDrops(t *testing.T) {
	t.Parallel()

	const burstSize = 1000

	sv := NewStreamViewer(nil, 40, 10, WithSVFocused(true))

	// Build the expected lines.
	allLines := make([]string, 0, burstSize)
	for i := 0; i < burstSize; i++ {
		line := fmt.Sprintf("burst-line-%04d", i)
		allLines = append(allLines, line)
	}

	// Set all lines at once (simulates burst append).
	sv.SetLines(allLines)

	// Verify no drops: every line is present in order.
	finalLines := sv.Lines()
	if len(finalLines) != burstSize {
		t.Fatalf("want %d lines, got %d", burstSize, len(finalLines))
	}

	for i, line := range finalLines {
		want := fmt.Sprintf("burst-line-%04d", i)
		if line != want {
			t.Errorf("line %d: want %q, got %q", i, want, line)
		}
	}
}

// TestBurstIncrementalAppends verifies that incrementally appending lines
// also preserves all items in order.
func TestBurstIncrementalAppends(t *testing.T) {
	t.Parallel()

	const burstSize = 1000
	sv := NewStreamViewer(nil, 40, 10, WithSVFocused(true))

	for i := 0; i < burstSize; i++ {
		line := fmt.Sprintf("incr-line-%04d", i)
		sv.SetLines(append(sv.Lines(), line))
	}

	finalLines := sv.Lines()
	if len(finalLines) != burstSize {
		t.Fatalf("want %d lines, got %d", burstSize, len(finalLines))
	}

	for i, line := range finalLines {
		want := fmt.Sprintf("incr-line-%04d", i)
		if line != want {
			t.Errorf("line %d: want %q, got %q", i, want, line)
		}
	}
}

// --- Tests: End re-engages follow ---

// TestEndReEngagesFollow verifies that pressing End (G) re-engages
// follow-tail and scrolls to the bottom.
func TestEndReEngagesFollow(t *testing.T) {
	t.Parallel()

	height := 5
	lines := makeLines(20)

	sv := NewStreamViewer(
		lines, 40, height,
		WithSVFocused(true),
	)

	// Scroll up to disengage follow.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if sv.FollowTail() {
		t.Fatal("should be disengaged after scroll up")
	}

	// Press End to re-engage.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyEnd})
	if !sv.FollowTail() {
		t.Fatal("End should re-engage follow-tail")
	}

	wantOffset := 20 - height
	if sv.ScrollOffset() != wantOffset {
		t.Errorf("after End: want offset=%d, got %d", wantOffset, sv.ScrollOffset())
	}
}

// --- Tests: Unfocused ignores keys ---

// TestStreamViewerUnfocusedIgnoresKeys verifies that scroll keys are ignored when the
// StreamViewer is not focused.
func TestStreamViewerUnfocusedIgnoresKeys(t *testing.T) {
	t.Parallel()

	lines := makeLines(20)
	sv := NewStreamViewer(
		lines, 40, 5,
		WithSVFocused(false),
	)

	offsetBefore := sv.ScrollOffset()
	consumed := sv.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if consumed {
		t.Error("↑ should not be consumed when unfocused")
	}
	if sv.ScrollOffset() != offsetBefore {
		t.Error("scroll offset should not change when unfocused")
	}
}

// --- Tests: Empty state ---

// TestEmptyViewerRendersMessage verifies the empty state message.
func TestEmptyViewerRendersMessage(t *testing.T) {
	t.Parallel()

	_, mt := renderStreamViewer(nil, 40, 5, WithSVFocused(true))
	output := mt.StringTrimmed()
	if !containsSubstring(output, "Waiting") {
		t.Errorf("empty viewer should show 'Waiting' message. Output:\n%s", output)
	}
}

// --- Tests: Focus indicator ---

// TestFocusedVsUnfocusedBorder verifies the focused StreamViewer has a
// visible focus indicator (colored left border) that is distinct from the
// unfocused state.
func TestFocusedVsUnfocusedBorder(t *testing.T) {
	t.Parallel()

	lines := makeLines(5)

	_, mtF := renderStreamViewer(lines, 40, 5, WithSVFocused(true))
	_, mtU := renderStreamViewer(lines, 40, 5, WithSVFocused(false))

	// Check that the left border column differs between focused and unfocused.
	focusedCell := mtF.CellAt(0, 0)
	unfocusedCell := mtU.CellAt(0, 0)

	// Focused should use BorderFocus color.
	if !focusedCell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("focused CellAt(0,0) fg: want BorderFocus, got %v", focusedCell.Style.Fg)
	}

	// Unfocused should NOT use BorderFocus color.
	if unfocusedCell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("unfocused CellAt(0,0) should NOT have BorderFocus fg")
	}
}

// --- Tests: Home key disengages follow ---

// TestHomeDisengagesFollow verifies pressing Home (g) disengages follow-tail
// and scrolls to the top.
func TestHomeDisengagesFollow(t *testing.T) {
	t.Parallel()

	lines := makeLines(20)
	sv := NewStreamViewer(lines, 40, 5, WithSVFocused(true))

	if !sv.FollowTail() {
		t.Fatal("expected follow=true initially")
	}

	sv.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	if sv.FollowTail() {
		t.Fatal("Home should disengage follow-tail")
	}
	if sv.ScrollOffset() != 0 {
		t.Errorf("after Home: want offset=0, got %d", sv.ScrollOffset())
	}
}

// --- Tests: MaxLines overflow ---

// TestMaxLinesOverflow verifies that when MaxLines is set, older lines are
// dropped when the content exceeds the limit.
func TestMaxLinesOverflow(t *testing.T) {
	t.Parallel()

	const maxLines = 100
	lines := makeLines(200)

	sv := NewStreamViewer(
		lines, 40, 5,
		WithSVMaxLines(maxLines),
	)

	// Should only retain the last maxLines items.
	finalLines := sv.Lines()
	if len(finalLines) > maxLines {
		t.Errorf("expected at most %d lines, got %d", maxLines, len(finalLines))
	}
}

// --- Tests: ScrollDownJKeybinding ---

func TestScrollDownJKeybinding(t *testing.T) {
	t.Parallel()

	lines := makeLines(20)
	sv := NewStreamViewer(lines, 40, 5, WithSVFocused(true))

	// Start at top.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyHome})

	// 'j' scrolls down.
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'j'})
	if sv.ScrollOffset() != 1 {
		t.Errorf("after j: want offset=1, got %d", sv.ScrollOffset())
	}
}

// TestScrollUpKKeybinding verifies 'k' scrolls up.
func TestScrollUpKKeybinding(t *testing.T) {
	t.Parallel()

	lines := makeLines(20)
	sv := NewStreamViewer(lines, 40, 5, WithSVFocused(true))

	// 'k' scrolls up (from bottom, should disengage follow).
	sv.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'k'})
	if sv.FollowTail() {
		t.Fatal("k should disengage follow-tail")
	}
}

// containsSubstring is already defined in drawer_test.go (same package).
// We use it directly.
