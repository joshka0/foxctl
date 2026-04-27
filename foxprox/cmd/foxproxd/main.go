// foxproxd is the Foxprox broker daemon binary.
//
// This is intentionally the smallest shell around foxprox/daemon: it
// resolves a socket path, handles SIGINT/SIGTERM with a graceful shutdown,
// and logs a few lifecycle events. All real behavior lives in the daemon
// package so tests exercise the same code the binary does.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joshka/foxprox/foxprox/daemon"
)

func main() {
	var (
		socket          = flag.String("socket", "", "unix socket path (default: $FOXCTL_Foxprox_SOCK or platform default)")
		dataDir         = flag.String("data-dir", "", "directory for foxprox.db (default: in-memory only)")
		shutdownTimeout = flag.Duration("shutdown-timeout", 5*time.Second, "max time to wait for inflight requests on shutdown")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	d := daemon.New(daemon.Options{
		DataDir:         *dataDir,
		SocketPath:      *socket,
		ShutdownTimeout: *shutdownTimeout,
		Logger:          logger,
	})
	if err := d.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "foxproxd: start: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "foxproxd: listening on %s\n", d.SocketPath())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Block on either a signal or a serve error from the listener. Both
	// conditions route through Shutdown so the socket file is cleaned up
	// even on an unexpected listener exit.
	errCh := make(chan error, 1)
	go func() { errCh <- d.Wait(context.Background()) }()

	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "foxproxd: signal received, shutting down")
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "foxproxd: serve error: %v\n", err)
		}
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), *shutdownTimeout)
	defer shutCancel()
	if err := d.Shutdown(shutCtx); err != nil {
		fmt.Fprintf(os.Stderr, "foxproxd: shutdown: %v\n", err)
		os.Exit(1)
	}
}
