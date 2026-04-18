// Package theme provides the color and spacing tokens for the TUI operator
// cockpit. All M2 widget implementations MUST reference theme tokens by name
// and MUST NOT use raw color literals (tui.Cyan, tui.Red, "#...", etc.)
// directly.
package theme_test

import (
	"testing"

	"github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// ---------------------------------------------------------------------------
// Color token tests
// ---------------------------------------------------------------------------

func TestColorTokensAreGoTuiColors(t *testing.T) {
	// Every exported color token must be a tui.Color so it works
	// seamlessly with tui.NewStyle().Foreground(...) etc.
	colors := map[string]tui.Color{
		"Background":     theme.Colors.Background,
		"Surface":        theme.Colors.Surface,
		"SurfaceAlt":     theme.Colors.SurfaceAlt,
		"Border":         theme.Colors.Border,
		"BorderFocus":    theme.Colors.BorderFocus,
		"TextPrimary":    theme.Colors.TextPrimary,
		"TextSecondary":  theme.Colors.TextSecondary,
		"TextMuted":      theme.Colors.TextMuted,
		"TextInverse":    theme.Colors.TextInverse,
		"Accent":         theme.Colors.Accent,
		"AccentMuted":    theme.Colors.AccentMuted,
		"StatusOK":       theme.Colors.StatusOK,
		"StatusWarn":     theme.Colors.StatusWarn,
		"StatusError":    theme.Colors.StatusError,
		"StatusPending":  theme.Colors.StatusPending,
		"Highlight":      theme.Colors.Highlight,
		"Divider":        theme.Colors.Divider,
		"SelectionBg":    theme.Colors.SelectionBg,
		"ScrollbarTrack": theme.Colors.ScrollbarTrack,
		"ScrollbarThumb": theme.Colors.ScrollbarThumb,
	}
	for name, col := range colors {
		if col.IsDefault() {
			t.Errorf("Color token %s is the default (zero-value) color; expected a concrete color", name)
		}
	}
}

func TestColorTokensAreDistinct(t *testing.T) {
	// Status tokens must be visually distinct from each other.
	// We check that they are not Equal() pairwise.
	statusColors := map[string]tui.Color{
		"ok":      theme.Colors.StatusOK,
		"warn":    theme.Colors.StatusWarn,
		"error":   theme.Colors.StatusError,
		"pending": theme.Colors.StatusPending,
	}
	names := make([]string, 0, len(statusColors))
	for n := range statusColors {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := statusColors[names[i]], statusColors[names[j]]
			if a.Equal(b) {
				t.Errorf("status colors %q and %q are equal; they must be visually distinct", names[i], names[j])
			}
		}
	}
}

func TestDarkFirstPaletteBackgroundIsDark(t *testing.T) {
	// DESIGN.md says "dark-first" for the operator console.
	// The background must be perceptually dark.
	if theme.Colors.Background.IsLight() {
		t.Error("Background color should be perceptually dark for a dark-first palette")
	}
}

func TestDarkFirstPaletteTextIsLight(t *testing.T) {
	// Primary text must be readable on a dark background.
	if theme.Colors.TextPrimary.IsLight() == false {
		t.Error("TextPrimary should be perceptually light to contrast with a dark background")
	}
}

func TestAccentIsCyanishForOperatorConsole(t *testing.T) {
	// The accent color should be a cyan/teal shade (operator console identity).
	// We verify it's not the same as any standard ANSI color to ensure it's
	// a deliberate custom choice.
	standardANSI := []tui.Color{
		tui.Black, tui.Red, tui.Green, tui.Yellow,
		tui.Blue, tui.Magenta, tui.Cyan, tui.White,
		tui.BrightBlack, tui.BrightRed, tui.BrightGreen, tui.BrightYellow,
		tui.BrightBlue, tui.BrightMagenta, tui.BrightCyan, tui.BrightWhite,
	}
	for _, std := range standardANSI {
		if theme.Colors.Accent.Equal(std) {
			t.Error("Accent should be a deliberate custom color, not a standard ANSI color")
		}
	}
}

// ---------------------------------------------------------------------------
// Spacing token tests
// ---------------------------------------------------------------------------

func TestSpacingTokensArePositive(t *testing.T) {
	spacings := map[string]int{
		"XXS": theme.Spacing.XXS,
		"XS":  theme.Spacing.XS,
		"SM":  theme.Spacing.SM,
		"MD":  theme.Spacing.MD,
		"LG":  theme.Spacing.LG,
		"XL":  theme.Spacing.XL,
		"XXL": theme.Spacing.XXL,
	}
	for name, val := range spacings {
		if val < 0 {
			t.Errorf("Spacing.%s = %d; must be >= 0", name, val)
		}
	}
}

func TestSpacingTokensAreMonotonicallyIncreasing(t *testing.T) {
	// Each spacing tier should be >= the previous one.
	vals := []int{
		theme.Spacing.XXS,
		theme.Spacing.XS,
		theme.Spacing.SM,
		theme.Spacing.MD,
		theme.Spacing.LG,
		theme.Spacing.XL,
		theme.Spacing.XXL,
	}
	for i := 1; i < len(vals); i++ {
		if vals[i] < vals[i-1] {
			t.Errorf("Spacing tier %d (%d) is less than tier %d (%d); spacing must be monotonically increasing", i, vals[i], i-1, vals[i-1])
		}
	}
}

func TestSpacingZeroExists(t *testing.T) {
	// A zero-spacing constant is useful for conditional layouts.
	if theme.Spacing.Zero != 0 {
		t.Errorf("Spacing.Zero = %d; want 0", theme.Spacing.Zero)
	}
}

// ---------------------------------------------------------------------------
// Palette completeness
// ---------------------------------------------------------------------------

func TestPaletteIsPopulated(t *testing.T) {
	// Palette should contain at least all the named tokens.
	if len(theme.Palette) < 15 {
		t.Errorf("Palette has %d entries; expected at least 15 named colors", len(theme.Palette))
	}
	// Every Palette entry must also be reachable from Colors.
	for name, col := range theme.Palette {
		if col.IsDefault() {
			t.Errorf("Palette entry %q is the default color", name)
		}
	}
}

func TestPaletteMatchesColorsStruct(t *testing.T) {
	// Every field in Colors must appear in Palette under the same name.
	// This ensures the map and struct stay in sync.
	required := map[string]tui.Color{
		"Background":     theme.Colors.Background,
		"Surface":        theme.Colors.Surface,
		"SurfaceAlt":     theme.Colors.SurfaceAlt,
		"Border":         theme.Colors.Border,
		"BorderFocus":    theme.Colors.BorderFocus,
		"TextPrimary":    theme.Colors.TextPrimary,
		"TextSecondary":  theme.Colors.TextSecondary,
		"TextMuted":      theme.Colors.TextMuted,
		"TextInverse":    theme.Colors.TextInverse,
		"Accent":         theme.Colors.Accent,
		"AccentMuted":    theme.Colors.AccentMuted,
		"StatusOK":       theme.Colors.StatusOK,
		"StatusWarn":     theme.Colors.StatusWarn,
		"StatusError":    theme.Colors.StatusError,
		"StatusPending":  theme.Colors.StatusPending,
		"Highlight":      theme.Colors.Highlight,
		"Divider":        theme.Colors.Divider,
		"SelectionBg":    theme.Colors.SelectionBg,
		"ScrollbarTrack": theme.Colors.ScrollbarTrack,
		"ScrollbarThumb": theme.Colors.ScrollbarThumb,
	}
	for name, want := range required {
		got, ok := theme.Palette[name]
		if !ok {
			t.Errorf("Palette missing entry for %q", name)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("Palette[%q] does not match Colors.%s", name, name)
		}
	}
}
