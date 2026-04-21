package components

import (
	"strings"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// ---------------------------------------------------------------------------
// StatusFooter
// ---------------------------------------------------------------------------

// KeybindHintConfig is a compact description of a single keybinding for the
// status footer strip.
type KeybindHintConfig struct {
	Key  string
	Desc string
}

// StatusFooterConfig configures a StatusFooter.
type StatusFooterConfig struct {
	// ConnectionStatus is the current connection health indicator.
	ConnectionStatus StatusVariant
	// ConnectionLabel is the text label for the connection status
	// (e.g., "connected", "degraded", "error", "connecting…").
	ConnectionLabel string
	// ActiveEntity is the currently selected entity label, e.g.
	// "agent: abc12345 (researcher)". Empty string means no active entity.
	ActiveEntity string
	// Keybinds is the list of current keybinding hints to display.
	// At least 3 are recommended per VAL-SKEL-012.
	Keybinds []KeybindHintConfig
	// Width is the available width in cells.
	Width int
}

// StatusFooter is a single-row status bar visible on every M3 screen.
// It contains three required elements per VAL-SKEL-012:
//
//	(i)   connection status indicator (ok / degraded / error)
//	(ii)  active entity label
//	(iii) compact keybinding hint strip naming ≥3 current bindings
//
// StatusFooter is stateless. It renders entirely from props with no internal
// state.
type StatusFooter struct {
	config StatusFooterConfig
}

// NewStatusFooter creates a new StatusFooter from the given config.
func NewStatusFooter(cfg StatusFooterConfig) *StatusFooter {
	return &StatusFooter{config: cfg}
}

// Render draws the StatusFooter into the given buffer. The footer always
// renders as a single row. If the content would exceed the available width,
// the active entity label is truncated first, then keybindings are dropped
// from the right until everything fits.
func (sf *StatusFooter) Render(buf *tui.Buffer) {
	width := sf.config.Width
	if width <= 0 || buf.Height() < 1 {
		return
	}

	// Build keybinding hints.
	var hints []string
	for _, kb := range sf.config.Keybinds {
		hints = append(hints, kb.Key+":"+kb.Desc)
	}
	hintText := strings.Join(hints, "  ")

	// Build badge text from icon + label.
	icon := statusBadgeIcon(sf.config.ConnectionStatus)
	var badgeStr string
	if icon != "" {
		badgeStr = icon + " " + sf.config.ConnectionLabel
	} else {
		badgeStr = sf.config.ConnectionLabel
	}
	badgeWidth := runeWidth(badgeStr)
	if badgeWidth > width {
		badgeStr = truncate(badgeStr, width)
		badgeWidth = runeWidth(badgeStr)
	}

	// Build the footer content in segments with their styles.
	// Each segment is {text, style}. We concatenate text and apply
	// SetString for each segment so that RenderFull picks it up.
	type segment struct {
		text  string
		style tui.Style
	}
	var segs []segment

	// Badge segment.
	badgeStyle := tui.NewStyle().Foreground(statusBadgeColor(sf.config.ConnectionStatus))
	segs = append(segs, segment{text: badgeStr, style: badgeStyle})

	// Separator.
	sepStyle := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	segs = append(segs, segment{text: "  ", style: sepStyle})

	// Active entity (if present).
	entityText := sf.config.ActiveEntity
	if entityText != "" {
		entityStyle := tui.NewStyle().Foreground(theme.Colors.TextSecondary)
		remaining := width - badgeWidth - 2 // badge + sep
		entityWidth := runeWidth(entityText)
		if entityWidth > remaining {
			entityText = truncate(entityText, remaining)
			entityWidth = runeWidth(entityText)
		}
		segs = append(segs, segment{text: entityText, style: entityStyle})
		segs = append(segs, segment{text: "  ", style: sepStyle})
		remaining -= entityWidth + 2
		_ = remaining
	}

	// Keybinding hints — drop from right until they fit.
	remaining := width - badgeWidth - 2
	if entityText != "" {
		remaining -= runeWidth(entityText) + 2
	}
	if remaining < 0 {
		remaining = 0
	}
	hintWidth := runeWidth(hintText)
	if hintWidth > remaining && remaining > 0 {
		for len(hints) > 0 {
			hints = hints[:len(hints)-1]
			hintText = strings.Join(hints, "  ")
			hintWidth = runeWidth(hintText)
			if hintWidth <= remaining {
				break
			}
		}
	}
	hintStyle := tui.NewStyle().Foreground(theme.Colors.TextMuted)
	segs = append(segs, segment{text: hintText, style: hintStyle})

	// Render each segment with SetString at the current x position.
	x := 0
	for _, seg := range segs {
		if x >= width {
			break
		}
		segText := seg.text
		segWidth := runeWidth(segText)
		if x+segWidth > width {
			segText = truncate(segText, width-x)
			segWidth = runeWidth(segText)
		}
		if segWidth > 0 {
			buf.SetString(x, 0, segText, seg.style)
			x += segWidth
		}
	}

	// Fill remainder with background-colored spaces.
	bgStyle := tui.NewStyle().Background(theme.Colors.Background)
	for x < width {
		buf.SetString(x, 0, " ", bgStyle)
		x++
	}
}
