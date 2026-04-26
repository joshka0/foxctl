// Command foxctl_web runs the foxctl web server.
//
// This server provides:
//   - REST API for jobs, CAS, skills, and other foxctl data
//   - SSE endpoint for real-time UI invalidation
//   - Optional static file serving for the built GUI
//   - Optional chat adapter (discord, telegram, teams)
//   - Optional embedded Foxprox daemon for terminal-backed agents
//
// Usage:
//
//	foxctl_web [flags]
//
// Flags:
//
//	-addr string
//	      HTTP listen address (default "127.0.0.1:8090")
//	-ui-dir string
//	      Path to built UI (e.g., ./packages/gui-agent/dist)
//	-dev-cors
//	      Enable permissive CORS for local dev (default true)
//	-chat string
//	      Chat adapter to enable (discord|telegram|teams)
//	-db-driver string
//	      Database driver (sqlite|libsql|turso|postgres)
//	-db-dsn string
//	      PostgreSQL DSN (overrides FOXCTL_POSTGRES_DSN)
//	-foxprox
//	      Start an embedded Foxprox daemon for terminal-backed agents
//	-foxprox-data-dir string
//	      Directory for embedded Foxprox state
//	-foxprox-socket string
//	      Unix socket path for embedded Foxprox daemon
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/foxproxbridge"
	"github.com/joshka0/foxctl/internal/interfaces/web"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/logging"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

func main() {
	var (
		addr           string
		uiDir          string
		devCORS        bool
		chat           string
		dbDriverFlag   string
		dbDSNFlag      string
		foxprox        bool
		foxproxDataDir string
		foxproxSocket  string
		requireAuth    bool
	)
	flag.StringVar(&addr, "addr", "127.0.0.1:8090", "HTTP listen address")
	flag.StringVar(&uiDir, "ui-dir", "", "Path to built UI (e.g., ./packages/gui-agent/dist)")
	flag.BoolVar(&devCORS, "dev-cors", true, "Enable permissive CORS for local dev")
	flag.StringVar(&chat, "chat", "", "Chat adapter to enable (discord|telegram|teams)")
	flag.StringVar(&dbDriverFlag, "db-driver", "", "Database driver (sqlite|libsql|turso|postgres)")
	flag.StringVar(&dbDSNFlag, "db-dsn", "", "PostgreSQL DSN (overrides FOXCTL_POSTGRES_DSN)")
	flag.BoolVar(&foxprox, "foxprox", false, "Start an embedded Foxprox daemon for terminal-backed agents")
	flag.StringVar(&foxproxDataDir, "foxprox-data-dir", defaultFoxproxDataDir(), "Directory for embedded Foxprox state")
	flag.StringVar(&foxproxSocket, "foxprox-socket", "", "Unix socket path for embedded Foxprox daemon")
	flag.BoolVar(&requireAuth, "require-auth", false, "Require authenticated identity for API requests (Tailscale or Better Auth)")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load env files
	config.LoadDotEnv()

	cfg, err := config.LoadCached(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Apply CLI flag overrides
	if dbDriverFlag != "" {
		cfg.Database.Driver = dbDriverFlag
	}
	if dbDSNFlag != "" {
		cfg.Database.Postgres.DSN = dbDSNFlag
	}

	logPath := cfg.Logging.Output
	if logPath == "" {
		logPath = filepath.Join(os.TempDir(), "foxctl-companion.log")
	}
	writer, closeWriter, err := logging.OpenWriter(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	if closeWriter != nil {
		defer func() {
			if err := closeWriter(); err != nil {
				fmt.Fprintf(os.Stderr, "failed to close log writer: %v\n", err)
			}
		}()
	}

	// Set up logger
	logger := logging.New(logging.Config{
		Level:  logging.ParseLevel(cfg.Logging.Level),
		Format: logging.ParseFormat(cfg.Logging.Format),
		Writer: writer,
	}).With().
		Timestamp().
		Str("service", "foxctl_web").
		Logger()

	// Start embedded Foxprox daemon if requested
	var foxproxd foxproxbridge.DaemonLifecycle
	var foxproxOwned bool
	var foxproxErrCh <-chan error
	if foxprox {
		started, waitCh, startErr := startFoxproxDaemon(writer)
		if startErr != nil {
			fmt.Fprintf(os.Stderr, "foxprox: %v\n", startErr)
			os.Exit(1)
		}
		foxproxd = started
		foxproxOwned = started != nil
		foxproxErrCh = waitCh
		if foxproxOwned {
			logger.Info().Str("socket", foxproxd.SocketPath()).Msg("Embedded Foxprox daemon started")
		} else {
			sk := foxproxSocket
			if sk == "" {
				sk = foxproxbridge.DefaultSocketPath()
			}
			logger.Info().Str("socket", sk).Msg("Foxprox daemon already running; reusing socket")
		}
	}

	// Create server
	srv, err := web.NewServer(ctx, web.Options{
		Addr:        addr,
		UIDir:       uiDir,
		DevCORS:     devCORS,
		ChatAdapter: chat,
		RequireAuth: requireAuth,
	}, cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create server")
	}

	// Start SSE hub
	go srv.Run(ctx)

	// Create HTTP server
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // SSE needs no write timeout
		IdleTimeout:       120 * time.Second,
	}

	// Start HTTP server
	go func() {
		logger.Info().
			Str("addr", addr).
			Bool("dev_cors", devCORS).
			Str("ui_dir", uiDir).
			Str("chat", chat).
			Bool("foxprox", foxprox).
			Msg("foxctl_web listening")

		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server failed")
		}
	}()

	// Wait for shutdown signal, foxprox error, or context cancellation
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-shutdownCh:
		logger.Info().Str("signal", sig.String()).Msg("shutting down")
	case err := <-foxproxErrCh:
		if err != nil {
			logger.Error().Err(err).Msg("foxprox daemon error")
		}
	case <-ctx.Done():
		logger.Info().Msg("context cancelled, shutting down")
	}

	// Cancel context to stop SSE hub and persistence goroutines
	cancel()

	// Wait for console transport persistence goroutines
	srv.WaitConsoleTransport()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Stop chat adapter if running
	srv.StopChatAdapter(shutdownCtx)

	// Stop foxprox daemon if owned
	if foxproxOwned && foxproxd != nil {
		if err := foxproxd.Shutdown(shutdownCtx); err != nil {
			observability.Emit(context.Background(), observability.NewEvent("web.foxprox_shutdown_error").
				WithComponent(observability.ComponentCLI).
				Error(err, 0))
		}
	}

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}

	logger.Info().Msg("shutdown complete")
}

func startFoxproxDaemon(logWriter io.Writer) (foxproxbridge.DaemonLifecycle, <-chan error, error) {
	d, err := foxproxbridge.NewDaemon(foxproxbridge.DaemonOptions{
		DataDir:    defaultFoxproxDataDir(),
		SocketPath: "",
		LogWriter:  logWriter,
	})
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return nil, nil, fmt.Errorf("foxprox: implementation not linked; import foxproxbridge/foxproxwire")
	}
	if err := d.Start(); err != nil {
		if errors.Is(err, foxproxbridge.ErrBrokerAlreadyRunning) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to start embedded Foxprox daemon: %w", err)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.Wait(context.Background())
	}()
	return d, errCh, nil
}

func defaultFoxproxDataDir() string {
	if dir := os.Getenv("FOXCTL_FOXPROX_DATA_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".foxctl", "foxprox")
}
