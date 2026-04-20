package components

import (
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- test helpers ---

// makeItems creates n test Entity items with sequential labels.
func makeItems(n int) []Entity {
	items := make([]Entity, n)
	for i := 0; i < n; i++ {
		items[i] = Entity{
			ID:       string(rune('a' + i)),
			Label:    string(rune('A' + i)),
			SubLabel: "sub",
		}
	}
	return items
}

// renderEntityList is a test helper that creates an EntityList, renders it to
// a MockTerminal, and returns the terminal for inspection.
func renderEntityList(items []Entity, width, height int, opts ...EntityListOption) *tui.MockTerminal {
	mt := tui.NewMockTerminal(width, height)
	buf := tui.NewBuffer(width, height)
	el := NewEntityList(items, width, height, opts...)
	el.Render(buf)
	tui.RenderFull(mt, buf)
	return mt
}

// --- Tests: Navigation ---

func TestNavigateDown(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(0), WithFocused(true))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 1 {
		t.Errorf("after ↓ from 0: want selectedIndex=1, got %d", el.SelectedIndex())
	}

	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 2 {
		t.Errorf("after ↓ from 1: want selectedIndex=2, got %d", el.SelectedIndex())
	}
}

func TestNavigateUp(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(2), WithFocused(true))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if el.SelectedIndex() != 1 {
		t.Errorf("after ↑ from 2: want selectedIndex=1, got %d", el.SelectedIndex())
	}

	el.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if el.SelectedIndex() != 0 {
		t.Errorf("after ↑ from 1: want selectedIndex=0, got %d", el.SelectedIndex())
	}
}

func TestNavigateDownJ(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(0), WithFocused(true))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'j'})
	if el.SelectedIndex() != 1 {
		t.Errorf("after 'j' from 0: want selectedIndex=1, got %d", el.SelectedIndex())
	}
}

func TestNavigateUpK(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(2), WithFocused(true))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'k'})
	if el.SelectedIndex() != 1 {
		t.Errorf("after 'k' from 2: want selectedIndex=1, got %d", el.SelectedIndex())
	}
}

func TestWrapAround(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(4), WithWrapAround(true), WithFocused(true))

	// At last item, ↓ wraps to 0.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 0 {
		t.Errorf("wrap ↓ from last: want selectedIndex=0, got %d", el.SelectedIndex())
	}

	// At first item, ↑ wraps to last.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if el.SelectedIndex() != 4 {
		t.Errorf("wrap ↑ from first: want selectedIndex=4, got %d", el.SelectedIndex())
	}
}

func TestNoWrapAround(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(4), WithWrapAround(false), WithFocused(true))

	// At last item, ↓ stays clamped at end.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 4 {
		t.Errorf("no-wrap ↓ from last: want selectedIndex=4, got %d", el.SelectedIndex())
	}

	el2 := NewEntityList(items, 40, 10, WithSelectedIndex(0), WithWrapAround(false), WithFocused(true))

	// At first item, ↑ stays clamped at start.
	el2.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if el2.SelectedIndex() != 0 {
		t.Errorf("no-wrap ↑ from first: want selectedIndex=0, got %d", el2.SelectedIndex())
	}
}

func TestHomeEnd(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(2), WithFocused(true))

	// Home jumps to 0.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	if el.SelectedIndex() != 0 {
		t.Errorf("Home: want selectedIndex=0, got %d", el.SelectedIndex())
	}

	// End jumps to last.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyEnd})
	if el.SelectedIndex() != 4 {
		t.Errorf("End: want selectedIndex=4, got %d", el.SelectedIndex())
	}
}

func TestHomeEndWithViKeys(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(2), WithFocused(true))

	// 'g' jumps to Home.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'g'})
	if el.SelectedIndex() != 0 {
		t.Errorf("'g': want selectedIndex=0, got %d", el.SelectedIndex())
	}

	// 'G' jumps to End.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'G'})
	if el.SelectedIndex() != 4 {
		t.Errorf("'G': want selectedIndex=4, got %d", el.SelectedIndex())
	}
}

func TestPageDownPageUp(t *testing.T) {
	t.Parallel()
	items := makeItems(20)
	el := NewEntityList(items, 40, 5, WithSelectedIndex(0), WithFocused(true))

	// PageDown advances by visible height (5).
	el.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	if el.SelectedIndex() != 5 {
		t.Errorf("PageDown from 0: want selectedIndex=5, got %d", el.SelectedIndex())
	}

	// PageUp retreats by visible height.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyPageUp})
	if el.SelectedIndex() != 0 {
		t.Errorf("PageUp from 5: want selectedIndex=0, got %d", el.SelectedIndex())
	}
}

func TestPageDownClampsAtEnd(t *testing.T) {
	t.Parallel()
	items := makeItems(7)
	el := NewEntityList(items, 40, 5, WithSelectedIndex(5), WithFocused(true))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	if el.SelectedIndex() != 6 {
		t.Errorf("PageDown clamp: want selectedIndex=6, got %d", el.SelectedIndex())
	}
}

// --- Tests: Focus indicator ---

func TestFocusIndicatorColoredLeftBorder(t *testing.T) {
	t.Parallel()
	items := makeItems(5)

	// Render with focus
	mt := renderEntityList(items, 40, 10,
		WithSelectedIndex(0),
		WithFocused(true),
	)

	// The first column (x=0) of the selected row should have the BorderFocus color.
	cell := mt.CellAt(0, 0)
	if cell.Rune == ' ' || cell.Rune == 0 {
		t.Errorf("focused list row 0, col 0: expected a visible indicator rune, got space/zero")
	}
	// The cell should have the BorderFocus color as foreground.
	if !cell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("focused list row 0, col 0: want BorderFocus fg, got %v", cell.Style.Fg)
	}
}

func TestUnfocusedNoFocusBorder(t *testing.T) {
	t.Parallel()
	items := makeItems(5)

	// Render without focus
	mt := renderEntityList(items, 40, 10,
		WithSelectedIndex(0),
		WithFocused(false),
	)

	// The first column of the selected row should NOT have the BorderFocus color.
	cell := mt.CellAt(0, 0)
	if cell.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("unfocused list row 0, col 0: should NOT have BorderFocus fg")
	}
}

func TestFocusIndicatorNotFontWeightOnly(t *testing.T) {
	t.Parallel()
	items := makeItems(5)

	mt := renderEntityList(items, 40, 10,
		WithSelectedIndex(0),
		WithFocused(true),
	)

	// Verify that the focus indicator uses something OTHER than just bold.
	// The left border cell should have a non-default background or non-default foreground color.
	cell := mt.CellAt(0, 0)

	hasNonDefaultFg := !cell.Style.Fg.IsDefault()
	hasNonDefaultBg := !cell.Style.Bg.IsDefault()
	hasReverse := cell.Style.HasAttr(tui.AttrReverse)

	if !hasNonDefaultFg && !hasNonDefaultBg && !hasReverse {
		t.Errorf("focus indicator must have visible indicator beyond font-weight; " +
			"cell has default fg, default bg, and no reverse attr")
	}
}

// --- Tests: Unfocused ignores keys ---

func TestUnfocusedIgnoresKeys(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(0), WithFocused(false))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 0 {
		t.Errorf("unfocused ↓: selectedIndex should stay 0, got %d", el.SelectedIndex())
	}

	el.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if el.SelectedIndex() != 0 {
		t.Errorf("unfocused ↑: selectedIndex should stay 0, got %d", el.SelectedIndex())
	}

	el.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	if el.SelectedIndex() != 0 {
		t.Errorf("unfocused Home: selectedIndex should stay 0, got %d", el.SelectedIndex())
	}
}

// --- Tests: Scroll keeps selection visible ---

func TestScrollKeepsSelectionVisible(t *testing.T) {
	t.Parallel()
	items := makeItems(20)

	// height=5, viewport shows items 0-4 initially.
	el := NewEntityList(items, 40, 5, WithSelectedIndex(0))

	// Jump to item 15 via End-like behavior (set directly).
	el.SetSelectedIndex(15)

	// scrollOffset should adjust to keep item 15 visible.
	// With height=5, the viewport should show items 11-15 at minimum.
	scrollOff := el.ScrollOffset()
	if scrollOff > 15 || scrollOff < 15-4 {
		t.Errorf("scrollOffset=%d after selecting item 15 with height=5, expected range [11,15]", scrollOff)
	}
}

// --- Tests: Empty state ---

func TestEmptyRendersEmptyState(t *testing.T) {
	t.Parallel()
	mt := renderEntityList(nil, 40, 10)

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("empty EntityList should render something")
	}
	// Should contain empty-state copy.
	if len(content) < 5 {
		t.Errorf("empty state content seems too short: %q", content)
	}
}

// --- Tests: Loading state ---

func TestLoadingRendersLoadingState(t *testing.T) {
	t.Parallel()
	mt := renderEntityList(nil, 40, 10, WithLoading(true))

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("loading EntityList should render something")
	}
}

// --- Tests: Error state ---

func TestErrorRendersError(t *testing.T) {
	t.Parallel()
	mt := renderEntityList(makeItems(3), 40, 10, WithErrorMessage("connection failed"))

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("error EntityList should render something")
	}
}

// --- Tests: Rendering correctness ---

func TestRenderShowsLabels(t *testing.T) {
	t.Parallel()
	items := []Entity{
		{ID: "1", Label: "Alpha"},
		{ID: "2", Label: "Beta"},
	}
	mt := renderEntityList(items, 40, 10, WithSelectedIndex(0), WithFocused(true))
	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("expected non-empty render output")
	}
	// Just verify something rendered; detailed snapshot tests via tuistory.
}

func TestSelectedRowHasSelectionBackground(t *testing.T) {
	t.Parallel()
	items := makeItems(5)
	mt := renderEntityList(items, 40, 10, WithSelectedIndex(1), WithFocused(true))

	// The selected row (y=1) should have SelectionBg as its background in the content area.
	cell := mt.CellAt(2, 1) // skip the focus border column
	if !cell.Style.Bg.Equal(theme.Colors.SelectionBg) {
		t.Errorf("selected row y=1, x=2: want SelectionBg, got %v", cell.Style.Bg)
	}
}

// --- Tests: No items navigation safety ---

func TestNavigationWithNoItems(t *testing.T) {
	t.Parallel()
	el := NewEntityList(nil, 40, 10, WithSelectedIndex(-1), WithFocused(true))

	// Should not panic on any key.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	el.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	el.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	el.HandleKey(tui.KeyEvent{Key: tui.KeyEnd})
	el.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	el.HandleKey(tui.KeyEvent{Key: tui.KeyPageUp})

	if el.SelectedIndex() != -1 {
		t.Errorf("empty list navigation: selectedIndex should stay -1, got %d", el.SelectedIndex())
	}
}

func TestSingleItemNavigation(t *testing.T) {
	t.Parallel()
	items := makeItems(1)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(0), WithFocused(true), WithWrapAround(false))

	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 0 {
		t.Errorf("single item ↓: want 0, got %d", el.SelectedIndex())
	}

	el.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if el.SelectedIndex() != 0 {
		t.Errorf("single item ↑: want 0, got %d", el.SelectedIndex())
	}
}

func TestAutoSelectFirstWhenSelectedNegative(t *testing.T) {
	t.Parallel()
	items := makeItems(3)
	el := NewEntityList(items, 40, 10, WithSelectedIndex(-1), WithFocused(true))

	// When pressing ↓ with selectedIndex=-1 and items exist, should select 0.
	el.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if el.SelectedIndex() != 0 {
		t.Errorf("↓ from -1: want selectedIndex=0, got %d", el.SelectedIndex())
	}
}
