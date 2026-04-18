// Package theme provides the color and spacing tokens for the TUI operator
// cockpit. All M2 widget implementations MUST reference theme tokens by name
// and MUST NOT use raw color literals (tui.Cyan, tui.Red, "#...", etc.)
// directly.
//
// # Design rationale
//
// DESIGN.md specifies a dark-first operator console with "quiet contrast,
// layered panels, disciplined accent use". The palette is purpose-built for
// information-dense terminal displays: muted surfaces for backgrounds,
// bright text for readability, and a single accent (teal/cyan) for interactive
// elements and focus indicators.
//
// # No-raw-color-literals rule
//
// Widget implementations under internal/interfaces/tui/ MUST import this
// package and reference tokens (e.g., theme.Colors.Accent) instead of using
// raw tui.Cyan, tui.Red, hex strings, or ANSI indices. This ensures palette
// consistency and makes global theme changes a single-file edit.
//
// The only files exempt from this rule are:
//   - This package (theme/) itself — it defines the tokens.
//   - Generated files (*_gsx.go) that were produced by the .gsx toolchain
//     from legacy code.
//   - Non-M2 files (legacy shell, smoke modes, adapters).
package theme

import "github.com/grindlemire/go-tui"

// ColorPalette groups the named color tokens used across all TUI widgets.
// Fields are intentionally flat (not nested by category) so that token names
// are grep-friendly and tab-completable: "theme.Colors.StatusError".
type ColorPalette struct {
	// Background is the root background for the operator console.
	Background tui.Color

	// Surface and SurfaceAlt are elevated panel backgrounds. SurfaceAlt is
	// slightly lighter than Surface to create layered depth.
	Surface    tui.Color
	SurfaceAlt tui.Color

	// Border and BorderFocus are used for panel borders. BorderFocus is the
	// accent color used when a panel has keyboard focus.
	Border     tui.Color
	BorderFocus tui.Color

	// TextPrimary, TextSecondary, TextMuted, and TextInverse cover the
	// typography hierarchy. Primary is for titles and labels; Secondary for
	// metadata; Muted for hints and placeholders; Inverse for accent-backed
	// text.
	TextPrimary   tui.Color
	TextSecondary tui.Color
	TextMuted     tui.Color
	TextInverse   tui.Color

	// Accent and AccentMuted are the interactive/brand accent color.
	// Accent is used for focus rings, active tabs, links, and key indicators.
	// AccentMuted is a desaturated variant for secondary accents.
	Accent      tui.Color
	AccentMuted tui.Color

	// Status tokens encode operational state. Each must be visually distinct
	// beyond just bold-vs-not-bold per VAL-CMP-009.
	StatusOK      tui.Color
	StatusWarn    tui.Color
	StatusError   tui.Color
	StatusPending tui.Color

	// Highlight is used for search matches, selection background accents,
	// and transient attention markers.
	Highlight tui.Color

	// Divider is the color for horizontal/vertical separator lines.
	Divider tui.Color

	// SelectionBg is the background color for selected/list-focused rows.
	SelectionBg tui.Color

	// ScrollbarTrack and ScrollbarThumb style the scrollbar gutter and thumb.
	ScrollbarTrack tui.Color
	ScrollbarThumb tui.Color
}

// Colors is the singleton color palette. All widgets reference this; it is
// safe to read concurrently after init.
var Colors = ColorPalette{
	// -- Background --
	Background: tui.ANSIColor(234), // dark gray #1c1c1c

	// -- Surfaces --
	Surface:    tui.ANSIColor(236), // #303030 — raised panel background
	SurfaceAlt: tui.ANSIColor(238), // #444444 — alternative/elevated surface

	// -- Borders --
	Border:     tui.ANSIColor(240), // #585858 — default panel border
	BorderFocus: tui.RGBColor(0, 150, 180), // teal accent for focused borders

	// -- Text --
	TextPrimary:   tui.ANSIColor(252), // #d0d0d0 — bright, readable text
	TextSecondary: tui.ANSIColor(248), // #bcbcbc — secondary labels
	TextMuted:     tui.ANSIColor(243), // #767676 — hints, placeholders
	TextInverse:   tui.ANSIColor(235), // #262626 — text on accent/highlight

	// -- Accent --
	Accent:      tui.RGBColor(0, 187, 205), // #00bbcd — teal/cyan brand
	AccentMuted: tui.ANSIColor(66),         // desaturated teal for secondary

	// -- Status --
	StatusOK:      tui.RGBColor(45, 212, 168),  // #2dd4a8 — green-ish
	StatusWarn:    tui.RGBColor(251, 191, 36),  // #fbbf24 — amber
	StatusError:   tui.RGBColor(248, 113, 113), // #f87171 — red
	StatusPending: tui.RGBColor(147, 130, 220), // #9382dc — purple-ish

	// -- Highlight --
	Highlight: tui.RGBColor(255, 214, 0), // #ffd600 — search/attention

	// -- Divider --
	Divider: tui.ANSIColor(239), // #4e4e4e

	// -- Selection --
	SelectionBg: tui.ANSIColor(237), // #3a3a3a — selected row background

	// -- Scrollbar --
	ScrollbarTrack: tui.ANSIColor(236), // matches Surface
	ScrollbarThumb: tui.ANSIColor(243), // matches TextMuted tone
}

// Palette is a map version of Colors, keyed by field name. Useful for
// iteration, testing, and tooling that needs to enumerate tokens.
var Palette = map[string]tui.Color{
	"Background":     Colors.Background,
	"Surface":        Colors.Surface,
	"SurfaceAlt":     Colors.SurfaceAlt,
	"Border":         Colors.Border,
	"BorderFocus":    Colors.BorderFocus,
	"TextPrimary":    Colors.TextPrimary,
	"TextSecondary":  Colors.TextSecondary,
	"TextMuted":      Colors.TextMuted,
	"TextInverse":    Colors.TextInverse,
	"Accent":         Colors.Accent,
	"AccentMuted":    Colors.AccentMuted,
	"StatusOK":       Colors.StatusOK,
	"StatusWarn":     Colors.StatusWarn,
	"StatusError":    Colors.StatusError,
	"StatusPending":  Colors.StatusPending,
	"Highlight":      Colors.Highlight,
	"Divider":        Colors.Divider,
	"SelectionBg":    Colors.SelectionBg,
	"ScrollbarTrack": Colors.ScrollbarTrack,
	"ScrollbarThumb": Colors.ScrollbarThumb,
}

// SpacingTokens groups named spacing constants. Values are in terminal cell
// units (characters). Widgets reference these for consistent padding, margins,
// and gaps.
type SpacingTokens struct {
	// Zero is 0 — useful for conditional layouts where spacing may be toggled.
	Zero int

	// XXS through XXL form a monotonically increasing scale.
	XXS int
	XS  int
	SM  int
	MD  int
	LG  int
	XL  int
	XXL int
}

// Spacing is the singleton spacing token set.
var Spacing = SpacingTokens{
	Zero: 0,
	XXS:  1,
	XS:   2,
	SM:   3,
	MD:   4,
	LG:   6,
	XL:   8,
	XXL:  12,
}
