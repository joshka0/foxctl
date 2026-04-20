package components

import (
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- Tabs test helpers ---

// renderTabs is a test helper that creates a Tabs widget, renders it to
// a MockTerminal, and returns both the widget and terminal for inspection.
func renderTabs(labels []string, width int, opts ...TabsOption) (*Tabs, *tui.MockTerminal) {
	mt := tui.NewMockTerminal(width, 1)
	buf := tui.NewBuffer(width, 1)
	tabs := NewTabs(labels, width, opts...)
	tabs.Render(buf)
	tui.RenderFull(mt, buf)
	return tabs, mt
}

// --- Tests: Navigation ---

func TestTabsForwardNavigation(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0))

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight}) {
		t.Fatal("→ should be consumed")
	}
	if tabs.ActiveIndex() != 1 {
		t.Errorf("after → from 0: want activeIndex=1, got %d", tabs.ActiveIndex())
	}

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight}) {
		t.Fatal("→ should be consumed")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("after → from 1: want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

func TestTabsForwardNavigationL(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0))

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'l'}) {
		t.Fatal("'l' should be consumed")
	}
	if tabs.ActiveIndex() != 1 {
		t.Errorf("after 'l' from 0: want activeIndex=1, got %d", tabs.ActiveIndex())
	}
}

func TestTabsBackwardNavigation(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(2))

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) {
		t.Fatal("← should be consumed")
	}
	if tabs.ActiveIndex() != 1 {
		t.Errorf("after ← from 2: want activeIndex=1, got %d", tabs.ActiveIndex())
	}

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) {
		t.Fatal("← should be consumed")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("after ← from 1: want activeIndex=0, got %d", tabs.ActiveIndex())
	}
}

func TestTabsBackwardNavigationH(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(2))

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'h'}) {
		t.Fatal("'h' should be consumed")
	}
	if tabs.ActiveIndex() != 1 {
		t.Errorf("after 'h' from 2: want activeIndex=1, got %d", tabs.ActiveIndex())
	}
}

// --- Tests: Wrap-around ---

func TestTabsWrapForward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(2))

	// At last tab, → wraps to 0 (default WrapAround=true).
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight}) {
		t.Fatal("→ should be consumed")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("wrap → from last: want activeIndex=0, got %d", tabs.ActiveIndex())
	}
}

func TestTabsWrapBackward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0))

	// At first tab, ← wraps to last.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) {
		t.Fatal("← should be consumed")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("wrap ← from first: want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

func TestTabsNoWrapForward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(2), WithTabsWrapAround(false))

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight}) {
		t.Fatal("→ should be consumed even at boundary")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("no-wrap → from last: want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

func TestTabsNoWrapBackward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0), WithTabsWrapAround(false))

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) {
		t.Fatal("← should be consumed even at boundary")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("no-wrap ← from first: want activeIndex=0, got %d", tabs.ActiveIndex())
	}
}

// --- Tests: Tab and Shift+Tab ---

func TestTabsTabForward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0))

	// Tab moves forward (cycles).
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab}) {
		t.Fatal("Tab should be consumed")
	}
	if tabs.ActiveIndex() != 1 {
		t.Errorf("Tab from 0: want activeIndex=1, got %d", tabs.ActiveIndex())
	}

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab}) {
		t.Fatal("Tab should be consumed")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("Tab from 1: want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

func TestTabsTabWrapsForward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(2))

	// Tab at last wraps to first.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab}) {
		t.Fatal("Tab should be consumed")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("Tab wrap from last: want activeIndex=0, got %d", tabs.ActiveIndex())
	}
}

func TestTabsShiftTabBackward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(2))

	// Shift+Tab moves backward.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift}) {
		t.Fatal("Shift+Tab should be consumed")
	}
	if tabs.ActiveIndex() != 1 {
		t.Errorf("Shift+Tab from 2: want activeIndex=1, got %d", tabs.ActiveIndex())
	}
}

func TestTabsShiftTabWrapsBackward(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0))

	// Shift+Tab at first wraps to last.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift}) {
		t.Fatal("Shift+Tab should be consumed")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("Shift+Tab wrap from first: want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

// --- Tests: Home/End ---

func TestTabsHomeEnd(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(1))

	// Home jumps to first tab.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyHome}) {
		t.Fatal("Home should be consumed")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("Home: want activeIndex=0, got %d", tabs.ActiveIndex())
	}

	// End jumps to last tab.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyEnd}) {
		t.Fatal("End should be consumed")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("End: want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

func TestTabsHomeEndViKeys(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(1))

	// 'g' jumps to first tab.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'g'}) {
		t.Fatal("'g' should be consumed")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("'g': want activeIndex=0, got %d", tabs.ActiveIndex())
	}

	// 'G' jumps to last tab.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'G'}) {
		t.Fatal("'G' should be consumed")
	}
	if tabs.ActiveIndex() != 2 {
		t.Errorf("'G': want activeIndex=2, got %d", tabs.ActiveIndex())
	}
}

// --- Tests: Single tab ---

func TestTabsSingleTabNoop(t *testing.T) {
	t.Parallel()
	labels := []string{"Only"}
	tabs := NewTabs(labels, 40, WithTabsFocused(true), WithTabsActiveIndex(0))

	// With 1 tab, → is consumed but no change.
	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight}) {
		t.Fatal("→ should still be consumed with 1 tab")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("single tab →: want activeIndex=0, got %d", tabs.ActiveIndex())
	}

	if !tabs.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) {
		t.Fatal("← should still be consumed with 1 tab")
	}
	if tabs.ActiveIndex() != 0 {
		t.Errorf("single tab ←: want activeIndex=0, got %d", tabs.ActiveIndex())
	}
}

// --- Tests: Focus behavior ---

func TestTabsUnfocusedIgnoresKeys(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsFocused(false), WithTabsActiveIndex(0))

	if tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight}) {
		t.Error("unfocused → should not be consumed")
	}
	if tabs.HandleKey(tui.KeyEvent{Key: tui.KeyLeft}) {
		t.Error("unfocused ← should not be consumed")
	}
	if tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab}) {
		t.Error("unfocused Tab should not be consumed")
	}
	if tabs.HandleKey(tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift}) {
		t.Error("unfocused Shift+Tab should not be consumed")
	}
	if tabs.HandleKey(tui.KeyEvent{Key: tui.KeyHome}) {
		t.Error("unfocused Home should not be consumed")
	}
	if tabs.HandleKey(tui.KeyEvent{Key: tui.KeyEnd}) {
		t.Error("unfocused End should not be consumed")
	}
}

// --- Tests: Active indicator visible beyond bold ---

func TestTabsActiveIndicatorColoredUnderline(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	_, mt := renderTabs(labels, 40, WithTabsActiveIndex(0), WithTabsFocused(true))

	// The first character of the active tab label ("A" of "Agents") should
	// have a distinct foreground (Accent) that is different from inactive tab labels.
	// We verify that the active tab's cells have Accent color as foreground.

	// Find the first non-space cell — it should be the start of "Agents" label.
	foundAccent := false
	for x := 0; x < 40; x++ {
		cell := mt.CellAt(x, 0)
		if cell.Rune == ' ' || cell.Rune == 0 {
			continue
		}
		if cell.Style.Fg.Equal(theme.Colors.Accent) {
			foundAccent = true
			break
		}
	}
	if !foundAccent {
		t.Error("focused tabs: expected at least one cell with Accent fg for active tab")
	}
}

func TestTabsActiveIndicatorNotFontWeightOnly(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	_, mt := renderTabs(labels, 40, WithTabsActiveIndex(1), WithTabsFocused(true))

	// The active tab should have cells with non-default foreground (Accent color).
	// This is beyond font-weight only — it uses a distinct color.
	activeLabel := "Rooms"
	activeFound := false
	for x := 0; x < 40; x++ {
		cell := mt.CellAt(x, 0)
		if cell.Rune == 0 || cell.Rune == ' ' {
			continue
		}
		// Check if this cell is part of the active label by matching rune.
		r := cell.Rune
		for _, lr := range activeLabel {
			if r == lr {
				// This might be part of the active label. Check for Accent color.
				if cell.Style.Fg.Equal(theme.Colors.Accent) {
					activeFound = true
				}
			}
		}
	}
	if !activeFound {
		t.Error("active tab label cells should have Accent foreground color")
	}
}

func TestTabsActiveVsInactiveDistinguishable(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}

	// Render with tab 0 active.
	_, mt0 := renderTabs(labels, 40, WithTabsActiveIndex(0), WithTabsFocused(true))

	// Render with tab 1 active.
	_, mt1 := renderTabs(labels, 40, WithTabsActiveIndex(1), WithTabsFocused(true))

	// The raw cell buffer at the start of "Agents" label should differ between
	// the two renders (different active index = different indicator position).
	// Find the x position of "Agents" in the first render and verify the
	// foreground color is Accent when active and not Accent when inactive.

	// In mt0, "Agents" is active → cells should have Accent fg.
	// In mt1, "Agents" is inactive → cells should NOT have Accent fg.
	agentsActive := false
	agentsInactive := false
	for x := 0; x < 40; x++ {
		cell0 := mt0.CellAt(x, 0)
		cell1 := mt1.CellAt(x, 0)
		if cell0.Rune == 'A' {
			if cell0.Style.Fg.Equal(theme.Colors.Accent) {
				agentsActive = true
			}
		}
		if cell1.Rune == 'A' {
			if !cell1.Style.Fg.Equal(theme.Colors.Accent) {
				agentsInactive = true
			}
		}
	}
	if !agentsActive {
		t.Error("Agents label should have Accent fg when it is the active tab")
	}
	if !agentsInactive {
		t.Error("Agents label should NOT have Accent fg when it is an inactive tab")
	}
}

// --- Tests: OnChange callback ---

func TestTabsOnChangeCallback(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	var lastIdx int
	tabs := NewTabs(labels, 40,
		WithTabsFocused(true),
		WithTabsActiveIndex(0),
		WithTabsOnChange(func(idx int) { lastIdx = idx }),
	)

	tabs.HandleKey(tui.KeyEvent{Key: tui.KeyRight})
	if lastIdx != 1 {
		t.Errorf("OnChange: want lastIdx=1, got %d", lastIdx)
	}
}

// --- Tests: Clamping ---

func TestTabsActiveIndexClamped(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms"}

	// Active index too high → clamped to last.
	tabs := NewTabs(labels, 40, WithTabsActiveIndex(10))
	if tabs.ActiveIndex() != 1 {
		t.Errorf("clamped activeIndex: want 1, got %d", tabs.ActiveIndex())
	}
}

func TestTabsActiveIndexNegativeClamped(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms"}

	// Active index negative → clamped to 0.
	tabs := NewTabs(labels, 40, WithTabsActiveIndex(-5))
	if tabs.ActiveIndex() != 0 {
		t.Errorf("clamped negative activeIndex: want 0, got %d", tabs.ActiveIndex())
	}
}

// --- Tests: Empty labels ---

func TestTabsEmptyLabelsNoPanic(t *testing.T) {
	t.Parallel()
	// Empty labels → widget renders nothing, doesn't panic.
	tabs := NewTabs(nil, 40)
	buf := tui.NewBuffer(40, 1)
	tabs.Render(buf) // should not panic
}

// --- Tests: Rendering correctness ---

func TestTabsRenderShowsLabels(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	_, mt := renderTabs(labels, 50, WithTabsActiveIndex(0), WithTabsFocused(true))

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("tabs should render non-empty content")
	}
}

func TestTabsSetLabels(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms"}
	tabs := NewTabs(labels, 40, WithTabsActiveIndex(1))

	// Replace with more labels; active index should be re-clamped.
	tabs.SetLabels([]string{"One", "Two", "Three", "Four"})
	// Active index 1 should still be valid.
	if tabs.ActiveIndex() != 1 {
		t.Errorf("after SetLabels: want activeIndex=1, got %d", tabs.ActiveIndex())
	}

	// Replace with fewer labels; active index should be clamped.
	tabs.SetLabels([]string{"Only"})
	if tabs.ActiveIndex() != 0 {
		t.Errorf("after SetLabels to single: want activeIndex=0, got %d", tabs.ActiveIndex())
	}
}

func TestTabsSetActiveIndex(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}
	tabs := NewTabs(labels, 40, WithTabsActiveIndex(0))

	tabs.SetActiveIndex(2)
	if tabs.ActiveIndex() != 2 {
		t.Errorf("SetActiveIndex(2): want 2, got %d", tabs.ActiveIndex())
	}

	// Out of range → clamped.
	tabs.SetActiveIndex(100)
	if tabs.ActiveIndex() != 2 {
		t.Errorf("SetActiveIndex(100): want 2 (clamped), got %d", tabs.ActiveIndex())
	}

	tabs.SetActiveIndex(-1)
	if tabs.ActiveIndex() != 0 {
		t.Errorf("SetActiveIndex(-1): want 0 (clamped), got %d", tabs.ActiveIndex())
	}
}

func TestTabsUnderlinePresent(t *testing.T) {
	t.Parallel()
	labels := []string{"Agents", "Rooms", "Events"}

	mt := tui.NewMockTerminal(50, 2)
	buf := tui.NewBuffer(50, 2)
	tabs := NewTabs(labels, 50, WithTabsActiveIndex(0), WithTabsFocused(true))
	tabs.Render(buf)
	tui.RenderFull(mt, buf)

	// Row 1 (below the labels) should have underline characters under the active tab.
	// At minimum, some cells in row 1 under the active tab should contain non-space runes
	// with the Accent color.
	underlineFound := false
	for x := 0; x < 50; x++ {
		cell := mt.CellAt(x, 1)
		if cell.Rune != ' ' && cell.Rune != 0 && cell.Style.Fg.Equal(theme.Colors.Accent) {
			underlineFound = true
			break
		}
	}
	if !underlineFound {
		t.Error("expected underline indicator cells in row 1 below active tab")
	}
}
