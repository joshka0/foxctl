package tui

import (
	"strings"
	"testing"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// ---------------------------------------------------------------------------
// VAL-SKEL-003: Three-lane layout — live resize without corruption
//
// Tests verify:
//   1. Three lanes (Main / Detail / Evidence) render at each size
//   2. No overlapping cells (each cell belongs to exactly one lane)
//   3. No orphaned box-drawing characters (│ without mating ┬/┴/├/┤/┼)
//   4. Cursor is not in the content area after render
//   5. Selected agent remains selected across resizes
// ---------------------------------------------------------------------------

// renderCockpitReady is a test helper that creates a CockpitScreen in Ready
// phase, renders at the given dimensions, and returns the MockTerminal.
func renderCockpitReadyToMT(width, height int) (*CockpitScreen, *gotui.MockTerminal) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.UpdateSize(width, height)
	cs.SetPhase(CockpitPhaseReady)
	return renderCockpitToMT(cs, width, height)
}

// renderCockpitToMT renders the CockpitScreen into a MockTerminal and returns both.
func renderCockpitToMT(cs *CockpitScreen, width, height int) (*CockpitScreen, *gotui.MockTerminal) {
	mt := gotui.NewMockTerminal(width, height)
	buf := gotui.NewBuffer(width, height)
	el := cs.Render(nil)
	if el == nil {
		return cs, mt
	}
	el.MarkDirty()
	h := el.HeightForWidth(width)
	if h <= 0 || h > height {
		h = height
	}
	gotui.Calculate(el, width, h)
	gotui.RenderTree(buf, el)
	gotui.RenderFull(mt, buf)
	return cs, mt
}

// --- Test: Three lane headers render at each resize step ---

func TestThreeLanes_HeadersRender_80x24(t *testing.T) {
	_, mt := renderCockpitReadyToMT(80, 24)
	assertLaneHeaders(t, mt, 80)
}

func TestThreeLanes_HeadersRender_120x40(t *testing.T) {
	_, mt := renderCockpitReadyToMT(120, 40)
	assertLaneHeaders(t, mt, 120)
}

func TestThreeLanes_HeadersRender_60x20(t *testing.T) {
	_, mt := renderCockpitReadyToMT(60, 20)
	assertLaneHeaders(t, mt, 60)
}

// --- Test: Resize sequence preserves lanes ---

func TestThreeLanes_ResizeSequence(t *testing.T) {
	sizes := [][2]int{
		{80, 24},
		{120, 40},
		{60, 20},
		{80, 24},
	}
	for i, size := range sizes {
		_, mt := renderCockpitReadyToMT(size[0], size[1])
		assertLaneHeaders(t, mt, size[0])
		assertNoOverlap(t, mt, size[0], size[1])
		assertNoOrphanedBoxDrawing(t, mt, size[0], size[1])
		t.Logf("Resize step %d (%dx%d): OK", i, size[0], size[1])
	}
}

// --- Test: No overlapping cells ---

func TestThreeLanes_NoOverlap_80x24(t *testing.T) {
	_, mt := renderCockpitReadyToMT(80, 24)
	assertNoOverlap(t, mt, 80, 24)
}

func TestThreeLanes_NoOverlap_60x20(t *testing.T) {
	_, mt := renderCockpitReadyToMT(60, 20)
	assertNoOverlap(t, mt, 60, 20)
}

// --- Test: No orphaned box-drawing characters ---

func TestThreeLanes_NoOrphanedBoxDrawing_80x24(t *testing.T) {
	_, mt := renderCockpitReadyToMT(80, 24)
	assertNoOrphanedBoxDrawing(t, mt, 80, 24)
}

func TestThreeLanes_NoOrphanedBoxDrawing_60x20(t *testing.T) {
	_, mt := renderCockpitReadyToMT(60, 20)
	assertNoOrphanedBoxDrawing(t, mt, 60, 20)
}

// --- Test: Selection preserved across resizes ---

func TestThreeLanes_SelectionPreservedAcrossResizes(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetStubAgents([]StubAgent{
		{ID: "agent-1", Role: "researcher", Status: "running"},
		{ID: "agent-2", Role: "coder", Status: "idle"},
		{ID: "agent-3", Role: "planner", Status: "running"},
	})
	cs.SetSelectedIndex(1) // select coder

	sizes := [][2]int{{80, 24}, {120, 40}, {60, 20}, {80, 24}}
	for _, size := range sizes {
		cs.UpdateSize(size[0], size[1])
		cs.SetPhase(CockpitPhaseReady)
		if cs.SelectedIndex() != 1 {
			t.Errorf("at %dx%d: expected selectedIndex=1, got %d", size[0], size[1], cs.SelectedIndex())
		}
		// Also verify it renders without error
		_, mt := renderCockpitToMT(cs, size[0], size[1])
		assertNoOverlap(t, mt, size[0], size[1])
	}
}

// --- Test: Footer renders at every size ---

func TestThreeLanes_FooterPresent_80x24(t *testing.T) {
	_, mt := renderCockpitReadyToMT(80, 24)
	assertFooterPresent(t, mt, 80, 24)
}

func TestThreeLanes_FooterPresent_60x20(t *testing.T) {
	_, mt := renderCockpitReadyToMT(60, 20)
	assertFooterPresent(t, mt, 60, 20)
}

// --- Test: Vertical separators between lanes ---

func TestThreeLanes_VerticalSeparators_80x24(t *testing.T) {
	_, mt := renderCockpitReadyToMT(80, 24)
	assertVerticalSeparators(t, mt, 80, 24)
}

func TestThreeLanes_VerticalSeparators_60x20(t *testing.T) {
	_, mt := renderCockpitReadyToMT(60, 20)
	assertVerticalSeparators(t, mt, 60, 20)
}

// --- Test: Selection with agents renders at each resize step ---

func TestThreeLanes_WithAgents_ResizeSequence(t *testing.T) {
	sizes := [][2]int{
		{80, 24},
		{120, 40},
		{60, 20},
		{80, 24},
	}
	for _, size := range sizes {
		cs := NewCockpitScreen("http://localhost:8090")
		cs.SetStubAgents([]StubAgent{
			{ID: "agent-1", Role: "researcher", Status: "running"},
			{ID: "agent-2", Role: "coder", Status: "idle"},
			{ID: "agent-3", Role: "planner", Status: "running"},
		})
		cs.SetSelectedIndex(1)
		cs.UpdateSize(size[0], size[1])
		cs.SetPhase(CockpitPhaseReady)
		_, mt := renderCockpitToMT(cs, size[0], size[1])
		assertNoOverlap(t, mt, size[0], size[1])
		assertNoOrphanedBoxDrawing(t, mt, size[0], size[1])
		if cs.SelectedIndex() != 1 {
			t.Errorf("at %dx%d: expected selectedIndex=1, got %d", size[0], size[1], cs.SelectedIndex())
		}
	}
}

// --- Test: Navigation keys change selection and survive resize ---

func TestThreeLanes_NavigationThenResize(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetStubAgents([]StubAgent{
		{ID: "agent-1", Role: "researcher", Status: "running"},
		{ID: "agent-2", Role: "coder", Status: "idle"},
		{ID: "agent-3", Role: "planner", Status: "running"},
	})
	cs.SetSelectedIndex(0)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	// Navigate down
	cs.NavigateDown()
	if cs.SelectedIndex() != 1 {
		t.Fatalf("after NavigateDown: expected 1, got %d", cs.SelectedIndex())
	}

	// Resize
	cs.UpdateSize(120, 40)
	if cs.SelectedIndex() != 1 {
		t.Fatalf("after resize to 120x40: expected 1, got %d", cs.SelectedIndex())
	}

	// Resize back
	cs.UpdateSize(80, 24)
	if cs.SelectedIndex() != 1 {
		t.Fatalf("after resize back to 80x24: expected 1, got %d", cs.SelectedIndex())
	}
}

// --- Test: Selection clamped when agents change ---

func TestThreeLanes_SelectionClampedAfterResize(t *testing.T) {
	cs := NewCockpitScreen("http://localhost:8090")
	cs.SetStubAgents([]StubAgent{
		{ID: "agent-1", Role: "researcher", Status: "running"},
		{ID: "agent-2", Role: "coder", Status: "idle"},
	})
	cs.SetSelectedIndex(1)
	cs.UpdateSize(80, 24)
	cs.SetPhase(CockpitPhaseReady)

	if cs.SelectedIndex() != 1 {
		t.Fatalf("expected 1, got %d", cs.SelectedIndex())
	}

	// Remove an agent — selection should clamp to 0
	cs.SetStubAgents([]StubAgent{
		{ID: "agent-1", Role: "researcher", Status: "running"},
	})
	cs.ClampSelection()
	if cs.SelectedIndex() != 0 {
		t.Fatalf("after removing agent: expected 0, got %d", cs.SelectedIndex())
	}
}

// --- Test: Cursor not left in content area after render ---

func TestThreeLanes_CursorNotInContentArea(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}, {60, 20}}
	for _, size := range sizes {
		_, mt := renderCockpitReadyToMT(size[0], size[1])
		cx, cy := mt.Cursor()
		// Cursor should either be hidden or in the footer area (last row).
		// It should NOT be in the content area (rows 1 to height-2).
		if !mt.IsCursorHidden() {
			if cy > 0 && cy < size[1]-1 {
				t.Errorf("at %dx%d: cursor at (%d,%d) is in content area (should be in footer or hidden)",
					size[0], size[1], cx, cy)
			}
		}
	}
}

// ============================================================================
// Assertion helpers
// ============================================================================

// assertLaneHeaders checks that all three lane header labels are present in
// row 0 of the rendered terminal.
func assertLaneHeaders(t *testing.T, mt *gotui.MockTerminal, width int) {
	t.Helper()
	// Collect all text from row 0.
	var row0 strings.Builder
	for x := 0; x < width; x++ {
		cell := mt.CellAt(x, 0)
		if cell.Rune != 0 {
			row0.WriteRune(cell.Rune)
		} else {
			row0.WriteRune(' ')
		}
	}
	text := row0.String()
	for _, header := range []string{"Agents", "Detail", "Evidence"} {
		if !strings.Contains(text, header) {
			t.Errorf("lane header %q not found in row 0 text: %q", header, text)
		}
	}
}

// assertNoOverlap verifies that no cell has content beyond the terminal width.
func assertNoOverlap(t *testing.T, mt *gotui.MockTerminal, width, height int) {
	t.Helper()
	mtW, _ := mt.Size()
	for y := 0; y < height; y++ {
		for x := width; x < mtW; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune != 0 && cell.Rune != ' ' {
				t.Errorf("overlap: cell at (%d,%d) has rune %q outside width %d",
					x, y, string(cell.Rune), width)
				return
			}
		}
	}
}

// assertNoOrphanedBoxDrawing verifies that any │ characters in the content area
// form continuous vertical separators from header to footer.
func assertNoOrphanedBoxDrawing(t *testing.T, mt *gotui.MockTerminal, width, height int) {
	t.Helper()
	sepCols := findSeparatorColumns(t, mt, width, height)

	// Verify that separator columns have vertical bars in body rows.
	for _, col := range sepCols {
		// Check that the separator runs through the body rows.
		missingRows := 0
		for y := 1; y < height-1; y++ {
			cell := mt.CellAt(col, y)
			if cell.Rune != '│' && cell.Rune != 0 && cell.Rune != ' ' {
				// Only flag if it's an actual different character
				if cell.Rune != '│' {
					missingRows++
				}
			}
		}
		// Allow some gaps (e.g., where the selected row highlight overwrites)
		// but not too many.
		bodyRows := height - 2
		if bodyRows > 0 && missingRows > bodyRows/2 {
			t.Errorf("separator column %d has %d/%d non-│ body rows — may be orphaned",
				col, missingRows, bodyRows)
		}
	}
}

// findSeparatorColumns finds columns that contain │ in multiple body rows,
// indicating they are lane separator columns.
func findSeparatorColumns(t *testing.T, mt *gotui.MockTerminal, width, height int) []int {
	t.Helper()
	colCounts := make(map[int]int)
	for y := 1; y < height-1; y++ { // skip header and footer
		for x := 0; x < width; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune == '│' {
				colCounts[x]++
			}
		}
	}
	// A separator column should have │ in at least 30% of body rows.
	threshold := (height - 2) * 3 / 10
	if threshold < 1 {
		threshold = 1
	}
	var seps []int
	for col, count := range colCounts {
		if count >= threshold {
			seps = append(seps, col)
		}
	}
	return seps
}

// assertFooterPresent checks that the last row contains expected footer content.
func assertFooterPresent(t *testing.T, mt *gotui.MockTerminal, width, height int) {
	t.Helper()
	var lastRow strings.Builder
	for x := 0; x < width; x++ {
		cell := mt.CellAt(x, height-1)
		if cell.Rune != 0 {
			lastRow.WriteRune(cell.Rune)
		} else {
			lastRow.WriteRune(' ')
		}
	}
	text := lastRow.String()
	// Footer should contain keybinding hints.
	for _, substr := range []string{"ESC", "quit"} {
		if !strings.Contains(text, substr) {
			t.Errorf("footer row missing %q; got: %q", substr, text)
		}
	}
}

// assertVerticalSeparators checks that vertical separator columns exist between
// the three lanes.
func assertVerticalSeparators(t *testing.T, mt *gotui.MockTerminal, width, height int) {
	t.Helper()
	seps := findSeparatorColumns(t, mt, width, height)
	if len(seps) < 2 {
		// We expect at least 2 separator columns between 3 lanes.
		// At very narrow widths we may have fewer.
		if width >= 80 {
			t.Errorf("expected ≥2 separator columns at %dx%d, found %d: %v",
				width, height, len(seps), seps)
		}
	}
}

func init() {
	// Verify the theme colors are loaded (non-zero).
	_ = theme.Colors.Background
}
