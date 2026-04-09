// Package gateway implements the tsnet-based HTTP/HTTPS gateway server for
// agentctl room sandbox terminal access. It supports two modes:
//   - Tailscale mode: HTTPS via tsnet.ListenTLS with auto-TLS
//   - Dev mode: plain HTTP on localhost (for development without Tailscale)
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/tsnet"

	"github.com/jkatigb/agentctl/internal/gateway/webterm"
)

const (
	// DefaultPort is the default port for dev mode HTTP listener.
	DefaultPort = 8765

	// DefaultStateDir is the default directory for tsnet state persistence.
	DefaultStateDir = "~/.agentctl/gateway"

	// ShutdownTimeout is the maximum time to wait for graceful shutdown.
	ShutdownTimeout = 15 * time.Second

	// HostnamePrefix is the tsnet hostname prefix.
	HostnamePrefix = "agentctl-gateway"
)

// HealthStatus represents the health of a subsystem.
type HealthStatus struct {
	Tsnet string `json:"tsnet"`
	Store string `json:"store"`
	Tmux  string `json:"tmux"`
}

// Options configures the gateway server.
type Options struct {
	// Dev enables development mode (localhost HTTP, no Tailscale).
	Dev bool

	// Port is the HTTP port for dev mode. Defaults to 8765.
	Port int

	// StateDir is the directory for tsnet state persistence.
	// Defaults to ~/.agentctl/gateway.
	StateDir string

	// AuthKey is the Tailscale auth key. Can also be set via TS_AUTHKEY env var.
	AuthKey string

	// Hostname is the tsnet hostname. Defaults to "agentctl-gateway".
	Hostname string
}

// DefaultOptions returns options with sensible defaults.
func DefaultOptions() Options {
	return Options{
		Port:     DefaultPort,
		StateDir: DefaultStateDir,
		Hostname: HostnamePrefix,
	}
}

// ResolveStateDir expands ~ and returns the absolute state directory path.
func ResolveStateDir(dir string) (string, error) {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, dir[2:])
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return abs, nil
}

// ResolveAuthKey returns the auth key from options or the TS_AUTHKEY env var.
func ResolveAuthKey(optKey string) string {
	if optKey != "" {
		return optKey
	}
	return os.Getenv("TS_AUTHKEY")
}

// Server is the gateway server.
type Server struct {
	opts    Options
	log     zerolog.Logger
	tsnet   *tsnet.Server
	http    *http.Server
	mu      sync.RWMutex
	health  HealthStatus
	started bool
	termHub *webterm.Hub
}

// NewServer creates a new gateway server.
func NewServer(opts Options, log zerolog.Logger) *Server {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	if opts.Hostname == "" {
		opts.Hostname = HostnamePrefix
	}
	return &Server{
		opts: opts,
		log:  log.With().Str("component", "gateway").Logger(),
		health: HealthStatus{
			Tsnet: "starting",
			Store: "starting",
			Tmux:  "ok",
		},
		termHub: webterm.NewHub(webterm.HubConfig{}, log),
	}
}

// Health returns the current subsystem health status.
func (s *Server) Health() HealthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.health
}

// setHealth updates the subsystem health status.
func (s *Server) setHealth(h HealthStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health = h
}

// Handler returns the HTTP handler for the gateway.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)

	// Web terminal routes
	termHandler := webterm.NewHandler(s.termHub, s.log)
	termHandler.RegisterRoutes(mux)

	return mux
}

// RegisterTerminalRoom registers a room for terminal access.
// The room will be accessible at /terminal/{roomID} and /ws/terminal/{roomID}.
func (s *Server) RegisterTerminalRoom(roomID, tmuxSession string, maxConnections int) {
	config := webterm.RoomConfig{
		TmuxSession:    tmuxSession,
		MaxConnections: maxConnections,
	}
	s.termHub.RegisterRoom(roomID, config)
	s.log.Info().
		Str("room", roomID).
		Str("tmux_session", tmuxSession).
		Msg("Terminal room registered")
}

// UnregisterTerminalRoom removes a room's terminal access.
func (s *Server) UnregisterTerminalRoom(roomID string) {
	s.termHub.UnregisterRoom(roomID)
	s.log.Info().
		Str("room", roomID).
		Msg("Terminal room unregistered")
}

// TerminalHub returns the web terminal hub for direct access.
func (s *Server) TerminalHub() *webterm.Hub {
	return s.termHub
}

// handleHealthz returns the subsystem health status as JSON.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":{"code":"EARG","message":"method not allowed"}}`, http.StatusMethodNotAllowed)
		return
	}

	health := s.Health()

	// Check if any subsystem is degraded
	degraded := false
	for _, status := range []string{health.Tsnet, health.Store, health.Tmux} {
		if status != "ok" && status != "starting" && status != "disabled" {
			degraded = true
			break
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	statusCode := http.StatusOK
	if degraded {
		statusCode = http.StatusServiceUnavailable
	}
	w.WriteHeader(statusCode)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(health)
}

// Start starts the gateway server. It blocks until the server is stopped
// or an error occurs.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("gateway already started")
	}
	s.started = true
	s.mu.Unlock()

	if s.opts.Dev {
		return s.startDev(ctx)
	}
	return s.startTsnet(ctx)
}

// startDev starts the gateway in dev mode (localhost HTTP).
func (s *Server) startDev(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.opts.Port)

	s.log.Warn().
		Str("addr", addr).
		Msg("Starting in dev mode — no Tailscale, no TLS, localhost only")

	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	s.setHealth(HealthStatus{
		Tsnet: "disabled",
		Store: "ok",
		Tmux:  "ok",
	})

	errCh := make(chan error, 1)
	go func() {
		s.log.Info().Str("addr", addr).Msg("Gateway HTTP server listening")
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

// startTsnet starts the gateway in Tailscale mode (HTTPS via tsnet).
func (s *Server) startTsnet(ctx context.Context) error {
	// Resolve auth key
	authKey := ResolveAuthKey(s.opts.AuthKey)

	// Resolve state directory
	stateDir, err := ResolveStateDir(s.opts.StateDir)
	if err != nil {
		return fmt.Errorf("resolve state directory: %w", err)
	}

	// Check if we have existing state (no auth key needed for restarts)
	hasState := false
	if info, statErr := os.Stat(stateDir); statErr == nil && info.IsDir() {
		// Check for tsnet identity file
		identityFile := filepath.Join(stateDir, "tailscaled.state")
		if _, idErr := os.Stat(identityFile); idErr == nil {
			hasState = true
		}
	}

	if !hasState && authKey == "" {
		return &AuthKeyError{
			StateDir: stateDir,
		}
	}

	// Create state directory if needed
	if mkdirErr := os.MkdirAll(stateDir, 0o700); mkdirErr != nil {
		return fmt.Errorf("create state directory: %w", mkdirErr)
	}

	// Create tsnet server
	ts := &tsnet.Server{
		Hostname: s.opts.Hostname,
		Dir:      stateDir,
		AuthKey:  authKey,
		Logf: func(format string, args ...any) {
			s.log.Debug().Msgf("tsnet: "+format, args...)
		},
	}
	s.tsnet = ts

	s.log.Info().
		Str("hostname", s.opts.Hostname).
		Str("state_dir", stateDir).
		Bool("has_state", hasState).
		Msg("Starting tsnet node")

	// Start tsnet
	if err := ts.Start(); err != nil {
		return fmt.Errorf("start tsnet: %w", err)
	}

	// Update health for tsnet
	s.setHealth(HealthStatus{
		Tsnet: "ok",
		Store: "ok",
		Tmux:  "ok",
	})

	// Wait for tsnet to come up and log status
	status, err := ts.Up(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("Failed to get tsnet status (continuing anyway)")
	} else {
		fields := map[string]any{
			"backend_state": status.BackendState,
		}
		if status.CurrentTailnet != nil {
			fields["tailnet_name"] = status.CurrentTailnet.Name
			fields["magic_dns_suffix"] = status.CurrentTailnet.MagicDNSSuffix
		}
		if len(status.TailscaleIPs) > 0 {
			fields["tailscale_ip"] = status.TailscaleIPs[0].String()
		}
		if status.Self != nil && status.Self.DNSName != "" {
			fields["dns_name"] = status.Self.DNSName
		}
		s.log.Info().Fields(fields).Msg("tsnet node online")
	}

	// Get HTTPS listener
	ln, err := ts.ListenTLS("tcp", ":443")
	if err != nil {
		return fmt.Errorf("tsnet ListenTLS: %w", err)
	}

	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(_ net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info().Msg("Gateway HTTPS server listening on tsnet")
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

// shutdown gracefully shuts down the server.
func (s *Server) shutdown() error {
	s.log.Info().Msg("Shutting down gateway")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	var errs []error

	if s.http != nil {
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("http shutdown: %w", err))
		}
	}

	if s.tsnet != nil {
		if err := s.tsnet.Close(); err != nil {
			errs = append(errs, fmt.Errorf("tsnet close: %w", err))
		}
	}

	// Close terminal hub (disconnects all web terminal clients)
	if s.termHub != nil {
		s.termHub.Close()
	}

	s.setHealth(HealthStatus{
		Tsnet: "stopped",
		Store: "stopped",
		Tmux:  "stopped",
	})

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	s.log.Info().Msg("Gateway stopped")
	return nil
}

// AuthKeyError is returned when no Tailscale auth key is available
// and no existing tsnet state exists.
type AuthKeyError struct {
	StateDir string
}

func (e *AuthKeyError) Error() string {
	return "no Tailscale auth key provided and no existing state"
}

// Envelope returns a JSON-envelope-friendly error representation.
func (e *AuthKeyError) Envelope() map[string]any {
	return map[string]any{
		"error": map[string]string{
			"code":    "EARG",
			"message": e.Error(),
		},
		"data": map[string]string{
			"hint":    "Set TS_AUTHKEY env var or pass --ts-authkey flag. Get a key from https://login.tailscale.com/admin/settings/keys",
			"state":   e.StateDir,
			"command": "agentctl gateway --ts-authkey tskey-auth-xxx",
		},
	}
}

// Run is the top-level entry point for the gateway command.
// It creates a server, handles signal-based shutdown, and returns
// appropriate error envelopes.
func Run(ctx context.Context, opts Options, log zerolog.Logger) error {
	srv := NewServer(opts, log)

	// Set up signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(ctx); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
		// Wait for shutdown to complete
		return <-errCh
	case err := <-errCh:
		if err == nil {
			return nil
		}
		// Check if it's an auth key error and return envelope
		var authErr *AuthKeyError
		if errors.As(err, &authErr) {
			return err
		}
		return err
	case <-ctx.Done():
		return <-errCh
	}
}
