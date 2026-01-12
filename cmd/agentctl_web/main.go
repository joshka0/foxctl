// Command agentctl_web runs the agentctl web server.
//
// This server provides:
//   - REST API for jobs, CAS, skills, and other agentctl data
//   - SSE endpoint for real-time UI invalidation
//   - Optional static file serving for the built GUI
//
// Usage:
//
//	agentctl_web [flags]
//
// Flags:
//
//	-addr string
//	      HTTP listen address (default "127.0.0.1:8090")
//	-ui-dir string
//	      Path to built UI (e.g., ./packages/gui/dist)
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
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web"
)

func main() {
	var (
		addr    string
		uiDir   string
		devCORS bool
	)
	flag.StringVar(&addr, "addr", "127.0.0.1:8090", "HTTP listen address")
	flag.StringVar(&uiDir, "ui-dir", "", "Path to built UI (e.g., ./packages/gui/dist)")
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

	// Set up logger
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
		With().
		Timestamp().
		Str("service", "agentctl_web").
		Logger()

	// Create server
	srv, err := web.NewServer(web.Options{
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
			Msg("agentctl_web listening")

		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("http server failed")
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	logger.Info().Str("signal", sig.String()).Msg("shutting down")

	// Cancel context to stop SSE hub
	cancel()

	// Graceful HTTP shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("http shutdown error")
	}

	logger.Info().Msg("shutdown complete")
}
