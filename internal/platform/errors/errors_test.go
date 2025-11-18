package errors

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
)

type mockCloser struct {
	closeErr error
	closed   bool
}

func (m *mockCloser) Close() error {
	m.closed = true
	return m.closeErr
}

func TestMustClose(t *testing.T) {
	t.Run("nil closer", func(_ *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		MustClose(nil, logger) // Should not panic
	})

	t.Run("successful close", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		mc := &mockCloser{}
		MustClose(mc, logger)
		if !mc.closed {
			t.Error("expected closer to be closed")
		}
	})

	t.Run("error on close", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		mc := &mockCloser{closeErr: errors.New("close error")}
		MustClose(mc, logger) // Should log error but not panic
		if !mc.closed {
			t.Error("expected closer to be closed despite error")
		}
	})
}

func TestCloseOnErr(t *testing.T) {
	t.Run("nil closer", func(_ *testing.T) {
		var err error
		CloseOnErr(nil, &err) // Should not panic
	})

	t.Run("nil error pointer", func(t *testing.T) {
		mc := &mockCloser{}
		CloseOnErr(mc, nil) // Should not panic
		if mc.closed {
			t.Error("expected closer not to be closed when errPtr is nil")
		}
	})

	t.Run("no error - closer not called", func(t *testing.T) {
		mc := &mockCloser{}
		var err error
		CloseOnErr(mc, &err)
		if mc.closed {
			t.Error("expected closer not to be closed when error is nil")
		}
	})

	t.Run("error present - closer called", func(t *testing.T) {
		mc := &mockCloser{}
		err := errors.New("original error")
		CloseOnErr(mc, &err)
		if !mc.closed {
			t.Error("expected closer to be closed when error is present")
		}
	})

	t.Run("error present and close fails", func(t *testing.T) {
		mc := &mockCloser{closeErr: errors.New("close error")}
		err := errors.New("original error")
		CloseOnErr(mc, &err)
		if !mc.closed {
			t.Error("expected closer to be closed")
		}
		if err.Error() != "original error (close error: close error)" {
			t.Errorf("expected combined error, got: %v", err)
		}
	})
}

func TestMust(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		result := Must(42, nil)
		if result != 42 {
			t.Errorf("expected 42, got %d", result)
		}
	})

	t.Run("with error", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic")
			}
		}()
		Must(42, errors.New("test error"))
	})
}

func TestIgnore(_ *testing.T) {
	// This function does nothing, just ensure it compiles and doesn't panic
	Ignore(errors.New("test error"), "testing ignore")
	Ignore(nil, "testing ignore with nil")
}

func TestLogOnErr(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("nil error", func(t *testing.T) {
		err := LogOnErr(nil, logger, "test")
		if err != nil {
			t.Error("expected nil error")
		}
	})

	t.Run("with error", func(t *testing.T) {
		testErr := errors.New("test error")
		err := LogOnErr(testErr, logger, "test message")
		if err != testErr {
			t.Error("expected same error to be returned")
		}
	})
}

func TestMultiClose(t *testing.T) {
	t.Run("all successful", func(t *testing.T) {
		mc1 := &mockCloser{}
		mc2 := &mockCloser{}
		err := MultiClose(mc1, mc2)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if !mc1.closed || !mc2.closed {
			t.Error("expected all closers to be closed")
		}
	})

	t.Run("one fails", func(t *testing.T) {
		mc1 := &mockCloser{closeErr: errors.New("error 1")}
		mc2 := &mockCloser{}
		err := MultiClose(mc1, mc2)
		if err == nil {
			t.Error("expected error")
		}
		if !mc1.closed || !mc2.closed {
			t.Error("expected all closers to be attempted")
		}
	})

	t.Run("with nil closers", func(t *testing.T) {
		mc1 := &mockCloser{}
		err := MultiClose(mc1, nil, nil)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if !mc1.closed {
			t.Error("expected non-nil closer to be closed")
		}
	})
}
