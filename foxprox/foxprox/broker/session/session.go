// Package session implements the broker-owned PTY session primitive for Foxprox.
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
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/oklog/ulid/v2"

	"github.com/joshka/foxprox/foxprox/broker/modetrack"
	"github.com/joshka/foxprox/foxprox/broker/termcaps"
	"github.com/joshka/foxprox/foxprox/broker/vtscreen"
)

var readinessRegexCache sync.Map

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
	// SubmitKey is an optional session-level default for TerminalSubmit when
	// the intent omits submit_key.
	SubmitKey string
	// EnableRawBytes allows trusted callers to use terminal.write_bytes.
	EnableRawBytes bool
	// Readiness configures broker-owned prompt readiness for this session.
	Readiness ReadinessProfile
}

// ReadinessProfile configures broker-owned prompt readiness. It is intentionally
// data-only so adapters, HTTP handlers, and future config files can share it.
type ReadinessProfile struct {
	ScreenRegex         string
	ThresholdBPS        float64
	Debounce            time.Duration
	RequireNotAltScreen bool
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

	OutputBytesTotal int64
	OutputRateBPS    float64
	LastOutputAt     time.Time
	ReadinessProfile ReadinessProfile
}

// OutputStats describes recent PTY output volume for a Session.
type OutputStats struct {
	BytesTotal   int64
	RateBPS      float64
	LastOutputAt time.Time
}

// PromptReadiness extends the byte-idle readiness heuristic with an optional
// rendered-screen prompt match from a ReadinessProfile.
type PromptReadiness struct {
	Readiness
	ScreenMatch bool
	ScreenRegex string
	ScreenLine  string
}

// Readiness is the cheap readiness heuristic used before the screen-renderer
// path exists: a session is idle when output has stayed below a byte-rate
// threshold for the full debounce window.
type Readiness struct {
	Idle         bool
	IdleFor      time.Duration
	OutputStats  OutputStats
	ThresholdBPS float64
	Debounce     time.Duration
}

// Session is one broker-owned PTY.
//
// The zero value is not usable; construct via New and then call Start.
type Session struct {
	id        string
	spec      Spec
	createdAt time.Time

	log       *OutputLog
	tracker   *modetrack.Tracker
	responder *termcaps.Responder
	screen    *vtscreen.Screen

	// mu guards all mutable fields below. The Run goroutine writes lifecycle
	// and output counters; readers use Snapshot() and may prune expired
	// output-rate samples while holding the same lock.
	mu                sync.RWMutex
	status            Status
	pid               int
	exitedAt          time.Time
	exitCode          int
	exitError         string
	outputBytesTotal  int64
	lastOutputAt      time.Time
	outputSamples     []outputSample
	outputSampleBytes int64

	cmd    *exec.Cmd
	ptyFd  *os.File
	cancel context.CancelFunc
	done   chan struct{}
	now    func() time.Time

	// Non-reentrant test hooks. The fields are nil in production use.
	// spawnOverride replaces the pty.Start call with an injected pipe/process pair.
	spawnOverride spawnFunc
}

type outputSample struct {
	at    time.Time
	bytes int
}

const outputRateWindow = time.Second

// spawnFunc is the hook used by tests to inject a PTY without a real child
// process. It must return an os.File pair (ptyMaster, optional slave) plus a
// cleanup func that is invoked when the session closes. The exec.Cmd may be
// nil if no process is being represented.
type spawnFunc func(ctx context.Context, spec Spec) (ptyMaster *os.File, cmd *exec.Cmd, cleanup func(), err error)

// Errors returned by session APIs.
var (
	ErrAlreadyStarted = errors.New("foxprox session: already started")
	ErrNotRunning     = errors.New("foxprox session: not running")
	ErrNoCommand      = errors.New("foxprox session: spec.Cmd is required")
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
		tracker:   modetrack.New(),
		responder: termcaps.New(),
		screen:    vtscreen.New(spec.Rows, spec.Cols),
		status:    StatusPending,
		done:      make(chan struct{}),
		now:       time.Now,
	}
	return s, nil
}

// ID returns the session's stable identifier.
func (s *Session) ID() string { return s.id }

// Log returns the session's output log for subscription and replay.
func (s *Session) Log() *OutputLog { return s.log }

// Tracker returns the session's terminal-mode tracker. Callers can query
// Snapshot() for bracketed-paste / alt-screen / cursor-keys state or subscribe
// to live mode transitions.
func (s *Session) Tracker() *modetrack.Tracker { return s.tracker }

// Screen returns the session's virtual terminal screen.
func (s *Session) Screen() *vtscreen.Screen { return s.screen }

// Done returns a channel that is closed after the session has exited and its
// Run goroutine has returned. Callers use this to synchronize lifecycle.
func (s *Session) Done() <-chan struct{} { return s.done }

// Snapshot returns an immutable view of the session's public state.
func (s *Session) Snapshot() Snapshot {
	now := s.nowUTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.outputStatsLocked(now)
	return Snapshot{
		ID:               s.id,
		Spec:             cloneSpec(s.spec),
		Status:           s.status,
		PID:              s.pid,
		CreatedAt:        s.createdAt,
		ExitedAt:         s.exitedAt,
		ExitCode:         s.exitCode,
		ExitError:        s.exitError,
		LastSeq:          s.log.NextSeq() - 1,
		OutputBytesTotal: stats.BytesTotal,
		OutputRateBPS:    stats.RateBPS,
		LastOutputAt:     stats.LastOutputAt,
		ReadinessProfile: cloneReadinessProfile(s.spec.Readiness),
	}
}

// OutputRate returns the current rolling one-second PTY output rate in bytes
// per second. It naturally decays to zero when no new output arrives.
func (s *Session) OutputRate() float64 {
	return s.OutputStats().RateBPS
}

// OutputStats returns cumulative and recent PTY output counters.
func (s *Session) OutputStats() OutputStats {
	return s.outputStatsAt(s.nowUTC())
}

// Readiness returns the current output-idle readiness heuristic for the
// supplied threshold and debounce window.
func (s *Session) Readiness(thresholdBPS float64, debounce time.Duration) Readiness {
	return s.ReadinessAt(thresholdBPS, debounce, s.nowUTC())
}

// ReadinessAt is exported for deterministic tests and callers that already
// sampled a clock.
func (s *Session) ReadinessAt(thresholdBPS float64, debounce time.Duration, now time.Time) Readiness {
	if thresholdBPS < 0 {
		thresholdBPS = 0
	}
	if debounce < 0 {
		debounce = 0
	}
	now = now.UTC()

	s.mu.Lock()
	stats := s.outputStatsLocked(now)
	createdAt := s.createdAt
	s.mu.Unlock()

	reference := stats.LastOutputAt
	if reference.IsZero() {
		reference = createdAt
	}
	idleFor := now.Sub(reference)
	if idleFor < 0 {
		idleFor = 0
	}
	idle := stats.RateBPS < thresholdBPS && idleFor >= debounce
	return Readiness{
		Idle:         idle,
		IdleFor:      idleFor,
		OutputStats:  stats,
		ThresholdBPS: thresholdBPS,
		Debounce:     debounce,
	}
}

// ProfileReadiness evaluates the session's configured ReadinessProfile.
func (s *Session) ProfileReadiness() PromptReadiness {
	return s.ReadinessForProfile(s.spec.Readiness, s.nowUTC())
}

// ReadinessForProfile evaluates byte-idle readiness plus optional rendered
// screen matching for profile.
func (s *Session) ReadinessForProfile(profile ReadinessProfile, now time.Time) PromptReadiness {
	threshold := profile.ThresholdBPS
	if threshold <= 0 {
		threshold = 32
	}
	debounce := profile.Debounce
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	base := s.ReadinessAt(threshold, debounce, now)
	out := PromptReadiness{
		Readiness:   base,
		ScreenRegex: profile.ScreenRegex,
	}
	if profile.ScreenRegex == "" {
		return out
	}
	snap := s.Screen().Snapshot()
	for _, line := range snap.Lines {
		if matchScreenLine(profile.ScreenRegex, line) {
			out.ScreenMatch = true
			out.ScreenLine = line
			break
		}
	}
	out.Idle = base.Idle && out.ScreenMatch
	return out
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
		return fmt.Errorf("foxprox session: spawn: %w", err)
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
	if err := pty.Setsize(fd, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		return err
	}
	s.screen.Resize(rows, cols)
	return nil
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
				s.recordOutputAt(n, s.nowUTC())
				s.tracker.Feed(buf[:n])
				s.screen.Feed(buf[:n])
				// Auto-answer terminal capability queries the
				// child emits during init (OSC 10/11/12 color
				// queries, DSR 5/6, DA1/DA2, kitty kbd ?u).
				// Without this, agents like codex block
				// indefinitely waiting for a reply that a real
				// xterm would auto-produce. Write responses back
				// to the PTY master — the child sees them on its
				// stdin. Write errors are intentionally
				// swallowed: a closed PTY will fail the next
				// Read below and exit this goroutine cleanly.
				if responses := s.responder.Feed(buf[:n]); len(responses) > 0 {
					for _, resp := range responses {
						_, _ = s.ptyFd.Write(resp)
					}
				}
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
//
// Termination policy: we deliberately do NOT pass ctx to exec.CommandContext,
// because that binds cancellation to os.Process.Kill() and would bypass the
// graceful SIGTERM path in Session.run. Instead, the Session goroutine owns
// termination and sends SIGTERM on ctx.Done, escalating to SIGKILL only after
// a grace window.
func defaultSpawn(ctx context.Context, spec Spec) (*os.File, *exec.Cmd, func(), error) {
	if len(spec.Cmd) == 0 {
		return nil, nil, nil, ErrNoCommand
	}
	_ = ctx // termination handled by Session.run, not CommandContext
	cmd := exec.Command(spec.Cmd[0], spec.Cmd[1:]...)
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
		Cmd:            append([]string(nil), s.Cmd...),
		Cwd:            s.Cwd,
		Rows:           s.Rows,
		Cols:           s.Cols,
		Adapter:        s.Adapter,
		SubmitKey:      s.SubmitKey,
		EnableRawBytes: s.EnableRawBytes,
		Readiness:      cloneReadinessProfile(s.Readiness),
	}
	if s.Env != nil {
		out.Env = append([]string(nil), s.Env...)
	}
	return out
}

func cloneReadinessProfile(p ReadinessProfile) ReadinessProfile {
	return p
}

func matchScreenLine(pattern, line string) bool {
	compiled, ok := readinessRegexCache.Load(pattern)
	var re *regexp.Regexp
	if ok {
		re = compiled.(*regexp.Regexp)
	} else {
		var err error
		re, err = regexp.Compile(pattern)
		if err != nil {
			return false
		}
		actual, _ := readinessRegexCache.LoadOrStore(pattern, re)
		re = actual.(*regexp.Regexp)
	}
	return re.MatchString(line)
}

func (s *Session) nowUTC() time.Time {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	return now().UTC()
}

func (s *Session) recordOutputAt(n int, at time.Time) {
	if n <= 0 {
		return
	}
	at = at.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputBytesTotal += int64(n)
	s.lastOutputAt = at
	s.outputSamples = append(s.outputSamples, outputSample{at: at, bytes: n})
	s.outputSampleBytes += int64(n)
	s.pruneOutputSamplesLocked(at)
}

func (s *Session) outputStatsAt(at time.Time) OutputStats {
	at = at.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outputStatsLocked(at)
}

func (s *Session) outputStatsLocked(at time.Time) OutputStats {
	s.pruneOutputSamplesLocked(at)
	return OutputStats{
		BytesTotal:   s.outputBytesTotal,
		RateBPS:      float64(s.outputSampleBytes) / outputRateWindow.Seconds(),
		LastOutputAt: s.lastOutputAt,
	}
}

func (s *Session) pruneOutputSamplesLocked(now time.Time) {
	cutoff := now.Add(-outputRateWindow)
	drop := 0
	for drop < len(s.outputSamples) && s.outputSamples[drop].at.Before(cutoff) {
		s.outputSampleBytes -= int64(s.outputSamples[drop].bytes)
		drop++
	}
	if drop == 0 {
		return
	}
	copy(s.outputSamples, s.outputSamples[drop:])
	clear(s.outputSamples[len(s.outputSamples)-drop:])
	s.outputSamples = s.outputSamples[:len(s.outputSamples)-drop]
	if s.outputSampleBytes < 0 {
		s.outputSampleBytes = 0
	}
}
