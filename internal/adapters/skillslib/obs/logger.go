package obs

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger provides structured logging for skills.
// It writes to stderr using zerolog and optionally emits wide events for warnings/errors.
type Logger struct {
	zl        zerolog.Logger
	command   string
	workspace string
	sessionID string
	agentID   string
	emitWarn  bool // emit wide events for warnings
	emitError bool // emit wide events for errors
}

// LoggerOption configures a Logger.
type LoggerOption func(*Logger)

// WithLogCommand sets the command name for log context.
func WithLogCommand(cmd string) LoggerOption {
	return func(l *Logger) { l.command = cmd }
}

// WithLogWorkspace sets the workspace for log context.
func WithLogWorkspace(ws string) LoggerOption {
	return func(l *Logger) { l.workspace = ws }
}

// WithLogSession sets session and agent IDs for log context.
func WithLogSession(sessionID, agentID string) LoggerOption {
	return func(l *Logger) {
		l.sessionID = sessionID
		l.agentID = agentID
	}
}

// WithEmitWarnings enables emitting wide events for warnings.
func WithEmitWarnings(emit bool) LoggerOption {
	return func(l *Logger) { l.emitWarn = emit }
}

// WithEmitErrors enables emitting wide events for errors.
func WithEmitErrors(emit bool) LoggerOption {
	return func(l *Logger) { l.emitError = emit }
}

// WithWriter sets a custom writer (default is os.Stderr).
func WithWriter(w io.Writer) LoggerOption {
	return func(l *Logger) {
		l.zl = zerolog.New(w).With().Timestamp().Logger()
	}
}

// NewLogger creates a new Logger for a skill.
// By default, it writes structured logs to stderr and does not emit wide events.
func NewLogger(opts ...LoggerOption) *Logger {
	l := &Logger{
		zl:        zerolog.New(os.Stderr).With().Timestamp().Logger(),
		emitWarn:  false,
		emitError: true, // Emit errors by default
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// baseEvent creates the base zerolog event with common fields.
func (l *Logger) baseEvent(e *zerolog.Event) *zerolog.Event {
	if l.command != "" {
		e = e.Str("command", l.command)
	}
	if l.workspace != "" {
		e = e.Str("workspace", l.workspace)
	}
	if l.sessionID != "" {
		e = e.Str("session_id", l.sessionID)
	}
	if l.agentID != "" {
		e = e.Str("agent_id", l.agentID)
	}
	return e
}

// Debug logs a debug message (not emitted as wide event).
func (l *Logger) Debug(msg string, fields ...Field) {
	e := l.zl.Debug()
	e = l.baseEvent(e)
	for _, f := range fields {
		e = f.Apply(e)
	}
	e.Msg(msg)
}

// Info logs an info message (not emitted as wide event).
func (l *Logger) Info(msg string, fields ...Field) {
	e := l.zl.Info()
	e = l.baseEvent(e)
	for _, f := range fields {
		e = f.Apply(e)
	}
	e.Msg(msg)
}

// Warn logs a warning message and optionally emits a wide event.
func (l *Logger) Warn(msg string, fields ...Field) {
	e := l.zl.Warn()
	e = l.baseEvent(e)
	for _, f := range fields {
		e = f.Apply(e)
	}
	e.Msg(msg)

	if l.emitWarn {
		l.emitEvent(context.Background(), "warn", msg, fields)
	}
}

// WarnCtx logs a warning with context for trace correlation.
func (l *Logger) WarnCtx(ctx context.Context, msg string, fields ...Field) {
	e := l.zl.Warn()
	e = l.baseEvent(e)
	for _, f := range fields {
		e = f.Apply(e)
	}
	e.Msg(msg)

	if l.emitWarn {
		l.emitEvent(ctx, "warn", msg, fields)
	}
}

// Error logs an error message and optionally emits a wide event.
func (l *Logger) Error(msg string, err error, fields ...Field) {
	e := l.zl.Error()
	e = l.baseEvent(e)
	if err != nil {
		e = e.Err(err)
	}
	for _, f := range fields {
		e = f.Apply(e)
	}
	e.Msg(msg)

	if l.emitError {
		allFields := append(fields, Err(err))
		l.emitEvent(context.Background(), "error", msg, allFields)
	}
}

// ErrorCtx logs an error with context for trace correlation.
func (l *Logger) ErrorCtx(ctx context.Context, msg string, err error, fields ...Field) {
	e := l.zl.Error()
	e = l.baseEvent(e)
	if err != nil {
		e = e.Err(err)
	}
	for _, f := range fields {
		e = f.Apply(e)
	}
	e.Msg(msg)

	if l.emitError {
		allFields := append(fields, Err(err))
		l.emitEvent(ctx, "error", msg, allFields)
	}
}

// emitEvent creates and emits a wide event for the log entry.
func (l *Logger) emitEvent(ctx context.Context, level, msg string, fields []Field) {
	op := "skill.log"
	if l.command != "" {
		op = l.command + ".log"
	}

	event := NewEvent(op).
		WithComponent(ComponentSkill).
		WithCommand(l.command).
		WithWorkspace(l.workspace).
		WithSession(l.sessionID, l.agentID).
		WithData("level", level).
		WithData("message", msg).
		EnrichFromContext(ctx)

	// Add fields to event data
	for _, f := range fields {
		if f.key != "" {
			event.WithData(f.key, f.value)
		}
	}

	// Use Success for warns, Error for errors
	if level == "error" {
		var err error
		for _, f := range fields {
			if f.err != nil {
				err = f.err
				break
			}
		}
		Emit(ctx, event.Error(err, 0))
	} else {
		Emit(ctx, event.Success(0))
	}
}

// Field represents a log field.
type Field struct {
	key   string
	value any
	err   error
}

// Apply adds the field to a zerolog event.
func (f Field) Apply(e *zerolog.Event) *zerolog.Event {
	if f.err != nil {
		return e.Err(f.err)
	}
	switch v := f.value.(type) {
	case string:
		return e.Str(f.key, v)
	case int:
		return e.Int(f.key, v)
	case int64:
		return e.Int64(f.key, v)
	case float64:
		return e.Float64(f.key, v)
	case bool:
		return e.Bool(f.key, v)
	case time.Duration:
		return e.Dur(f.key, v)
	case time.Time:
		return e.Time(f.key, v)
	case error:
		return e.AnErr(f.key, v)
	default:
		return e.Interface(f.key, v)
	}
}

// Field constructors

// Str creates a string field.
func Str(key, value string) Field {
	return Field{key: key, value: value}
}

// Int creates an int field.
func Int(key string, value int) Field {
	return Field{key: key, value: value}
}

// Int64 creates an int64 field.
func Int64(key string, value int64) Field {
	return Field{key: key, value: value}
}

// Float64 creates a float64 field.
func Float64(key string, value float64) Field {
	return Field{key: key, value: value}
}

// Bool creates a bool field.
func Bool(key string, value bool) Field {
	return Field{key: key, value: value}
}

// Dur creates a duration field.
func Dur(key string, value time.Duration) Field {
	return Field{key: key, value: value}
}

// Time creates a time field.
func Time(key string, value time.Time) Field {
	return Field{key: key, value: value}
}

// Any creates a field with any value.
func Any(key string, value any) Field {
	return Field{key: key, value: value}
}

// Err creates an error field.
func Err(err error) Field {
	return Field{err: err}
}
