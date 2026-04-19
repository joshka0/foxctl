// Package session implements the broker-owned PTY session primitive for ATCP.
//
// A Session owns exactly one child process running under a pseudoterminal.
// The session goroutine is the single writer for session state (ID,
// ExitCode, ExitedAt) and the single reader of the PTY output stream;
// readers consume Snapshots and subscribe to the output log.
//
// Design rules (from AGENTS.md Go-native runtime):
//
//   - Run(ctx) component pattern.
//   - Single-writer state ownership via the session goroutine.
//   - Immutable Snapshot returned to observers.
//   - Output log is append-only; viewers subscribe to live chunks.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/oklog/ulid/v2"
)

// Spec describes how to spawn a Session's child process.
type Spec struct {
	// Cmd is the program + arguments. Cmd[0] is the executable; remaining
	// elements are its args. Required.
	Cmd []string
	// Cwd is the working directory. Empty means inherit.
	Cwd string
	// Env is the explicit environment. Nil means inherit os.Environ().
	Env []string
	// Rows / Cols are the initial PTY dimensions. Zero means use a default.
	Rows uint16
	Cols uint16
	// Adapter is an optional hint for downstream intent compilation
	// (e.g. "generic-tty", "posix-shell", "claude"). Not interpreted here;
	// stored on the Session for observability.
	Adapter string
}

// Status reports session lifecycle state.
type Status int

const (
	// StatusPending is the brief interval between construction and Start.
	StatusPending Status = iota + 1
	// StatusRunning means the child process is alive.
	StatusRunning
	// StatusExited means the child has terminated and ExitedAt is set.
	StatusExited
	// StatusError means the session failed to spawn or was terminated abnormally.
	StatusError
)

// String returns a stable, lowercase status name useful for JSON serialization.
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusExited:
		return "exited"
	case StatusError:
		return "error"
	}
	return "unknown"
}

// Snapshot is an immutable view of a Session's public state at a moment in time.
// It is safe to hand to any reader (tests, HTTP handlers, event emitters).
type Snapshot struct {
	ID        string
	Spec      Spec
	Status    Status
	PID       int
	CreatedAt time.Time
	ExitedAt  time.Time
	ExitCode  int
	ExitError string
	LastSeq   uint64
}

// Session is one broker-owned PTY.
//
// The zero value is not usable; construct via New and then call Start.
type Session struct {
	id        string
	spec      Spec
	createdAt time.Time

	log *OutputLog

	// mu guards all mutable fields below. Only the Run goroutine writes;
	// readers use Snapshot() which takes the same lock and returns copies.
	mu        sync.RWMutex
	status    Status
	pid       int
	exitedAt  time.Time
	exitCode  int
	exitError string

	cmd    *exec.Cmd
	ptyFd  *os.File
	cancel context.CancelFunc
	done   chan struct{}

	// Non-reentrant test hooks. The fields are nil in production use.
	// spawnOverride replaces the pty.Start call with an injected pipe/process pair.
	spawnOverride spawnFunc
}

// spawnFunc is the hook used by tests to inject a PTY without a real child
// process. It must return an os.File pair (ptyMaster, optional slave) plus a
// cleanup func that is invoked when the session closes. The exec.Cmd may be
// nil if no process is being represented.
type spawnFunc func(ctx context.Context, spec Spec) (ptyMaster *os.File, cmd *exec.Cmd, cleanup func(), err error)

// Errors returned by session APIs.
var (
	ErrAlreadyStarted = errors.New("atcp session: already started")
	ErrNotRunning     = errors.New("atcp session: not running")
	ErrNoCommand      = errors.New("atcp session: spec.Cmd is required")
)

// New constructs a Session with a fresh ULID id. The session is in
// StatusPending until Start returns successfully.
func New(spec Spec, logOpts OutputLogOptions) (*Session, error) {
	if len(spec.Cmd) == 0 {
		return nil, ErrNoCommand
	}
	s := &Session{
		id:        ulid.Make().String(),
		spec:      spec,
		createdAt: time.Now().UTC(),
		log:       NewOutputLog(logOpts),
		status:    StatusPending,
		done:      make(chan struct{}),
	}
	return s, nil
}

// ID returns the session's stable identifier.
func (s *Session) ID() string { return s.id }

// Log returns the session's output log for subscription and replay.
func (s *Session) Log() *OutputLog { return s.log }

// Done returns a channel that is closed after the session has exited and its
// Run goroutine has returned. Callers use this to synchronize lifecycle.
func (s *Session) Done() <-chan struct{} { return s.done }

// Snapshot returns an immutable view of the session's public state.
func (s *Session) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Snapshot{
		ID:        s.id,
		Spec:      cloneSpec(s.spec),
		Status:    s.status,
		PID:       s.pid,
		CreatedAt: s.createdAt,
		ExitedAt:  s.exitedAt,
		ExitCode:  s.exitCode,
		ExitError: s.exitError,
		LastSeq:   s.log.NextSeq() - 1,
	}
}

// Start spawns the child process and begins the Run goroutine.
//
// The returned context ties the session lifetime to parent. Cancelling parent
// (or calling Close) sends SIGTERM to the child.
func (s *Session) Start(parent context.Context) error {
	s.mu.Lock()
	if s.status != StatusPending {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel

	var (
		ptyMaster *os.File
		cmd       *exec.Cmd
		cleanup   func()
		err       error
	)
	if s.spawnOverride != nil {
		ptyMaster, cmd, cleanup, err = s.spawnOverride(ctx, s.spec)
	} else {
		ptyMaster, cmd, cleanup, err = defaultSpawn(ctx, s.spec)
	}
	if err != nil {
		s.status = StatusError
		s.exitError = err.Error()
		cancel()
		s.mu.Unlock()
		close(s.done)
		return fmt.Errorf("atcp session: spawn: %w", err)
	}

	s.cmd = cmd
	s.ptyFd = ptyMaster
	if cmd != nil && cmd.Process != nil {
		s.pid = cmd.Process.Pid
	}
	s.status = StatusRunning
	s.mu.Unlock()

	go s.run(ctx, cleanup)
	return nil
}

// Close signals the session to terminate. Idempotent.
func (s *Session) Close() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Write forwards data to the PTY master. Returns ErrNotRunning once the session
// has exited.
func (s *Session) Write(data []byte) (int, error) {
	s.mu.RLock()
	if s.status != StatusRunning || s.ptyFd == nil {
		s.mu.RUnlock()
		return 0, ErrNotRunning
	}
	fd := s.ptyFd
	s.mu.RUnlock()
	return fd.Write(data)
}

// Resize adjusts the PTY window dimensions. Silently tolerant when the session
// is not running.
func (s *Session) Resize(rows, cols uint16) error {
	s.mu.RLock()
	fd := s.ptyFd
	running := s.status == StatusRunning
	s.mu.RUnlock()
	if !running || fd == nil {
		return ErrNotRunning
	}
	return pty.Setsize(fd, &pty.Winsize{Rows: rows, Cols: cols})
}

func (s *Session) run(ctx context.Context, cleanup func()) {
	defer close(s.done)
	defer func() {
		if cleanup != nil {
			cleanup()
		}
		s.log.Close()
	}()

	// Reader goroutine copies PTY output into the log. A second goroutine below
	// waits on the child process exit so we can terminate reader cleanly when
	// the child dies (io.Copy on a closed master returns io.EOF / *os.PathError).
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 4096)
		for {
			n, err := s.ptyFd.Read(buf)
			if n > 0 {
				if _, appendErr := s.log.Append(buf[:n]); appendErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	var exitErr error
	waitDone := make(chan error, 1)
	if s.cmd != nil {
		go func() { waitDone <- s.cmd.Wait() }()
	} else {
		// No process (test mode). Wait for context cancellation or reader close.
		waitDone <- nil
	}

	select {
	case exitErr = <-waitDone:
	case <-ctx.Done():
		// Cancellation: try a graceful SIGTERM, give the child a moment, then
		// force-kill. We still wait on cmd.Wait to reap.
		s.terminate()
		if s.cmd != nil {
			select {
			case exitErr = <-waitDone:
			case <-time.After(2 * time.Second):
				_ = s.kill()
				exitErr = <-waitDone
			}
		}
	}

	// Close master so the reader goroutine unblocks.
	if s.ptyFd != nil {
		_ = s.ptyFd.Close()
	}
	<-readerDone

	s.mu.Lock()
	s.exitedAt = time.Now().UTC()
	if exitErr != nil {
		s.exitError = exitErr.Error()
		if exitError, ok := exitErr.(*exec.ExitError); ok {
			s.exitCode = exitError.ExitCode()
			s.status = StatusExited
		} else {
			s.exitCode = -1
			s.status = StatusError
		}
	} else {
		s.exitCode = 0
		s.status = StatusExited
	}
	s.mu.Unlock()
}

func (s *Session) terminate() {
	s.mu.RLock()
	cmd := s.cmd
	s.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
}

func (s *Session) kill() error {
	s.mu.RLock()
	cmd := s.cmd
	s.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// defaultSpawn starts spec.Cmd under a PTY using creack/pty. Callers who need
// to inject a test double should set spawnOverride instead of replacing this.
func defaultSpawn(ctx context.Context, spec Spec) (*os.File, *exec.Cmd, func(), error) {
	if len(spec.Cmd) == 0 {
		return nil, nil, nil, ErrNoCommand
	}
	cmd := exec.CommandContext(ctx, spec.Cmd[0], spec.Cmd[1:]...)
	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}
	if spec.Env != nil {
		cmd.Env = append([]string(nil), spec.Env...)
	}
	winsize := &pty.Winsize{}
	if spec.Rows > 0 {
		winsize.Rows = spec.Rows
	} else {
		winsize.Rows = 40
	}
	if spec.Cols > 0 {
		winsize.Cols = spec.Cols
	} else {
		winsize.Cols = 120
	}
	master, err := pty.StartWithSize(cmd, winsize)
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() {
		_ = master.Close()
	}
	return master, cmd, cleanup, nil
}

func cloneSpec(s Spec) Spec {
	out := Spec{
		Cmd:     append([]string(nil), s.Cmd...),
		Cwd:     s.Cwd,
		Rows:    s.Rows,
		Cols:    s.Cols,
		Adapter: s.Adapter,
	}
	if s.Env != nil {
		out.Env = append([]string(nil), s.Env...)
	}
	return out
}
