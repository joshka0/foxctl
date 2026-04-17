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
// The returned cleanup must be called to release optional stream/ask/cancel runtimes.
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
	shell                *Shell
	consoleStreamPump    *ConsoleStreamPump
	consoleAskRuntime    *ConsoleAskRuntime
	consoleCancelRuntime *ConsoleCancelRuntime
	close                func()
}

func newShellRuntime(ctx context.Context, opts Options, initialState ShellState) (*shellRuntime, error) {
	runtime := &shellRuntime{
		shell: NewShellWithRuntimes(
			initialState,
			nil,
			nil,
			nil,
			nil,
			nil,
			opts.TranscriptLimit,
			defaultComposerAskEnqueueTimeout,
			defaultConsoleCancelEnqueueTimeout,
		),
		close: func() {},
	}

	if !shouldAttachConsoleStream(opts) {
		if shouldAttachAgentCompanion(opts) {
			client, err := NewAPIClient(opts.APIBaseURL, nil)
			if err != nil {
				return nil, err
			}
			agentAdapter, err := NewAgentAdapter(client)
			if err != nil {
				return nil, err
			}
			submitter, err := NewHTTPAgentAskSubmitter(agentAdapter, opts.AgentID)
			if err != nil {
				return nil, err
			}
			askRuntime, err := NewConsoleAskRuntime(ctx, submitter, 0, 0)
			if err != nil {
				return nil, err
			}
			runtime.consoleAskRuntime = askRuntime
			runtime.shell = NewShellWithRuntimes(
				initialState,
				nil,
				askRuntime.Updates(),
				askRuntime.Enqueue,
				nil,
				nil,
				opts.TranscriptLimit,
				defaultComposerAskEnqueueTimeout,
				defaultConsoleCancelEnqueueTimeout,
			)
			runtime.close = func() {
				askRuntime.Close()
			}
		}
		return runtime, nil
	}

	client, err := NewAPIClient(opts.APIBaseURL, nil)
	if err != nil {
		return nil, err
	}

	adapter, err := NewConsoleAdapter(client)
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

	submitter, err := NewHTTPConsoleAskSubmitter(adapter, opts.ConsoleSessionID)
	if err != nil {
		pump.Close()
		return nil, err
	}
	askRuntime, err := NewConsoleAskRuntime(ctx, submitter, 0, 0)
	if err != nil {
		pump.Close()
		return nil, err
	}

	canceler, err := NewHTTPConsoleCanceler(adapter, opts.ConsoleSessionID)
	if err != nil {
		askRuntime.Close()
		pump.Close()
		return nil, err
	}
	cancelRuntime, err := NewConsoleCancelRuntime(ctx, canceler, 0, 0)
	if err != nil {
		askRuntime.Close()
		pump.Close()
		return nil, err
	}

	runtime.consoleStreamPump = pump
	runtime.consoleAskRuntime = askRuntime
	runtime.consoleCancelRuntime = cancelRuntime
	runtime.shell = NewShellWithRuntimes(
		initialState,
		pump.Updates(),
		askRuntime.Updates(),
		askRuntime.Enqueue,
		cancelRuntime.Updates(),
		cancelRuntime.Enqueue,
		opts.TranscriptLimit,
		defaultComposerAskEnqueueTimeout,
		defaultConsoleCancelEnqueueTimeout,
	)
	runtime.close = func() {
		cancelRuntime.Close()
		askRuntime.Close()
		pump.Close()
	}
	return runtime, nil
}
