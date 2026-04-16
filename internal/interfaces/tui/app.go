package tui

import (
	"context"

	gotui "github.com/grindlemire/go-tui"
)

// Run starts the Go-native foxctl terminal shell.
func Run(ctx context.Context, opts Options) error {
	app, err := NewApp(ctx, opts)
	if err != nil {
		return err
	}
	defer app.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			app.Stop()
		case <-done:
		}
	}()
	defer close(done)

	if err := app.Run(); err != nil {
		return err
	}
	return ctx.Err()
}

// NewApp builds the shell app without starting the terminal event loop.
func NewApp(ctx context.Context, opts Options) (*gotui.App, error) {
	initialState, err := LoadInitialShellState(ctx, opts)
	if err != nil {
		return nil, err
	}

	return gotui.NewApp(
		gotui.WithRootComponent(NewShell(initialState)),
	)
}
