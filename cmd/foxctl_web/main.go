// Command foxctl_web runs the foxctl web server.
//
// This server provides:
//   - REST API for jobs, CAS, skills, and other foxctl data
//   - SSE endpoint for real-time UI invalidation
//   - Optional static file serving for the built GUI
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
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/interfaces/web"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/logging"
)

func main() {
	var (
		addr    string
		uiDir   string
		devCORS bool
	)
	flag.StringVar(&addr, "addr", "127.0.0.1:8090", "HTTP listen address")
	flag.StringVar(&uiDir, "ui-dir", "", "Path to built UI (e.g., ./packages/gui-agent/dist)")
	flag.BoolVar(&devCORS, "dev-cors", true, "Enable permissive CORS for local dev")
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

	// Create server
	srv, err := web.NewServer(ctx, web.Options{
		Addr:    addr,
		UIDir:   uiDir,
		DevCORS: devCORS,
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
			Msg("foxctl_web listening")

		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server failed")
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	logger.Info().Str("signal", sig.String()).Msg("shutting down")

	// Cancel context to stop SSE hub and persistence goroutines
	cancel()

	// Wait for console transport persistence goroutines
	srv.WaitConsoleTransport()

	// Graceful HTTP shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}

	logger.Info().Msg("shutdown complete")
}
