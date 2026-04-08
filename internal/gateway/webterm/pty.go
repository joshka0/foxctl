package webterm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/creack/pty"
)

// PTYProcess manages a PTY connected to a tmux attach session.
type PTYProcess struct {
	cmd     *exec.Cmd
	ptmx    *os.File
	running atomic.Bool

	mu          sync.Mutex
	subscribers map[uint64]chan<- []byte
	nextSubID   uint64

	closeOnce sync.Once
	done      chan struct{}
}

// TmuxOptions configures the tmux PTY process.
type TmuxOptions struct {
	// Session is the tmux session name.
	Session string

	// Cols and Rows are the initial PTY dimensions.
	Cols uint16
	Rows uint16

	// TmuxPath is the path to tmux. Empty means "tmux".
	TmuxPath string

	// Shell is the fallback shell command when not using tmux.
	Shell string
}

// StartTmuxAttach starts a tmux attach (or new -A) session in a PTY.
// If no tmux session exists, it creates one with tmux new -A -s <name>.
func StartTmuxAttach(ctx context.Context, opts TmuxOptions) (*PTYProcess, error) {
	if opts.Cols <= 0 {
		opts.Cols = DefaultInitialCols
	}
	if opts.Rows <= 0 {
		opts.Rows = DefaultInitialRows
	}

	tmuxBin := opts.TmuxPath
	if tmuxBin == "" {
		tmuxBin = "tmux"
	}

	// Use new -A to create session if it doesn't exist, or attach if it does
	args := []string{"new", "-A", "-s", opts.Session}

	cmd := exec.CommandContext(ctx, tmuxBin, args...)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	// Start the command in a PTY
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: opts.Cols,
		Rows: opts.Rows,
	})
	if err != nil {
		return nil, fmt.Errorf("start tmux new -A -s %s: %w", opts.Session, err)
	}

	p := &PTYProcess{
		cmd:         cmd,
		ptmx:        ptmx,
		subscribers: make(map[uint64]chan<- []byte),
		done:        make(chan struct{}),
	}
	p.running.Store(true)

	// Start reading PTY output in background
	go p.readOutput()

	return p, nil
}

// readOutput reads from the PTY and broadcasts to all subscribers.
func (p *PTYProcess) readOutput() {
	defer close(p.done)
	defer p.running.Store(false)

	buf := make([]byte, 4096)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			p.broadcast(data)
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			// EIO is normal when the PTY master is closed
			if pathErr, ok := err.(*os.PathError); ok && pathErr.Err == syscall.EIO {
				return
			}
			return
		}
	}
}

// broadcast sends data to all output subscribers.
func (p *PTYProcess) broadcast(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ch := range p.subscribers {
		select {
		case ch <- data:
		default:
			// Buffer full — drop frame (prevents OOM with slow clients)
		}
	}
}

// SubscribeOutput registers a channel to receive PTY output.
// Returns a subscription ID for later unsubscription.
func (p *PTYProcess) SubscribeOutput(ch chan<- []byte) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := p.nextSubID
	p.nextSubID++
	p.subscribers[id] = ch
	return id
}

// UnsubscribeOutput removes an output subscription.
func (p *PTYProcess) UnsubscribeOutput(id uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.subscribers, id)
}

// WriteInput writes data to the PTY stdin (user keyboard input).
func (p *PTYProcess) WriteInput(data []byte) error {
	if !p.running.Load() {
		return fmt.Errorf("PTY not running")
	}
	_, err := p.ptmx.Write(data)
	return err
}

// Resize changes the PTY window size.
func (p *PTYProcess) Resize(cols, rows uint16) error {
	if !p.running.Load() {
		return fmt.Errorf("PTY not running")
	}
	return pty.Setsize(p.ptmx, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// IsRunning returns whether the PTY process is still running.
func (p *PTYProcess) IsRunning() bool {
	return p.running.Load()
}

// Close shuts down the PTY process. Safe to call multiple times.
func (p *PTYProcess) Close() {
	p.closeOnce.Do(func() {
		p.running.Store(false)

		if p.ptmx != nil {
			_ = p.ptmx.Close()
		}

		if p.cmd != nil && p.cmd.Process != nil {
			// Send SIGTERM to the tmux process
			_ = p.cmd.Process.Signal(syscall.SIGTERM)
			// Wait briefly for it to exit
			done := make(chan struct{})
			go func() {
				_ = p.cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-p.done:
			}
		}

		// Clear subscribers
		p.mu.Lock()
		p.subscribers = make(map[uint64]chan<- []byte)
		p.mu.Unlock()
	})
}

// Wait blocks until the PTY process exits.
func (p *PTYProcess) Wait() <-chan struct{} {
	return p.done
}
