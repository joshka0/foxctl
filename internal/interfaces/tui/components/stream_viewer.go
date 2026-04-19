package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// StreamViewer is a scrollable, append-only content viewer for streaming text
// (token replies, event streams). It implements sticky-bottom (follow-tail)
// behavior with automatic disengage on scroll-up.
//
// "At bottom" is defined as: scrollOffset + height >= len(lines). When
// follow-tail is engaged and new lines are appended, the viewer auto-scrolls
// so the last line is always visible. When the user scrolls up by ≥1 line,
// follow-tail disengages and the absolute scroll anchor is preserved across
// new item arrivals.
//
// Follow-tail re-engages when the user presses End or 'G'.
//
// Theme tokens are used for all colors; no raw color literals appear in this
// file.
type StreamViewer struct {
	lines        []string
	width        int
	height       int
	scrollOffset int
	followTail   bool
	focused      bool
	maxLines     int
}

// StreamViewerOption configures a StreamViewer at construction time.
type StreamViewerOption func(*StreamViewer)

// WithSVFocused sets the initial focus state.
func WithSVFocused(f bool) StreamViewerOption {
	return func(sv *StreamViewer) { sv.focused = f }
}

// WithSVScrollOffset sets the initial scroll offset (for test use).
func WithSVScrollOffset(offset int) StreamViewerOption {
	return func(sv *StreamViewer) { sv.scrollOffset = offset }
}

// WithSVFollowTail sets the initial follow-tail state.
func WithSVFollowTail(f bool) StreamViewerOption {
	return func(sv *StreamViewer) { sv.followTail = f }
}

// WithSVMaxLines sets the maximum number of retained lines. Older lines are
// dropped when content exceeds this limit.
func WithSVMaxLines(n int) StreamViewerOption {
	return func(sv *StreamViewer) { sv.maxLines = n }
}

// NewStreamViewer creates a new StreamViewer with the given content lines and
// dimensions. Follow-tail defaults to true.
func NewStreamViewer(lines []string, width, height int, opts ...StreamViewerOption) *StreamViewer {
	sv := &StreamViewer{
		lines:      lines,
		width:      width,
		height:     height,
		followTail: true,
		maxLines:   10000, // default per spec
	}
	for _, opt := range opts {
		opt(sv)
	}
	sv.enforceMaxLines()
	// If follow-tail is engaged, start at the bottom.
	if sv.followTail {
		sv.scrollToBottom()
	}
	sv.clampScroll()
	return sv
}

// Lines returns the current content lines.
func (sv *StreamViewer) Lines() []string {
	return sv.lines
}

// ScrollOffset returns the current vertical scroll offset (first visible line index).
func (sv *StreamViewer) ScrollOffset() int {
	return sv.scrollOffset
}

// FollowTail reports whether auto-scroll to bottom is engaged.
func (sv *StreamViewer) FollowTail() bool {
	return sv.followTail
}

// SetLines replaces the content lines. If follow-tail is engaged, the
// viewport scrolls to show the new bottom. If follow-tail is disengaged,
// the scroll anchor is preserved.
func (sv *StreamViewer) SetLines(lines []string) {
	sv.lines = lines
	sv.enforceMaxLines()

	if sv.followTail {
		sv.scrollToBottom()
	}
	// When follow is disengaged, scrollOffset stays the same (anchor preserved).
	sv.clampScroll()
}

// AppendLines adds new lines to the end of the content. Follows the same
// follow-tail behavior as SetLines.
func (sv *StreamViewer) AppendLines(newLines []string) {
	sv.SetLines(append(sv.lines, newLines...))
}

// HandleKey processes a key event. Returns true if the key was consumed.
// When focused:
//   - ↑/k: scroll up 1 line, disengage follow-tail
//   - ↓/j: scroll down 1 line
//   - PageUp/Ctrl+U: scroll up by height lines, disengage follow-tail
//   - PageDown/Ctrl+D: scroll down by height lines
//   - Home/g: scroll to top, disengage follow-tail
//   - End/G: scroll to bottom, re-engage follow-tail
func (sv *StreamViewer) HandleKey(evt tui.KeyEvent) bool {
	if !sv.focused {
		return false
	}
	n := len(sv.lines)
	if n == 0 {
		return false
	}

	switch {
	case evt.Key == tui.KeyDown || (evt.Key == tui.KeyRune && evt.Rune == 'j'):
		return sv.scrollDown(1)
	case evt.Key == tui.KeyUp || (evt.Key == tui.KeyRune && evt.Rune == 'k'):
		return sv.scrollUp(1)
	case evt.Key == tui.KeyPageDown || (evt.Key == tui.KeyRune && evt.Rune == 'd' && evt.Mod.Has(tui.ModCtrl)):
		return sv.scrollDown(sv.height)
	case evt.Key == tui.KeyPageUp || (evt.Key == tui.KeyRune && evt.Rune == 'u' && evt.Mod.Has(tui.ModCtrl)):
		return sv.scrollUp(sv.height)
	case evt.Key == tui.KeyHome || (evt.Key == tui.KeyRune && evt.Rune == 'g'):
		sv.scrollOffset = 0
		sv.followTail = false
		return true
	case evt.Key == tui.KeyEnd || (evt.Key == tui.KeyRune && evt.Rune == 'G'):
		sv.followTail = true
		sv.scrollToBottom()
		return true
	}
	return false
}

// scrollUp scrolls up by n lines and disengages follow-tail.
func (sv *StreamViewer) scrollUp(n int) bool {
	sv.followTail = false
	sv.scrollOffset -= n
	if sv.scrollOffset < 0 {
		sv.scrollOffset = 0
	}
	return true
}

// scrollDown scrolls down by n lines.
func (sv *StreamViewer) scrollDown(n int) bool {
	maxScroll := sv.maxScroll()
	sv.scrollOffset += n
	if sv.scrollOffset >= maxScroll {
		// Reached bottom → re-engage follow-tail.
		sv.scrollOffset = maxScroll
		sv.followTail = true
	}
	return true
}

// scrollToBottom sets scrollOffset so the last line is visible.
func (sv *StreamViewer) scrollToBottom() {
	sv.scrollOffset = sv.maxScroll()
}

// maxScroll returns the maximum valid scroll offset.
func (sv *StreamViewer) maxScroll() int {
	n := len(sv.lines)
	max := n - sv.height
	if max < 0 {
		max = 0
	}
	return max
}

// clampScroll ensures scrollOffset is within valid range.
func (sv *StreamViewer) clampScroll() {
	max := sv.maxScroll()
	if sv.scrollOffset > max {
		sv.scrollOffset = max
	}
	if sv.scrollOffset < 0 {
		sv.scrollOffset = 0
	}
}

// enforceMaxLines drops older lines when content exceeds maxLines.
func (sv *StreamViewer) enforceMaxLines() {
	if sv.maxLines <= 0 {
		return
	}
	if len(sv.lines) > sv.maxLines {
		excess := len(sv.lines) - sv.maxLines
		sv.lines = sv.lines[excess:]
	}
}

// Render draws the StreamViewer into the given buffer.
func (sv *StreamViewer) Render(buf *tui.Buffer) {
	if sv.width <= 0 || sv.height <= 0 {
		return
	}

	// Empty state.
	if len(sv.lines) == 0 {
		sv.renderEmpty(buf)
		return
	}

	// Normal rendering.
	sv.renderContent(buf)
}

// renderEmpty draws the empty/waiting state message.
func (sv *StreamViewer) renderEmpty(buf *tui.Buffer) {
	msg := "Waiting for output…"
	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	startY := sv.height / 2
	if startY >= sv.height {
		startY = 0
	}
	buf.SetString(0, startY, center(msg, sv.width), style)
}

// renderContent draws the scrollable content lines with a focus indicator.
func (sv *StreamViewer) renderContent(buf *tui.Buffer) {
	n := len(sv.lines)

	// Determine border style based on focus state.
	borderStyle := tui.NewStyle().Foreground(theme.Colors.Border)
	if sv.focused {
		borderStyle = tui.NewStyle().Foreground(theme.Colors.BorderFocus)
	}

	// Content starts after the left border (column 1).
	contentX := 1
	contentWidth := sv.width - contentX
	if contentWidth <= 0 {
		return
	}

	bodyStyle := tui.NewStyle().Foreground(theme.Colors.TextPrimary)
	mutedStyle := tui.NewStyle().Foreground(theme.Colors.TextMuted)

	for row := 0; row < sv.height; row++ {
		y := row
		lineIdx := sv.scrollOffset + row

		// Left border (focus indicator — colored when focused).
		buf.SetRune(0, y, '│', borderStyle)

		if lineIdx >= n {
			break
		}

		line := sv.lines[lineIdx]
		displayLine := truncate(line, contentWidth)
		buf.SetString(contentX, y, displayLine, bodyStyle)
	}

	// Follow-tail status indicator at bottom-right corner.
	if sv.height > 0 {
		indicator := "↓" // follow engaged
		indicatorStyle := mutedStyle
		if sv.followTail {
			indicatorStyle = tui.NewStyle().Foreground(theme.Colors.Accent)
		}
		// Place indicator at the bottom-right.
		indX := sv.width - 2
		indY := sv.height - 1
		if indX > contentX && indY >= 0 {
			buf.SetString(indX, indY, indicator, indicatorStyle)
		}
	}
}
