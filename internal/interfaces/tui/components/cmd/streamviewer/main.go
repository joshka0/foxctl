// streamviewer_tui is a standalone TUI demo for StreamViewer widget snapshot
// capture via tuistory. It renders the viewer and handles scroll keys.
//
// Usage: streamviewer_tui [--mode follow|scrolled]
package main

import (
	"fmt"
	"os"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/components"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

const (
	svWidth  = 50
	svHeight = 8
)

// streamViewerApp is a go-tui Component that wraps a StreamViewer.
type streamViewerApp struct {
	sv   *components.StreamViewer
	mode string
}

// Render implements tui.Component.
func (a *streamViewerApp) Render(_ *gotui.App) *gotui.Element {
	buf := gotui.NewBuffer(svWidth, svHeight)
	a.sv.Render(buf)

	mt := gotui.NewMockTerminal(svWidth, svHeight)
	gotui.RenderFull(mt, buf)
	content := mt.StringTrimmed()

	root := gotui.New(
		gotui.WithWidth(svWidth),
		gotui.WithHeight(svHeight),
		gotui.WithBackground(gotui.NewStyle().Background(theme.Colors.Background)),
		gotui.WithText(content),
		gotui.WithTextStyle(gotui.NewStyle().Foreground(theme.Colors.TextPrimary)),
	)

	return root
}

// KeyMap implements tui.KeyListener.
func (a *streamViewerApp) KeyMap() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.OnStop(gotui.AnyKey, func(e gotui.KeyEvent) {
			if e.Key == gotui.KeyEscape || (e.Key == gotui.KeyRune && e.Rune == 'q') {
				if e.App() != nil {
					e.App().Stop()
				}
				return
			}
			a.sv.HandleKey(e)
		}),
	}
}

func main() {
	mode := "follow"
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	// Create content lines.
	lines := make([]string, 15)
	for i := range lines {
		lines[i] = fmt.Sprintf("Stream line %d: some content here", i)
	}

	var sv *components.StreamViewer
	switch mode {
	case "follow":
		// Follow-tail engaged: viewer at bottom, showing last lines.
		sv = components.NewStreamViewer(
			lines, svWidth, svHeight,
			components.WithSVFocused(true),
		)
	case "scrolled":
		// Follow-tail disengaged: user has scrolled up.
		sv = components.NewStreamViewer(
			lines, svWidth, svHeight,
			components.WithSVFocused(true),
		)
		// Scroll up to disengage follow.
		sv.HandleKey(gotui.KeyEvent{Key: gotui.KeyUp})
		sv.HandleKey(gotui.KeyEvent{Key: gotui.KeyUp})
		sv.HandleKey(gotui.KeyEvent{Key: gotui.KeyUp})
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", mode)
		os.Exit(1)
	}

	_ = theme.Colors // ensure theme is imported

	app := &streamViewerApp{sv: sv, mode: mode}

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
