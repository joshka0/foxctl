package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// Drawer is a slide-over panel that opens on top of the right portion of the
// screen. It is used for raw payload inspection, memory surfaces, and
// evidence viewing per component-spec.md.
//
// When open, the drawer traps focus: Tab and Shift-Tab cycle inside the
// drawer and do not escape to the parent. ESC closes the drawer and restores
// focus to the previously-focused element.
//
// Double-close is safe: calling Close() on an already-closed drawer is a
// no-op (no panic, no double-fire of OnClose).
type Drawer struct {
	title        string
	content      []string
	width        int
	height       int
	drawerWidth  int // width of the drawer panel when open
	open         bool
	focused      bool
	scrollOffset int

	previouslyFocused string           // ref identifier of the element to restore focus to
	onClose           func()           // fires exactly once per open→close cycle
	onRestoreFocus    func(ref string) // fires on close with the previously-focused ref
	closeFired        bool             // tracks whether OnClose has fired for the current cycle
}

// DrawerOption configures a Drawer at construction time.
type DrawerOption func(*Drawer)

// WithDrawerOpen sets the initial open state.
func WithDrawerOpen(o bool) DrawerOption {
	return func(d *Drawer) {
		d.open = o
		if o {
			d.closeFired = false
		}
	}
}

// WithDrawerFocused sets the initial focus state.
func WithDrawerFocused(f bool) DrawerOption {
	return func(d *Drawer) { d.focused = f }
}

// WithDrawerOnClose sets the callback fired when the drawer closes.
// It fires exactly once per open→close cycle.
func WithDrawerOnClose(fn func()) DrawerOption {
	return func(d *Drawer) { d.onClose = fn }
}

// WithDrawerPreviouslyFocused sets a reference identifier for the element
// that had focus before the drawer opened. On close, OnRestoreFocus is
// called with this ref.
func WithDrawerPreviouslyFocused(ref string) DrawerOption {
	return func(d *Drawer) { d.previouslyFocused = ref }
}

// WithDrawerOnRestoreFocus sets the callback that receives the
// previously-focused ref on drawer close.
func WithDrawerOnRestoreFocus(fn func(ref string)) DrawerOption {
	return func(d *Drawer) { d.onRestoreFocus = fn }
}

// WithDrawerWidth sets the drawer panel width in cells (default 30).
func WithDrawerWidth(w int) DrawerOption {
	return func(d *Drawer) { d.drawerWidth = w }
}

// NewDrawer creates a new Drawer widget with the given title, content lines,
// and dimensions.
func NewDrawer(title string, content []string, width, height int, opts ...DrawerOption) *Drawer {
	d := &Drawer{
		title:       title,
		content:     content,
		width:       width,
		height:      height,
		drawerWidth: 30,
		closeFired:  true, // starts closed, so close has been "fired"
	}
	for _, opt := range opts {
		opt(d)
	}
	d.clampScroll()
	return d
}

// IsOpen reports whether the drawer is currently open.
func (d *Drawer) IsOpen() bool {
	return d.open
}

// Focused reports whether the drawer has focus.
func (d *Drawer) Focused() bool {
	return d.focused
}

// ScrollOffset returns the current scroll offset for the body content.
func (d *Drawer) ScrollOffset() int {
	return d.scrollOffset
}

// Open opens the drawer programmatically. Resets the close-fired flag so the
// next Close() will fire OnClose exactly once.
func (d *Drawer) Open() {
	d.open = true
	d.closeFired = false
	d.scrollOffset = 0
	d.clampScroll()
}

// Close closes the drawer programmatically. OnClose fires exactly once per
// open→close cycle. Double-close is a no-op (no panic, no double-fire).
func (d *Drawer) Close() {
	if !d.open {
		return
	}
	d.open = false

	// Fire OnClose exactly once per open→close cycle.
	if !d.closeFired {
		d.closeFired = true
		if d.onClose != nil {
			d.onClose()
		}
	}

	// Restore focus to the previously-focused element.
	if d.onRestoreFocus != nil && d.previouslyFocused != "" {
		d.onRestoreFocus(d.previouslyFocused)
	}
}

// SetContent replaces the drawer's content lines and re-clamps scroll.
func (d *Drawer) SetContent(content []string) {
	d.content = content
	d.clampScroll()
}

// SetOpen programmatically opens or closes the drawer.
func (d *Drawer) SetOpen(open bool) {
	if open && !d.open {
		d.Open()
	} else if !open && d.open {
		d.Close()
	}
}

// HandleKey processes a key event. Returns true if the key was consumed.
// When the drawer is open and focused:
//   - ESC closes the drawer and restores focus.
//   - Tab / Shift+Tab are consumed (focus trap — no escape).
//   - ↑/↓/PageUp/PageDown/Home/End scroll the body content.
func (d *Drawer) HandleKey(evt tui.KeyEvent) bool {
	if !d.open || !d.focused {
		return false
	}

	// ESC closes the drawer.
	if evt.Key == tui.KeyEscape {
		d.Close()
		return true
	}

	// Focus trap: Tab and Shift+Tab are consumed but don't escape.
	if evt.Key == tui.KeyTab {
		// Tab cycles inside the drawer — no-op on focus position for now,
		// but the key is consumed so focus doesn't escape.
		return true
	}

	// Scroll keys.
	bodyHeight := d.bodyHeight()
	contentLen := len(d.content)

	switch {
	case evt.Key == tui.KeyDown || (evt.Key == tui.KeyRune && evt.Rune == 'j'):
		if contentLen > bodyHeight && d.scrollOffset < contentLen-bodyHeight {
			d.scrollOffset++
			return true
		}
		return true // consumed even if at boundary
	case evt.Key == tui.KeyUp || (evt.Key == tui.KeyRune && evt.Rune == 'k'):
		if d.scrollOffset > 0 {
			d.scrollOffset--
			return true
		}
		return true
	case evt.Key == tui.KeyPageDown:
		d.scrollOffset += bodyHeight
		maxScroll := contentLen - bodyHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		if d.scrollOffset > maxScroll {
			d.scrollOffset = maxScroll
		}
		return true
	case evt.Key == tui.KeyPageUp:
		d.scrollOffset -= bodyHeight
		if d.scrollOffset < 0 {
			d.scrollOffset = 0
		}
		return true
	case evt.Key == tui.KeyHome || (evt.Key == tui.KeyRune && evt.Rune == 'g'):
		d.scrollOffset = 0
		return true
	case evt.Key == tui.KeyEnd || (evt.Key == tui.KeyRune && evt.Rune == 'G'):
		maxScroll := contentLen - bodyHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		d.scrollOffset = maxScroll
		return true
	}

	return false
}

// Render draws the Drawer into the given buffer. When closed, Render is a
// no-op. When open, the drawer is drawn as a right-aligned panel with:
//   - Row 0: header (title + ESC close hint)
//   - Rows 1..height-1: scrollable body content
//   - Left border using BorderFocus when focused, Border when unfocused
func (d *Drawer) Render(buf *tui.Buffer) {
	if !d.open || d.width <= 0 || d.height <= 0 {
		return
	}

	dw := d.drawerWidth
	if dw > d.width {
		dw = d.width
	}
	// Drawer starts from the right edge.
	startX := d.width - dw

	// Determine border style.
	borderStyle := tui.NewStyle().Foreground(theme.Colors.Border)
	if d.focused {
		borderStyle = tui.NewStyle().Foreground(theme.Colors.BorderFocus)
	}

	// Draw left border.
	for y := 0; y < d.height; y++ {
		buf.SetRune(startX, y, '│', borderStyle)
	}

	// Header row (row 0).
	headerStyle := tui.NewStyle().
		Foreground(theme.Colors.TextPrimary).
		Background(theme.Colors.SurfaceAlt).Bold()
	headerX := startX + 1
	headerW := dw - 1 // minus left border

	// Fill header background.
	for x := headerX; x < startX+dw && x < d.width; x++ {
		buf.SetRune(x, 0, ' ', headerStyle)
	}

	// Title text.
	titleText := truncate(d.title, headerW-12) // leave room for close hint
	if titleW := runeWidth(titleText); titleW < headerW {
		buf.SetString(headerX, 0, titleText, headerStyle)
	}

	// Close hint at the right side of header.
	closeHint := " ESC to close "
	hintW := runeWidth(closeHint)
	if hintW < headerW {
		hintX := startX + dw - hintW
		if hintX > headerX {
			buf.SetString(hintX, 0, closeHint,
				tui.NewStyle().Foreground(theme.Colors.TextMuted).
					Background(theme.Colors.SurfaceAlt))
		}
	}

	// Body rows (row 1..height-1).
	bodyStyle := tui.NewStyle().Foreground(theme.Colors.TextPrimary)
	contentX := headerX
	contentW := dw - 1
	if contentW < 0 {
		contentW = 0
	}

	bodyH := d.bodyHeight()
	for row := 0; row < bodyH; row++ {
		y := row + 1
		if y >= d.height {
			break
		}
		lineIdx := d.scrollOffset + row
		if lineIdx < len(d.content) {
			line := truncate(d.content[lineIdx], contentW)
			buf.SetString(contentX, y, line, bodyStyle)
		}
	}

	// Scroll indicator when content overflows.
	if len(d.content) > bodyH && bodyH > 0 {
		// Show a subtle scrollbar indicator on the right edge.
		thumbY := 1 + (d.scrollOffset * bodyH / len(d.content))
		if thumbY < d.height {
			scrollStyle := tui.NewStyle().Foreground(theme.Colors.ScrollbarThumb)
			buf.SetRune(startX+dw-1, thumbY, '▐', scrollStyle)
		}
	}
}

// bodyHeight returns the number of rows available for body content
// (total height minus 1 for the header row).
func (d *Drawer) bodyHeight() int {
	if d.height <= 1 {
		return 0
	}
	return d.height - 1
}

// clampScroll ensures scrollOffset is within valid range.
func (d *Drawer) clampScroll() {
	bh := d.bodyHeight()
	n := len(d.content)
	maxScroll := n - bh
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scrollOffset < 0 {
		d.scrollOffset = 0
	}
	if d.scrollOffset > maxScroll {
		d.scrollOffset = maxScroll
	}
}
