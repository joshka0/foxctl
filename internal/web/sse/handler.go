package sse

import (
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
)

// Handler returns an HTTP handler for the SSE endpoint.
func Handler(hub *Hub) http.HandlerFunc {
	return TopicHandler(hub)
}

// TopicHandler returns an SSE handler that subscribes the client to the given
// topics. Clients with no topics receive the full global event feed.
func TopicHandler(hub *Hub, topics ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only allow GET
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

		// Flush headers immediately
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		flusher.Flush()

		// Create client
		clientID := ulid.Make().String()
		topicSet := make(map[string]bool, len(topics))
		for _, topic := range topics {
			if topic == "" {
				continue
			}
			topicSet[topic] = true
		}
		client := &Client{
			ID:        clientID,
			Send:      make(chan []byte, 64),
			Topics:    topicSet,
			CreatedAt: time.Now(),
		}

		// Register with hub
		hub.Register(client)
		defer hub.Unregister(client)

		// Send initial connected event
		_, _ = w.Write([]byte("data: {\"type\":\"connected\",\"client_id\":\"" + clientID + "\"}\n\n"))
		flusher.Flush()

		// Stream events to client
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return

			case msg, ok := <-client.Send:
				if !ok {
					return
				}

				_, err := w.Write(msg)
				if err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}
