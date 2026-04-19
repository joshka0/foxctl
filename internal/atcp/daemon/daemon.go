// Package daemon composes the ATCP broker with its HTTP/JSON transport and a
// Unix-socket listener into a single long-lived process.
//
// The daemon is deliberately thin: it does NOT touch persistence, mailboxes,
// or the legacy room runtime. Its only job is to own a *broker.Broker for
// the life of the process and expose it on a socket so separate client
// processes can drive it.
//
// Persistence (atcp_sessions, atcp_rooms, atcp_room_members) is a follow-up;
// today the daemon's state is volatile by design. This lets us prove the
// wire protocol first and decide on the schema once call patterns are real.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker"
	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
	"github.com/joshka0/foxctl/internal/atcp/transport/unixsocket"
)

// Options configures the daemon at construction time.
type Options struct {
	// SocketPath is the Unix socket to bind. Empty means DefaultSocketPath().
	SocketPath string
	// BrokerOptions tunes the broker itself. Zero value is sufficient.
	BrokerOptions broker.Options
	// ShutdownTimeout caps how long Shutdown waits for inflight requests.
	// Zero means 5s.
	ShutdownTimeout time.Duration
	// Logger is the structured logger used for lifecycle events. nil means
	// slog.Default().
	Logger *slog.Logger
}

// Daemon owns the broker and its transport. Not safe to call Start concurrently
// with itself, but Shutdown/Close are safe from any goroutine.
type Daemon struct {
	opts   Options
	logger *slog.Logger

	mu       sync.Mutex
	broker   *broker.Broker
	listener *unixsocket.Server
	started  bool
	stopped  bool
	serveErr chan error
}

// New builds a Daemon. Call Start to begin serving.
func New(opts Options) *Daemon {
	if opts.SocketPath == "" {
		opts.SocketPath = DefaultSocketPath()
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 5 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{opts: opts, logger: logger, serveErr: make(chan error, 1)}
}

// DefaultSocketPath returns the canonical socket location for a foxctl ATCP
// daemon. Uses $XDG_RUNTIME_DIR on Linux when set, falling back to $TMPDIR
// on macOS and elsewhere.
func DefaultSocketPath() string {
	if p := os.Getenv("FOXCTL_ATCP_SOCK"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "foxctl-atcp.sock")
	}
	return filepath.Join(os.TempDir(), "foxctl-atcp.sock")
}

// Start binds the socket and begins serving. Returns once the listener is
// accepting connections; Serve errors are reported via Wait.
func (d *Daemon) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return errors.New("atcp daemon: already started")
	}

	b := broker.New(d.opts.BrokerOptions)
	srv := httpjson.NewServer(b)
	ls, err := unixsocket.Listen(d.opts.SocketPath, srv.Handler())
	if err != nil {
		b.Stop()
		return fmt.Errorf("atcp daemon: listen: %w", err)
	}
	d.broker = b
	d.listener = ls
	d.started = true

	go func() {
		// Serve blocks until Shutdown/Close. Report whatever it returns so
		// Wait callers can surface unexpected errors.
		d.serveErr <- ls.Serve()
	}()

	d.logger.Info("atcp daemon started",
		slog.String("socket", d.opts.SocketPath),
	)
	return nil
}

// Broker returns the in-process broker so tests can poke it directly. Not
// useful for out-of-process clients.
func (d *Daemon) Broker() *broker.Broker {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.broker
}

// SocketPath returns the path the daemon bound to.
func (d *Daemon) SocketPath() string { return d.opts.SocketPath }

// Wait blocks until the listener exits, returning whatever Serve returned.
// Safe to call once after Start; subsequent calls return an error immediately.
func (d *Daemon) Wait(ctx context.Context) error {
	select {
	case err := <-d.serveErr:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown gracefully stops the listener and tears the broker down. Idempotent.
func (d *Daemon) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	if !d.started || d.stopped {
		d.mu.Unlock()
		return nil
	}
	d.stopped = true
	ls := d.listener
	b := d.broker
	d.mu.Unlock()

	shutCtx, cancel := context.WithTimeout(ctx, d.opts.ShutdownTimeout)
	defer cancel()
	err := ls.Shutdown(shutCtx)
	b.Stop()
	d.logger.Info("atcp daemon shut down", slog.String("socket", d.opts.SocketPath))
	return err
}

// Close is the hard-stop variant — use Shutdown for draining.
func (d *Daemon) Close() error {
	d.mu.Lock()
	if !d.started || d.stopped {
		d.mu.Unlock()
		return nil
	}
	d.stopped = true
	ls := d.listener
	b := d.broker
	d.mu.Unlock()
	err := ls.Close()
	b.Stop()
	return err
}
