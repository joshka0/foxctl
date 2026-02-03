package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/web"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Web server commands",
}

var webServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web API server",
	Long: `Start the agentctl web API server.

The server provides REST API endpoints for the GUI and other clients.
By default it listens on port 8090.

Examples:
  # Start server on default port 8090
  agentctl web serve

  # Start server on custom port
  agentctl web serve --port 3000

  # Start server with CORS enabled for development
  agentctl web serve --dev-cors

  # Start server with static UI files
  agentctl web serve --ui-dir ./packages/gui/dist`,
	RunE: runWebServe,
}

var (
	webPort    int
	webDevCORS bool
	webUIDir   string
)

func init() {
	rootCmd.AddCommand(webCmd)
	webCmd.AddCommand(webServeCmd)

	webServeCmd.Flags().IntVarP(&webPort, "port", "p", 8090, "Port to listen on")
	webServeCmd.Flags().BoolVar(&webDevCORS, "dev-cors", false, "Enable CORS for development (allows localhost:5173)")
	webServeCmd.Flags().StringVar(&webUIDir, "ui-dir", "", "Directory containing static UI files to serve")
}

func runWebServe(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Load config
	cfg, err := loadConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger for web server internals
	// TODO: Migrate web server to use observability instead of zerolog
	log := zerolog.New(os.Stderr).With(). //nolint:forbidigo // web server requires zerolog internally
						Timestamp().
						Str("component", "web").
						Logger()

	// Create web server
	opts := web.Options{
		DevCORS: webDevCORS,
		UIDir:   webUIDir,
	}

	server, err := web.NewServer(ctx, opts, cfg, log)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start SSE hub in background
	go server.Run(ctx)

	// Create HTTP server with appropriate timeouts
	// Note: WriteTimeout is 0 to support SSE/streaming connections
	addr := fmt.Sprintf(":%d", webPort)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Set up graceful shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGTERM, syscall.SIGINT)

	// Start server in goroutine
	errCh := make(chan error, 1)
	go func() {
		fmt.Fprintf(os.Stderr, "Starting agentctl web server on http://localhost%s\n", addr)
		fmt.Fprintf(os.Stderr, "Health: http://localhost%s/api/health\n", addr)
		if webDevCORS {
			fmt.Fprintf(os.Stderr, "CORS enabled for development (localhost:5173)\n")
		}
		if webUIDir != "" {
			fmt.Fprintf(os.Stderr, "Serving UI from: %s\n", webUIDir)
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-shutdownCh:
		fmt.Fprintf(os.Stderr, "\nReceived %s, shutting down gracefully...\n", sig)
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		fmt.Fprintf(os.Stderr, "\nContext cancelled, shutting down...\n")
	}

	// Cancel context to stop persistence goroutines
	cancel()

	// Wait for console hub persistence goroutines
	server.ConsoleHub().Wait()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		observability.Emit(context.Background(), observability.NewEvent("web.shutdown_error").
			WithComponent(observability.ComponentCLI).
			Error(err, 0))
		return err
	}

	fmt.Fprintf(os.Stderr, "Server stopped\n")
	return nil
}
