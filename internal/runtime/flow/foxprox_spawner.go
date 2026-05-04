package flow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshka/foxprox/foxprox/broker/vtscreen"
	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
)

// FoxproxClient is the interface wrapping the foxprox HTTP client methods
// needed by the spawner. This allows tests to inject mocks.
type FoxproxClient interface {
	CreateSession(ctx context.Context, req httpjson.CreateSessionRequest) (httpjson.SessionResponse, error)
	DeleteSession(ctx context.Context, id string) error
	SessionReadiness(ctx context.Context, id string, opts client.SessionReadinessOptions) (httpjson.ReadinessResponse, error)
	SessionScreen(ctx context.Context, id string) (vtscreen.Snapshot, error)
	TerminalSubmit(ctx context.Context, req client.TerminalSubmitRequest) (client.TerminalSubmitResponse, error)
}

// FoxproxSpawnerConfig configures the foxprox agent spawner.
type FoxproxSpawnerConfig struct {
	// CLICmd is the CLI agent command to launch (default: "droid").
	CLICmd string

	// PollInterval controls how often readiness is polled during Ask.
	// Default: 500ms.
	PollInterval time.Duration

	// ReadinessDebounceMS is the output-idle debounce window in milliseconds.
	// Default: 2000 (2 seconds of output silence = idle).
	ReadinessDebounceMS int64

	// ReadinessThresholdBPS is the bytes-per-second threshold below which
	// output is considered idle. Default: 50.
	ReadinessThresholdBPS float64
}

// defaults applies zero-value defaults.
func (c *FoxproxSpawnerConfig) defaults() {
	if c.CLICmd == "" {
		c.CLICmd = "droid"
	}
	if c.PollInterval == 0 {
		c.PollInterval = 500 * time.Millisecond
	}
	if c.ReadinessDebounceMS == 0 {
		c.ReadinessDebounceMS = 2000
	}
	if c.ReadinessThresholdBPS == 0 {
		c.ReadinessThresholdBPS = 50
	}
}

// foxproxAgentSpawner implements AgentSpawner using a foxprox HTTP client.
// It drives CLI agents (droid, claude, etc.) via PTY sessions managed by
// the foxprox daemon. Foxprox handles all PTY management (bracketed paste,
// submit keys, output readiness, screen model). The spawner just orchestrates
// session lifecycle and output capture.
type foxproxAgentSpawner struct {
	client FoxproxClient
	config FoxproxSpawnerConfig
}

// NewFoxproxAgentSpawner creates a new AgentSpawner backed by foxprox.
func NewFoxproxAgentSpawner(client FoxproxClient, config FoxproxSpawnerConfig) *foxproxAgentSpawner {
	config.defaults()
	return &foxproxAgentSpawner{
		client: client,
		config: config,
	}
}

// Spawn creates a foxprox PTY session with the CLI agent command.
// The session ID serves as both agent_id and session_id.
// If opts.CLICmd is set, it overrides the spawner's default CLICmd.
func (s *foxproxAgentSpawner) Spawn(ctx context.Context, role, prompt string, opts AgentSpawnOptions) (*AgentSpawnResult, error) {
	// Determine the CLI command: per-node override > spawner default.
	cliCmd := s.config.CLICmd
	if opts.CLICmd != "" {
		cliCmd = opts.CLICmd
	}

	// Determine the working directory.
	cwd := opts.Workspace

	// Create the session with readiness profile for output-idle detection.
	req := httpjson.CreateSessionRequest{
		Cmd: []string{cliCmd},
		Cwd: cwd,
		Readiness: &httpjson.ReadinessProfileDTO{
			DebounceMS:          s.config.ReadinessDebounceMS,
			ThresholdBPS:        s.config.ReadinessThresholdBPS,
			RequireNotAltScreen: true,
		},
	}

	resp, err := s.client.CreateSession(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("foxprox spawner: spawn: %w", err)
	}

	return &AgentSpawnResult{
		AgentID:   resp.ID,
		SessionID: resp.ID,
	}, nil
}

// Ask sends a prompt via terminal.submit, waits for output-idle readiness,
// then captures the screen content as the reply.
func (s *foxproxAgentSpawner) Ask(ctx context.Context, agentID string, message string, timeoutMS int) (*AgentAskResult, error) {
	// Submit the prompt via terminal.submit.
	_, err := s.client.TerminalSubmit(ctx, client.TerminalSubmitRequest{
		SessionID: agentID,
		Text:      message,
	})
	if err != nil {
		return nil, fmt.Errorf("foxprox spawner: ask: submit: %w", err)
	}

	// Poll readiness until output-idle or timeout.
	deadline := time.Now().Add(time.Duration(timeoutMS) * time.Millisecond)
	pollTicker := time.NewTicker(s.config.PollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("foxprox spawner: ask: context cancelled: %w", ctx.Err())
		case <-pollTicker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("foxprox spawner: ask: timed out after %dms waiting for output idle", timeoutMS)
			}

			readiness, rErr := s.client.SessionReadiness(ctx, agentID, client.SessionReadinessOptions{})
			if rErr != nil {
				// Readiness error likely means session is gone.
				return nil, fmt.Errorf("foxprox spawner: ask: readiness: %w", rErr)
			}

			if readiness.Idle {
				// Output is idle — capture the screen.
				screen, sErr := s.client.SessionScreen(ctx, agentID)
				if sErr != nil {
					return nil, fmt.Errorf("foxprox spawner: ask: screen: %w", sErr)
				}

				reply := screenToString(screen)
				return &AgentAskResult{
					Reply:  reply,
					Status: "replied",
				}, nil
			}
			// Not idle yet, keep polling.
		}
	}
}

// Info checks the session status via the readiness endpoint.
// If the session exists and has output activity, it's "running".
// If the readiness check fails (session not found), it's "exited".
func (s *foxproxAgentSpawner) Info(ctx context.Context, agentID string) (*AgentInfoResult, error) {
	readiness, err := s.client.SessionReadiness(ctx, agentID, client.SessionReadinessOptions{})
	if err != nil {
		// Session likely exited or was deleted.
		return &AgentInfoResult{
			Status: "exited",
		}, nil
	}

	// Session exists — it's running (may be idle or active).
	// Use readiness info for optional summary detail.
	_ = readiness
	return &AgentInfoResult{
		Status: "running",
	}, nil
}

// Kill deletes the foxprox session, terminating the PTY process.
func (s *foxproxAgentSpawner) Kill(ctx context.Context, sessionID string) error {
	err := s.client.DeleteSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("foxprox spawner: kill: %w", err)
	}
	return nil
}

// screenToString converts a vtscreen.Snapshot to a trimmed string,
// joining non-empty lines with newlines.
func screenToString(snap vtscreen.Snapshot) string {
	var lines []string
	for _, line := range snap.Lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}
