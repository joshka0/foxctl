package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNew_RequiresCmd(t *testing.T) {
	_, err := New(Spec{}, OutputLogOptions{})
	if !errors.Is(err, ErrNoCommand) {
		t.Fatalf("want ErrNoCommand, got %v", err)
	}
}

func TestSession_StartEchoCapturesOutput(t *testing.T) {
	s, err := New(Spec{Cmd: []string{"sh", "-c", "printf hello"}}, OutputLogOptions{MaxChunks: 32, MaxBytes: 1024})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-s.Done()

	snap := s.Snapshot()
	if snap.Status != StatusExited {
		t.Errorf("Status = %v, want StatusExited", snap.Status)
	}
	if snap.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (exitError=%q)", snap.ExitCode, snap.ExitError)
	}

	chunks := s.Log().Since(0, 0)
	var buf strings.Builder
	for _, c := range chunks {
		buf.Write(c.Bytes)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("output = %q, want to contain hello", buf.String())
	}
}

func TestSession_StartTwiceRejected(t *testing.T) {
	s, err := New(Spec{Cmd: []string{"sh", "-c", "printf ok"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	if err := s.Start(ctx); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("Start 2 want ErrAlreadyStarted, got %v", err)
	}
	<-s.Done()
}

func TestSession_CloseTerminatesLongRunningChild(t *testing.T) {
	s, err := New(Spec{Cmd: []string{"sleep", "30"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	start := time.Now()
	s.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit within 5s of Close")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("shutdown took %v; expected to be fast", elapsed)
	}

	snap := s.Snapshot()
	if snap.Status == StatusRunning {
		t.Errorf("Status = %v, want non-running after Close", snap.Status)
	}
	if snap.ExitedAt.IsZero() {
		t.Error("ExitedAt must be set after close")
	}
}

func TestSession_WriteFailsWhenNotRunning(t *testing.T) {
	s, err := New(Spec{Cmd: []string{"sh", "-c", "exit 0"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := s.Write([]byte("x")); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}

func TestSession_ResizeFailsWhenNotRunning(t *testing.T) {
	s, err := New(Spec{Cmd: []string{"sh", "-c", "exit 0"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Resize(20, 80); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("want ErrNotRunning, got %v", err)
	}
}

func TestSession_WriteForwardsToPTY(t *testing.T) {
	// cat with no args echoes stdin back on the PTY. We write a small message,
	// then close its input by sending EOF via Ctrl-D (0x04).
	s, err := New(Spec{Cmd: []string{"cat"}}, OutputLogOptions{MaxChunks: 64, MaxBytes: 4096})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := s.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Allow cat to echo back.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		chunks := s.Log().Since(0, 0)
		var buf strings.Builder
		for _, c := range chunks {
			buf.Write(c.Bytes)
		}
		if strings.Contains(buf.String(), "ping") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Send EOF so cat exits.
	_, _ = s.Write([]byte{0x04})
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		s.Close()
		<-s.Done()
		t.Fatal("cat did not exit after EOF")
	}
}

func TestSession_SpawnErrorYieldsStatusError(t *testing.T) {
	s, err := New(Spec{Cmd: []string{"/definitely/not/a/real/binary"}}, OutputLogOptions{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = s.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to fail for missing binary")
	}
	snap := s.Snapshot()
	if snap.Status != StatusError {
		t.Errorf("Status = %v, want StatusError", snap.Status)
	}
	// Done channel should already be closed so callers are not blocked.
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed after spawn failure")
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusPending: "pending",
		StatusRunning: "running",
		StatusExited:  "exited",
		StatusError:   "error",
		Status(999):   "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", s, got, want)
		}
	}
}
