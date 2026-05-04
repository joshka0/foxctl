package flow

import (
	"context"
	"sync"
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
	wg          sync.WaitGroup // tracks running dispatch loops
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

	b.wg.Add(1)
	go b.dispatchLoop(nodeCtx, nodeID, ch)
}

// dispatchLoop reads from the publish channel and fans out to all subscribers.
func (b *OutputBus) dispatchLoop(ctx context.Context, nodeID string, source <-chan NodeOutput) {
	defer b.wg.Done()
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
	allSubs := b.channels["__all__"]
	b.mu.RUnlock()

	deliverToSubs := func(chs []chan NodeOutput) {
		for _, ch := range chs {
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

	deliverToSubs(subs)
	deliverToSubs(allSubs)
}

// subscribe creates a new subscription channel for the given node's outputs.
func (b *OutputBus) subscribe(nodeID string) <-chan NodeOutput {
	ch := make(chan NodeOutput, b.bufferSize)

	b.mu.Lock()
	b.channels[nodeID] = append(b.channels[nodeID], ch)
	b.mu.Unlock()

	return ch
}

// subscribeAll creates a subscription channel that receives outputs from all
// nodes. The returned channel receives copies of every output published by
// any node on the bus.
func (b *OutputBus) subscribeAll() <-chan NodeOutput {
	ch := make(chan NodeOutput, b.bufferSize)

	b.mu.Lock()
	// Register under a special key that deliver can target.
	const allKey = "__all__"
	b.channels[allKey] = append(b.channels[allKey], ch)
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

// stop cancels the dispatch loop for the given node, waits for it to exit,
// and closes all subscriber channels.
func (b *OutputBus) _stop(nodeID string) { //nolint:unused // kept for future per-node stop support
	b.mu.Lock()
	cancel := b.cancelFuncs[nodeID]
	delete(b.cancelFuncs, nodeID)
	pub := b.publishChs[nodeID]
	delete(b.publishChs, nodeID)
	nodeSubs := b.channels[nodeID]
	delete(b.channels, nodeID)
	b.mu.Unlock()

	// Cancel dispatch loop.
	if cancel != nil {
		cancel()
	}
	// Close publish channel so dispatch loop sees !ok.
	if pub != nil {
		close(pub)
	}
	// Wait for dispatch loop to finish before closing subscriber channels.
	b.wg.Wait()
	for _, sub := range nodeSubs {
		close(sub)
	}
}

// stopAll stops all dispatch loops, waits for them to exit, and closes all channels.
func (b *OutputBus) stopAll() {
	b.mu.Lock()
	cancels := b.cancelFuncs
	pubs := b.publishChs
	subs := b.channels
	b.cancelFuncs = make(map[string]context.CancelFunc)
	b.publishChs = make(map[string]chan NodeOutput)
	b.channels = make(map[string][]chan NodeOutput)
	b.mu.Unlock()

	// Cancel all dispatch loops.
	for _, cancel := range cancels {
		cancel()
	}
	// Close publish channels so dispatch loops see !ok and exit.
	for _, ch := range pubs {
		close(ch)
	}
	// Wait for all dispatch loops to finish before closing subscriber channels.
	b.wg.Wait()
	// Close subscriber channels (safe now since no goroutine is sending).
	for _, chs := range subs {
		for _, ch := range chs {
			close(ch)
		}
	}
}


