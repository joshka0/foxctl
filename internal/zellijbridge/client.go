package zellijbridge

import (
	"context"
	"fmt"
	"os/exec"
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
	args := []string{"--session", session, "run"}
	if cwd := strings.TrimSpace(opts.CWD); cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = append(args, "--name", name, "--")
	args = append(args, buildEnvCommand(command, participantID, opts.ParentParticipant, opts.ParentAgentID, opts.RoomID, opts.RoomRole, opts.RoomAccess)...)
	if _, stderr, err := c.runner.Run(ctx, "zellij", args...); err != nil {
		if strings.TrimSpace(stderr) == "" {
			return CreatePaneResult{}, err
		}
		return CreatePaneResult{}, fmt.Errorf("zellij run: %s", strings.TrimSpace(stderr))
	}
	return CreatePaneResult{
		Session:       session,
		PaneName:      name,
		ParticipantID: participantID,
	}, nil
}

func (c *Client) ensureSession(ctx context.Context, session string) error {
	session = strings.TrimSpace(session)
	if session == "" {
		return fmt.Errorf("session is required")
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

func buildEnvCommand(command, participantID, parentParticipant, parentAgentID, roomID, roomRole, roomAccess string) []string {
	args := []string{
		"env",
		"AGENTCTL_PARTICIPANT_ID=" + strings.TrimSpace(participantID),
		"AGENTCTL_ZELLIJ_PARTICIPANT=" + strings.TrimSpace(participantID),
		"AGENTCTL_MUX_BACKEND=zellij",
	}
	if value := strings.TrimSpace(parentParticipant); value != "" {
		args = append(args, "AGENTCTL_PARENT_PARTICIPANT_ID="+value)
	}
	if value := strings.TrimSpace(parentAgentID); value != "" {
		args = append(args, "AGENTCTL_PARENT_AGENT_ID="+value)
	}
	if strings.TrimSpace(roomAccess) == "direct" {
		if value := strings.TrimSpace(roomID); value != "" {
			args = append(args, "AGENTCTL_ROOM_ID="+value)
		}
		if value := strings.TrimSpace(roomRole); value != "" {
			args = append(args, "AGENTCTL_ROOM_ROLE="+value)
		}
	}
	args = append(args, "sh", "-lc", command)
	return args
}
