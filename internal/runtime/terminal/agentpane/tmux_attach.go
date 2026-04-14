package agentpane

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/creack/pty"
)

// TmuxAttachOptions configures a tmux new -A attach session behind a PTY.
type TmuxAttachOptions struct {
	Session    string
	Cols       uint16
	Rows       uint16
	TmuxPath   string
	Env        []string
	UseContext bool
}

// TmuxAttachProcess is a running tmux attach session bound to a PTY.
type TmuxAttachProcess struct {
	Cmd  *exec.Cmd
	PTMX *os.File
}

// StartTmuxAttach starts `tmux new -A -s <session>` behind a PTY.
func StartTmuxAttach(ctx context.Context, opts TmuxAttachOptions) (*TmuxAttachProcess, error) {
	session := strings.TrimSpace(opts.Session)
	if session == "" {
		return nil, fmt.Errorf("tmux session is required")
	}

	tmuxBin := strings.TrimSpace(opts.TmuxPath)
	if tmuxBin == "" {
		tmuxBin = "tmux"
	}

	var cmd *exec.Cmd
	if opts.UseContext {
		cmd = exec.CommandContext(ctx, tmuxBin, "new", "-A", "-s", session)
	} else {
		cmd = exec.Command(tmuxBin, "new", "-A", "-s", session)
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(os.Environ(), opts.Env...)
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: opts.Cols,
		Rows: opts.Rows,
	})
	if err != nil {
		return nil, fmt.Errorf("start tmux new -A -s %s: %w", session, err)
	}

	return &TmuxAttachProcess{
		Cmd:  cmd,
		PTMX: ptmx,
	}, nil
}

// WriteInput writes bytes to the tmux PTY.
func (p *TmuxAttachProcess) WriteInput(data []byte) error {
	if p == nil || p.PTMX == nil {
		return fmt.Errorf("tmux PTY is not running")
	}
	_, err := p.PTMX.Write(data)
	return err
}

// Resize changes the tmux PTY window size.
func (p *TmuxAttachProcess) Resize(cols, rows uint16) error {
	if p == nil || p.PTMX == nil {
		return fmt.Errorf("tmux PTY is not running")
	}
	return SetPTYSize(p.PTMX, cols, rows)
}

// Signal forwards a signal to the tmux client process.
func (p *TmuxAttachProcess) Signal(sig syscall.Signal) error {
	if p == nil || p.Cmd == nil || p.Cmd.Process == nil {
		return fmt.Errorf("tmux process is not running")
	}
	return p.Cmd.Process.Signal(sig)
}

// Close closes the PTY master, causing the tmux client to detach.
func (p *TmuxAttachProcess) Close() error {
	if p == nil || p.PTMX == nil {
		return nil
	}
	return p.PTMX.Close()
}

// Wait waits for the tmux client process to exit.
func (p *TmuxAttachProcess) Wait() error {
	if p == nil || p.Cmd == nil {
		return nil
	}
	return p.Cmd.Wait()
}

// CopyOutput copies tmux PTY output to the destination writer.
func (p *TmuxAttachProcess) CopyOutput(dst io.Writer) (int64, error) {
	if p == nil || p.PTMX == nil {
		return 0, fmt.Errorf("tmux PTY is not running")
	}
	return io.Copy(dst, p.PTMX)
}

// CopyInput copies bytes from the source reader into the tmux PTY.
func (p *TmuxAttachProcess) CopyInput(src io.Reader) (int64, error) {
	if p == nil || p.PTMX == nil {
		return 0, fmt.Errorf("tmux PTY is not running")
	}
	return io.Copy(p.PTMX, src)
}

// SetPTYSize applies a terminal size to a PTY file.
func SetPTYSize(ptmx *os.File, cols, rows uint16) error {
	return pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}
