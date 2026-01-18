// Package errors provides utilities for error handling patterns used across agentctl.
package errors

import (
	"fmt"
	"io"
	"log/slog"
)

// Closer is an interface for anything that can be closed.
type Closer interface {
	Close() error
}

// MustClose closes the resource and logs any error using the provided logger.
// This is intended for use in defer statements where returning the error is not possible.
//
// Example:
//
//	defer errors.MustClose(file, logger)
func MustClose(c Closer, logger *slog.Logger) {
	if c == nil {
		return
	}
	if err := c.Close(); err != nil {
		logger.Error("failed to close resource", "error", err)
	}
}

// CloseOnErr closes the resource only if errPtr points to a non-nil error.
// This is useful for cleanup in functions that return errors, where you want
// to clean up only if the function is returning an error.
//
// Example:
//
//	func process() (err error) {
//	    tx, err := db.Begin()
//	    if err != nil {
//	        return err
//	    }
//	    defer errors.CloseOnErr(tx, &err)
//	    // ... do work ...
//	    return nil
//	}
func CloseOnErr(c Closer, errPtr *error) {
	if c == nil || errPtr == nil || *errPtr == nil {
		return
	}
	if closeErr := c.Close(); closeErr != nil {
		*errPtr = fmt.Errorf("%w (close error: %v)", *errPtr, closeErr)
	}
}

// Ignore explicitly acknowledges that an error is being ignored and documents
// the reason. This function does nothing but serves as documentation.
//
// Example:
//
//	errors.Ignore(os.Remove(tempFile), "temp file cleanup failure is not critical")
func Ignore(err error, reason string) {
	// This function intentionally does nothing.
	// It exists solely for documentation purposes.
	_ = err
	_ = reason
}

// LogOnErr logs the error if it's non-nil. Returns the error unchanged.
// This is useful for logging errors in places where you can't handle them
// but want visibility.
//
// Example:
//
//	defer errors.LogOnErr(file.Close(), logger, "failed to close file")
func LogOnErr(err error, logger *slog.Logger, msg string) error {
	if err != nil && logger != nil {
		logger.Error(msg, "error", err)
	}
	return err
}

// MultiClose closes multiple closers and returns the first error encountered.
// All closers are attempted regardless of errors.
//
// Example:
//
//	return errors.MultiClose(file1, file2, conn)
func MultiClose(closers ...io.Closer) error {
	var firstErr error
	for _, c := range closers {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
