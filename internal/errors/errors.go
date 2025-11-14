// Package errors provides error handling utilities for agentctl.
package errors

import (
	"io"
	"log/slog"
)

// MustClose logs an error if closing fails, but doesn't panic.
// Use in defer statements where cleanup failure is non-fatal.
//
// Example:
//
//	defer errors.MustClose(store, logger)
func MustClose(closer io.Closer, logger *slog.Logger) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		if logger != nil {
			logger.Warn("close failed", "error", err)
		}
	}
}

// CloseOnErr closes a resource only if err is non-nil.
// Useful for cleanup in error paths.
//
// Example:
//
//	func process() (err error) {
//	    f, err := os.Open("file")
//	    if err != nil {
//	        return err
//	    }
//	    defer errors.CloseOnErr(&err, f)
//	    // ... work that might fail ...
//	}
func CloseOnErr(err *error, closer io.Closer) {
	if err == nil || closer == nil {
		return
	}
	if *err != nil {
		_ = closer.Close() // Already failing, ignore cleanup errors
	}
}

// Must panics if err is non-nil.
// Only use in init() or main() for must-succeed operations.
//
// Example:
//
//	func init() {
//	    errors.Must(validateConfig())
//	}
func Must(err error) {
	if err != nil {
		panic(err)
	}
}

// Ignore returns a function that ignores an error.
// Forces explicit acknowledgment of ignored errors.
//
// Example:
//
//	defer errors.Ignore()(file.Close())
func Ignore() func(error) {
	return func(error) {}
}
