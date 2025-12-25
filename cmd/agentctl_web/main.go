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
	"github.com/rs/zerolog/log"
)

const version = "0.1.0"

func main() {
	// Setup structured logging to stderr
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	port := flag.Int("port", 8090, "HTTP port to listen on")
	workspace := flag.String("workspace", "", "Filter by workspace path")
	showVersion := flag.Bool("version", false, "Show version")
	showHelp := flag.Bool("help", false, "Show help")
	flag.Parse()

	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("agentctl-web v%s\n", version)
		os.Exit(0)
	}

	server := NewServer(*workspace)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: server,
	}

	log.Info().
		Str("version", version).
		Int("port", *port).
		Str("workspace", workspaceDisplay(*workspace)).
		Msg("agentctl-web starting")

	// Start server in goroutine
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server error")
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Info().Msg("Shutting down gracefully...")
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}
}

func workspaceDisplay(ws string) string {
	if ws == "" {
		return "(all)"
	}
	return ws
}

func printHelp() {
	fmt.Print(`agentctl-web - Web UI for agentctl harness

USAGE:
    agentctl-web [OPTIONS]

OPTIONS:
    --port <PORT>         HTTP port to listen on (default: 8090)
    --workspace <PATH>    Filter by workspace path
    --version             Show version
    --help                Show this help

EXAMPLES:
    # Start web UI on default port
    agentctl-web

    # Start on custom port with workspace filter
    agentctl-web --port 3000 --workspace /path/to/project
`)
}
