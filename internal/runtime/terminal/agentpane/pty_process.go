package agentpane

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
)

// PTYProcess manages a PTY connected to a tmux attach session.
type PTYProcess struct {
	cmd     *os.Process
	wait    func() error
	ptmx    *os.File
	running atomic.Bool

	mu          sync.Mutex
	subscribers map[uint64]chan<- []byte
	nextSubID   uint64

	closeOnce sync.Once
	done      chan struct{}
}

// SharedPTYOptions configures the shared tmux PTY process.
type SharedPTYOptions struct {
	Session  string
	Cols     uint16
	Rows     uint16
	TmuxPath string
	Shell    string
}

// StartSharedTmuxPTY starts a tmux attach (or new -A) session in a PTY.
func StartSharedTmuxPTY(ctx context.Context, opts SharedPTYOptions) (*PTYProcess, error) {
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}

	proc, err := StartTmuxAttach(ctx, TmuxAttachOptions{
		Session:    opts.Session,
		Cols:       opts.Cols,
		Rows:       opts.Rows,
		TmuxPath:   opts.TmuxPath,
		Env:        []string{"TERM=xterm-256color"},
		UseContext: true,
	})
	if err != nil {
		return nil, err
	}

	p := &PTYProcess{
		cmd:         proc.Cmd.Process,
		wait:        proc.Wait,
		ptmx:        proc.PTMX,
		subscribers: make(map[uint64]chan<- []byte),
		done:        make(chan struct{}),
	}
	p.running.Store(true)

	go p.readOutput()

	return p, nil
}

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
			if pathErr, ok := err.(*os.PathError); ok && pathErr.Err == syscall.EIO {
				return
			}
			return
		}
	}
}

func (p *PTYProcess) broadcast(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ch := range p.subscribers {
		select {
		case ch <- data:
		default:
		}
	}
}

// SubscribeOutput registers a channel to receive PTY output.
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

// WriteInput writes data to the PTY stdin.
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
	return SetPTYSize(p.ptmx, cols, rows)
}

// IsRunning reports whether the PTY process is still running.
func (p *PTYProcess) IsRunning() bool {
	return p.running.Load()
}

// Close shuts down the PTY process.
func (p *PTYProcess) Close() {
	p.closeOnce.Do(func() {
		p.running.Store(false)

		if p.ptmx != nil {
			_ = p.ptmx.Close()
		}

		if p.cmd != nil {
			_ = p.cmd.Signal(syscall.SIGTERM)
			done := make(chan struct{})
			go func() {
				if p.wait != nil {
					_ = p.wait()
				}
				close(done)
			}()
			select {
			case <-done:
			case <-p.done:
			}
		}

		p.mu.Lock()
		p.subscribers = make(map[uint64]chan<- []byte)
		p.mu.Unlock()
	})
}

// Wait blocks until the PTY process exits.
func (p *PTYProcess) Wait() <-chan struct{} {
	return p.done
}
