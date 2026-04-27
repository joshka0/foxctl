package tui

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// EventsSubscriberConfig configures the live-refresh SSE subscriber.
type EventsSubscriberConfig struct {
	// APIClient is the HTTP client used for SSE connections and re-fetches.
	// Required.
	APIClient *APIClient

	// AgentAdapter is used to re-fetch the agent inventory when SSE events
	// arrive. Required.
	AgentAdapter *AgentAdapter

	// Screen is the CockpitScreen to update with fresh agent data. Optional;
	// if nil, the subscriber fetches but does not update any screen.
	Screen *CockpitScreen

	// Debounce is the minimum time between re-fetches. Rapid SSE events are
	// coalesced into a single re-fetch. Default is 2 seconds.
	Debounce time.Duration

	// ReconnectDelay is the wait time before reconnecting after a connection
	// drop. Default is 1 second.
	ReconnectDelay time.Duration
}

// EventsSubscriber maintains a single SSE subscription to /api/events and
// triggers debounced re-fetches of the agent inventory.
//
// Start() begins the subscription. Stop() cancels it and cleans up.
// The subscriber automatically reconnects if the connection drops.
type EventsSubscriber struct {
	mu             sync.Mutex
	apiClient      *APIClient
	agentAdapter   *AgentAdapter
	screen         *CockpitScreen
	debounce       time.Duration
	reconnectDelay time.Duration
	cancelFn       context.CancelFunc
	done           chan struct{}
	started        bool
}

// defaultDebounce is the default debounce duration between re-fetches.
const defaultDebounce = 2 * time.Second

// defaultReconnectDelay is the default wait before reconnecting.
const defaultReconnectDelay = 1 * time.Second

// NewEventsSubscriber creates a new subscriber. Call Start() to begin.
func NewEventsSubscriber(cfg EventsSubscriberConfig) *EventsSubscriber {
	debounce := cfg.Debounce
	if debounce <= 0 {
		debounce = defaultDebounce
	}
	reconnectDelay := cfg.ReconnectDelay
	if reconnectDelay <= 0 {
		reconnectDelay = defaultReconnectDelay
	}

	return &EventsSubscriber{
		apiClient:      cfg.APIClient,
		agentAdapter:   cfg.AgentAdapter,
		screen:         cfg.Screen,
		debounce:       debounce,
		reconnectDelay: reconnectDelay,
		done:           make(chan struct{}),
	}
}

// Start begins the background SSE subscription goroutine. It returns
// immediately and does not block. Safe to call only once; subsequent calls
// are ignored until Stop() is called.
func (es *EventsSubscriber) Start() {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.started {
		return
	}
	es.started = true

	ctx, cancel := context.WithCancel(context.Background())
	es.cancelFn = cancel

	go es.run(ctx)
}

// Stop cancels the background goroutine and waits for it to finish.
// It is safe to call multiple times.
func (es *EventsSubscriber) Stop() {
	es.mu.Lock()
	cancelFn := es.cancelFn
	es.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}

	<-es.done

	es.mu.Lock()
	es.started = false
	es.mu.Unlock()
}

// run is the background goroutine that maintains the SSE connection.
func (es *EventsSubscriber) run(ctx context.Context) {
	defer close(es.done)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		es.connectAndListen(ctx)

		// If context is cancelled, exit; otherwise reconnect after delay.
		select {
		case <-ctx.Done():
			return
		case <-time.After(es.reconnectDelay):
		}
	}
}

// connectAndListen opens a single SSE connection and listens until the
// connection drops or the context is cancelled.
func (es *EventsSubscriber) connectAndListen(ctx context.Context) {
	if es.apiClient == nil {
		return
	}

	url := es.apiClient.BaseURL() + "/api/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := es.apiClient.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	// Debounce timer and pending flag.
	var debounceTimer *time.Timer
	var debouncePending bool
	var debounceMu sync.Mutex

	// flushDebounce performs the actual re-fetch.
	flushDebounce := func() {
		debounceMu.Lock()
		debouncePending = false
		debounceMu.Unlock()

		es.refetchAgents(ctx)
	}

	// triggerDebounce schedules a re-fetch after the debounce window.
	triggerDebounce := func() {
		debounceMu.Lock()
		defer debounceMu.Unlock()

		if debouncePending {
			// A re-fetch is already scheduled; extend the timer.
			if debounceTimer != nil {
				debounceTimer.Reset(es.debounce)
			}
			return
		}

		debouncePending = true
		debounceTimer = time.AfterFunc(es.debounce, flushDebounce)
	}

	// Parse the SSE stream. Any event triggers the debounced re-fetch.
	_ = ParseConsoleEventStream(resp.Body, func(event ConsoleStreamEvent) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Any event (heartbeat, agent.chat, connected, etc.) triggers re-fetch.
		triggerDebounce()
		return nil
	})

	// Clean up any pending timer.
	debounceMu.Lock()
	if debounceTimer != nil {
		debounceTimer.Stop()
	}
	debounceMu.Unlock()
}

// refetchAgents fetches the agent inventory and updates the screen.
func (es *EventsSubscriber) refetchAgents(ctx context.Context) {
	if es.agentAdapter == nil {
		return
	}

	resp, err := es.agentAdapter.ListAgents(ctx, 0)
	if err != nil {
		return
	}

	if es.screen != nil {
		items := make([]AgentInventoryItem, len(resp.Agents))
		for i, a := range resp.Agents {
			items[i] = agentRecordToInventoryItem(a)
		}
		es.screen.SetAgents(items)
	}
}
