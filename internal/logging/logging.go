// Package logging provides structured logging helpers for agentctl.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Level represents the configured logging verbosity.
type Level string

const (
	// LevelDebug enables debug messages.
	LevelDebug Level = "debug"
	// LevelInfo enables informational messages (default).
	LevelInfo Level = "info"
	// LevelWarn enables warning messages.
	LevelWarn Level = "warn"
	// LevelError enables error messages only.
	LevelError Level = "error"
)

// Format controls how log records are rendered.
type Format string

const (
	// FormatText outputs human-readable text logs.
	FormatText Format = "text"
	// FormatJSON outputs structured JSON logs.
	FormatJSON Format = "json"
)

// Config controls logger construction.
type Config struct {
	Level  Level
	Format Format
	Writer io.Writer
}

type ctxKey struct{}

var (
	defaultOnce sync.Once
	defaultLog  *slog.Logger

	discardOnce sync.Once
	discardLog  *slog.Logger
)

// New builds a slog.Logger using the supplied configuration.
func New(cfg Config) *slog.Logger {
	writer := cfg.Writer
	if writer == nil {
		writer = os.Stderr
	}

	opts := &slog.HandlerOptions{Level: parseLevel(cfg.Level)}

	var handler slog.Handler
	switch ParseFormat(string(cfg.Format)) {
	case FormatJSON:
		handler = slog.NewJSONHandler(writer, opts)
	default:
		handler = slog.NewTextHandler(writer, opts)
	}

	return slog.New(handler)
}

// Default returns a shared logger writing to stderr at info level.
func Default() *slog.Logger {
	defaultOnce.Do(func() {
		defaultLog = New(Config{Level: LevelInfo, Format: FormatText})
	})
	return defaultLog
}

// Discard returns a logger that drops all log records.
func Discard() *slog.Logger {
	discardOnce.Do(func() {
		handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})
		discardLog = slog.New(handler)
	})
	return discardLog
}

// WithContext attaches the logger to the context for downstream consumers.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext retrieves the logger previously stored on the context.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return nil
	}
	if logger, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return logger
	}
	return nil
}

// ParseLevel normalizes the provided string into a Level value.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	case "info", "":
		return LevelInfo
	default:
		return LevelInfo
	}
}

// ParseFormat normalizes the provided string into a Format value.
func ParseFormat(s string) Format {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json":
		return FormatJSON
	default:
		return FormatText
	}
}

func parseLevel(level Level) slog.Leveler {
	switch Level(strings.ToLower(string(level))) {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
