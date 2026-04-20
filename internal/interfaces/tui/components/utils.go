package components

import (
	"strings"

	"github.com/grindlemire/go-tui"
)

// truncate truncates s so that its display width does not exceed maxCells
// terminal cells. If truncation is needed, an ellipsis "…" is appended.
func truncate(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	totalWidth := 0
	for i, r := range s {
		rw := int(tui.RuneWidth(r))
		if totalWidth+rw > maxCells {
			// Need to truncate. Try to fit an ellipsis.
			if maxCells >= 1 {
				// Back up to make room for "…" (1 cell).
				ellipsisWidth := int(tui.RuneWidth('…')) // always 1
				availForContent := maxCells - ellipsisWidth
				if availForContent <= 0 {
					return "…"
				}
				// Re-truncate to fit content + ellipsis.
				w := 0
				for j, rr := range s {
					rrw := int(tui.RuneWidth(rr))
					if w+rrw > availForContent {
						return string([]rune(s[:j])) + "…"
					}
					w += rrw
				}
			}
			return string([]rune(s[:i])) + "…"
		}
		totalWidth += rw
	}
	return s
}

// runeWidth returns the number of terminal cells occupied by s.
func runeWidth(s string) int {
	w := 0
	for _, r := range s {
		w += int(tui.RuneWidth(r))
	}
	return w
}

// center pads s to width cells with spaces on both sides, respecting display
// width rather than rune count.
func center(s string, width int) string {
	if width <= 0 {
		return s
	}
	sw := runeWidth(s)
	if sw >= width {
		// String already fills or exceeds width; truncate to fit.
		return truncate(s, width)
	}
	pad := (width - sw) / 2
	leftPad := strings.Repeat(" ", pad)
	rightPad := strings.Repeat(" ", width-sw-pad)
	return leftPad + s + rightPad
}

// padOrTruncate pads s to exactly width display cells with trailing spaces, or
// truncates with … if s exceeds width in display width.
func padOrTruncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	sw := runeWidth(s)
	if sw > width {
		if width <= 1 {
			return "…"
		}
		return truncate(s, width)
	}
	// Pad with spaces to reach exactly width display cells.
	if sw < width {
		return s + strings.Repeat(" ", width-sw)
	}
	return s
}
