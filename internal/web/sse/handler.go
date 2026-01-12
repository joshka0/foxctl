package sse

import (
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"
)

// Handler returns an HTTP handler for the SSE endpoint.
func Handler(hub *Hub, log zerolog.Logger) http.HandlerFunc {
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
		client := &Client{
			ID:        clientID,
			Send:      make(chan []byte, 64),
			Topics:    make(map[string]bool),
			CreatedAt: time.Now(),
		}

		// Register with hub
		hub.Register(client)
		defer hub.Unregister(client)

		log.Debug().Str("client_id", clientID).Msg("sse client connected")

		// Send initial connected event
		_, _ = w.Write([]byte("data: {\"type\":\"connected\",\"client_id\":\"" + clientID + "\"}\n\n"))
		flusher.Flush()

		// Stream events to client
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				log.Debug().Str("client_id", clientID).Msg("sse client disconnected (context)")
				return

			case msg, ok := <-client.Send:
				if !ok {
					log.Debug().Str("client_id", clientID).Msg("sse client channel closed")
					return
				}

				_, err := w.Write(msg)
				if err != nil {
					log.Debug().Str("client_id", clientID).Err(err).Msg("sse write error")
					return
				}
				flusher.Flush()
			}
		}
	}
}
