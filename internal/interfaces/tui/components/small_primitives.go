package components

import (
	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// ---------------------------------------------------------------------------
// StatusBadge
// ---------------------------------------------------------------------------

// StatusBadge is a small inline badge that renders a status indicator with a
// distinct visual style per variant. Used in EntityList rows, DetailPane
// headers, and the status footer.
//
// Each variant MUST render with a distinct ANSI color sequence (not just
// bold-vs-not-bold) per VAL-CMP-009. The colors are defined as theme tokens.
//
// StatusBadge is stateless. It renders entirely from props with no internal
// state.
type StatusBadge struct {
	variant StatusVariant
	label   string
	width   int // if > 0, pad/truncate to this width; if 0, auto-size.
}

// NewStatusBadge creates a new StatusBadge with the given variant and label.
// If width > 0, the badge is padded/truncated to that width. If width == 0,
// the badge auto-sizes to fit the content.
func NewStatusBadge(variant StatusVariant, label string, width int) *StatusBadge {
	return &StatusBadge{
		variant: variant,
		label:   label,
		width:   width,
	}
}

// Render draws the StatusBadge into the given buffer.
func (sb *StatusBadge) Render(buf *tui.Buffer) {
	color := statusBadgeColor(sb.variant)
	icon := statusBadgeIcon(sb.variant)

	text := sb.label
	if icon != "" {
		text = icon + " " + sb.label
	}

	style := tui.NewStyle().Foreground(color)

	if sb.width > 0 {
		text = padOrTruncate(text, sb.width)
		buf.SetString(0, 0, text, style)
	} else {
		buf.SetString(0, 0, text, style)
	}
}

// ---------------------------------------------------------------------------
// EmptyState
// ---------------------------------------------------------------------------

// EmptyState is a small, centered informational widget that renders when a
// list or panel has no content. Used by EntityList, DetailPane, and other
// containers. Per DESIGN.md: empty states must include an action.
//
// EmptyState is stateless. It renders entirely from props with no internal
// state.
type EmptyState struct {
	message string
	cta     string
	width   int
	height  int
	icon    string
}

// EmptyStateOption configures an EmptyState at construction time.
type EmptyStateOption func(*EmptyState)

// WithIcon sets an optional icon character.
func WithIcon(icon string) EmptyStateOption {
	return func(es *EmptyState) { es.icon = icon }
}

// NewEmptyState creates a new EmptyState with the given message, optional CTA,
// and dimensions.
func NewEmptyState(message, cta string, width, height int, opts ...EmptyStateOption) *EmptyState {
	es := &EmptyState{
		message: message,
		cta:     cta,
		width:   width,
		height:  height,
	}
	for _, opt := range opts {
		opt(es)
	}
	return es
}

// Render draws the EmptyState into the given buffer.
func (es *EmptyState) Render(buf *tui.Buffer) {
	if es.width <= 0 || es.height <= 0 {
		return
	}

	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	ctaStyle := tui.NewStyle().Foreground(theme.Colors.TextSecondary)

	// Center vertically.
	startY := es.height / 2
	if startY >= es.height {
		startY = 0
	}

	row := startY

	// Optional icon line.
	if es.icon != "" && row < es.height {
		iconStyle := tui.NewStyle().Foreground(theme.Colors.TextSecondary)
		buf.SetString(0, row, center(es.icon, es.width), iconStyle)
		row++
	}

	// Message line.
	if row < es.height {
		buf.SetString(0, row, center(es.message, es.width), style)
		row++
	}

	// CTA line.
	if es.cta != "" && row < es.height {
		buf.SetString(0, row, center(es.cta, es.width), ctaStyle)
	}
}

// ---------------------------------------------------------------------------
// LoadingState
// ---------------------------------------------------------------------------

// LoadingState is a small, centered informational widget that renders during
// async operations. Displays a spinner and explanatory message. Per
// information-architecture.md §(h): loading states must explain what is being
// prepared.
//
// LoadingState holds a spinnerIndex that is advanced externally (by a watcher
// timer in the parent component). The Render function reads it to determine
// which spinner frame to display.
type LoadingState struct {
	message       string
	width         int
	height        int
	spinnerFrames []string
	spinnerIndex  int
}

// LoadingStateOption configures a LoadingState at construction time.
type LoadingStateOption func(*LoadingState)

// WithSpinnerFrames sets custom spinner frame characters.
func WithSpinnerFrames(frames []string) LoadingStateOption {
	return func(ls *LoadingState) { ls.spinnerFrames = frames }
}

// defaultSpinnerFrames is the standard braille spinner sequence.
var defaultSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewLoadingState creates a new LoadingState with the given message and
// dimensions.
func NewLoadingState(message string, width, height int, opts ...LoadingStateOption) *LoadingState {
	ls := &LoadingState{
		message:       message,
		width:         width,
		height:        height,
		spinnerFrames: defaultSpinnerFrames,
		spinnerIndex:  0,
	}
	for _, opt := range opts {
		opt(ls)
	}
	return ls
}

// SetSpinnerIndex sets the current spinner frame index. Wraps around
// automatically.
func (ls *LoadingState) SetSpinnerIndex(idx int) {
	n := len(ls.spinnerFrames)
	if n == 0 {
		return
	}
	ls.spinnerIndex = idx % n
}

// SpinnerIndex returns the current spinner frame index.
func (ls *LoadingState) SpinnerIndex() int {
	return ls.spinnerIndex
}

// Render draws the LoadingState into the given buffer.
func (ls *LoadingState) Render(buf *tui.Buffer) {
	if ls.width <= 0 || ls.height <= 0 {
		return
	}

	frames := ls.spinnerFrames
	if len(frames) == 0 {
		frames = []string{""}
	}
	spinner := frames[ls.spinnerIndex%len(frames)]
	compact := ls.height <= 3

	style := tui.NewStyle().Foreground(theme.Colors.TextMuted)

	if compact {
		// Single-line: spinner + message.
		text := spinner + " " + ls.message
		text = padOrTruncate(text, ls.width)
		buf.SetString(0, 0, text, style)
	} else {
		// Centered: spinner on one line, message below.
		startY := ls.height/2 - 1
		if startY < 0 {
			startY = 0
		}

		// Spinner line.
		if startY < ls.height {
			spinnerStyle := tui.NewStyle().Foreground(theme.Colors.TextSecondary)
			buf.SetString(0, startY, center(spinner, ls.width), spinnerStyle)
		}

		// Message line.
		if startY+1 < ls.height {
			buf.SetString(0, startY+1, center(ls.message, ls.width), style)
		}
	}
}

// ---------------------------------------------------------------------------
// KeybindHint
// ---------------------------------------------------------------------------

// KeybindHint is a compact inline display of a keybinding hint (key +
// description). Used in the status footer strip and as inline hints in empty
// states and help overlays.
//
// KeybindHint is stateless. It renders entirely from props with no internal
// state.
type KeybindHint struct {
	key       string
	desc      string
	separator string
	compact   bool
}

// NewKeybindHint creates a new KeybindHint with the given key, description,
// separator (empty for default ":"), and compact mode.
func NewKeybindHint(key, desc, separator string, compact bool) *KeybindHint {
	sep := separator
	if sep == "" {
		sep = ":"
	}
	return &KeybindHint{
		key:       key,
		desc:      desc,
		separator: sep,
		compact:   compact,
	}
}

// Render draws the KeybindHint into the given buffer.
func (kh *KeybindHint) Render(buf *tui.Buffer) {
	keyStyle := tui.NewStyle().Foreground(theme.Colors.TextSecondary).Bold()
	descStyle := tui.NewStyle().Foreground(theme.Colors.TextMuted)

	if kh.compact {
		// Single-line: Key:Desc
		text := kh.key + kh.separator + kh.desc
		buf.SetString(0, 0, text, keyStyle)
	} else {
		// Two-line: Key on line 0, Description on line 1.
		buf.SetString(0, 0, kh.key, keyStyle)
		if buf.Height() > 1 {
			buf.SetString(0, 1, kh.desc, descStyle)
		}
	}
}
