// detailpane_tui is a standalone TUI demo for DetailPane widget snapshot
// capture via tuistory. It renders the pane and handles scroll keys.
//
// Usage: detailpane_tui [--mode populated|empty|scrolled|truncated-title]
package main

import (
	"fmt"
	"os"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/components"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// detailPaneApp is a go-tui Component that wraps a DetailPane.
type detailPaneApp struct {
	dp   *components.DetailPane
	mode string
}

// Render implements tui.Component.
func (a *detailPaneApp) Render(_ *gotui.App) *gotui.Element {
	buf := gotui.NewBuffer(50, 15)
	a.dp.Render(buf)

	mt := gotui.NewMockTerminal(50, 15)
	gotui.RenderFull(mt, buf)
	content := mt.StringTrimmed()

	root := gotui.New(
		gotui.WithWidth(50),
		gotui.WithHeight(15),
		gotui.WithBackground(gotui.NewStyle().Background(theme.Colors.Background)),
		gotui.WithText(content),
		gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextPrimary)),
	)

	return root
}

// KeyMap implements tui.KeyListener.
func (a *detailPaneApp) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnStop(gotui.AnyKey, func(e gotui.KeyEvent) {
			if e.Key == gotui.KeyEscape || (e.Key == gotui.KeyRune && e.Rune == 'q') {
				if e.App() != nil {
					e.App().Stop()
				}
				return
			}
			a.dp.HandleKey(e)
		}),
	}
}

func main() {
	mode := "populated"
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	var dp *components.DetailPane

	switch mode {
	case "populated":
		sections := []components.Section{
			{Title: "Runtime", Lines: []string{
				"state: running",
				"provider: openrouter",
				"model: aurora-alpha",
				"uptime: 2h30m",
			}},
			{Title: "Hierarchy", Lines: []string{
				"parent: none",
				"children: 2",
			}},
			{Title: "Recent Activity", Lines: []string{
				"ask: 'review git diff' → done",
				"ask: 'fix lint errors' → running",
			}},
		}
		dp = components.NewDetailPane("agent-abc12345", components.StatusOK, sections, 50, 15,
			components.WithHasEntity(true),
			components.WithDPFocused(true),
		)

	case "empty":
		dp = components.NewDetailPane("", components.StatusNone, nil, 50, 15,
			components.WithHasEntity(false),
		)

	case "scrolled":
		lines := make([]string, 30)
		for i := range lines {
			lines[i] = "detail content line"
		}
		sections := []components.Section{
			{Title: "Details", Lines: lines},
		}
		dp = components.NewDetailPane("agent-abc12345", components.StatusOK, sections, 50, 15,
			components.WithHasEntity(true),
			components.WithDPFocused(true),
			components.WithScrollOffset(10),
		)

	case "truncated-title":
		sections := []components.Section{
			{Title: "Info", Lines: []string{"some detail"}},
		}
		dp = components.NewDetailPane(
			"agent-with-a-very-long-name-that-exceeds-the-width-of-the-detail-pane-header",
			components.StatusWarn, sections, 25, 10,
			components.WithHasEntity(true),
			components.WithDPFocused(true),
		)

	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}

	_ = theme.Colors // ensure theme is imported

	app := &detailPaneApp{dp: dp, mode: mode}

	tuiApp, err := gotui.NewApp(gotui.WithRootComponent(app))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create app: %v\n", err)
		os.Exit(1)
	}
	defer tuiApp.Close()

	if err := tuiApp.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "app error: %v\n", err)
		os.Exit(1)
	}
}
