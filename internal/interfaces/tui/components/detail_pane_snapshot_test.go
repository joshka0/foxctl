package components

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// --- DetailPane snapshot tests ---
// These tests capture rendered output for tuistory evidence. Snapshots are
// written to testdata/snapshots/ and correspond to the four snapshots
// required by VAL-CMP-005:
//   - detailpane-populated
//   - detailpane-empty
//   - detailpane-scrolled
//   - detailpane-truncated-title

func TestSnapshotDetailPanePopulated(t *testing.T) {
	sections := []Section{
		{Title: "Runtime", Lines: []string{
			"state: running",
			"provider: openrouter",
			"model: aurora-alpha",
			"uptime: 2h30m",
		}},
		{Title: "Hierarchy", Lines: []string{
			"parent: none",
			"children: 2",
		}},
		{Title: "Recent Activity", Lines: []string{
			"ask: 'review git diff' → done",
			"ask: 'fix lint errors' → running",
		}},
	}

	mt := renderDetailPaneEntity("agent-abc12345", StatusOK, sections, 50, 15,
		WithDPFocused(true),
	)

	snapshot := mt.StringTrimmed()
	writeSnapshot(t, "detailpane-populated.txt", snapshot)
}

func TestSnapshotDetailPaneEmpty(t *testing.T) {
	mt := renderDetailPane("agent-abc12345", StatusNone, nil, 50, 15,
		WithHasEntity(false),
	)

	snapshot := mt.StringTrimmed()
	writeSnapshot(t, "detailpane-empty.txt", snapshot)
}

func TestSnapshotDetailPaneScrolled(t *testing.T) {
	// Create enough content to require scrolling.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "detail content line"
	}
	sections := []Section{
		{Title: "Details", Lines: lines},
	}

	dp := NewDetailPane("agent-abc12345", StatusOK, sections, 50, 10,
		WithHasEntity(true),
		WithDPFocused(true),
		WithScrollOffset(10), // scrolled down
	)

	mt := tui.NewMockTerminal(50, 10)
	buf := tui.NewBuffer(50, 10)
	dp.Render(buf)
	tui.RenderFull(mt, buf)

	snapshot := mt.StringTrimmed()
	writeSnapshot(t, "detailpane-scrolled.txt", snapshot)

	// Verify scrollbar is visible when scrolled.
	hasScrollbar := false
	for y := 1; y < 10; y++ {
		cell := mt.CellAt(49, y)
		if cell.Rune == '┃' {
			hasScrollbar = true
			break
		}
	}
	if !hasScrollbar {
		t.Errorf("scrolled DetailPane should show scrollbar thumb '┃'")
	}
}

func TestSnapshotDetailPaneTruncatedTitle(t *testing.T) {
	longTitle := "agent-with-a-very-long-name-that-exceeds-the-width-of-the-detail-pane-header"
	sections := []Section{
		{Title: "Info", Lines: []string{"some detail"}},
	}

	// Use narrow width to force title truncation.
	mt := renderDetailPaneEntity(longTitle, StatusWarn, sections, 25, 10,
		WithDPFocused(true),
	)

	snapshot := mt.StringTrimmed()
	writeSnapshot(t, "detailpane-truncated-title.txt", snapshot)

	// Verify the ellipsis character appears in the header row.
	hasEllipsis := false
	for x := 0; x < 25; x++ {
		cell := mt.CellAt(x, 0)
		if cell.Rune == '…' {
			hasEllipsis = true
			break
		}
	}
	if !hasEllipsis {
		t.Errorf("truncated title snapshot should contain '…' in header row")
	}
}

// TestSnapshotDetailPaneFocusedVsUnfocused verifies the focus indicator is
// visually distinguishable between focused and unfocused states.
func TestSnapshotDetailPaneFocusedVsUnfocused(t *testing.T) {
	sections := []Section{
		{Title: "Info", Lines: []string{"data"}},
	}

	mtF := renderDetailPaneEntity("agent-x", StatusOK, sections, 40, 10,
		WithDPFocused(true),
	)
	mtU := renderDetailPaneEntity("agent-x", StatusOK, sections, 40, 10,
		WithDPFocused(false),
	)

	fc := mtF.CellAt(0, 0)
	uc := mtU.CellAt(0, 0)

	// Focused should have BorderFocus color; unfocused should not.
	if !fc.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("focused CellAt(0,0) should have BorderFocus fg, got %v", fc.Style.Fg)
	}
	if uc.Style.Fg.Equal(theme.Colors.BorderFocus) {
		t.Errorf("unfocused CellAt(0,0) should NOT have BorderFocus fg")
	}
}

// writeSnapshot is a shared helper that writes a snapshot string to the
// testdata/snapshots/ directory.
func writeSnapshot(t *testing.T, name, content string) {
	t.Helper()
	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, name)
	if err := os.WriteFile(snapPath, []byte(content+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}
	t.Logf("Snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", content)
}
