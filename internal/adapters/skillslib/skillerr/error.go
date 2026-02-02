package skillerr

import (
	"errors"
	"fmt"
)

// Error is a structured skill error with code, message, hint, and optional data.
type Error struct {
	Code    string         // Machine-readable error code (EARG, ERUNTIME, etc.)
	Message string         // Human-readable error message
	Hint    string         // Optional user guidance for resolution
	Data    map[string]any // Optional context data
	Cause   error          // Wrapped underlying error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is reports whether target matches this error's code.
func (e *Error) Is(target error) bool {
	var t *Error
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// Option configures an Error.
type Option func(*Error)

// WithHint adds a user-facing hint for error resolution.
func WithHint(hint string) Option {
	return func(e *Error) {
		e.Hint = hint
	}
}

// WithData adds contextual data to the error.
func WithData(key string, value any) Option {
	return func(e *Error) {
		if e.Data == nil {
			e.Data = make(map[string]any)
		}
		e.Data[key] = value
	}
}

// WithCause wraps an underlying error.
func WithCause(err error) Option {
	return func(e *Error) {
		e.Cause = err
	}
}

// newError creates a new Error with the given code and message.
func newError(code, msg string, opts ...Option) *Error {
	e := &Error{
		Code:    code,
		Message: msg,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Arg creates an argument/input validation error (EARG).
func Arg(msg string, opts ...Option) *Error {
	return newError(CodeArg, msg, opts...)
}

// Argf creates a formatted argument error.
func Argf(format string, args ...any) *Error {
	return newError(CodeArg, fmt.Sprintf(format, args...))
}

// Runtime creates a runtime/execution error (ERUNTIME).
func Runtime(msg string, opts ...Option) *Error {
	return newError(CodeRuntime, msg, opts...)
}

// Runtimef creates a formatted runtime error.
func Runtimef(format string, args ...any) *Error {
	return newError(CodeRuntime, fmt.Sprintf(format, args...))
}

// Parse creates a parsing error (EPARSE).
func Parse(msg string, opts ...Option) *Error {
	return newError(CodeParse, msg, opts...)
}

// Parsef creates a formatted parsing error.
func Parsef(format string, args ...any) *Error {
	return newError(CodeParse, fmt.Sprintf(format, args...))
}

// IO creates an I/O error (EIO).
func IO(msg string, opts ...Option) *Error {
	return newError(CodeIO, msg, opts...)
}

// IOf creates a formatted I/O error.
func IOf(format string, args ...any) *Error {
	return newError(CodeIO, fmt.Sprintf(format, args...))
}

// Auth creates an authentication/authorization error (EAUTH).
func Auth(msg string, opts ...Option) *Error {
	return newError(CodeAuth, msg, opts...)
}

// Authf creates a formatted auth error.
func Authf(format string, args ...any) *Error {
	return newError(CodeAuth, fmt.Sprintf(format, args...))
}

// Validation creates a validation error (EVALIDATION).
func Validation(msg string, opts ...Option) *Error {
	return newError(CodeValidation, msg, opts...)
}

// Validationf creates a formatted validation error.
func Validationf(format string, args ...any) *Error {
	return newError(CodeValidation, fmt.Sprintf(format, args...))
}

// NotFound creates a not found error (ENOTFOUND).
func NotFound(msg string, opts ...Option) *Error {
	return newError(CodeNotFound, msg, opts...)
}

// NotFoundf creates a formatted not found error.
func NotFoundf(format string, args ...any) *Error {
	return newError(CodeNotFound, fmt.Sprintf(format, args...))
}

// Integration creates an external integration error (EINTEGRATION).
func Integration(msg string, opts ...Option) *Error {
	return newError(CodeIntegration, msg, opts...)
}

// Integrationf creates a formatted integration error.
func Integrationf(format string, args ...any) *Error {
	return newError(CodeIntegration, fmt.Sprintf(format, args...))
}

// Capability creates a capability/feature not supported error (ECAPABILITY).
func Capability(msg string, opts ...Option) *Error {
	return newError(CodeCapability, msg, opts...)
}

// Capabilityf creates a formatted capability error.
func Capabilityf(format string, args ...any) *Error {
	return newError(CodeCapability, fmt.Sprintf(format, args...))
}

// Wrap wraps an existing error with a skill error code.
func Wrap(code, msg string, err error, opts ...Option) *Error {
	allOpts := append([]Option{WithCause(err)}, opts...)
	return newError(code, msg, allOpts...)
}

// WrapRuntime wraps an error as a runtime error.
func WrapRuntime(msg string, err error, opts ...Option) *Error {
	return Wrap(CodeRuntime, msg, err, opts...)
}

// WrapArg wraps an error as an argument error.
func WrapArg(msg string, err error, opts ...Option) *Error {
	return Wrap(CodeArg, msg, err, opts...)
}

// WrapIO wraps an error as an I/O error.
func WrapIO(msg string, err error, opts ...Option) *Error {
	return Wrap(CodeIO, msg, err, opts...)
}

// WrapParse wraps an error as a parse error.
func WrapParse(msg string, err error, opts ...Option) *Error {
	return Wrap(CodeParse, msg, err, opts...)
}

// WrapValidation wraps an error as a validation error.
func WrapValidation(msg string, err error, opts ...Option) *Error {
	return Wrap(CodeValidation, msg, err, opts...)
}

// IsCode checks if an error has a specific skill error code.
func IsCode(err error, code string) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == code
	}
	return false
}

// GetCode extracts the error code from an error, or empty string if not a skill error.
func GetCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// ToEnvelopeData converts an Error to a map suitable for envelope.Error data field.
func (e *Error) ToEnvelopeData() map[string]any {
	data := make(map[string]any)
	if e.Hint != "" {
		data["hint"] = e.Hint
	}
	if e.Cause != nil {
		data["cause"] = e.Cause.Error()
	}
	for k, v := range e.Data {
		data[k] = v
	}
	if len(data) == 0 {
		return nil
	}
	return data
}
