// entitylist_tui is a standalone TUI demo for EntityList widget snapshot
// capture via tuistory. It renders the list and handles navigation.
//
// Usage: entitylist_tui [--mode focused|unfocused|empty]
package main

import (
	"fmt"
	"os"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/components"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// entityListApp is a go-tui Component that wraps an EntityList.
// It renders entity rows using text elements.
type entityListApp struct {
	el   *components.EntityList
	mode string
}

// Render implements tui.Component.
func (a *entityListApp) Render(_ *gotui.App) *gotui.Element {
	// Render via buffer and then extract text for display.
	buf := gotui.NewBuffer(50, 10)
	a.el.Render(buf)

	// Build element tree from buffer content.
	mt := gotui.NewMockTerminal(50, 10)
	gotui.RenderFull(mt, buf)
	content := mt.StringTrimmed()

	root := gotui.New(
		gotui.WithWidth(50),
		gotui.WithHeight(10),
		gotui.WithBackground(gotui.NewStyle().Background(theme.Colors.Background)),
		gotui.WithText(content),
		gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextPrimary)),
	)

	return root
}

// KeyMap implements tui.KeyListener.
func (a *entityListApp) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnStop(gotui.AnyKey, func(e gotui.KeyEvent) {
			if e.Key == gotui.KeyEscape || (e.Key == gotui.KeyRune && e.Rune == 'q') {
				if e.App() != nil {
					e.App().Stop()
				}
				return
			}
			a.el.HandleKey(e)
		}),
	}
}

func main() {
	mode := "focused"
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	items := []components.Entity{
		{ID: "a1", Label: "agent-abc12345", SubLabel: "researcher"},
		{ID: "a2", Label: "agent-def67890", SubLabel: "coder"},
		{ID: "a3", Label: "agent-ghi11111", SubLabel: "planner"},
		{ID: "a4", Label: "agent-jkl22222", SubLabel: "reviewer"},
		{ID: "a5", Label: "agent-mno33333", SubLabel: "overseer"},
	}

	var el *components.EntityList
	switch mode {
	case "focused":
		el = components.NewEntityList(
			items, 50, 10,
			components.WithSelectedIndex(2),
			components.WithFocused(true),
		)
	case "unfocused":
		el = components.NewEntityList(
			items, 50, 10,
			components.WithSelectedIndex(2),
			components.WithFocused(false),
		)
	case "empty":
		el = components.NewEntityList(
			nil, 50, 10,
			components.WithFocused(true),
			components.WithEmptyMessage("No agents running."),
		)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}

	_ = theme.Colors // ensure theme is imported

	app := &entityListApp{el: el, mode: mode}

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
