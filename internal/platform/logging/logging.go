//nolint:forbidigo // This IS the logging infrastructure - zerolog/stderr usage is intentional
package logging

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/secrets"
	"github.com/mattn/go-colorable"
	"github.com/rs/zerolog"
)

// Level represents the configured logging verbosity.
type Level string

const (
	// LevelTrace enables the most verbose log output.
	LevelTrace Level = "trace"
	// LevelDebug enables debug logging.
	LevelDebug Level = "debug"
	// LevelInfo emits informational logging (default).
	LevelInfo Level = "info"
	// LevelWarn emits warnings and errors only.
	LevelWarn Level = "warn"
	// LevelError emits error messages only.
	LevelError Level = "error"
)

// Format controls how log records are rendered.
type Format string

const (
	// FormatText outputs console-friendly logs.
	FormatText Format = "text"
	// FormatJSON writes structured JSON logs.
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
	defaultLog  zerolog.Logger

	discardOnce sync.Once
	discardLog  zerolog.Logger
)

// New builds a zerolog.Logger using the supplied configuration.
func New(cfg Config) zerolog.Logger {
	writer := cfg.Writer
	if writer == nil {
		writer = os.Stderr
	}

	useConsole := ParseFormat(string(cfg.Format)) == FormatText
	if useConsole {
		if f, ok := writer.(*os.File); ok {
			writer = colorable.NewColorable(f)
		}
		writer = &zerolog.ConsoleWriter{Out: writer, TimeFormat: time.RFC3339, NoColor: false}
	}

	writer = &redactingWriter{w: writer}

	lvl, err := zerolog.ParseLevel(strings.ToLower(string(cfg.Level)))
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	logger := zerolog.New(writer).Level(lvl).With().Timestamp().Logger()
	return logger
}

// Default returns a shared logger writing to stderr at info level.
func Default() zerolog.Logger {
	defaultOnce.Do(func() {
		defaultLog = New(Config{Level: LevelInfo, Format: FormatText})
	})
	return defaultLog
}

// Discard returns a logger that drops all log records.
func Discard() zerolog.Logger {
	discardOnce.Do(func() {
		discardLog = zerolog.New(io.Discard)
	})
	return discardLog
}

// WithContext attaches the logger to the context for downstream consumers.
func WithContext(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext retrieves the logger previously stored on the context.
func FromContext(ctx context.Context) zerolog.Logger {
	if ctx == nil {
		return Default()
	}
	if logger, ok := ctx.Value(ctxKey{}).(zerolog.Logger); ok {
		return logger
	}
	return Default()
}

// ParseLevel normalizes the provided string into a Level value.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace
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

type redactingWriter struct {
	w io.Writer
}

func (rw *redactingWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	redacted := secrets.Redact(string(p))
	if _, err := rw.w.Write([]byte(redacted)); err != nil {
		return 0, err
	}
	return len(p), nil
}
