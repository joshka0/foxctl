package webterm

import (
	"context"

	"github.com/jkatigb/agentctl/internal/agentpane"
)

// PTYProcess manages a PTY connected to a tmux attach session.
type PTYProcess = agentpane.PTYProcess

// TmuxOptions configures the tmux PTY process.
type TmuxOptions = agentpane.SharedPTYOptions

// StartTmuxAttach starts a tmux attach (or new -A) session in a PTY.
func StartTmuxAttach(ctx context.Context, opts TmuxOptions) (*PTYProcess, error) {
	return agentpane.StartSharedTmuxPTY(ctx, opts)
}
