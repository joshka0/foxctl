package tui

import (
	"context"

	gotui "github.com/grindlemire/go-tui"
)

// Run starts the Go-native foxctl terminal shell.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}

	app, cleanup, err := NewApp(ctx, opts)
	if err != nil {
		return err
	}
	defer cleanup()
	defer app.Close()

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
// The returned cleanup must be called to release stream pump resources.
func NewApp(ctx context.Context, opts Options) (*gotui.App, func(), error) {
	initialState, err := LoadInitialShellState(ctx, opts)
	if err != nil {
		return nil, nil, err
	}

	runtime, err := newShellRuntime(ctx, opts, initialState)
	if err != nil {
		return nil, nil, err
	}

	app, err := gotui.NewApp(
		gotui.WithRootComponent(runtime.shell),
	)
	if err != nil {
		runtime.close()
		return nil, nil, err
	}

	return app, runtime.close, nil
}

type shellRuntime struct {
	shell *Shell
	close func()
}

func newShellRuntime(ctx context.Context, opts Options, initialState ShellState) (*shellRuntime, error) {
	runtime := &shellRuntime{
		shell: NewShellWithStream(initialState, nil, opts.TranscriptLimit),
		close: func() {},
	}

	if !shouldAttachConsoleStream(opts) {
		return runtime, nil
	}

	client, err := NewAPIClient(opts.APIBaseURL, nil)
	if err != nil {
		return nil, err
	}

	source := NewHTTPConsoleStreamSource(
		client,
		opts.ConsoleSessionID,
		ConsoleEventStreamOptions{PayloadFormat: true},
	)
	pump, err := NewConsoleStreamPump(ctx, source, opts.ConsoleStreamBuffer)
	if err != nil {
		return nil, err
	}

	runtime.shell = NewShellWithStream(initialState, pump.Updates(), opts.TranscriptLimit)
	runtime.close = pump.Close
	return runtime, nil
}
