package components

import (
	"strings"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- DetailPane test helpers ---

// renderDetailPane is a test helper that creates a DetailPane, renders it to
// a MockTerminal, and returns the terminal for inspection.
func renderDetailPane(title string, status StatusVariant, sections []Section, width, height int, opts ...DetailPaneOption) *tui.MockTerminal {
	mt := tui.NewMockTerminal(width, height)
	buf := tui.NewBuffer(width, height)
	dp := NewDetailPane(title, status, sections, width, height, opts...)
	dp.Render(buf)
	tui.RenderFull(mt, buf)
	return mt
}

// renderDetailPaneEntity renders a DetailPane with HasEntity=true.
func renderDetailPaneEntity(title string, status StatusVariant, sections []Section, width, height int, opts ...DetailPaneOption) *tui.MockTerminal {
	opts = append(opts, WithHasEntity(true))
	return renderDetailPane(title, status, sections, width, height, opts...)
}

// --- Tests: Header render ---

func TestDetailPaneHeaderRender(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Runtime", Lines: []string{"state: running"}},
	}
	mt := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 10, WithDPFocused(true))

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("DetailPane should render content")
	}

	// Title should be visible in the output.
	if !strings.Contains(content, "agent-abc123") {
		t.Errorf("header should contain title 'agent-abc123', got: %s", content)
	}
}

func TestDetailPaneHeaderShowsStatusBadge(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Runtime", Lines: []string{"state: running"}},
	}
	mt := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 10, WithDPFocused(true))

	// The header row should contain the status badge icon for StatusOK.
	content := mt.StringTrimmed()
	if !strings.Contains(content, "●") {
		t.Errorf("header should contain StatusOK icon '●', got: %s", content)
	}
}

// --- Tests: Title truncation ---

func TestDetailPaneTitleTruncation(t *testing.T) {
	t.Parallel()
	longTitle := "this-is-a-very-long-title-that-exceeds-the-width-of-the-pane-and-should-be-truncated"
	sections := []Section{
		{Title: "Info", Lines: []string{"some data"}},
	}

	// Use a narrow width that forces truncation.
	mt := renderDetailPaneEntity(longTitle, StatusOK, sections, 20, 10, WithDPFocused(true))

	// The title should be truncated with an ellipsis.
	// Check the raw buffer: the header row (y=0) should end with "…" within the
	// width boundary.
	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("expected non-empty render")
	}

	// Verify the full un-truncated title is NOT present (it should be cut off).
	if strings.Contains(content, longTitle) {
		t.Errorf("title should have been truncated but full title appears in output")
	}

	// Verify an ellipsis character appears somewhere in the header area.
	// We check if the rendered title in the header row contains "…".
	hasEllipsis := false
	for x := 0; x < 20; x++ {
		cell := mt.CellAt(x, 0)
		if cell.Rune == '…' {
			hasEllipsis = true
			break
		}
	}
	if !hasEllipsis {
		t.Errorf("truncated title should contain '…' rune in header row, but none found")
	}
}

// --- Tests: Section separators ---

func TestDetailPaneSectionSeparators(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Runtime", Lines: []string{"state: running"}},
		{Title: "Hierarchy", Lines: []string{"parent: none"}},
	}
	mt := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 15, WithDPFocused(true))

	// Sections should be separated by visible horizontal rules.
	// Look for a divider character (─ or similar) in the rendered output.
	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("expected non-empty render")
	}

	// Check for any separator rune in the buffer. The separator should be a
	// horizontal line character.
	hasSeparator := false
	for y := 0; y < 15; y++ {
		for x := 0; x < 40; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune == '─' || cell.Rune == '╌' || cell.Rune == '-' {
				hasSeparator = true
				break
			}
		}
		if hasSeparator {
			break
		}
	}
	if !hasSeparator {
		t.Errorf("sections should be separated by visible separators, none found in output:\n%s", content)
	}
}

// --- Tests: Body scroll ---

func TestDetailPaneBodyScroll(t *testing.T) {
	t.Parallel()

	// Create enough sections/lines to overflow the viewport.
	sections := []Section{
		{Title: "Runtime", Lines: []string{
			"state: running",
			"provider: openrouter",
			"model: aurora-alpha",
			"uptime: 2h30m",
			"tasks: 12",
			"iterations: 47",
			"context: 12.5k tokens",
			"last activity: 3s ago",
		}},
		{Title: "Hierarchy", Lines: []string{
			"parent: none",
			"children: 2",
		}},
	}

	// Use a small height that forces scrolling (header takes 1-2 rows, so body is tiny).
	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 5,
		WithHasEntity(true),
		WithDPFocused(true),
	)

	// Verify there is scrollable content.
	totalLines := dp.totalContentLines()
	if totalLines <= 3 {
		t.Errorf("expected totalContentLines > 3 for scrolling, got %d", totalLines)
	}

	maxScroll := dp.maxScrollOffset()
	if maxScroll <= 0 {
		t.Errorf("expected maxScrollOffset > 0 for overflow, got %d", maxScroll)
	}

	// Scroll down.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if dp.ScrollOffset() <= 0 {
		t.Errorf("after ↓, scrollOffset should be > 0, got %d", dp.ScrollOffset())
	}

	// Scroll up.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyUp})
	if dp.ScrollOffset() != 0 {
		t.Errorf("after ↑ from near-top, scrollOffset should be 0, got %d", dp.ScrollOffset())
	}
}

func TestDetailPaneScrollKeysWhenContentFits(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: []string{"one line"}},
	}

	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 20,
		WithHasEntity(true),
		WithDPFocused(true),
	)

	// Content fits in viewport; scroll keys should be no-ops.
	initialOffset := dp.ScrollOffset()
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if dp.ScrollOffset() != initialOffset {
		t.Errorf("scroll ↓ with no overflow should be no-op, offset changed from %d to %d", initialOffset, dp.ScrollOffset())
	}
}

func TestDetailPanePageDownPageUp(t *testing.T) {
	t.Parallel()

	// Create enough content for multiple pages.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "content line"
	}
	sections := []Section{
		{Title: "Details", Lines: lines},
	}

	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 8,
		WithHasEntity(true),
		WithDPFocused(true),
	)

	// PageDown should scroll by bodyHeight.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyPageDown})
	off1 := dp.ScrollOffset()
	if off1 <= 0 {
		t.Errorf("PageDown: expected scrollOffset > 0, got %d", off1)
	}

	// PageUp should scroll back.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyPageUp})
	if dp.ScrollOffset() != 0 {
		t.Errorf("PageUp: expected scrollOffset=0, got %d", dp.ScrollOffset())
	}
}

func TestDetailPaneHomeEnd(t *testing.T) {
	t.Parallel()

	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "content line"
	}
	sections := []Section{
		{Title: "Details", Lines: lines},
	}

	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 8,
		WithHasEntity(true),
		WithDPFocused(true),
	)

	// End scrolls to bottom.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyEnd})
	if dp.ScrollOffset() != dp.maxScrollOffset() {
		t.Errorf("End: expected scrollOffset=%d, got %d", dp.maxScrollOffset(), dp.ScrollOffset())
	}

	// Home scrolls to top.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyHome})
	if dp.ScrollOffset() != 0 {
		t.Errorf("Home: expected scrollOffset=0, got %d", dp.ScrollOffset())
	}
}

func TestDetailPaneHomeEndViKeys(t *testing.T) {
	t.Parallel()

	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "content line"
	}
	sections := []Section{
		{Title: "Details", Lines: lines},
	}

	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 8,
		WithHasEntity(true),
		WithDPFocused(true),
	)

	// 'G' scrolls to bottom.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'G'})
	if dp.ScrollOffset() != dp.maxScrollOffset() {
		t.Errorf("'G': expected scrollOffset=%d, got %d", dp.maxScrollOffset(), dp.ScrollOffset())
	}

	// 'g' scrolls to top.
	dp.HandleKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'g'})
	if dp.ScrollOffset() != 0 {
		t.Errorf("'g': expected scrollOffset=0, got %d", dp.ScrollOffset())
	}
}

// --- Tests: Scroll indicator ---

func TestDetailPaneScrollIndicator(t *testing.T) {
	t.Parallel()

	// Create enough content to overflow.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "content line"
	}
	sections := []Section{
		{Title: "Details", Lines: lines},
	}

	mt := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 8,
		WithDPFocused(true),
		WithScrollOffset(5),
	)

	// When scrolled, a scroll indicator should be visible.
	// Check for indicator runes like '│', '▾', '▸', or a scrollbar thumb.
	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("expected non-empty render")
	}

	// We check the rightmost column for scrollbar indicators.
	hasIndicator := false
	for y := 1; y < 8; y++ {
		cell := mt.CellAt(39, y) // rightmost column
		if cell.Rune != 0 && cell.Rune != ' ' {
			// Non-empty cell on the right edge indicates scrollbar.
			hasIndicator = true
			break
		}
	}
	if !hasIndicator {
		// Also check for a scroll indicator character anywhere.
		for y := 1; y < 8; y++ {
			cell := mt.CellAt(39, y)
			if cell.Rune == '│' || cell.Rune == '▾' || cell.Rune == '▸' || cell.Rune == '┃' {
				hasIndicator = true
				break
			}
		}
	}
	if !hasIndicator {
		t.Errorf("scroll indicator should be visible when content is scrolled")
	}
}

// --- Tests: Empty state (no entity) ---

func TestDetailPaneEmptyNoEntity(t *testing.T) {
	t.Parallel()
	mt := renderDetailPane("agent-abc123", StatusNone, nil, 40, 10,
		WithHasEntity(false),
	)

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("DetailPane with HasEntity=false should render empty state")
	}

	// Should contain the empty-state guidance copy per component-spec.md.
	if !strings.Contains(content, "Select") {
		t.Errorf("empty state should contain 'Select' guidance, got: %s", content)
	}
}

// --- Tests: Empty state (no sections) ---

func TestDetailPaneEmptyNoSections(t *testing.T) {
	t.Parallel()
	mt := renderDetailPaneEntity("agent-abc123", StatusOK, nil, 40, 10)

	content := mt.StringTrimmed()
	if content == "" {
		t.Fatal("DetailPane with no sections should render something")
	}

	// Should show "No details available." per component-spec.md.
	if !strings.Contains(content, "No details") {
		t.Errorf("empty entity with no sections should show 'No details' message, got: %s", content)
	}
}

// --- Tests: Focus indicator ---

func TestDetailPaneFocusIndicator(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: []string{"data"}},
	}

	// Render with focus.
	mt := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 10,
		WithDPFocused(true),
	)

	// The focused pane should have a visible focus indicator — colored left
	// border or colored top border. Check that some cell has BorderFocus color.
	hasBorderFocus := false
outer:
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			cell := mt.CellAt(x, y)
			if cell.Style.Fg.Equal(theme.Colors.BorderFocus) ||
				cell.Style.Bg.Equal(theme.Colors.BorderFocus) {
				hasBorderFocus = true
				break outer
			}
		}
	}
	if !hasBorderFocus {
		t.Errorf("focused DetailPane should have a cell with BorderFocus color")
	}
}

func TestDetailPaneUnfocusedNoBorderFocus(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: []string{"data"}},
	}

	// Render without focus.
	mt := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 10,
		WithDPFocused(false),
	)

	// The unfocused pane should NOT have BorderFocus color on any cell.
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			cell := mt.CellAt(x, y)
			if cell.Style.Fg.Equal(theme.Colors.BorderFocus) {
				t.Errorf("unfocused DetailPane should NOT have BorderFocus fg at (%d,%d)", x, y)
			}
		}
	}
}

func TestDetailPaneFocusIndicatorNotFontWeightOnly(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: []string{"data"}},
	}

	// Focused.
	mtF := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 10,
		WithDPFocused(true),
	)

	// Find a cell that differs from the unfocused version.
	mtU := renderDetailPaneEntity("agent-abc123", StatusOK, sections, 40, 10,
		WithDPFocused(false),
	)

	// There should be at least one cell where focus changes the appearance
	// beyond just bold. Look for color difference.
	anyColorDiff := false
	for y := 0; y < 10; y++ {
		for x := 0; x < 40; x++ {
			fc := mtF.CellAt(x, y)
			uc := mtU.CellAt(x, y)
			if !fc.Style.Fg.Equal(uc.Style.Fg) || !fc.Style.Bg.Equal(uc.Style.Bg) {
				anyColorDiff = true
				break
			}
		}
		if anyColorDiff {
			break
		}
	}
	if !anyColorDiff {
		t.Errorf("focused vs unfocused should differ in color, not just font-weight")
	}
}

// --- Tests: Unfocused ignores keys ---

func TestDetailPaneUnfocusedIgnoresKeys(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: make([]string, 20)},
	}
	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 8,
		WithHasEntity(true),
		WithDPFocused(false),
	)

	dp.HandleKey(tui.KeyEvent{Key: tui.KeyDown})
	if dp.ScrollOffset() != 0 {
		t.Errorf("unfocused ↓ should not scroll, got offset=%d", dp.ScrollOffset())
	}
}

// --- Tests: Status variants in header ---

func TestDetailPaneStatusVariants(t *testing.T) {
	t.Parallel()
	sections := []Section{{Title: "Info", Lines: []string{"data"}}}

	variants := []StatusVariant{StatusOK, StatusWarn, StatusError, StatusPending}
	for _, sv := range variants {
		mt := renderDetailPaneEntity("agent-x", sv, sections, 40, 10, WithDPFocused(true))
		content := mt.StringTrimmed()
		if content == "" {
			t.Errorf("StatusVariant %v: expected non-empty render", sv)
		}
	}
}

// --- Tests: Scroll offset clamping ---

func TestDetailPaneScrollOffsetClamped(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: make([]string, 30)},
	}

	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 8,
		WithHasEntity(true),
		WithDPFocused(true),
		WithScrollOffset(9999), // way past max
	)

	// ScrollOffset should be clamped to maxScrollOffset.
	maxOff := dp.maxScrollOffset()
	if dp.ScrollOffset() != maxOff {
		t.Errorf("scrollOffset should be clamped to maxScrollOffset=%d, got %d", maxOff, dp.ScrollOffset())
	}
}

func TestDetailPaneNegativeScrollOffsetClamped(t *testing.T) {
	t.Parallel()
	sections := []Section{
		{Title: "Info", Lines: make([]string, 30)},
	}

	dp := NewDetailPane("agent-abc123", StatusOK, sections, 40, 8,
		WithHasEntity(true),
		WithDPFocused(true),
		WithScrollOffset(-5),
	)

	if dp.ScrollOffset() != 0 {
		t.Errorf("negative scrollOffset should be clamped to 0, got %d", dp.ScrollOffset())
	}
}
