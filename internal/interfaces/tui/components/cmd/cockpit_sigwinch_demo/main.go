// cockpit_sigwinch_demo is a standalone TUI demo for VAL-SKEL-015:
// SIGWINCH mid-stream — resize during active token streaming must not drop
// tokens, duplicate rows, lose composer focus, or break the cancel key binding.
//
// Usage: cockpit_sigwinch_demo [--mode streaming]
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	gotui "github.com/grindlemire/go-tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui"
	"github.com/joshka0/foxctl/internal/interfaces/tui/theme"
)

// sigwinchDemoApp wraps a CockpitScreen and drives a simulated ask-stream.
type sigwinchDemoApp struct {
	cs     *tui.CockpitScreen
	rt     *tui.AgentAskStreamRuntime
	tokens []string
	tokIdx int
}

// Render implements tui.Component.
func (a *sigwinchDemoApp) Render(app *gotui.App) *gotui.Element {
	return a.cs.Render(app)
}

// KeyMap implements tui.KeyListener.
func (a *sigwinchDemoApp) KeyMap() gotui.KeyMap {
	km := a.cs.KeyMap()
	// Append 's' to manually trigger a stream token for demo purposes.
	km = append(km, gotui.On(gotui.Rune('s'), func(ke gotui.KeyEvent) {
		if a.tokIdx < len(a.tokens) {
			tok := a.tokens[a.tokIdx]
			a.tokIdx++
			a.cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
				Type:  tui.AgentAskUpdateToken,
				Token: &tui.AgentAskToken{Delta: tok},
			})
			if a.tokIdx >= len(a.tokens) {
				a.cs.ApplyAskStreamUpdate(tui.AgentAskStreamUpdate{
					Type: tui.AgentAskUpdateDone,
					Done: &tui.AgentAskDone{OK: true},
				})
			}
		}
	}))
	return km
}

// Watchers implements tui.WatcherProvider.
func (a *sigwinchDemoApp) Watchers() []gotui.Watcher {
	return a.cs.Watchers()
}

// BindApp implements gotui.AppBinder.
func (a *sigwinchDemoApp) BindApp(app *gotui.App) {
	a.cs.BindApp(app)
}

// UnbindApp implements gotui.AppUnbinder.
func (a *sigwinchDemoApp) UnbindApp() {
	a.cs.UnbindApp()
}

func main() {
	mode := "streaming"
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "--mode" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}

	cs := tui.NewCockpitScreen("http://localhost:8090")
	cs.SetAgents([]tui.AgentInventoryItem{
		{ID: "agent-abc12345", Role: "researcher", Status: "running", Workspace: "default", ParentID: "—", LastActive: "2m ago"},
		{ID: "agent-def67890", Role: "coder", Status: "idle", Workspace: "default", ParentID: "—", LastActive: "5m ago"},
		{ID: "agent-ghi11111", Role: "planner", Status: "running", Workspace: "default", ParentID: "—", LastActive: "1m ago"},
	})
	cs.SetSelectedIndex(0)
	cs.SetFocusedLane(1) // Detail lane focused
	cs.UpdateSize(120, 40)
	cs.SetPhase(tui.CockpitPhaseReady)

	// Pre-seed a user message so the stream area is visible.
	cs.SetComposerText("Hello agent")
	_ = cs.SubmitComposer()

	app := &sigwinchDemoApp{
		cs: cs,
		tokens: []string{
			"The", " quick", " brown", " fox", " jumps",
			" over", " the", " lazy", " dog", ".",
		},
	}

	if mode == "streaming" {
		// Start an auto-emitting background stream.
		source := tui.AgentAskStreamSourceFunc(func(ctx context.Context, onEvent func(tui.AgentAskStreamEvent) error) error {
			tokens := []string{
				"The", " quick", " brown", " fox", " jumps",
				" over", " the", " lazy", " dog", ".",
			}
			for _, tok := range tokens {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				if err := onEvent(tui.AgentAskStreamEvent{Phase: "delta", ContentDelta: tok}); err != nil {
					return err
				}
				time.Sleep(200 * time.Millisecond)
			}
			return nil
		})

		rt, err := tui.NewAgentAskStreamRuntime(context.Background(), source, 16)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create runtime: %v\n", err)
			os.Exit(1)
		}
		app.rt = rt
		cs.SetAskStreamRuntime(rt)

		// Submit triggers the stream.
		cs.SetComposerText("tell me a story")
		if err := cs.SubmitComposer(); err != nil {
			fmt.Fprintf(os.Stderr, "submit error: %v\n", err)
			os.Exit(1)
		}

		// Background goroutine: drain updates and apply them to the cockpit.
		go func() {
			for update := range rt.Updates() {
				cs.ApplyAskStreamUpdate(update)
			}
		}()
	}

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
