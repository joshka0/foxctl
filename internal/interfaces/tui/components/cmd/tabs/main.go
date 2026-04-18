// tabs_tui is a standalone TUI demo for Tabs widget snapshot
// capture via tuistory. It renders tabs and handles navigation.
//
// Usage: tabs_tui [--mode focused|unfocused|middle]
package main

import (
	"fmt"
	"os"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/components"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

const (
	tabsWidth  = 50
	tabsHeight = 4 // 2 for tabs + 2 for padding
)

// tabsApp is a go-tui Component that wraps a Tabs widget.
type tabsApp struct {
	tabs *components.Tabs
	mode string
}

// Render implements tui.Component.
func (a *tabsApp) Render(_ *gotui.App) *gotui.Element {
	buf := gotui.NewBuffer(tabsWidth, tabsHeight)
	a.tabs.Render(buf)

	mt := gotui.NewMockTerminal(tabsWidth, tabsHeight)
	gotui.RenderFull(mt, buf)
	content := mt.StringTrimmed()

	root := gotui.New(
		gotui.WithWidth(tabsWidth),
		gotui.WithHeight(tabsHeight),
		gotui.WithBackground(gotui.NewStyle().Background(theme.Colors.Background)),
		gotui.WithText(content),
		gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextPrimary)),
	)

	return root
}

// KeyMap implements tui.KeyListener.
func (a *tabsApp) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnStop(gotui.AnyKey, func(e gotui.KeyEvent) {
			if e.Key == gotui.KeyEscape || (e.Key == gotui.KeyRune && e.Rune == 'q') {
				if e.App() != nil {
					e.App().Stop()
				}
				return
			}
			a.tabs.HandleKey(e)
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

	labels := []string{"Agents", "Rooms", "Events"}

	var tabs *components.Tabs
	switch mode {
	case "focused":
		tabs = components.NewTabs(labels, tabsWidth,
			components.WithTabsActiveIndex(0),
			components.WithTabsFocused(true),
		)
	case "unfocused":
		tabs = components.NewTabs(labels, tabsWidth,
			components.WithTabsActiveIndex(0),
			components.WithTabsFocused(false),
		)
	case "middle":
		tabs = components.NewTabs(labels, tabsWidth,
			components.WithTabsActiveIndex(1),
			components.WithTabsFocused(true),
		)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}

	_ = theme.Colors // ensure theme is imported

	app := &tabsApp{tabs: tabs, mode: mode}

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
