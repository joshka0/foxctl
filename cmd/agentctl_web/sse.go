package main

import (
	"fmt"
	"log"
	"net/http"
	"sync"
)

// SSEHub manages SSE client connections and broadcasts
type SSEHub struct {
	mu         sync.RWMutex
	clients    map[chan SSEEvent]bool
	register   chan chan SSEEvent
	unregister chan chan SSEEvent
	broadcast  chan SSEEvent
}

// SSEEvent represents a server-sent event
type SSEEvent struct {
	Event string
	Data  string
}

// NewSSEHub creates a new SSE hub
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients:    make(map[chan SSEEvent]bool),
		register:   make(chan chan SSEEvent),
		unregister: make(chan chan SSEEvent),
		broadcast:  make(chan SSEEvent, 256),
	}
}

// Run starts the hub's main loop
func (h *SSEHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("SSE: Client connected (total: %d)", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client)
			}
			h.mu.Unlock()
			log.Printf("SSE: Client disconnected (total: %d)", len(h.clients))

		case event := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client <- event:
				default:
					// Client buffer full, skip
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Broadcast sends an event to all connected clients
func (h *SSEHub) Broadcast(event, data string) {
	h.broadcast <- SSEEvent{Event: event, Data: data}
}

// Handler is the HTTP handler for SSE connections
func (h *SSEHub) Handler(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create client channel
	client := make(chan SSEEvent, 16)
	h.register <- client

	// Ensure cleanup on disconnect
	defer func() {
		h.unregister <- client
	}()

	// Send initial keepalive
	fmt.Fprintf(w, ": connected\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Stream events
	for {
		select {
		case event, ok := <-client:
			if !ok {
				return
			}
			// Format: event: <name>\ndata: <data>\n\n
			if event.Event != "" {
				fmt.Fprintf(w, "event: %s\n", event.Event)
			}
			fmt.Fprintf(w, "data: %s\n\n", event.Data)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

		case <-r.Context().Done():
			return
		}
	}
}
