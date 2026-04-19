// Package unixsocket provides the ATCP broker's Unix domain socket listener.
//
// The socket is created with mode 0600 so only the owning user can connect.
// Callers supply an http.Handler (typically the httpjson.Server handler) and
// receive a fully-wired *http.Server plus a stop hook that tears down the
// listener and removes the on-disk socket file.
package unixsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultSocketPath returns the default ATCP socket path. Unix tradition uses
// $XDG_RUNTIME_DIR when set, otherwise a dotfile under $HOME.
func DefaultSocketPath() string {
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "foxctl", "atcp.sock")
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".foxctl", "atcp.sock")
	}
	return filepath.Join(os.TempDir(), "foxctl-atcp.sock")
}

// Server pairs a Unix socket listener with an http.Server.
type Server struct {
	path   string
	listen net.Listener
	http   *http.Server
}

// ErrBrokerAlreadyRunning is returned when Listen detects a live peer bound
// to the requested socket path.
var ErrBrokerAlreadyRunning = errors.New("atcp unixsocket: broker already running at path")

// Listen creates the socket file at path (creating parent dirs as needed),
// ensures it is chmod 0600, and returns a Server ready for Serve.
//
// If the path already exists as a socket, Listen dials it briefly to test
// whether a live broker owns it. A successful dial returns ErrBrokerAlreadyRunning
// — we refuse to remove a live socket and steal its binding. A connection
// refused (the classical stale-socket indicator) lets Listen proceed.
func Listen(path string, handler http.Handler) (*Server, error) {
	if path == "" {
		return nil, errors.New("atcp unixsocket: path is required")
	}
	if handler == nil {
		return nil, errors.New("atcp unixsocket: handler is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("atcp unixsocket: mkdir: %w", err)
	}
	if err := assertSocketIsStale(path); err != nil {
		return nil, err
	}
	if err := removeIfSocket(path); err != nil {
		return nil, fmt.Errorf("atcp unixsocket: cleanup stale: %w", err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("atcp unixsocket: listen: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("atcp unixsocket: chmod: %w", err)
	}
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
	}
	return &Server{path: path, listen: ln, http: srv}, nil
}

// Path returns the on-disk socket path.
func (s *Server) Path() string { return s.path }

// Serve runs the HTTP server. Blocks until Shutdown or the listener closes.
func (s *Server) Serve() error {
	err := s.http.Serve(s.listen)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown gracefully stops accepting connections, waits up to timeout for
// inflight requests, then removes the socket file.
func (s *Server) Shutdown(ctx context.Context) error {
	shutdownErr := s.http.Shutdown(ctx)
	_ = os.Remove(s.path)
	return shutdownErr
}

// Close is the hard-stop variant: closes the listener immediately and removes
// the socket file. Use Shutdown for graceful drain.
func (s *Server) Close() error {
	closeErr := s.http.Close()
	_ = os.Remove(s.path)
	return closeErr
}

// assertSocketIsStale rejects Listen when another process is currently bound
// to path. The probe is a short-timeout dial. Any error *other* than a
// successful connect is treated as "safe to proceed"; removeIfSocket runs
// next and will still refuse to delete non-socket files.
func assertSocketIsStale(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		// Stat failure will be surfaced by the real listener; don't pre-empt it.
		return nil
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %s", ErrBrokerAlreadyRunning, path)
	}
	return nil
}

// removeIfSocket unlinks path iff it is a socket. Any other file type is left
// alone to avoid clobbering user data.
func removeIfSocket(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket at %s", path)
	}
	return os.Remove(path)
}
