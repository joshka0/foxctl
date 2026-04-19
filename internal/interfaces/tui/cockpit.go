package tui

import (
	"sort"
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

// AgentInventoryItem is a row in the Main lane agent inventory. It contains
// the six required display fields per information-architecture.md.
type AgentInventoryItem struct {
	ID         string
	Role       string
	Status     string
	Workspace  string
	ParentID   string // "—" if root (no parent)
	LastActive string // formatted timestamp or "—" if never
}

// StubAgent is a lightweight agent record used by the walking skeleton to
// display inventory rows before the live API is connected.
// Deprecated: use AgentInventoryItem for new code.
type StubAgent struct {
	ID     string
	Role   string
	Status string
}

// CockpitScreen is the root component for the M3 walking-skeleton operator
// cockpit. It renders a three-lane layout (Main / Detail / Evidence) per
// architecture.md and information-architecture.md.
//
// When the terminal is smaller than MinTermWidth×MinTermHeight, it renders a
// single-line "terminal too small" guard message instead. ESC exits with code
// 0 from either state.
type CockpitScreen struct {
	mu            sync.Mutex
	apiURL        string
	width         int
	height        int
	tooSmall      bool
	phase         CockpitPhase
	agents        []AgentInventoryItem
	selectedIndex int
	bootManager   *BootManager // nil until SetBootManager is called
	phaseChanges  chan CockpitPhase
	app           *gotui.App // reference for triggering re-renders
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
		apiURL:        apiURL,
		phase:         CockpitPhaseLoading,
		selectedIndex: -1,
		phaseChanges:  make(chan CockpitPhase, 8),
	}
}

// SetStubAgents sets the stub agent list for the walking-skeleton inventory.
// Deprecated: use SetAgents for new code.
func (c *CockpitScreen) SetStubAgents(agents []StubAgent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	items := make([]AgentInventoryItem, len(agents))
	for i, a := range agents {
		items[i] = AgentInventoryItem{
			ID:     a.ID,
			Role:   a.Role,
			Status: a.Status,
		}
	}
	c.agents = items
	c.clampSelectionLocked()
}

// SetAgents sets the live agent inventory items. The items are sorted
// deterministically (by Role ascending, then ID ascending) before storage.
func (c *CockpitScreen) SetAgents(agents []AgentInventoryItem) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents = sortAgents(agents)
	c.clampSelectionLocked()
}

// Agents returns a copy of the current agent inventory items.
func (c *CockpitScreen) Agents() []AgentInventoryItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]AgentInventoryItem, len(c.agents))
	copy(result, c.agents)
	return result
}

// SelectedIndex returns the index of the currently selected agent (-1 if none).
func (c *CockpitScreen) SelectedIndex() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selectedIndex
}

// SetSelectedIndex sets the selected agent index. The value is clamped to
// [0, len(stubAgents)-1].
func (c *CockpitScreen) SetSelectedIndex(idx int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.selectedIndex = idx
	c.clampSelectionLocked()
}

// ClampSelection clamps the current selectedIndex to a valid range.
func (c *CockpitScreen) ClampSelection() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clampSelectionLocked()
}

// clampSelectionLocked clamps selectedIndex. Caller must hold c.mu.
func (c *CockpitScreen) clampSelectionLocked() {
	n := len(c.agents)
	if n == 0 {
		c.selectedIndex = -1
		return
	}
	if c.selectedIndex < 0 {
		c.selectedIndex = 0
	}
	if c.selectedIndex >= n {
		c.selectedIndex = n - 1
	}
}

// NavigateDown moves the selection to the next agent (wraps around).
func (c *CockpitScreen) NavigateDown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.agents)
	if n == 0 {
		return
	}
	if c.selectedIndex < 0 {
		c.selectedIndex = 0
		return
	}
	c.selectedIndex = (c.selectedIndex + 1) % n
}

// NavigateUp moves the selection to the previous agent (wraps around).
func (c *CockpitScreen) NavigateUp() {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.agents)
	if n == 0 {
		return
	}
	if c.selectedIndex <= 0 {
		c.selectedIndex = n - 1
	} else {
		c.selectedIndex--
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
// is forced to CockpitPhaseTooSmall regardless. The phase change is broadcast
// on the phaseChanges channel. The channel watcher in Watchers() will pick it
// up on the main loop and toggle renderTrigger to force a re-render.
//
// This method is safe to call from any goroutine — it only sends on a buffered
// channel and does not call State.Set() directly.
func (c *CockpitScreen) SetPhase(phase CockpitPhase) {
	c.mu.Lock()
	if c.tooSmall {
		c.mu.Unlock()
		return
	}
	c.phase = phase
	c.mu.Unlock()
	// Non-blocking send to notify watchers. The watcher handler runs on the
	// main loop and will toggle renderTrigger.
	select {
	case c.phaseChanges <- phase:
	default:
	}
}

// APIURL returns the configured API URL.
func (c *CockpitScreen) APIURL() string {
	return c.apiURL
}

// SetBootManager sets the boot manager for retry handling. When the screen is
// in CockpitPhaseError, pressing 'r' will call bm.Retry().
func (c *CockpitScreen) SetBootManager(bm *BootManager) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bootManager = bm
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

	var el *gotui.Element
	if tooSmall {
		el = c.renderTooSmall(width, height)
	} else {
		switch phase {
		case CockpitPhaseLoading:
			el = c.renderLoading(width, height, apiURL)
		case CockpitPhaseError:
			el = c.renderError(width, height, apiURL)
		case CockpitPhaseReady:
			el = c.renderReady(width, height)
		default:
			el = c.renderLoading(width, height, apiURL)
		}
	}

	return el
}

// KeyMap implements gotui.KeyListener.
func (c *CockpitScreen) KeyMap() gotui.KeyMap {
	km := gotui.KeyMap{
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
		gotui.On(gotui.Rune('r'), func(ke gotui.KeyEvent) {
			c.mu.Lock()
			bm := c.bootManager
			phase := c.phase
			c.mu.Unlock()

			// Only retry in error phase.
			if phase == CockpitPhaseError && bm != nil {
				bm.Retry()
			}
		}),
		gotui.On(gotui.KeyDown, func(ke gotui.KeyEvent) {
			c.NavigateDown()
		}),
		gotui.On(gotui.Rune('j'), func(ke gotui.KeyEvent) {
			c.NavigateDown()
		}),
		gotui.On(gotui.KeyUp, func(ke gotui.KeyEvent) {
			c.NavigateUp()
		}),
		gotui.On(gotui.Rune('k'), func(ke gotui.KeyEvent) {
			c.NavigateUp()
		}),
	}
	return km
}

// Watchers implements the gotui WatcherProvider interface. Returns channel
// watchers that bridge the BootManager's background goroutine into the main
// render loop.
func (c *CockpitScreen) Watchers() []gotui.Watcher {
	return []gotui.Watcher{
		// Channel watcher: receives phase changes from BootManager goroutine.
		// The handler runs on the main loop and marks the app as dirty to
		// trigger a re-render.
		gotui.Watch(c.phaseChanges, func(_ CockpitPhase) {
			c.mu.Lock()
			a := c.app
			c.mu.Unlock()
			if a != nil {
				a.MarkDirty()
			}
		}),
	}
}

// BindApp stores the app reference for triggering re-renders. Implements
// gotui.AppBinder.
func (c *CockpitScreen) BindApp(app *gotui.App) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.app = app
}

// UnbindApp clears the app reference. Implements gotui.AppUnbinder.
func (c *CockpitScreen) UnbindApp() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.app = nil
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
			gotui.WithText("ESC:quit  r:retry"),
			gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)),
		),
	)
	return root
}

// renderReady renders the three-lane layout (Main / Detail / Evidence) with
// vertical box-drawing separators between lanes. The layout adapts to terminal
// width. At very narrow widths the Evidence lane may collapse to 0 width.
func (c *CockpitScreen) renderReady(width, height int) *gotui.Element {
	c.mu.Lock()
	agents := c.agents
	selIdx := c.selectedIndex
	c.mu.Unlock()

	bgStyle := gotui.NewStyle().Background(theme.Colors.Background)
	headerStyle := gotui.NewStyle().Foreground(theme.Colors.TextPrimary).Background(theme.Colors.Background)
	mutedStyle := gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)
	sepStyle := gotui.NewStyle().Foreground(theme.Colors.Divider).Background(theme.Colors.Background)
	selStyle := gotui.NewStyle().Foreground(theme.Colors.TextPrimary).Background(theme.Colors.SelectionBg)

	// Compute lane widths. Reserve 2 columns for separators between the 3 lanes.
	// Layout: Main (~40%) | sep | Detail (~35%) | sep | Evidence (~25%)
	var mainW, detailW, evidenceW, sepCount int
	if width >= 80 {
		sepCount = 2 // two separator columns between 3 lanes
		availW := width - sepCount
		mainW = availW * 40 / 100
		detailW = availW * 35 / 100
		evidenceW = availW - mainW - detailW
	} else if width >= 60 {
		// At minimum width: 2 separators, narrower lanes.
		sepCount = 2
		availW := width - sepCount
		mainW = availW * 45 / 100
		detailW = availW * 35 / 100
		evidenceW = availW - mainW - detailW
	} else {
		// Below minimum — only show Main lane.
		sepCount = 0
		mainW = width
		detailW = 0
		evidenceW = 0
	}

	contentHeight := height - 1 // reserve footer
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Build the header row with lane names and separators.
	headerText := buildLanedRow(
		padString("Agents", mainW),
		padString("Detail", detailW),
		padString("Evidence", evidenceW),
		"┬", sepCount, width,
	)

	// Build body rows.
	bodyLines := make([]string, 0, contentHeight-1)
	if contentHeight > 1 {
		if len(agents) == 0 {
			// Empty state: show guidance in Main lane.
			emptyMsg := "No agents running."
			ctaMsg := "Spawn: foxctl agent spawn --role ..."
			for row := 0; row < contentHeight-1; row++ {
				var mainBody, detailBody, evidenceBody string
				mid := (contentHeight - 1) / 2
				if row == mid {
					mainBody = centerInWidth(emptyMsg, mainW)
					detailBody = centerInWidth("Select an agent.", detailW)
				} else if row == mid+1 {
					mainBody = centerInWidth(ctaMsg, mainW)
					detailBody = padString("", detailW)
				} else {
					mainBody = padString("", mainW)
					detailBody = padString("", detailW)
				}
				evidenceBody = padString("", evidenceW)
				bodyLines = append(bodyLines, buildLanedRow(
					mainBody, detailBody, evidenceBody,
					"│", sepCount, width,
				))
			}
		} else {
			// Render agent rows with six required fields:
			// short ID, role, status, workspace label, parent link, last-activity time.
			for row := 0; row < contentHeight-1; row++ {
				var mainBody, detailBody, evidenceBody string
				if row < len(agents) {
					a := agents[row]
					label := buildAgentInventoryLabel(a, mainW)
					if row == selIdx {
						label = "▸ " + label
						mainBody = padString(label, mainW)
						// Detail shows selected agent info.
						detailBody = padString("Role: "+a.Role+"  Status: "+a.Status, detailW)
					} else {
						mainBody = padString("  "+label, mainW)
						detailBody = padString("", detailW)
					}
				} else {
					mainBody = padString("", mainW)
					detailBody = padString("", detailW)
				}
				evidenceBody = padString("", evidenceW)
				bodyLines = append(bodyLines, buildLanedRow(
					mainBody, detailBody, evidenceBody,
					"│", sepCount, width,
				))
			}
		}
	}

	// Build footer row with bottom T-junctions.
	footerText := buildLanedRow(
		padString("", mainW),
		padString("", detailW),
		padString("", evidenceW),
		"┴", sepCount, width,
	)

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

	// Header row.
	contentArea.AddChild(gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(1),
		gotui.WithBackground(bgStyle),
		gotui.WithText(headerText),
		gotui.WithTextStyle(headerStyle),
	))

	// Body rows.
	for i, line := range bodyLines {
		lineStyle := mutedStyle
		// If this is the selected agent row, apply selection style to the main
		// lane portion of the text.
		if len(agents) > 0 && i == selIdx {
			lineStyle = selStyle
		}
		contentArea.AddChild(gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText(line),
			gotui.WithTextStyle(lineStyle),
		))
	}

	// Bottom separator / footer junction row.
	if contentHeight > 1 {
		contentArea.AddChild(gotui.New(
			gotui.WithWidth(width),
			gotui.WithHeight(1),
			gotui.WithBackground(bgStyle),
			gotui.WithText(footerText),
			gotui.WithTextStyle(sepStyle),
		))
	}

	root.AddChild(contentArea)

	// Status footer.
	footerHint := "● connected  ESC:quit  ↑↓:nav  Enter:submit  e:evidence"
	root.AddChild(gotui.New(
		gotui.WithWidth(width),
		gotui.WithHeight(1),
		gotui.WithBackground(bgStyle),
		gotui.WithText(footerHint),
		gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextMuted).Background(theme.Colors.Background)),
	))
	return root
}

// buildAgentInventoryLabel builds a compact inventory row label from an
// AgentInventoryItem, fitting as many of the six required fields as possible
// into the given width: short ID, role, status, workspace, parent, last active.
func buildAgentInventoryLabel(a AgentInventoryItem, width int) string {
	if width <= 0 {
		return ""
	}
	parent := a.ParentID
	if parent == "" {
		parent = "—"
	}
	last := a.LastActive
	if last == "" {
		last = "—"
	}
	ws := a.Workspace
	if ws == "" {
		ws = "—"
	}

	// Try the full format first.
	full := shortID(a.ID) + " " + a.Role + " " + a.Status + " " + ws + " " + parent + " " + last
	if runeWidth(full) <= width {
		return full
	}

	// Fallback: drop last-active.
	medium := shortID(a.ID) + " " + a.Role + " " + a.Status + " " + ws + " " + parent
	if runeWidth(medium) <= width {
		return medium
	}

	// Fallback: drop parent too.
	short := shortID(a.ID) + " " + a.Role + " " + a.Status + " " + ws
	if runeWidth(short) <= width {
		return short
	}

	// Fallback: just ID + role + status.
	minimal := shortID(a.ID) + " " + a.Role + " " + a.Status
	if runeWidth(minimal) <= width {
		return minimal
	}

	// Ultimate fallback: truncate.
	return truncateLabel(minimal, width)
}

// truncateLabel truncates s to fit within maxCells display width.
func truncateLabel(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	totalWidth := 0
	for i, r := range s {
		rw := int(gotui.RuneWidth(r))
		if totalWidth+rw > maxCells {
			if maxCells >= 1 {
				avail := maxCells - 1
				w := 0
				for j, rr := range s {
					rrw := int(gotui.RuneWidth(rr))
					if w+rrw > avail {
						return string([]rune(s[:j])) + "…"
					}
					w += rrw
				}
			}
			return string([]rune(s[:i])) + "…"
		}
		totalWidth += rw
	}
	return s
}

// runeWidth returns the number of terminal cells occupied by s.
func runeWidth(s string) int {
	w := 0
	for _, r := range s {
		w += int(gotui.RuneWidth(r))
	}
	return w
}

// sortAgents returns a deterministically sorted copy of agents:
// by Role ascending, then ID ascending.
func sortAgents(agents []AgentInventoryItem) []AgentInventoryItem {
	result := make([]AgentInventoryItem, len(agents))
	copy(result, agents)
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Role != result[j].Role {
			return result[i].Role < result[j].Role
		}
		return result[i].ID < result[j].ID
	})
	return result
}

// buildLanedRow concatenates Main + Detail + Evidence lane content with
// separator characters between them. The sepRune is used for the separator
// (│ for body, ┬ for header, ┴ for footer). The result is padded to exactly
// width characters.
func buildLanedRow(main, detail, evidence, sepRune string, sepCount, width int) string {
	if sepCount == 0 {
		return padString(main, width)
	}

	// Extract first rune correctly for multi-byte UTF-8 separators like │, ┬, ┴.
	runes := []rune(sepRune)
	sep := "│"
	if len(runes) > 0 {
		sep = string(runes[0])
	}

	if evidence == "" && sepCount >= 2 {
		// Two lanes only: Main | Detail
		result := main + sep + detail
		return padString(result, width)
	}

	result := main + sep + detail + sep + evidence
	return padString(result, width)
}

// shortID returns a shortened agent ID for display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// centerInWidth centers text within a given width.
func centerInWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if len(s) >= w {
		return s[:w]
	}
	pad := (w - len(s)) / 2
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", w-pad-len(s))
}

func padString(s string, width int) string {
	if width <= 0 {
		return ""
	}
	// Use display width (not byte count) for correct truncation of Unicode.
	sw := 0
	for _, r := range s {
		sw += int(gotui.RuneWidth(r))
	}
	if sw > width {
		// Truncate by display width.
		w := 0
		for i, r := range s {
			w += int(gotui.RuneWidth(r))
			if w > width {
				return string([]rune(s[:i]))
			}
		}
		return s
	}
	return s + strings.Repeat(" ", width-sw)
}

// tooSmallMessage is the guard message shown when the terminal is below
// minimum size.
const tooSmallMessage = "terminal too small — resize to ≥60x15"

// RunCockpit is the entry point for the M3 walking-skeleton cockpit screen.
// It creates a go-tui App with the CockpitScreen as the root component,
// starts the async BootManager, and runs the event loop. ESC or q exits
// cleanly with code 0.
//
// The screen starts in CockpitPhaseLoading. The BootManager performs a
// background health check against the API URL and transitions the screen to
// CockpitPhaseReady or CockpitPhaseError based on the result. No synchronous
// HTTP is performed on the UI thread.
func RunCockpit(apiURL string) error {
	cockpit := NewCockpitScreen(apiURL)

	// Build an API client and agent adapter so the boot manager can fetch
	// the live agent inventory after the health check succeeds.
	apiClient, err := NewAPIClient(apiURL, nil)
	if err != nil {
		return err
	}
	agentAdapter, err := NewAgentAdapter(apiClient)
	if err != nil {
		return err
	}

	bm := NewBootManager(BootConfig{
		APIURL:       apiURL,
		Screen:       cockpit,
		AgentAdapter: agentAdapter,
	})
	cockpit.SetBootManager(bm)

	app, err := gotui.NewApp(gotui.WithRootComponent(cockpit))
	if err != nil {
		return err
	}

	// Start the async boot check after the app is created but before Run().
	// This ensures the first frame renders in Loading state immediately.
	bm.Start()

	// Cleanup: stop the boot manager and close the app.
	defer bm.Stop()
	defer app.Close()

	return app.Run()
}
