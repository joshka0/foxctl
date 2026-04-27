// malformed_sse_demo is a standalone TUI demo for VAL-SKEL-016:
// Malformed SSE — inject malformed frames into an ask-stream and verify
// the UI surfaces a visible indicator while continuing to render well-formed
// frames.
//
// Usage: malformed_sse_demo
package main

import (
	"fmt"
	"os"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// malformedDemoApp wraps a CockpitScreen and drives a simulated ask-stream
// with interleaved malformed events.
type malformedDemoApp struct {
	cs *tui.CockpitScreen
}

// Render implements tui.Component.
func (a *malformedDemoApp) Render(app *gotui.App) *gotui.Element {
	return a.cs.Render(app)
}

// KeyMap implements tui.KeyListener.
func (a *malformedDemoApp) KeyMap() gotui.KeyMap {
	return a.cs.KeyMap()
}

// Watchers implements tui.WatcherProvider.
func (a *malformedDemoApp) Watchers() []gotui.Watcher {
	return a.cs.Watchers()
}

// BindApp implements gotui.AppBinder.
func (a *malformedDemoApp) BindApp(app *gotui.App) {
	a.cs.BindApp(app)
}

// UnbindApp implements gotui.AppUnbinder.
func (a *malformedDemoApp) UnbindApp() {
	a.cs.UnbindApp()
}

func main() {
	cs := tui.NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]tui.AgentInventoryItem{
		{ID: "agent-abc12345", Role: "researcher", Status: "running", Workspace: "default", ParentID: "—", LastActive: "2m ago"},
		{ID: "agent-def67890", Role: "coder", Status: "idle", Workspace: "default", ParentID: "—", LastActive: "5m ago"},
		{ID: "agent-ghi11111", Role: "planner", Status: "running", Workspace: "default", ParentID: "—", LastActive: "1m ago"},
	})
	cs.SetSelectedIndex(0)
	cs.SetFocusedLane(1) // Detail lane focused
	cs.UpdateSize(80, 24)
	cs.SetPhase(tui.CockpitPhaseReady)

	app := &malformedDemoApp{cs: cs}

	// Pre-seed the stream state with a user message and the full sequence of
	// well-formed + malformed + well-formed updates so the initial render
	// shows the malformed-event indicator and later successful tokens.
	cs.SetComposerText("tell me a story")
	_ = cs.SubmitComposer()

	cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
		Type:  tui.AgentAskUpdateToken,
		Token: &tui.AgentAskToken{Delta: "Hello"},
	})
	cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
		Type:      tui.AgentAskUpdateMalformed,
		Malformed: &tui.AgentAskMalformed{RawPhase: "gibberish", RawData: "???"},
	})
	cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
		Type:      tui.AgentAskUpdateMalformed,
		Malformed: &tui.AgentAskMalformed{RawPhase: "", RawData: "{not json"},
	})
	cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
		Type:  tui.AgentAskUpdateToken,
		Token: &tui.AgentAskToken{Delta: " world"},
	})
	cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
		Type:  tui.AgentAskUpdateToken,
		Token: &tui.AgentAskToken{Delta: "!"},
	})
	cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
		Type: tui.AgentAskUpdateDone,
		Done: &tui.AgentAskDone{OK: true},
	})

	_ = theme.Colors

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
