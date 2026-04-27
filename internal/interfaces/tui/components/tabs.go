package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// Tabs renders a tabbed header with a visible active indicator that is NOT
// font-weight only. The active tab is indicated by a colored underline
// (Accent color) and colored label text. Inactive tabs use muted text.
//
// Supports keyboard navigation: →/←/h/l, Tab/Shift-Tab, Home/End/g/G.
// Wrap-around is on by default.
//
// Per component-spec.md §(7): the active tab indicator uses a colored
// underline below the active tab label, which is visually distinct from
// inactive tabs even in the raw cell buffer.
type Tabs struct {
	labels     []string
	width      int
	activeIdx  int
	focused    bool
	wrapAround bool
	onChange   func(index int)
}

// TabsOption configures a Tabs widget at construction time.
type TabsOption func(*Tabs)

// WithTabsFocused sets the initial focus state.
func WithTabsFocused(f bool) TabsOption {
	return func(t *Tabs) { t.focused = f }
}

// WithTabsActiveIndex sets the initially active tab index.
func WithTabsActiveIndex(idx int) TabsOption {
	return func(t *Tabs) { t.activeIdx = idx }
}

// WithTabsWrapAround sets wrap-around navigation behavior (default true).
func WithTabsWrapAround(w bool) TabsOption {
	return func(t *Tabs) { t.wrapAround = w }
}

// WithTabsOnChange sets the callback fired when the active tab changes.
func WithTabsOnChange(fn func(int)) TabsOption {
	return func(t *Tabs) { t.onChange = fn }
}

// NewTabs creates a new Tabs widget with the given labels, width, and options.
func NewTabs(labels []string, width int, opts ...TabsOption) *Tabs {
	t := &Tabs{
		labels:     labels,
		width:      width,
		activeIdx:  0,
		wrapAround: true,
	}
	for _, opt := range opts {
		opt(t)
	}
	t.clampActive()
	return t
}

// ActiveIndex returns the currently active tab index.
func (t *Tabs) ActiveIndex() int {
	return t.activeIdx
}

// SetActiveIndex sets the active tab index, clamped to valid range.
func (t *Tabs) SetActiveIndex(idx int) {
	t.activeIdx = idx
	t.clampActive()
}

// SetLabels replaces the tab labels and re-clamps the active index.
func (t *Tabs) SetLabels(labels []string) {
	t.labels = labels
	t.clampActive()
}

// clampActive ensures activeIdx is within [0, len(labels)-1].
func (t *Tabs) clampActive() {
	n := len(t.labels)
	if n == 0 {
		t.activeIdx = 0
		return
	}
	if t.activeIdx < 0 {
		t.activeIdx = 0
	}
	if t.activeIdx >= n {
		t.activeIdx = n - 1
	}
}

// HandleKey processes a key event. Returns true if the key was consumed.
// Navigation only occurs when the widget is focused and there are >1 tabs.
func (t *Tabs) HandleKey(evt tui.KeyEvent) bool {
	if !t.focused {
		return false
	}
	n := len(t.labels)
	if n <= 1 {
		// Single tab: consume navigation keys but no change.
		if evt.Key == tui.KeyRight || evt.Key == tui.KeyLeft ||
			evt.Key == tui.KeyTab || evt.Key == tui.KeyHome || evt.Key == tui.KeyEnd ||
			(evt.Key == tui.KeyRune && (evt.Rune == 'h' || evt.Rune == 'l' || evt.Rune == 'g' || evt.Rune == 'G')) {
			return true
		}
		// Check Shift+Tab.
		if evt.Key == tui.KeyTab && evt.Mod.Has(tui.ModShift) {
			return true
		}
		return false
	}

	switch {
	case evt.Key == tui.KeyRight || (evt.Key == tui.KeyRune && evt.Rune == 'l'):
		return t.moveNext()
	case evt.Key == tui.KeyLeft || (evt.Key == tui.KeyRune && evt.Rune == 'h'):
		return t.movePrev()
	case evt.Key == tui.KeyTab && !evt.Mod.Has(tui.ModShift):
		return t.moveNext()
	case evt.Key == tui.KeyTab && evt.Mod.Has(tui.ModShift):
		return t.movePrev()
	case evt.Key == tui.KeyHome || (evt.Key == tui.KeyRune && evt.Rune == 'g'):
		return t.moveFirst()
	case evt.Key == tui.KeyEnd || (evt.Key == tui.KeyRune && evt.Rune == 'G'):
		return t.moveLast()
	}
	return false
}

func (t *Tabs) moveNext() bool {
	n := len(t.labels)
	if n <= 1 {
		return true
	}
	next := t.activeIdx + 1
	if next >= n {
		if t.wrapAround {
			next = 0
		} else {
			return true // consumed, no change
		}
	}
	t.activeIdx = next
	t.fireOnChange()
	return true
}

func (t *Tabs) movePrev() bool {
	n := len(t.labels)
	if n <= 1 {
		return true
	}
	prev := t.activeIdx - 1
	if prev < 0 {
		if t.wrapAround {
			prev = n - 1
		} else {
			return true
		}
	}
	t.activeIdx = prev
	t.fireOnChange()
	return true
}

func (t *Tabs) moveFirst() bool {
	if len(t.labels) == 0 {
		return true
	}
	if t.activeIdx == 0 {
		return true // already at first — consume key but don't fire onChange
	}
	t.activeIdx = 0
	t.fireOnChange()
	return true
}

func (t *Tabs) moveLast() bool {
	n := len(t.labels)
	if n == 0 {
		return true
	}
	if t.activeIdx == n-1 {
		return true // already at last — consume key but don't fire onChange
	}
	t.activeIdx = n - 1
	t.fireOnChange()
	return true
}

func (t *Tabs) fireOnChange() {
	if t.onChange != nil {
		t.onChange(t.activeIdx)
	}
}

// Render draws the Tabs into the given buffer. The widget uses 2 rows:
//   - Row 0: tab labels (active in Accent color, inactive in TextSecondary)
//   - Row 1: colored underline below the active tab
//
// If labels is empty, Render is a no-op.
func (t *Tabs) Render(buf *tui.Buffer) {
	if len(t.labels) == 0 || t.width <= 0 {
		return
	}

	// Compute label positions: evenly distribute across width.
	positions := t.computePositions()

	// Row 0: labels.
	for i, label := range t.labels {
		if i >= len(positions) {
			break
		}
		startX := positions[i]
		isActive := i == t.activeIdx

		var style tui.Style
		if isActive {
			// Active tab: bright Accent color foreground — clearly distinct from inactive.
			style = tui.NewStyle().Foreground(theme.Colors.Accent).Bold()
		} else {
			// Inactive tab: muted secondary text.
			style = tui.NewStyle().Foreground(theme.Colors.TextMuted)
		}

		// Render label characters.
		maxW := t.width - startX
		if maxW <= 0 {
			continue
		}
		rendered := truncate(label, maxW)
		buf.SetString(startX, 0, rendered, style)
	}

	// Row 1: colored underline below the active tab.
	if t.activeIdx < len(positions) {
		startX := positions[t.activeIdx]
		label := t.labels[t.activeIdx]
		labelW := runeWidth(label)
		if labelW > t.width-startX {
			labelW = t.width - startX
		}

		underlineStyle := tui.NewStyle().Foreground(theme.Colors.Accent)
		if !t.focused {
			// When unfocused, the underline uses a dimmer accent variant.
			underlineStyle = tui.NewStyle().Foreground(theme.Colors.AccentMuted)
		}

		for x := startX; x < startX+labelW; x++ {
			buf.SetRune(x, 1, '━', underlineStyle)
		}
	}
}

// Height returns the number of rows the Tabs widget occupies (always 2:
// one for labels, one for the active indicator underline).
func (t *Tabs) Height() int {
	if len(t.labels) == 0 {
		return 0
	}
	return 2
}

// computePositions determines the x-start position for each tab label.
// Tabs are evenly distributed across the available width.
func (t *Tabs) computePositions() []int {
	n := len(t.labels)
	if n == 0 {
		return nil
	}

	// Calculate total label widths and minimum spacing.
	totalLabelW := 0
	labelWidths := make([]int, n)
	for i, label := range t.labels {
		w := runeWidth(label)
		labelWidths[i] = w
		totalLabelW += w
	}

	// Spacing between labels (minimum 2 cells gap).
	minGap := 2
	totalGap := (n - 1) * minGap

	// If everything fits, center the whole tab bar.
	avail := t.width
	if totalLabelW+totalGap <= avail {
		// Distribute evenly. Calculate the gap to fill remaining space.
		remaining := avail - totalLabelW
		gap := remaining
		if n > 1 {
			gap = remaining / (n - 1)
		}
		if gap < minGap {
			gap = minGap
		}

		positions := make([]int, n)
		x := 0
		// Left-pad to center if there's extra space.
		totalBar := totalLabelW + (n-1)*gap
		if totalBar < avail {
			x = (avail - totalBar) / 2
		}
		for i := 0; i < n; i++ {
			positions[i] = x
			x += labelWidths[i] + gap
		}
		return positions
	}

	// Labels don't fit with spacing — compress with minimum gap.
	gap := minGap
	positions := make([]int, n)
	x := 0
	for i := 0; i < n; i++ {
		positions[i] = x
		x += labelWidths[i] + gap
	}
	return positions
}
