// Package components implements the reusable TUI widget primitives for the
// foxctl operator cockpit. Each widget is a stateless-ish render function
// that receives props and renders into a go-tui Buffer. Keyboard handling is
// provided via a HandleKey method that returns the new state.
//
// # Theme tokens
//
// All widgets reference theme tokens from the theme package. Raw color
// literals (tui.Cyan, "#...", etc.) MUST NOT appear in this package.
//
// # Focus indicator rule
//
// Focusable widgets MUST display a visible focus indicator that is NOT
// font-weight only. Acceptable indicators include colored left borders,
// inverse backgrounds, and colored backgrounds.
package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// Entity is a row in an EntityList. Each row has a primary label, optional
// sub-label, and an ID for identification.
type Entity struct {
	ID       string
	Label    string
	SubLabel string
	Detail   string // optional detail text (e.g., workspace, role)
}

// EntityList renders a scrollable, focusable, selectable list of typed
// entities. It supports keyboard navigation (up/down, home/end, page
// up/down), wrap-around behavior, and a visible focus indicator (colored
// left border — NOT font-weight only).
//
// EntityList integrates EmptyState when items is empty and loading is false,
// and LoadingState when loading is true.
type EntityList struct {
	items         []Entity
	width         int
	height        int
	selectedIndex int
	scrollOffset  int
	focused       bool
	loading       bool
	emptyMessage  string
	errorMessage  string
	wrapAround    bool
	onSelect      func(index int)
	onActivate    func(index int)
}

// EntityListOption configures an EntityList at construction time.
type EntityListOption func(*EntityList)

// WithSelectedIndex sets the initially selected row index.
func WithSelectedIndex(idx int) EntityListOption {
	return func(el *EntityList) { el.selectedIndex = idx }
}

// WithFocused sets the initial focus state.
func WithFocused(f bool) EntityListOption {
	return func(el *EntityList) { el.focused = f }
}

// WithLoading sets the loading state.
func WithLoading(l bool) EntityListOption {
	return func(el *EntityList) { el.loading = l }
}

// WithEmptyMessage sets a custom empty-state message.
func WithEmptyMessage(msg string) EntityListOption {
	return func(el *EntityList) { el.emptyMessage = msg }
}

// WithErrorMessage sets the error state message.
func WithErrorMessage(msg string) EntityListOption {
	return func(el *EntityList) { el.errorMessage = msg }
}

// WithWrapAround sets wrap-around navigation behavior.
func WithWrapAround(w bool) EntityListOption {
	return func(el *EntityList) { el.wrapAround = w }
}

// WithOnSelect sets the callback fired when the selected index changes.
func WithOnSelect(fn func(int)) EntityListOption {
	return func(el *EntityList) { el.onSelect = fn }
}

// WithOnActivate sets the callback fired when Enter is pressed.
func WithOnActivate(fn func(int)) EntityListOption {
	return func(el *EntityList) { el.onActivate = fn }
}

// NewEntityList creates a new EntityList with the given items, dimensions,
// and options.
func NewEntityList(items []Entity, width, height int, opts ...EntityListOption) *EntityList {
	el := &EntityList{
		items:         items,
		width:         width,
		height:        height,
		selectedIndex: -1,
		wrapAround:    true, // default per spec
	}
	for _, opt := range opts {
		opt(el)
	}
	el.clampScroll()
	return el
}

// SelectedIndex returns the currently selected row index (-1 if none).
func (el *EntityList) SelectedIndex() int {
	return el.selectedIndex
}

// ScrollOffset returns the current scroll offset (first visible row index).
func (el *EntityList) ScrollOffset() int {
	return el.scrollOffset
}

// SetSelectedIndex sets the selected index and adjusts scroll to keep it visible.
func (el *EntityList) SetSelectedIndex(idx int) {
	n := len(el.items)
	if n == 0 {
		el.selectedIndex = -1
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	el.selectedIndex = idx
	el.clampScroll()
}

// SetItems replaces the entity list items and re-clamps selection and scroll.
func (el *EntityList) SetItems(items []Entity) {
	el.items = items
	if len(items) == 0 {
		el.selectedIndex = -1
		el.scrollOffset = 0
		return
	}
	if el.selectedIndex >= len(items) {
		el.selectedIndex = len(items) - 1
	}
	if el.selectedIndex < 0 {
		el.selectedIndex = 0
	}
	el.clampScroll()
}

// clampScroll adjusts scrollOffset so that selectedIndex is visible within
// the viewport.
func (el *EntityList) clampScroll() {
	n := len(el.items)
	if n == 0 || el.height <= 0 {
		return
	}
	if el.selectedIndex < 0 {
		return
	}
	// If selected is above viewport.
	if el.selectedIndex < el.scrollOffset {
		el.scrollOffset = el.selectedIndex
	}
	// If selected is below viewport.
	visibleRows := el.height
	if el.selectedIndex >= el.scrollOffset+visibleRows {
		el.scrollOffset = el.selectedIndex - visibleRows + 1
	}
	// Clamp scrollOffset to valid range.
	maxScroll := n - el.height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if el.scrollOffset < 0 {
		el.scrollOffset = 0
	}
	if el.scrollOffset > maxScroll {
		el.scrollOffset = maxScroll
	}
}

// HandleKey processes a key event. Returns true if the key was consumed.
// Navigation only occurs when the list is focused, not loading, not empty,
// and has no error.
func (el *EntityList) HandleKey(evt tui.KeyEvent) bool {
	if !el.focused {
		return false
	}
	if el.loading || el.errorMessage != "" {
		return false
	}
	n := len(el.items)
	if n == 0 {
		return false
	}

	switch {
	case evt.Key == tui.KeyDown || (evt.Key == tui.KeyRune && evt.Rune == 'j'):
		return el.moveDown()
	case evt.Key == tui.KeyUp || (evt.Key == tui.KeyRune && evt.Rune == 'k'):
		return el.moveUp()
	case evt.Key == tui.KeyHome || (evt.Key == tui.KeyRune && evt.Rune == 'g'):
		return el.moveHome()
	case evt.Key == tui.KeyEnd || (evt.Key == tui.KeyRune && evt.Rune == 'G'):
		return el.moveEnd()
	case evt.Key == tui.KeyPageDown || (evt.Key == tui.KeyRune && evt.Rune == 'd' && evt.Mod.Has(tui.ModCtrl)):
		return el.movePageDown()
	case evt.Key == tui.KeyPageUp || (evt.Key == tui.KeyRune && evt.Rune == 'u' && evt.Mod.Has(tui.ModCtrl)):
		return el.movePageUp()
	case evt.Key == tui.KeyEnter:
		if el.selectedIndex >= 0 && el.onActivate != nil {
			el.onActivate(el.selectedIndex)
		}
		return true
	}
	return false
}

func (el *EntityList) moveDown() bool {
	n := len(el.items)
	if n == 0 {
		return false
	}
	// If no selection, select first.
	if el.selectedIndex < 0 {
		el.selectedIndex = 0
		el.clampScroll()
		el.fireOnSelect()
		return true
	}
	if el.selectedIndex >= n-1 {
		if el.wrapAround {
			el.selectedIndex = 0
		} else {
			return true // consumed but no change
		}
	} else {
		el.selectedIndex++
	}
	el.clampScroll()
	el.fireOnSelect()
	return true
}

func (el *EntityList) moveUp() bool {
	n := len(el.items)
	if n == 0 {
		return false
	}
	if el.selectedIndex < 0 {
		el.selectedIndex = 0
		el.clampScroll()
		el.fireOnSelect()
		return true
	}
	if el.selectedIndex <= 0 {
		if el.wrapAround {
			el.selectedIndex = n - 1
		} else {
			return true
		}
	} else {
		el.selectedIndex--
	}
	el.clampScroll()
	el.fireOnSelect()
	return true
}

func (el *EntityList) moveHome() bool {
	if len(el.items) == 0 {
		return false
	}
	el.selectedIndex = 0
	el.clampScroll()
	el.fireOnSelect()
	return true
}

func (el *EntityList) moveEnd() bool {
	n := len(el.items)
	if n == 0 {
		return false
	}
	el.selectedIndex = n - 1
	el.clampScroll()
	el.fireOnSelect()
	return true
}

func (el *EntityList) movePageDown() bool {
	n := len(el.items)
	if n == 0 {
		return false
	}
	if el.selectedIndex < 0 {
		el.selectedIndex = 0
		el.clampScroll()
		el.fireOnSelect()
		return true
	}
	el.selectedIndex += el.height
	if el.selectedIndex >= n {
		el.selectedIndex = n - 1
	}
	el.clampScroll()
	el.fireOnSelect()
	return true
}

func (el *EntityList) movePageUp() bool {
	n := len(el.items)
	if n == 0 {
		return false
	}
	if el.selectedIndex < 0 {
		el.selectedIndex = 0
		el.clampScroll()
		el.fireOnSelect()
		return true
	}
	el.selectedIndex -= el.height
	if el.selectedIndex < 0 {
		el.selectedIndex = 0
	}
	el.clampScroll()
	el.fireOnSelect()
	return true
}

func (el *EntityList) fireOnSelect() {
	if el.onSelect != nil {
		el.onSelect(el.selectedIndex)
	}
}

// Render draws the EntityList into the given buffer.
func (el *EntityList) Render(buf *tui.Buffer) {
	if el.width <= 0 || el.height <= 0 {
		return
	}

	// Error state takes precedence.
	if el.errorMessage != "" {
		el.renderError(buf)
		return
	}

	// Loading state.
	if el.loading {
		el.renderLoading(buf)
		return
	}

	// Empty state.
	if len(el.items) == 0 {
		el.renderEmpty(buf)
		return
	}

	// Normal list rendering.
	el.renderList(buf)
}

// renderList draws the scrollable entity rows.
func (el *EntityList) renderList(buf *tui.Buffer) {
	n := len(el.items)
	focusBarStyle := tui.NewStyle().Foreground(theme.Colors.BorderFocus)
	selectedStyle := tui.NewStyle().
		Background(theme.Colors.SelectionBg).
		Foreground(theme.Colors.TextPrimary)
	normalStyle := tui.NewStyle().
		Foreground(theme.Colors.TextSecondary)
	selectedLabelStyle := tui.NewStyle().
		Background(theme.Colors.SelectionBg).
		Foreground(theme.Colors.TextPrimary).Bold()
	labelStyle := tui.NewStyle().
		Foreground(theme.Colors.TextPrimary)

	for row := 0; row < el.height; row++ {
		itemIdx := el.scrollOffset + row
		if itemIdx >= n {
			break
		}
		y := row

		// Focus indicator: colored left border (2 cells wide) when focused.
		if el.focused {
			// Column 0: colored bar character for the selected row only.
			if itemIdx == el.selectedIndex {
				buf.SetRune(0, y, '▌', focusBarStyle)
			} else {
				// Non-selected rows: subtle border.
				buf.SetRune(0, y, '│', tui.NewStyle().Foreground(theme.Colors.Border))
			}
		} else {
			// Unfocused: dim border for selected row.
			if itemIdx == el.selectedIndex {
				buf.SetRune(0, y, '│', tui.NewStyle().Foreground(theme.Colors.AccentMuted))
			}
		}

		// Column 1: spacer
		buf.SetRune(1, y, ' ', tui.NewStyle())

		// Content area: starts at column 2.
		contentX := 2
		contentWidth := el.width - contentX
		if contentWidth <= 0 {
			continue
		}

		isSelected := itemIdx == el.selectedIndex

		// Fill the row background for selected item.
		if isSelected {
			for x := contentX; x < el.width; x++ {
				buf.SetRune(x, y, ' ', selectedStyle)
			}
		}

		item := el.items[itemIdx]

		// Render label.
		if isSelected {
			buf.SetString(contentX, y, truncate(item.Label, contentWidth), selectedLabelStyle)
		} else {
			buf.SetString(contentX, y, truncate(item.Label, contentWidth), labelStyle)
		}

		// Render sub-label if there's room.
		if item.SubLabel != "" {
			labelWidth := runeWidth(item.Label)
			subX := contentX + labelWidth + 1
			remaining := contentWidth - labelWidth - 1
			if remaining > 0 {
				subText := truncate(item.SubLabel, remaining)
				if isSelected {
					buf.SetString(subX, y, subText, selectedStyle)
				} else {
					buf.SetString(subX, y, subText, normalStyle)
				}
			}
		}
	}
}

// renderEmpty draws the empty-state message.
func (el *EntityList) renderEmpty(buf *tui.Buffer) {
	msg := el.emptyMessage
	if msg == "" {
		msg = "No items found."
	}
	ct := ""
	if len(el.items) == 0 {
		ct = "Press ? for help"
	}

	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	// Center the message vertically.
	startY := el.height / 2
	if startY >= el.height {
		startY = 0
	}
	buf.SetString(0, startY, center(msg, el.width), style)
	if ct != "" && startY+1 < el.height {
		buf.SetString(0, startY+1, center(ct, el.width), tui.NewStyle().Foreground(theme.Colors.TextMuted))
	}
}

// renderLoading draws the loading state.
func (el *EntityList) renderLoading(buf *tui.Buffer) {
	msg := "Loading…"
	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	startY := el.height / 2
	if startY >= el.height {
		startY = 0
	}
	buf.SetString(0, startY, center(msg, el.width), style)
}

// renderError draws the error state.
func (el *EntityList) renderError(buf *tui.Buffer) {
	msg := "Error: " + el.errorMessage
	style := tui.NewStyle().Foreground(theme.Colors.StatusError)
	startY := el.height / 2
	if startY >= el.height {
		startY = 0
	}
	buf.SetString(0, startY, center(msg, el.width), style)
	if startY+1 < el.height {
		buf.SetString(0, startY+1, center("Press r to retry", el.width),
			tui.NewStyle().Foreground(theme.Colors.TextMuted))
	}
}
