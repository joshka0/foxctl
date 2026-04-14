package web

// Options configures the web server.
type Options struct {
	// Addr is the HTTP listen address (e.g., "127.0.0.1:8090").
	Addr string

	// UIDir is an optional path to serve static UI build files.
	// If empty, the server only serves API endpoints.
	UIDir string

	// DevCORS enables permissive CORS headers for local development.
	DevCORS bool

	// ChatAdapter selects the chat platform adapter to enable (e.g. "discord").
	// If empty, no chat adapter is started.
	ChatAdapter string
}
