package zellijbridge

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner executes subprocesses for zellij bridge operations.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error)
}

// OSRunner executes commands using the local OS process runner.
type OSRunner struct{}

// Run executes a command and returns stdout, stderr, and error.
func (OSRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", strings.TrimSpace(string(output)), err
	}
	return strings.TrimSpace(string(output)), "", nil
}

// CreatePaneOptions describes a named zellij pane launch.
type CreatePaneOptions struct {
	Session           string
	CWD               string
	Name              string
	Command           string
	ParticipantID     string
	ParentParticipant string
	ParentAgentID     string
	RoomID            string
	RoomRole          string
	RoomAccess        string
}

// CreatePaneResult describes one named zellij pane launch.
type CreatePaneResult struct {
	Session       string `json:"session"`
	PaneName      string `json:"pane_name"`
	ParticipantID string `json:"participant_id"`
}

// Submit modes for mux submit (see [Client.Submit]).
const (
	SubmitModeEscapeEnter = "escape_enter"
	SubmitModeEnterOnly   = "enter_only"
)

// SubmitOptions configures [Client.Submit].
type SubmitOptions struct {
	// Mode defaults to [SubmitModeEscapeEnter]. Use [SubmitModeEnterOnly] when only
	// Enter should be sent to the terminal (Escape can clear TUIs).
	Mode string
	// PaneID optionally targets a pane (e.g. terminal_2 or 2). Requires a zellij
	// build where `zellij action write` accepts --pane-id; when empty, writes go
	// to the focused pane for the session.
	PaneID string
}

// SubmitResult describes a submit action for a zellij session/pane.
type SubmitResult struct {
	Session string `json:"session"`
	Mode    string `json:"mode"`
	PaneID  string `json:"pane_id,omitempty"`
}

// InterruptResult describes an interrupt action for a zellij session/pane.
type InterruptResult struct {
	Session string `json:"session"`
	PaneID  string `json:"pane_id,omitempty"`
}

// TTYRegistryFile returns the zellij pane tty registry file for one session/participant pair.
func TTYRegistryFile(session, participantID string) string {
	session = strings.TrimSpace(session)
	participantID = strings.TrimSpace(participantID)
	return filepath.Join(os.TempDir(), "foxctl-zellij-tty", sanitizeTTYRegistryComponent(session), sanitizeTTYRegistryComponent(participantID)+".tty")
}

// Client exposes minimal zellij pane creation for agent tenancy.
type Client struct {
	runner Runner
}

// New returns a client using the local OS runner.
func New() *Client {
	return &Client{runner: OSRunner{}}
}

// NewWithRunner returns a client with an injected runner.
func NewWithRunner(runner Runner) *Client {
	if runner == nil {
		runner = OSRunner{}
	}
	return &Client{runner: runner}
}

// CreatePane launches a named pane in the target zellij session.
func (c *Client) CreatePane(ctx context.Context, opts CreatePaneOptions) (CreatePaneResult, error) {
	session := strings.TrimSpace(opts.Session)
	if session == "" {
		return CreatePaneResult{}, fmt.Errorf("session is required")
	}
	if err := c.ensureSession(ctx, session); err != nil {
		return CreatePaneResult{}, err
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return CreatePaneResult{}, fmt.Errorf("pane name is required")
	}
	command := strings.TrimSpace(opts.Command)
	if command == "" {
		return CreatePaneResult{}, fmt.Errorf("command is required")
	}
	participantID := strings.TrimSpace(opts.ParticipantID)
	if participantID == "" {
		participantID = name
	}
	args := []string{"--session", session, "action", "new-pane"}
	if cwd := strings.TrimSpace(opts.CWD); cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = append(args, "--name", name, "--")
	args = append(args, buildEnvCommand(session, name, command, participantID, opts.ParentParticipant, opts.ParentAgentID, opts.RoomID, opts.RoomRole, opts.RoomAccess)...)
	if _, stderr, err := c.runner.Run(ctx, "zellij", args...); err != nil {
		if strings.TrimSpace(stderr) == "" {
			return CreatePaneResult{}, err
		}
		return CreatePaneResult{}, fmt.Errorf("zellij new-pane: %s", strings.TrimSpace(stderr))
	}
	return CreatePaneResult{
		Session:       session,
		PaneName:      name,
		ParticipantID: participantID,
	}, nil
}

// Submit injects a submit sequence for a session. By default it sends Escape (27)
// then Enter (13); use [SubmitOptions] with [SubmitModeEnterOnly] for Enter-only.
func (c *Client) Submit(ctx context.Context, session string, opts SubmitOptions) (SubmitResult, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return SubmitResult{}, fmt.Errorf("session is required")
	}
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = SubmitModeEscapeEnter
	}
	switch mode {
	case SubmitModeEscapeEnter, SubmitModeEnterOnly:
	default:
		return SubmitResult{}, fmt.Errorf("unsupported submit mode %q", mode)
	}
	paneID := strings.TrimSpace(opts.PaneID)
	if mode != SubmitModeEnterOnly {
		if err := c.sessionWriteByte(ctx, session, paneID, "27"); err != nil {
			return SubmitResult{}, err
		}
	}
	if err := c.sessionWriteByte(ctx, session, paneID, "13"); err != nil {
		return SubmitResult{}, err
	}
	return SubmitResult{Session: session, Mode: mode, PaneID: paneID}, nil
}

// Interrupt injects a single Escape byte into the target zellij pane/session.
func (c *Client) Interrupt(ctx context.Context, session, paneID string) (InterruptResult, error) {
	session = strings.TrimSpace(session)
	if session == "" {
		return InterruptResult{}, fmt.Errorf("session is required")
	}
	paneID = strings.TrimSpace(paneID)
	if err := c.sessionWriteByte(ctx, session, paneID, "27"); err != nil {
		return InterruptResult{}, err
	}
	return InterruptResult{Session: session, PaneID: paneID}, nil
}

func (c *Client) sessionWriteByte(ctx context.Context, session, paneID, byteToken string) error {
	args := []string{"--session", session, "action", "write"}
	if pid := strings.TrimSpace(paneID); pid != "" {
		args = append(args, "--pane-id", pid)
	}
	args = append(args, byteToken)
	_, stderr, err := c.runner.Run(ctx, "zellij", args...)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			return err
		}
		if strings.Contains(msg, "pane-id") && strings.Contains(msg, "wasn't expected") {
			return fmt.Errorf("%s (this zellij build may not support --pane-id on action write; upgrade zellij or omit --pane-id)", msg)
		}
		return fmt.Errorf("zellij action write: %s", msg)
	}
	return nil
}

func (c *Client) ensureSession(ctx context.Context, session string) error {
	session = strings.TrimSpace(session)
	if session == "" {
		return fmt.Errorf("session is required")
	}
	if current := strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")); current != "" && current == session {
		return nil
	}
	if _, stderr, err := c.runner.Run(ctx, "zellij", "attach", "--create-background", session); err != nil {
		msg := strings.TrimSpace(stderr)
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "session already exists") {
			return nil
		}
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("zellij ensure session: %s", msg)
	}
	return nil
}

func buildEnvCommand(session, paneName, command, participantID, parentParticipant, parentAgentID, roomID, roomRole, roomAccess string) []string {
	ttyFile := TTYRegistryFile(session, participantID)
	args := []string{
		"env",
		"FOXCTL_PARTICIPANT_ID=" + strings.TrimSpace(participantID),
		"FOXCTL_ZELLIJ_PARTICIPANT=" + strings.TrimSpace(participantID),
		"FOXCTL_MUX_BACKEND=zellij",
		"FOXCTL_MUX_SESSION=" + strings.TrimSpace(session),
		"FOXCTL_MUX_PANE_ID=" + strings.TrimSpace(paneName),
		"FOXCTL_MUX_TTY_FILE=" + ttyFile,
	}
	if value := strings.TrimSpace(parentParticipant); value != "" {
		args = append(args, "FOXCTL_PARENT_PARTICIPANT_ID="+value)
	}
	if value := strings.TrimSpace(parentAgentID); value != "" {
		args = append(args, "FOXCTL_PARENT_AGENT_ID="+value)
	}
	if strings.TrimSpace(roomAccess) == "direct" {
		if value := strings.TrimSpace(roomID); value != "" {
			args = append(args, "FOXCTL_ROOM_ID="+value)
		}
		if value := strings.TrimSpace(roomRole); value != "" {
			args = append(args, "FOXCTL_ROOM_ROLE="+value)
		}
	}
	args = append(args, "sh", "-lc", buildPaneBootstrapCommand(command))
	return args
}

func buildPaneBootstrapCommand(command string) string {
	ttyFileVar := "${FOXCTL_MUX_TTY_FILE:-}"
	return "if [ -n \"" + ttyFileVar + "\" ]; then mkdir -p \"$(dirname \"" + ttyFileVar + "\")\" && tty > \"" + ttyFileVar + "\"; fi; exec " + command
}

func sanitizeTTYRegistryComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
