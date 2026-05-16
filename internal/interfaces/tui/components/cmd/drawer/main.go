// drawer_tui is a standalone TUI demo for Drawer widget snapshot
// capture via tuistory. It renders a drawer and handles keyboard input.
//
// Usage: drawer_tui [--mode open|scrolled]
package main

import (
	"fmt"
	"os"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/components"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

const (
	drawerWidth  = 60
	drawerHeight = 15
)

// drawerApp is a go-tui Component that wraps a Drawer widget.
type drawerApp struct {
	drawer *components.Drawer
	mode   string
}

// Render implements tui.Component.
func (a *drawerApp) Render(_ *gotui.App) *gotui.Element {
	buf := gotui.NewBuffer(drawerWidth, drawerHeight)
	a.drawer.Render(buf)

	mt := gotui.NewMockTerminal(drawerWidth, drawerHeight)
	gotui.RenderFull(mt, buf)
	content := mt.StringTrimmed()

	root := gotui.New(
		gotui.WithWidth(drawerWidth),
		gotui.WithHeight(drawerHeight),
		gotui.WithBackground(gotui.NewStyle().Background(theme.Colors.Background)),
		gotui.WithText(content),
		gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextPrimary)),
	)

	return root
}

// KeyMap implements tui.KeyListener.
func (a *drawerApp) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnStop(gotui.AnyKey, func(e gotui.KeyEvent) {
			if e.Key == gotui.KeyEscape || (e.Key == gotui.KeyRune && e.Rune == 'q') {
				// If drawer is open, ESC closes it first.
				if a.drawer.IsOpen() {
					a.drawer.HandleKey(e)
					return
				}
				if e.App() != nil {
					e.App().Stop()
				}
				return
			}
			a.drawer.HandleKey(e)
		}),
	}
}

func main() {
	mode := "open"
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	content := []string{
		"Agent ID: abc-123",
		"Role: researcher",
		"Status: running",
		"Workspace: /home/user/project",
		"Last activity: 2m ago",
		"",
		"Raw payload:",
		`{"id":"abc-123","role":"researcher"}`,
	}

	var drawer *components.Drawer
	switch mode {
	case "open":
		drawer = components.NewDrawer(
			"Agent Detail", content, drawerWidth, drawerHeight,
			components.WithDrawerOpen(true),
			components.WithDrawerFocused(true),
			components.WithDrawerWidth(30),
		)
	case "scrolled":
		longContent := make([]string, 20)
		for i := range longContent {
			longContent[i] = "Line of content that is fairly long to test scrolling"
		}
		drawer = components.NewDrawer(
			"Evidence", longContent, drawerWidth, drawerHeight,
			components.WithDrawerOpen(true),
			components.WithDrawerFocused(true),
			components.WithDrawerWidth(30),
		)
		// Scroll down 3 lines.
		for i := 0; i < 3; i++ {
			drawer.HandleKey(gotui.KeyEvent{Key: gotui.KeyDown})
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}

	_ = theme.Colors // ensure theme is imported

	app := &drawerApp{drawer: drawer, mode: mode}

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
