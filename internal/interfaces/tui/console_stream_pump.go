package tui

import (
	"context"
	"errors"
	"sync"
)

const defaultConsoleStreamPumpBufferSize = 16

// ConsoleStreamSource reads console stream events and invokes onEvent for each event.
type ConsoleStreamSource interface {
	Stream(ctx context.Context, onEvent func(ConsoleStreamEvent) error) error
}

// ConsoleStreamSourceFunc adapts a function into a ConsoleStreamSource.
type ConsoleStreamSourceFunc func(ctx context.Context, onEvent func(ConsoleStreamEvent) error) error

// Stream implements ConsoleStreamSource.
func (fn ConsoleStreamSourceFunc) Stream(ctx context.Context, onEvent func(ConsoleStreamEvent) error) error {
	if fn == nil {
		return errors.New("console stream source function is required")
	}
	return fn(ctx, onEvent)
}

// NewHTTPConsoleStreamSource wraps ReadConsoleEventStream into a ConsoleStreamSource.
func NewHTTPConsoleStreamSource(client *APIClient, sessionID string, opts ConsoleEventStreamOptions) ConsoleStreamSource {
	return ConsoleStreamSourceFunc(func(ctx context.Context, onEvent func(ConsoleStreamEvent) error) error {
		return ReadConsoleEventStream(ctx, client, sessionID, opts, onEvent)
	})
}

// ConsoleStreamUpdateType defines the kind of stream update emitted by the pump.
type ConsoleStreamUpdateType string

const (
	ConsoleStreamUpdateEvent ConsoleStreamUpdateType = "event"
	ConsoleStreamUpdateError ConsoleStreamUpdateType = "error"
	ConsoleStreamUpdateDone  ConsoleStreamUpdateType = "done"
)

// ConsoleStreamUpdate is a typed stream notification for future watcher integration.
type ConsoleStreamUpdate struct {
	Type  ConsoleStreamUpdateType
	Event ConsoleStreamEvent
	Err   error
}

// ConsoleStreamPump owns one stream-reading goroutine and emits bounded updates.
type ConsoleStreamPump struct {
	source  ConsoleStreamSource
	updates chan ConsoleStreamUpdate

	ctx    context.Context
	cancel context.CancelFunc

	stopOnce  sync.Once
	waitGroup sync.WaitGroup
}

// NewConsoleStreamPump creates and starts a bounded stream pump.
func NewConsoleStreamPump(
	parent context.Context,
	source ConsoleStreamSource,
	bufferSize int,
) (*ConsoleStreamPump, error) {
	if source == nil {
		return nil, errors.New("console stream source is required")
	}

	if parent == nil {
		parent = context.Background()
	}

	if bufferSize < 0 {
		return nil, errors.New("console stream buffer size must be >= 0")
	}
	if bufferSize == 0 {
		bufferSize = defaultConsoleStreamPumpBufferSize
	}

	ctx, cancel := context.WithCancel(parent)
	pump := &ConsoleStreamPump{
		source:  source,
		updates: make(chan ConsoleStreamUpdate, bufferSize),
		ctx:     ctx,
		cancel:  cancel,
	}
	pump.waitGroup.Add(1)
	go pump.run()
	return pump, nil
}

// Updates returns the bounded receive-only update channel.
func (pump *ConsoleStreamPump) Updates() <-chan ConsoleStreamUpdate {
	return pump.updates
}

// Stop cancels stream reading and waits for the pump goroutine to exit.
func (pump *ConsoleStreamPump) Stop() {
	if pump == nil {
		return
	}
	pump.stopOnce.Do(func() {
		pump.cancel()
		pump.waitGroup.Wait()
	})
}

// Close is an alias for Stop.
func (pump *ConsoleStreamPump) Close() {
	pump.Stop()
}

func (pump *ConsoleStreamPump) run() {
	defer pump.waitGroup.Done()
	defer close(pump.updates)

	streamErr := pump.source.Stream(pump.ctx, func(event ConsoleStreamEvent) error {
		return pump.sendEventUpdate(ConsoleStreamUpdate{
			Type:  ConsoleStreamUpdateEvent,
			Event: event,
		})
	})
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, context.DeadlineExceeded) {
		pump.sendTerminalUpdate(ConsoleStreamUpdate{
			Type: ConsoleStreamUpdateError,
			Err:  streamErr,
		})
		return
	}

	pump.sendTerminalUpdate(ConsoleStreamUpdate{
		Type: ConsoleStreamUpdateDone,
	})
}

func (pump *ConsoleStreamPump) sendEventUpdate(update ConsoleStreamUpdate) error {
	select {
	case <-pump.ctx.Done():
		return pump.ctx.Err()
	case pump.updates <- update:
		return nil
	}
}

func (pump *ConsoleStreamPump) sendTerminalUpdate(update ConsoleStreamUpdate) {
	select {
	case <-pump.ctx.Done():
		return
	case pump.updates <- update:
		return
	}
}
