package tui

import (
	"strings"
	"sync"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

const (
	// MinTermWidth is the minimum terminal width the cockpit requires.
	MinTermWidth = 60
	// MinTermHeight is the minimum terminal height the cockpit requires.
	MinTermHeight = 15
)

// CockpitScreen is the root component for the M3 walking-skeleton operator
// cockpit. It renders a three-lane layout (Main / Detail / Evidence) per
// architecture.md and information-architecture.md.
//
// When the terminal is smaller than MinTermWidth×MinTermHeight, it renders a
// single-line "terminal too small" guard message instead. ESC exits with code
// 0 from either state.
type CockpitScreen struct {
	mu       sync.Mutex
	apiURL   string
	width    int
	height   int
	tooSmall bool
	phase    CockpitPhase
}

// CockpitPhase represents the current display phase of the cockpit.
type CockpitPhase string

const (
	// CockpitPhaseLoading is the initial async-boot loading phase.
	CockpitPhaseLoading CockpitPhase = "loading"
	// CockpitPhaseReady is the normal operational phase.
	CockpitPhaseReady CockpitPhase = "ready"
	// CockpitPhaseError is shown when the API is unreachable after timeout.
	CockpitPhaseError CockpitPhase = "error"
	// CockpitPhaseTooSmall is the guard shown when the terminal is below
	// the minimum size.
	CockpitPhaseTooSmall CockpitPhase = "too_small"
)

// NewCockpitScreen creates a new CockpitScreen. The apiURL is displayed in
// loading/error states.
func NewCockpitScreen(apiURL string) *CockpitScreen {
	return &CockpitScreen{
		apiURL: apiURL,
		phase:  CockpitPhaseLoading,
	}
}

// UpdateSize updates the terminal dimensions and returns whether the "too
// small" guard should be shown. This is called on resize events and at
// initial render.
func (c *CockpitScreen) UpdateSize(width, height int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.width = width
	c.height = height
	c.tooSmall = width < MinTermWidth || height < MinTermHeight
	if c.tooSmall {
		c.phase = CockpitPhaseTooSmall
	}
}

// IsTooSmall returns whether the current terminal size is below the minimum.
func (c *CockpitScreen) IsTooSmall() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tooSmall
}

// Phase returns the current cockpit phase.
func (c *CockpitScreen) Phase() CockpitPhase {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase
}

// SetPhase sets the cockpit phase. If the terminal is too small, the phase
// is forced to CockpitPhaseTooSmall regardless.
func (c *CockpitScreen) SetPhase(phase CockpitPhase) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tooSmall {
		c.phase = CockpitPhaseTooSmall
		return
	}
	c.phase = phase
}

// APIURL returns the configured API URL.
func (c *CockpitScreen) APIURL() string {
	return c.apiURL
}

// Render implements gotui.Component.
func (c *CockpitScreen) Render(app *gotui.App) *gotui.Element {
	if app != nil {
		w, h := app.Size()
		c.UpdateSize(w, h)
	}

	c.mu.Lock()
	tooSmall := c.tooSmall
	phase := c.phase
	width := c.width
	height := c.height
	apiURL := c.apiURL
	c.mu.Unlock()

	if width <= 0 || height <= 0 {
		// Use a reasonable default before first resize.
		width = 80
		height = 24
	}

	if tooSmall {
		return c.renderTooSmall(width, height)
	}

	switch phase {
	case CockpitPhaseLoading:
		return c.renderLoading(width, height, apiURL)
	case CockpitPhaseError:
		return c.renderError(width, height, apiURL)
	case CockpitPhaseReady:
		return c.renderReady(width, height)
	default:
		return c.renderLoading(width, height, apiURL)
	}
}

// KeyMap implements gotui.KeyListener.
func (c *CockpitScreen) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.On(gotui.KeyEscape, func(ke gotui.KeyEvent) {
			if ke.App() != nil {
				ke.App().Stop()
			}
		}),
		gotui.On(gotui.Rune('q'), func(ke gotui.KeyEvent) {
			if ke.App() != nil {
				ke.App().Stop()
			}
		}),
		gotui.On(gotui.Rune('c').Ctrl(), func(ke gotui.KeyEvent) {
			if ke.App() != nil {
				ke.App().Stop()
			}
		}),
	}
}

// renderTooSmall renders the minimum-terminal-size guard message.
func (c *CockpitScreen) renderTooSmall(width, height int) *gotui.Element {
	bgStyle := gotui.NewStyle().Background(theme.Colors.Background)
	textStyle := gotui.NewStyle().Foreground(theme.Colors.StatusWarn).Background(theme.Colors.Background)

	return gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(height),
		gotui.WithBackground(bgStyle),
		gotui.WithText(tooSmallMessage),
		gotui.WithTextStyle(textStyle),
	)
}

// renderLoading renders the loading state with a spinner and target URL.
func (c *CockpitScreen) renderLoading(width, height int, apiURL string) *gotui.Element {
	connectMsg := "⠋ Connecting to daemon"
	if apiURL != "" {
		connectMsg += " at " + apiURL
	}
	connectMsg += "…"

	bgStyle := gotui.NewStyle().Background(theme.Colors.Background)
	textStyle := gotui.NewStyle().Foreground(theme.Colors.TextSecondary).Background(theme.Colors.Background)

	contentHeight := height - 1 // reserve 1 row for footer
	if contentHeight < 1 {
		contentHeight = 1
	}

	root := gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(height),
		gotui.WithBackground(bgStyle),
	)
	root.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(contentHeight),
			gotui.WithBackground(bgStyle),
			gotui.WithText(connectMsg),
			gotui.WithTextStyle(textStyle),
		),
	)
	root.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText("ESC:quit"),
			gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)),
		),
	)
	return root
}

// renderError renders the connection error state.
func (c *CockpitScreen) renderError(width, height int, apiURL string) *gotui.Element {
	errMsg := "Cannot reach daemon"
	if apiURL != "" {
		errMsg += " at " + apiURL
	}

	bgStyle := gotui.NewStyle().Background(theme.Colors.Background)
	errStyle := gotui.NewStyle().Foreground(theme.Colors.StatusError).Background(theme.Colors.Background)
	hintStyle := gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)

	retryMsg := "Press r to retry or ESC to quit"
	contentHeight := height - 1 // reserve footer
	if contentHeight < 2 {
		contentHeight = 2
	}

	root := gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(height),
		gotui.WithBackground(bgStyle),
	)

	content := gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(contentHeight),
		gotui.WithBackground(bgStyle),
	)
	content.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText(errMsg),
			gotui.WithTextStyle(errStyle),
		),
	)
	content.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText(retryMsg),
			gotui.WithTextStyle(hintStyle),
		),
	)
	root.AddChild(content)
	root.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText("ESC:quit"),
			gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)),
		),
	)
	return root
}

// renderReady renders the three-lane layout (Main / Detail / Evidence).
// In the walking skeleton, this shows lane headers and an empty/loading state.
func (c *CockpitScreen) renderReady(width, height int) *gotui.Element {
	bgStyle := gotui.NewStyle().Background(theme.Colors.Background)
	headerStyle := gotui.NewStyle().Foreground(theme.Colors.TextPrimary).Background(theme.Colors.Background)
	mutedStyle := gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)

	// Layout: Main (~40%) | Detail (~35%) | Evidence (~25%)
	mainW := width * 40 / 100
	detailW := width * 35 / 100
	evidenceW := width - mainW - detailW
	if evidenceW < 0 {
		evidenceW = 0
	}

	contentHeight := height - 1 // reserve footer
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Lane headers
	mainHeader := padString("Agents", mainW)
	detailHeader := padString("Detail", detailW)
	evidenceHeader := padString("Evidence", evidenceW)

	// Empty state messages
	mainBody := padString("No agents loaded.", mainW)
	detailBody := padString("Select an agent.", detailW)
	evidenceBody := padString("", evidenceW)

	root := gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(height),
		gotui.WithBackground(bgStyle),
	)

	contentArea := gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(contentHeight),
		gotui.WithBackground(bgStyle),
	)
	// Header row
	contentArea.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText(mainHeader+detailHeader+evidenceHeader),
			gotui.WithTextStyle(headerStyle),
		),
	)
	// Body row
	if contentHeight > 1 {
		contentArea.AddChild(
			gotui.New(
				gotui.WithWidth(width),
				gotui.WithHeight(contentHeight-1),
				gotui.WithBackground(bgStyle),
				gotui.WithText(mainBody+detailBody+evidenceBody),
				gotui.WithTextStyle(mutedStyle),
			),
		)
	}
	root.AddChild(contentArea)

	// Footer
	root.AddChild(
		gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText("● connected  ESC:quit  ↑↓:nav  Enter:submit  e:evidence"),
			gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)),
		),
	)
	return root
}

func padString(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) > width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

// tooSmallMessage is the guard message shown when the terminal is below
// minimum size.
const tooSmallMessage = "terminal too small — resize to ≥60x15"

// RunCockpit is the entry point for the M3 walking-skeleton cockpit screen.
// It creates a go-tui App with the CockpitScreen as the root component and
// runs the event loop. ESC or q exits cleanly with code 0.
func RunCockpit(apiURL string) error {
	cockpit := NewCockpitScreen(apiURL)

	app, err := gotui.NewApp(gotui.WithRootComponent(cockpit))
	if err != nil {
		return err
	}
	defer app.Close()

	return app.Run()
}
