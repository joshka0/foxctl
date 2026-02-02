// Package sse provides Server-Sent Events (SSE) functionality for the web server.
package sse

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
)

// Event represents an SSE event to be sent to clients.
type Event struct {
	// Type is the event type (e.g., "invalidate", "heartbeat").
	Type string `json:"type"`

	// Data is the event payload.
	Data any `json:"data,omitempty"`

	// Timestamp is when the event was created.
	Timestamp time.Time `json:"ts"`
}

// InvalidateData is the payload for invalidation events.
type InvalidateData struct {
	// Keys are the query keys to invalidate (e.g., ["jobs", "tasks"]).
	Keys []string `json:"keys"`
}

// Client represents a connected SSE client.
type Client struct {
	// ID is a unique identifier for the client.
	ID string

	// Send is the channel for sending events to the client.
	Send chan []byte

	// Topics are the topics this client is subscribed to.
	Topics map[string]bool

	// CreatedAt is when the client connected.
	CreatedAt time.Time
}

// Hub manages SSE client connections and event broadcasting.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client

	// broadcast is for sending events to all clients.
	broadcast chan Event

	// register is for adding new clients.
	register chan *Client

	// unregister is for removing clients.
	unregister chan *Client

	// done signals shutdown.
	done chan struct{}
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	h := &Hub{
		clients:    make(map[string]*Client),
		broadcast:  make(chan Event, 256),
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		done:       make(chan struct{}),
	}
	return h
}

// Run starts the hub's event loop. Call in a goroutine.
//
// Index:
// - Purpose: Broadcast SSE events and manage client lifecycle
// - Flow: tick heartbeat → handle register/unregister → publish events
// - SideEffects: sends events to connected clients
// - FailureModes: none (best-effort delivery)
// - Related: Hub.Publish, Hub.Register
// - Keywords: sse, broadcast, clients, heartbeat
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(h.done)
			h.closeAllClients()
			return

		case <-h.done:
			h.closeAllClients()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.broadcastEvent(ctx, event)

		case <-ticker.C:
			// Send heartbeat to all clients
			h.Publish("heartbeat", nil)
		}
	}
}

// Register adds a new client to the hub.
func (h *Hub) Register(client *Client) {
	select {
	case h.register <- client:
	case <-h.done:
	}
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(client *Client) {
	select {
	case h.unregister <- client:
	case <-h.done:
	}
}

// Publish sends an event to all connected clients.
func (h *Hub) Publish(eventType string, data any) {
	event := Event{
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now().UTC(),
	}

	select {
	case h.broadcast <- event:
	case <-h.done:
	default:
		// Channel full, drop event
		observability.Emit(context.Background(), observability.NewEvent("sse.broadcast_channel_full").
			WithComponent("sse").
			WithData("event_type", eventType).
			Error(nil, 0))
	}
}

// Invalidate publishes an invalidation event for the given query keys.
func (h *Hub) Invalidate(keys ...string) {
	h.Publish("invalidate", InvalidateData{Keys: keys})
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close shuts down the hub.
func (h *Hub) Close() {
	select {
	case <-h.done:
		// Already closed
	default:
		close(h.done)
	}
}

func (h *Hub) broadcastEvent(ctx context.Context, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("sse.marshal_failed").
			WithComponent("sse").
			WithData("event_type", event.Type).
			Error(err, 0))
		return
	}

	// Format as SSE: "data: <json>\n\n"
	msg := append([]byte("data: "), data...)
	msg = append(msg, '\n', '\n')

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range h.clients {
		select {
		case client.Send <- msg:
		default:
			// Client buffer full, skip (too verbose for wide events)
		}
	}
}

func (h *Hub) closeAllClients() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id, client := range h.clients {
		close(client.Send)
		delete(h.clients, id)
	}
}
