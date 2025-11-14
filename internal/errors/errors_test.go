package errors_test

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/jkatigb/agentctl/internal/errors"
)

type failCloser struct {
	err error
}

func (f *failCloser) Close() error {
	return f.err
}

type successCloser struct {
	closed bool
}

func (s *successCloser) Close() error {
	s.closed = true
	return nil
}

func TestMustClose_Success(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	closer := &successCloser{}
	errors.MustClose(closer, logger)

	if !closer.closed {
		t.Error("expected closer to be closed")
	}

	if logBuf.Len() > 0 {
		t.Errorf("expected no log output, got: %s", logBuf.String())
	}
}

func TestMustClose_Failure(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	closer := &failCloser{err: fmt.Errorf("close failed")}
	errors.MustClose(closer, logger)

	if !bytes.Contains(logBuf.Bytes(), []byte("close failed")) {
		t.Errorf("expected log to contain 'close failed', got: %s", logBuf.String())
	}
}

func TestMustClose_NilCloser(t *testing.T) {
	t.Helper()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// Should not panic
	errors.MustClose(nil, logger)
}

func TestMustClose_NilLogger(t *testing.T) {
	t.Helper()
	closer := &failCloser{err: fmt.Errorf("close failed")}

	// Should not panic even with nil logger
	errors.MustClose(closer, nil)
}

func TestCloseOnErr_ErrorPath(t *testing.T) {
	closer := &successCloser{}
	err := fmt.Errorf("operation failed")

	errors.CloseOnErr(&err, closer)

	if !closer.closed {
		t.Error("expected closer to be closed on error path")
	}
}

func TestCloseOnErr_SuccessPath(t *testing.T) {
	closer := &successCloser{}
	var err error // nil

	errors.CloseOnErr(&err, closer)

	if closer.closed {
		t.Error("expected closer NOT to be closed on success path")
	}
}

func TestCloseOnErr_NilPointer(t *testing.T) {
	closer := &successCloser{}
	// Should not panic even if err pointer is nil
	errors.CloseOnErr(nil, closer)
	if closer.closed {
		t.Error("expected closer to remain open when err pointer is nil")
	}
}

func TestMust_NoError(t *testing.T) {
	t.Helper()
	// Should not panic
	errors.Must(nil)
}

func TestMust_WithError(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected Must to panic with error")
		}
	}()

	errors.Must(fmt.Errorf("test error"))
}

func TestIgnore(t *testing.T) {
	t.Helper()
	// Should not panic or do anything
	ignoreFunc := errors.Ignore()
	ignoreFunc(fmt.Errorf("this error is ignored"))
	ignoreFunc(nil)
}

// Example of proper usage in a function
func exampleFunction() (err error) {
	f := &successCloser{}
	defer errors.CloseOnErr(&err, f)

	// Simulate work that might fail
	return fmt.Errorf("simulated error")
}

func TestExampleFunction(t *testing.T) {
	err := exampleFunction()
	if err == nil {
		t.Error("expected error from example function")
	}
}

// Benchmark MustClose
func BenchmarkMustClose(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	closer := &successCloser{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		closer.closed = false
		errors.MustClose(closer, logger)
	}
}
