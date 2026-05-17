package components

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grindlemire/go-tui"
)

// ---------------------------------------------------------------------------
// Test strings for unicode categories per VAL-CMP-012:
//   (a) CJK ideographs
//   (b) combining diacritical marks
//   (c) zero-width-joiner emoji
//   (d) long RTL-only string
// ---------------------------------------------------------------------------

const (
	// (a) CJK: each character is 2 cells wide.
	cjkString = "研究員エージェント"

	// (b) Combining diacritical marks: base letter + combining accent.
	combiningString = "Age\u0301nt" // "Agént" with combining acute

	// (c) Zero-width-joiner emoji: person + ZWJ + laptop → should be 2 cells.
	zwjEmoji = "\U0001F468\u200D\U0001F4BB" // man technologist

	// (d) Long RTL-only string (Arabic).
	rtlString = "الباحثالعاملالعربي"
)

// TestUnicodeWidth is the top-level test for VAL-CMP-012. It runs subtests
// for each widget × unicode category combination. Discoverable via
// `go test -run Unicode`.
func TestUnicodeWidth(t *testing.T) {
	t.Run("EntityList", func(t *testing.T) {
		t.Run("CJK_NoPanic", func(t *testing.T) {
			items := []Entity{
				{ID: "cjk1", Label: cjkString, SubLabel: "研究"},
			}
			mt := renderEntityList(items, 30, 5, WithSelectedIndex(0), WithFocused(true))
			if mt == nil {
				t.Fatal("renderEntityList returned nil")
			}
		})
		t.Run("CJK_RowWidth", func(t *testing.T) {
			width := 30
			items := []Entity{
				{ID: "cjk1", Label: cjkString, SubLabel: "研究"},
			}
			mt := renderEntityList(items, width, 5, WithSelectedIndex(0), WithFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "EntityList CJK")
		})
		t.Run("CJK_NoCellOverlap", func(t *testing.T) {
			width := 30
			items := []Entity{
				{ID: "cjk1", Label: cjkString, SubLabel: "研究"},
			}
			mt := renderEntityList(items, width, 5, WithSelectedIndex(0), WithFocused(true))
			assertNoWideCharOverlap(t, mt, width, 5, "EntityList CJK")
		})
		t.Run("Combining_NoPanic", func(t *testing.T) {
			items := []Entity{
				{ID: "comb1", Label: combiningString, SubLabel: "active"},
			}
			mt := renderEntityList(items, 30, 5, WithSelectedIndex(0), WithFocused(true))
			if mt == nil {
				t.Fatal("renderEntityList returned nil")
			}
		})
		t.Run("Combining_RowWidth", func(t *testing.T) {
			width := 30
			items := []Entity{
				{ID: "comb1", Label: combiningString, SubLabel: "active"},
			}
			mt := renderEntityList(items, width, 5, WithSelectedIndex(0), WithFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "EntityList combining")
		})
		t.Run("ZWJ_NoPanic", func(t *testing.T) {
			items := []Entity{
				{ID: "zwj1", Label: "dev " + zwjEmoji, SubLabel: "coding"},
			}
			mt := renderEntityList(items, 30, 5, WithSelectedIndex(0), WithFocused(true))
			if mt == nil {
				t.Fatal("renderEntityList returned nil")
			}
		})
		t.Run("ZWJ_RowWidth", func(t *testing.T) {
			width := 30
			items := []Entity{
				{ID: "zwj1", Label: "dev " + zwjEmoji, SubLabel: "coding"},
			}
			mt := renderEntityList(items, width, 5, WithSelectedIndex(0), WithFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "EntityList ZWJ emoji")
		})
		t.Run("RTL_NoPanic", func(t *testing.T) {
			items := []Entity{
				{ID: "rtl1", Label: rtlString, SubLabel: "active"},
			}
			mt := renderEntityList(items, 50, 5, WithSelectedIndex(0), WithFocused(true))
			if mt == nil {
				t.Fatal("renderEntityList returned nil")
			}
		})
		t.Run("RTL_RowWidth", func(t *testing.T) {
			width := 50
			items := []Entity{
				{ID: "rtl1", Label: rtlString, SubLabel: "active"},
			}
			mt := renderEntityList(items, width, 5, WithSelectedIndex(0), WithFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "EntityList RTL")
		})
		t.Run("CJK_SubLabelNoOverlap", func(t *testing.T) {
			width := 25
			items := []Entity{
				{ID: "cjk1", Label: "研究員", SubLabel: "active"},
			}
			mt := renderEntityList(items, width, 5, WithSelectedIndex(0), WithFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "EntityList CJK sublabel")
			assertNoWideCharOverlap(t, mt, width, 5, "EntityList CJK sublabel")
		})
	})

	t.Run("DetailPane", func(t *testing.T) {
		t.Run("CJK_NoPanic", func(t *testing.T) {
			sections := []Section{
				{Title: cjkString, Lines: []string{"ステータス: アクティブ"}},
			}
			mt := renderDetailPaneEntity(cjkString, StatusOK, sections, 40, 10, WithDPFocused(true))
			if mt == nil {
				t.Fatal("renderDetailPaneEntity returned nil")
			}
		})
		t.Run("CJK_RowWidth", func(t *testing.T) {
			width := 40
			sections := []Section{
				{Title: cjkString, Lines: []string{"ステータス: アクティブ"}},
			}
			mt := renderDetailPaneEntity(cjkString, StatusOK, sections, width, 10, WithDPFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "DetailPane CJK")
		})
		t.Run("Combining_NoPanic", func(t *testing.T) {
			sections := []Section{
				{Title: combiningString, Lines: []string{"Status: active"}},
			}
			mt := renderDetailPaneEntity(combiningString, StatusOK, sections, 40, 10, WithDPFocused(true))
			if mt == nil {
				t.Fatal("renderDetailPaneEntity returned nil")
			}
		})
		t.Run("ZWJ_NoPanic", func(t *testing.T) {
			sections := []Section{
				{Title: "Developer " + zwjEmoji, Lines: []string{"Working hard"}},
			}
			mt := renderDetailPaneEntity("Developer "+zwjEmoji, StatusOK, sections, 40, 10, WithDPFocused(true))
			if mt == nil {
				t.Fatal("renderDetailPaneEntity returned nil")
			}
		})
		t.Run("RTL_NoPanic", func(t *testing.T) {
			sections := []Section{
				{Title: rtlString, Lines: []string{"حالة: نشط"}},
			}
			mt := renderDetailPaneEntity(rtlString, StatusOK, sections, 60, 10, WithDPFocused(true))
			if mt == nil {
				t.Fatal("renderDetailPaneEntity returned nil")
			}
		})
		t.Run("RTL_RowWidth", func(t *testing.T) {
			width := 60
			sections := []Section{
				{Title: rtlString, Lines: []string{"حالة: نشط"}},
			}
			mt := renderDetailPaneEntity(rtlString, StatusOK, sections, width, 10, WithDPFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "DetailPane RTL")
		})
	})

	t.Run("StreamViewer", func(t *testing.T) {
		t.Run("CJK_NoPanic", func(t *testing.T) {
			lines := []string{cjkString, "行2: 出力ストリーム", "行3: 終了"}
			_, mt := renderStreamViewer(lines, 40, 5, WithSVFocused(true))
			if mt == nil {
				t.Fatal("renderStreamViewer returned nil")
			}
		})
		t.Run("CJK_RowWidth", func(t *testing.T) {
			width := 20
			lines := []string{cjkString, "行2"}
			_, mt := renderStreamViewer(lines, width, 5, WithSVFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "StreamViewer CJK")
		})
		t.Run("Combining_NoPanic", func(t *testing.T) {
			lines := []string{combiningString, "Status: ok"}
			_, mt := renderStreamViewer(lines, 30, 5, WithSVFocused(true))
			if mt == nil {
				t.Fatal("renderStreamViewer returned nil")
			}
		})
		t.Run("Combining_RowWidth", func(t *testing.T) {
			width := 30
			lines := []string{combiningString, "Status: ok"}
			_, mt := renderStreamViewer(lines, width, 5, WithSVFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "StreamViewer combining")
		})
		t.Run("ZWJ_NoPanic", func(t *testing.T) {
			lines := []string{"User " + zwjEmoji + " typing", "Output line"}
			_, mt := renderStreamViewer(lines, 40, 5, WithSVFocused(true))
			if mt == nil {
				t.Fatal("renderStreamViewer returned nil")
			}
		})
		t.Run("RTL_NoPanic", func(t *testing.T) {
			lines := []string{rtlString, "English line after RTL"}
			_, mt := renderStreamViewer(lines, 60, 5, WithSVFocused(true))
			if mt == nil {
				t.Fatal("renderStreamViewer returned nil")
			}
		})
		t.Run("RTL_RowWidth", func(t *testing.T) {
			width := 60
			lines := []string{rtlString}
			_, mt := renderStreamViewer(lines, width, 5, WithSVFocused(true))
			assertNoRowExceedsWidth(t, mt, width, "StreamViewer RTL")
		})
	})

	t.Run("StatusBadge", func(t *testing.T) {
		t.Run("CJK_NoPanic", func(t *testing.T) {
			sb := NewStatusBadge(StatusOK, cjkString, 0)
			mt := renderSmallWidget(sb, 30, 1)
			if mt == nil {
				t.Fatal("renderSmallWidget returned nil")
			}
		})
		t.Run("CJK_RowWidth", func(t *testing.T) {
			width := 12
			sb := NewStatusBadge(StatusOK, cjkString, width)
			mt := renderSmallWidget(sb, width, 1)
			assertNoRowExceedsWidth(t, mt, width, "StatusBadge CJK")
		})
		t.Run("Combining_NoPanic", func(t *testing.T) {
			sb := NewStatusBadge(StatusOK, combiningString, 0)
			mt := renderSmallWidget(sb, 20, 1)
			if mt == nil {
				t.Fatal("renderSmallWidget returned nil")
			}
		})
		t.Run("ZWJ_NoPanic", func(t *testing.T) {
			sb := NewStatusBadge(StatusOK, "dev "+zwjEmoji, 0)
			mt := renderSmallWidget(sb, 20, 1)
			if mt == nil {
				t.Fatal("renderSmallWidget returned nil")
			}
		})
		t.Run("RTL_NoPanic", func(t *testing.T) {
			sb := NewStatusBadge(StatusOK, rtlString, 0)
			mt := renderSmallWidget(sb, 40, 1)
			if mt == nil {
				t.Fatal("renderSmallWidget returned nil")
			}
		})
		t.Run("RTL_RowWidth", func(t *testing.T) {
			width := 20
			sb := NewStatusBadge(StatusOK, rtlString, width)
			mt := renderSmallWidget(sb, width, 1)
			assertNoRowExceedsWidth(t, mt, width, "StatusBadge RTL")
		})
	})
}

// TestUnicodeTruncateWidth verifies that the truncate helper respects display
// width, not rune count.
func TestUnicodeTruncateWidth(t *testing.T) {
	t.Parallel()

	// CJK "研究員" = 6 display cells. truncate with 5 should produce ≤5 cells.
	result := truncate(cjkString, 5)
	dw := runeWidth(result)
	if dw > 5 {
		t.Errorf("truncate(%q, 5): display width %d exceeds 5 (result: %q)", cjkString, dw, result)
	}

	// ASCII: truncate("hello world", 5) should be ≤5 cells.
	result = truncate("hello world", 5)
	dw = runeWidth(result)
	if dw > 5 {
		t.Errorf("truncate('hello world', 5): display width %d exceeds 5 (result: %q)", dw, result)
	}

	// Exact width CJK: "研究" = 4 cells. truncate with 4 should return unchanged.
	result = truncate("研究", 4)
	if result != "研究" {
		t.Errorf("truncate('研究', 4): expected unchanged, got %q", result)
	}

	// Truncation CJK: "研究員" = 6 cells. truncate with 4 should give ≤4 cells.
	result = truncate("研究員", 4)
	dw = runeWidth(result)
	if dw > 4 {
		t.Errorf("truncate('研究員', 4): display width %d exceeds 4 (result: %q)", dw, result)
	}
}

// TestUnicodePadOrTruncateWidth verifies padOrTruncate respects display width.
func TestUnicodePadOrTruncateWidth(t *testing.T) {
	t.Parallel()

	result := padOrTruncate(cjkString, 8)
	dw := runeWidth(result)
	if dw > 8 {
		t.Errorf("padOrTruncate(%q, 8): display width %d exceeds 8 (result: %q)", cjkString, dw, result)
	}
}

// TestUnicodeCenterWidth verifies center respects display width.
func TestUnicodeCenterWidth(t *testing.T) {
	t.Parallel()

	result := center("研究", 10)
	dw := runeWidth(result)
	if dw > 10 {
		t.Errorf("center('研究', 10): display width %d exceeds 10 (result: %q)", dw, result)
	}
}

// TestSnapshotEntityListCJKRoleLabel produces a tuistory-compatible snapshot
// with CJK agent role labels visible in an EntityList. VAL-CMP-012 requires
// at least one such snapshot.
func TestSnapshotEntityListCJKRoleLabel(t *testing.T) {
	items := []Entity{
		{ID: "cjk1", Label: "agent-研究員", SubLabel: "active"},
		{ID: "cjk2", Label: "agent-コーダー", SubLabel: "idle"},
		{ID: "cjk3", Label: "agent-計画者", SubLabel: "busy"},
	}

	mt := renderEntityList(
		items, 35, 5,
		WithSelectedIndex(0),
		WithFocused(true),
	)

	snapshot := mt.StringTrimmed()

	// Write snapshot to testdata.
	snapDir := filepath.Join("testdata", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		t.Fatalf("failed to create snapshot dir: %v", err)
	}
	snapPath := filepath.Join(snapDir, "entitylist-cjk-role-label.txt")
	if err := os.WriteFile(snapPath, []byte(snapshot+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	t.Logf("CJK snapshot written to %s", snapPath)
	t.Logf("Content:\n%s", snapshot)

	// Verify no row exceeds the declared width.
	assertNoRowExceedsWidth(t, mt, 35, "EntityList CJK snapshot")
}

// --- assertion helpers ---

// assertNoRowExceedsWidth checks that no cell beyond width-1 contains a non-space
// character. This verifies that row length respects declared widget width.
func assertNoRowExceedsWidth(t *testing.T, mt *tui.MockTerminal, width int, label string) {
	t.Helper()
	mtW, mtH := mt.Size()
	for y := 0; y < mtH; y++ {
		for x := width; x < mtW; x++ {
			cell := mt.CellAt(x, y)
			if cell.Rune != 0 && cell.Rune != ' ' {
				t.Errorf("%s: cell at (%d,%d) has rune %q outside declared width %d",
					label, x, y, string(cell.Rune), width)
				return
			}
		}
	}
}

// assertNoWideCharOverlap verifies that wide characters don't cause overlap
// by checking that continuation cells don't have content in the next column
// that belongs to a different write.
func assertNoWideCharOverlap(t *testing.T, mt *tui.MockTerminal, width, height int, label string) {
	t.Helper()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := mt.CellAt(x, y)
			// If this is a wide char (width=2), the next cell must be a continuation (width=0).
			if cell.Width == 2 && x+1 < width {
				nextCell := mt.CellAt(x+1, y)
				if nextCell.Width != 0 {
					t.Errorf("%s: wide char %q at (%d,%d) but next cell (%d,%d) has width %d (not continuation)",
						label, string(cell.Rune), x, y, x+1, y, nextCell.Width)
				}
			}
		}
	}
}
