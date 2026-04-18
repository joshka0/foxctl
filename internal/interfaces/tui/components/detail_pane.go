package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// StatusVariant represents the status badge variant for DetailPane headers.
// Each variant maps to a distinct color from theme.Colors.
type StatusVariant int

const (
	StatusNone    StatusVariant = iota // No badge; label only.
	StatusOK                           // Green ● — running, healthy.
	StatusWarn                         // Yellow ◐ — idle, degraded.
	StatusError                        // Red ○ — error, failed.
	StatusPending                      // Blue/gray … — loading, starting.
)

// statusBadgeIcon returns the icon character for a status variant.
func statusBadgeIcon(sv StatusVariant) string {
	switch sv {
	case StatusOK:
		return "●"
	case StatusWarn:
		return "◐"
	case StatusError:
		return "○"
	case StatusPending:
		return "…"
	default:
		return ""
	}
}

// statusBadgeColor returns the theme color for a status variant.
func statusBadgeColor(sv StatusVariant) tui.Color {
	switch sv {
	case StatusOK:
		return theme.Colors.StatusOK
	case StatusWarn:
		return theme.Colors.StatusWarn
	case StatusError:
		return theme.Colors.StatusError
	case StatusPending:
		return theme.Colors.StatusPending
	default:
		return theme.Colors.TextPrimary
	}
}

// Section is a titled group of content lines within a DetailPane body.
type Section struct {
	Title string
	Lines []string
}

// DetailPane renders a scrollable detail view for the selected entity. It
// shows a header with title and status badge, sectioned body content with
// visible separators between sections, and an empty state when no entity is
// selected.
//
// When body content exceeds the viewport height, scrolling is enabled with a
// visible scroll indicator (scrollbar thumb) on the right edge. The title
// truncates with an ellipsis at the width boundary.
//
// Empty-state copy follows the guidance in component-spec.md:
// "Select an item to view details." with navigation hints.
type DetailPane struct {
	title        string
	status       StatusVariant
	sections     []Section
	width        int
	height       int
	focused      bool
	hasEntity    bool
	scrollOffset int
}

// DetailPaneOption configures a DetailPane at construction time.
type DetailPaneOption func(*DetailPane)

// WithDPFocused sets the initial focus state.
func WithDPFocused(f bool) DetailPaneOption {
	return func(dp *DetailPane) { dp.focused = f }
}

// WithHasEntity sets whether an entity is selected.
func WithHasEntity(h bool) DetailPaneOption {
	return func(dp *DetailPane) { dp.hasEntity = h }
}

// WithScrollOffset sets the initial scroll position.
func WithScrollOffset(off int) DetailPaneOption {
	return func(dp *DetailPane) { dp.scrollOffset = off }
}

// NewDetailPane creates a new DetailPane with the given title, status,
// sections, dimensions, and options.
func NewDetailPane(title string, status StatusVariant, sections []Section, width, height int, opts ...DetailPaneOption) *DetailPane {
	dp := &DetailPane{
		title:    title,
		status:   status,
		sections: sections,
		width:    width,
		height:   height,
	}
	for _, opt := range opts {
		opt(dp)
	}
	dp.clampScroll()
	return dp
}

// ScrollOffset returns the current scroll position.
func (dp *DetailPane) ScrollOffset() int {
	return dp.scrollOffset
}

// SetScrollOffset sets the scroll position (clamped to valid range).
func (dp *DetailPane) SetScrollOffset(off int) {
	dp.scrollOffset = off
	dp.clampScroll()
}

// totalContentLines computes the total number of body lines across all
// sections, including section titles and separators.
func (dp *DetailPane) totalContentLines() int {
	if len(dp.sections) == 0 {
		return 0
	}
	total := 0
	for i, s := range dp.sections {
		if i > 0 {
			total++ // separator line before section (except first)
		}
		total++ // section title line
		total += len(s.Lines)
	}
	return total
}

// maxScrollOffset returns the maximum valid scroll offset.
func (dp *DetailPane) maxScrollOffset() int {
	bodyHeight := dp.bodyHeight()
	if bodyHeight <= 0 {
		return 0
	}
	total := dp.totalContentLines()
	max := total - bodyHeight
	if max < 0 {
		max = 0
	}
	return max
}

// bodyHeight returns the number of rows available for the body (below header).
func (dp *DetailPane) bodyHeight() int {
	if dp.height <= 1 {
		return 0
	}
	return dp.height - 1 // header takes 1 row
}

// clampScroll ensures scrollOffset is within [0, maxScrollOffset].
func (dp *DetailPane) clampScroll() {
	max := dp.maxScrollOffset()
	if dp.scrollOffset < 0 {
		dp.scrollOffset = 0
	}
	if dp.scrollOffset > max {
		dp.scrollOffset = max
	}
}

// HandleKey processes a key event. Returns true if the key was consumed.
// Scroll keys only work when focused, HasEntity is true, and content overflows.
func (dp *DetailPane) HandleKey(evt tui.KeyEvent) bool {
	if !dp.focused || !dp.hasEntity {
		return false
	}
	if dp.bodyHeight() <= 0 {
		return false
	}
	// Only scroll when content overflows.
	if dp.totalContentLines() <= dp.bodyHeight() {
		return false
	}

	switch {
	case evt.Key == tui.KeyDown || (evt.Key == tui.KeyRune && evt.Rune == 'j'):
		dp.scrollOffset++
		dp.clampScroll()
		return true
	case evt.Key == tui.KeyUp || (evt.Key == tui.KeyRune && evt.Rune == 'k'):
		dp.scrollOffset--
		dp.clampScroll()
		return true
	case evt.Key == tui.KeyPageDown || (evt.Key == tui.KeyRune && evt.Rune == 'd' && evt.Mod.Has(tui.ModCtrl)):
		dp.scrollOffset += dp.bodyHeight()
		dp.clampScroll()
		return true
	case evt.Key == tui.KeyPageUp || (evt.Key == tui.KeyRune && evt.Rune == 'u' && evt.Mod.Has(tui.ModCtrl)):
		dp.scrollOffset -= dp.bodyHeight()
		dp.clampScroll()
		return true
	case evt.Key == tui.KeyHome || (evt.Key == tui.KeyRune && evt.Rune == 'g'):
		dp.scrollOffset = 0
		return true
	case evt.Key == tui.KeyEnd || (evt.Key == tui.KeyRune && evt.Rune == 'G'):
		dp.scrollOffset = dp.maxScrollOffset()
		return true
	}
	return false
}

// Render draws the DetailPane into the given buffer.
func (dp *DetailPane) Render(buf *tui.Buffer) {
	if dp.width <= 0 || dp.height <= 0 {
		return
	}

	// No entity: empty state.
	if !dp.hasEntity {
		dp.renderEmptyState(buf)
		return
	}

	// Header: always visible (1 row).
	dp.renderHeader(buf)

	// Body: scrollable area below header.
	if dp.bodyHeight() > 0 {
		if len(dp.sections) == 0 {
			dp.renderNoDetails(buf)
		} else {
			dp.renderBody(buf)
		}
	}
}

// renderHeader draws the title and status badge in row 0.
func (dp *DetailPane) renderHeader(buf *tui.Buffer) {
	// Focus indicator: colored left border (2 cells) when focused.
	if dp.focused {
		focusStyle := tui.NewStyle().Foreground(theme.Colors.BorderFocus)
		buf.SetRune(0, 0, '▌', focusStyle)
		buf.SetRune(1, 0, ' ', focusStyle)
	} else {
		normalStyle := tui.NewStyle().Foreground(theme.Colors.Border)
		buf.SetRune(0, 0, '│', normalStyle)
		buf.SetRune(1, 0, ' ', normalStyle)
	}

	// Content starts at column 2.
	contentX := 2
	contentWidth := dp.width - contentX
	if contentWidth <= 0 {
		return
	}

	// Fill header background.
	headerStyle := tui.NewStyle().
		Foreground(theme.Colors.TextPrimary).Bold()
	for x := contentX; x < dp.width; x++ {
		buf.SetRune(x, 0, ' ', headerStyle)
	}

	// Status badge icon.
	icon := statusBadgeIcon(dp.status)
	if icon != "" {
		badgeStyle := tui.NewStyle().Foreground(statusBadgeColor(dp.status)).Bold()
		buf.SetString(contentX, 0, icon, badgeStyle)
		iconWidth := runeWidth(icon)
		contentX += iconWidth + 1 // icon + space
		contentWidth = dp.width - contentX
		if contentWidth <= 0 {
			return
		}
	}

	// Title (truncated with ellipsis at width boundary).
	truncatedTitle := truncate(dp.title, contentWidth)
	buf.SetString(contentX, 0, truncatedTitle, headerStyle)
}

// renderEmptyState draws the empty-state guidance when HasEntity=false.
func (dp *DetailPane) renderEmptyState(buf *tui.Buffer) {
	msg := "Select an item to view details."
	cta := "Use ↓/↑ to navigate, Enter to open."

	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	startY := dp.height / 2
	if startY >= dp.height {
		startY = 0
	}

	// Center horizontally.
	buf.SetString(0, startY, center(msg, dp.width), style)
	if startY+1 < dp.height {
		buf.SetString(0, startY+1, center(cta, dp.width),
			tui.NewStyle().Foreground(theme.Colors.TextMuted))
	}
}

// renderNoDetails draws "No details available." when HasEntity=true but
// Sections is empty.
func (dp *DetailPane) renderNoDetails(buf *tui.Buffer) {
	msg := "No details available."
	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	bodyY := 1
	bodyH := dp.bodyHeight()
	centerY := bodyY + bodyH/2
	if centerY >= dp.height {
		centerY = bodyY
	}
	buf.SetString(0, centerY, center(msg, dp.width), style)
}

// renderBody draws the scrollable sections with separators and an optional
// scrollbar indicator.
func (dp *DetailPane) renderBody(buf *tui.Buffer) {
	bodyY := 1 // header takes row 0
	bodyH := dp.bodyHeight()
	totalLines := dp.totalContentLines()
	hasOverflow := totalLines > bodyH

	// Build a flat list of content lines for scrolling.
	type contentLine struct {
		text  string
		style tui.Style
	}
	var lines []contentLine

	for i, s := range dp.sections {
		// Separator before sections after the first.
		if i > 0 {
			lines = append(lines, contentLine{
				text:  "",
				style: tui.NewStyle().Foreground(theme.Colors.Divider),
			})
		}

		// Section title.
		lines = append(lines, contentLine{
			text:  s.Title,
			style: tui.NewStyle().Foreground(theme.Colors.TextSecondary).Bold(),
		})

		// Section lines.
		bodyStyle := tui.NewStyle().Foreground(theme.Colors.TextPrimary)
		for _, l := range s.Lines {
			lines = append(lines, contentLine{
				text:  l,
				style: bodyStyle,
			})
		}
	}

	// Render visible lines from scrollOffset.
	for row := 0; row < bodyH; row++ {
		lineIdx := dp.scrollOffset + row
		if lineIdx >= len(lines) {
			break
		}
		y := bodyY + row
		if y >= dp.height {
			break
		}

		line := lines[lineIdx]

		// Separator line.
		if line.text == "" && lineIdx > 0 {
			// Draw horizontal rule.
			sepStyle := tui.NewStyle().Foreground(theme.Colors.Divider)
			for x := 0; x < dp.width-1; x++ {
				buf.SetRune(x, y, '─', sepStyle)
			}
			continue
		}

		// Indent content by 2 cells for body.
		indent := 2
		contentWidth := dp.width - indent - 1 // -1 for scrollbar column
		if contentWidth <= 0 {
			continue
		}

		text := truncate(line.text, contentWidth)
		buf.SetString(indent, y, text, line.style)
	}

	// Scrollbar: visible when content overflows.
	if hasOverflow {
		dp.renderScrollbar(buf, bodyY, bodyH, totalLines)
	}
}

// renderScrollbar draws a minimal scrollbar indicator on the rightmost column
// of the body area.
func (dp *DetailPane) renderScrollbar(buf *tui.Buffer, bodyY, bodyH, totalLines int) {
	if bodyH <= 0 || totalLines <= 0 || dp.width <= 0 {
		return
	}

	scrollCol := dp.width - 1
	trackStyle := tui.NewStyle().Foreground(theme.Colors.ScrollbarTrack)
	thumbStyle := tui.NewStyle().Foreground(theme.Colors.ScrollbarThumb)

	// Draw track (full body height).
	for row := 0; row < bodyH; row++ {
		y := bodyY + row
		if y >= dp.height {
			break
		}
		buf.SetRune(scrollCol, y, '│', trackStyle)
	}

	// Compute thumb position.
	thumbLen := max(1, bodyH*bodyH/totalLines)
	// Thumb start position proportional to scroll offset.
	maxOff := dp.maxScrollOffset()
	var thumbStart int
	if maxOff > 0 {
		thumbStart = dp.scrollOffset * (bodyH - thumbLen) / maxOff
	} else {
		thumbStart = 0
	}

	// Draw thumb.
	for i := 0; i < thumbLen; i++ {
		y := bodyY + thumbStart + i
		if y >= dp.height {
			break
		}
		buf.SetRune(scrollCol, y, '┃', thumbStyle)
	}
}
