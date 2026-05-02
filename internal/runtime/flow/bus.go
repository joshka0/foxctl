package flow

import (
	"context"
	"sync"

	"github.com/joshka0/foxctl/internal/domain/envelope"
)

// OutputBus provides buffered channel-based fan-out for node outputs.
// Each node has a publish endpoint; consumers subscribe to receive copies
// of every output published by that node.
//
// The bus uses bounded channels to prevent unbounded memory growth.
// When a consumer's channel is full, publish blocks (backpressure).
type OutputBus struct {
	mu          sync.RWMutex
	bufferSize  int
	channels    map[string][]chan NodeOutput
	publishChs  map[string]chan NodeOutput
	cancelFuncs map[string]context.CancelFunc
}

// newOutputBus creates a new OutputBus with the given per-consumer buffer size.
func newOutputBus(bufferSize int) *OutputBus {
	if bufferSize <= 0 {
		bufferSize = defaultBusBufferSize
	}
	return &OutputBus{
		bufferSize:  bufferSize,
		channels:    make(map[string][]chan NodeOutput),
		publishChs:  make(map[string]chan NodeOutput),
		cancelFuncs: make(map[string]context.CancelFunc),
	}
}

// start initializes the publish channel for a node and starts the dispatch loop.
func (b *OutputBus) start(ctx context.Context, nodeID string) {
	nodeCtx, cancel := context.WithCancel(ctx)

	ch := make(chan NodeOutput, b.bufferSize)

	b.mu.Lock()
	b.publishChs[nodeID] = ch
	b.cancelFuncs[nodeID] = cancel
	b.mu.Unlock()

	go b.dispatchLoop(nodeCtx, nodeID, ch)
}

// dispatchLoop reads from the publish channel and fans out to all subscribers.
func (b *OutputBus) dispatchLoop(ctx context.Context, nodeID string, source <-chan NodeOutput) {
	for {
		select {
		case <-ctx.Done():
			return
		case out, ok := <-source:
			if !ok {
				return
			}
			b.deliver(nodeID, out)
		}
	}
}

// deliver sends the output to all subscribers of the given node.
// This blocks if a subscriber's channel is full (backpressure).
func (b *OutputBus) deliver(nodeID string, out NodeOutput) {
	b.mu.RLock()
	subs := b.channels[nodeID]
	b.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- out:
		default:
			// If the channel is full, we still deliver (blocking backpressure)
			// but we use a select with context awareness in the dispatch loop
			// to prevent deadlocks on shutdown.
			ch <- out
		}
	}
}

// subscribe creates a new subscription channel for the given node's outputs.
func (b *OutputBus) subscribe(nodeID string) <-chan NodeOutput {
	ch := make(chan NodeOutput, b.bufferSize)

	b.mu.Lock()
	b.channels[nodeID] = append(b.channels[nodeID], ch)
	b.mu.Unlock()

	return ch
}

// publish sends a node output to all subscribers of the given node.
// If there are no subscribers, the output is discarded.
// This is non-blocking: the output goes to the publish channel which
// the dispatch loop reads from.
func (b *OutputBus) publish(nodeID string, out NodeOutput) {
	b.mu.RLock()
	ch, ok := b.publishChs[nodeID]
	b.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case ch <- out:
	default:
		// Publish channel full — block with backpressure.
		// This shouldn't happen under normal operation since the publish
		// channel has the same buffer size as subscriber channels.
		ch <- out
	}
}

// stop cancels the dispatch loop for the given node and closes all subscriber channels.
func (b *OutputBus) stop(nodeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if cancel, ok := b.cancelFuncs[nodeID]; ok {
		cancel()
		delete(b.cancelFuncs, nodeID)
	}

	if ch, ok := b.publishChs[nodeID]; ok {
		close(ch)
		delete(b.publishChs, nodeID)
	}

	for _, sub := range b.channels[nodeID] {
		close(sub)
	}
	delete(b.channels, nodeID)
}

// stopAll stops all dispatch loops and closes all channels.
func (b *OutputBus) stopAll() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for id, cancel := range b.cancelFuncs {
		cancel()
		if ch, ok := b.publishChs[id]; ok {
			close(ch)
		}
		for _, sub := range b.channels[id] {
			close(sub)
		}
	}
	b.cancelFuncs = make(map[string]context.CancelFunc)
	b.publishChs = make(map[string]chan NodeOutput)
	b.channels = make(map[string][]chan NodeOutput)
}

// makeErrorOutput creates a NodeOutput containing an error envelope.
func makeErrorOutput(nodeID, code, message string) NodeOutput {
	return NodeOutput{
		Envelope: envelope.Error("flow/engine", code, message, nil),
		NodeID:   nodeID,
	}
}
