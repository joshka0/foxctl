package flow

import (
	"context"
	"fmt"

	"github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
)

// FoxproxClient is the interface wrapping the foxprox HTTP client methods
// needed by the spawner. This allows tests to inject mocks.
//
// Room-based coordination: the spawner creates a foxprox room for each flow
// run, joins the agent's PTY session to the room as a member, and sends tasks
// via room messages. The agent (droid) receives messages naturally through its
// TUI, does the work, then pushes structured output back via
// `foxctl flow output`. This replaces the old screen-scraping model with
// structured message delivery through foxprox's room fan-out.
type FoxproxClient interface {
	// Session lifecycle
	CreateSession(ctx context.Context, req httpjson.CreateSessionRequest) (httpjson.SessionResponse, error)
	DeleteSession(ctx context.Context, id string) error
	SessionReadiness(ctx context.Context, id string, opts client.SessionReadinessOptions) (httpjson.ReadinessResponse, error)

	// Room coordination
	CreateRoom(ctx context.Context, req httpjson.CreateRoomRequest) (httpjson.RoomResponse, error)
	JoinRoom(ctx context.Context, roomID string, req httpjson.JoinRoomRequest) (httpjson.MemberResponse, error)
	LeaveRoom(ctx context.Context, roomID string, req httpjson.LeaveRoomRequest) (httpjson.MemberResponse, error)
	SendMessage(ctx context.Context, req httpjson.SendMessageRequest) (httpjson.SendMessageResponse, error)
}

// FoxproxSpawnerConfig configures the foxprox agent spawner.
type FoxproxSpawnerConfig struct {
	// CLICmd is the CLI agent command to launch (default: "droid").
	CLICmd string

	// ReadinessDebounceMS is the output-idle debounce window in milliseconds.
	// Default: 2000 (2 seconds of output silence = idle).
	ReadinessDebounceMS int64

	// ReadinessThresholdBPS is the bytes-per-second threshold below which
	// output is considered idle. Default: 50.
	ReadinessThresholdBPS float64

	// FlowRunID, when set, is injected into the agent's prompt so the agent
	// knows where to push output via `foxctl flow output`.
	FlowRunID string

	// FlowNodeID, when set, is injected into the agent's prompt.
	FlowNodeID string

	// RoomID, when set, reuses an existing room instead of creating a new one.
	// When empty, Spawn creates a new room with a generated title.
	RoomID string
}

// defaults applies zero-value defaults.
func (c *FoxproxSpawnerConfig) defaults() {
	if c.CLICmd == "" {
		c.CLICmd = "droid"
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
// the foxprox daemon, using room-based coordination for structured message
// delivery. The spawner creates a foxprox room for each agent, joins the
// PTY session as a member, and sends tasks via room messages. Output comes
// back through the flow engine's push API, not screen capture.
type foxproxAgentSpawner struct {
	client FoxproxClient
	config FoxproxSpawnerConfig
	roomID string // set by Spawn, used by Ask/Kill
}

// NewFoxproxAgentSpawner creates a new AgentSpawner backed by foxprox.
func NewFoxproxAgentSpawner(client FoxproxClient, config FoxproxSpawnerConfig) *foxproxAgentSpawner {
	config.defaults()
	return &foxproxAgentSpawner{
		client: client,
		config: config,
	}
}

// Spawn creates a foxprox PTY session, creates a foxprox room, and joins
// the session to the room. The session ID serves as both agent_id and
// session_id.
//
// Steps:
//  1. Create PTY session with the CLI agent command
//  2. Create (or reuse) a foxprox room for the flow run
//  3. Join the PTY session to the room as a member
//
// If the config has FlowRunID and FlowNodeID set, these are injected
// into the prompt so the agent knows where to push output.
func (s *foxproxAgentSpawner) Spawn(ctx context.Context, role, prompt string, opts AgentSpawnOptions) (*AgentSpawnResult, error) {
	// Determine the CLI command: per-node override > spawner default.
	cliCmd := s.config.CLICmd
	if opts.CLICmd != "" {
		cliCmd = opts.CLICmd
	}

	// Determine the working directory.
	cwd := opts.Workspace

	// Inject flow run_id and node_id into the prompt if configured.
	// This tells the agent where to push its output via `foxctl flow output`.
	if s.config.FlowRunID != "" && s.config.FlowNodeID != "" {
		flowContext := fmt.Sprintf(
			"\n\n--- Flow Output Push Configuration ---\n"+
				"You are running as part of a flow. When you have completed your task, "+
				"push your structured output back to the flow engine by running:\n\n"+
				"  foxctl flow output %s --node %s --data '<your-json-output>' --workspace '%s'\n\n"+
				"Replace <your-json-output> with your actual result as a JSON object.\n"+
				"This is REQUIRED as your final step before completing.\n",
			s.config.FlowRunID, s.config.FlowNodeID, cwd,
		)
		prompt = prompt + flowContext
	}

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
		return nil, fmt.Errorf("foxprox spawner: spawn: create session: %w", err)
	}

	// Create or reuse a foxprox room for this agent.
	roomID := s.config.RoomID
	if roomID == "" {
		roomTitle := fmt.Sprintf("flow-run-%s", s.config.FlowRunID)
		if s.config.FlowRunID == "" {
			roomTitle = fmt.Sprintf("agent-%s", resp.ID)
		}
		roomResp, rErr := s.client.CreateRoom(ctx, httpjson.CreateRoomRequest{
			Workspace: cwd,
			Title:     roomTitle,
		})
		if rErr != nil {
			// Clean up the session we just created.
			_ = s.client.DeleteSession(ctx, resp.ID)
			return nil, fmt.Errorf("foxprox spawner: spawn: create room: %w", rErr)
		}
		roomID = roomResp.ID
	}
	s.roomID = roomID

	// Join the PTY session to the room as a member.
	_, err = s.client.JoinRoom(ctx, roomID, httpjson.JoinRoomRequest{
		AgentID:   resp.ID,
		SessionID: resp.ID,
		Role:      role,
		CanMutate: true,
	})
	if err != nil {
		// Clean up session and leave room on join failure.
		_ = s.client.DeleteSession(ctx, resp.ID)
		_, _ = s.client.LeaveRoom(ctx, roomID, httpjson.LeaveRoomRequest{
			AgentID: resp.ID,
		})
		return nil, fmt.Errorf("foxprox spawner: spawn: join room: %w", err)
	}

	return &AgentSpawnResult{
		AgentID:   resp.ID,
		SessionID: resp.ID,
	}, nil
}

// Ask sends the task as a room message via foxprox client.SendMessage.
// Foxprox's router delivers the message to the agent's PTY through the room
// fan-out. This is fire-and-forget: the method returns immediately after the
// message is sent. Output comes back via the flow engine's push API
// (`foxctl flow output`), not via screen capture.
func (s *foxproxAgentSpawner) Ask(ctx context.Context, agentID string, message string, timeoutMS int) (*AgentAskResult, error) {
	if s.roomID == "" {
		return nil, fmt.Errorf("foxprox spawner: ask: no room (spawn not called?)")
	}

	// Send the task as a room message. The foxprox router delivers it to
	// the agent's PTY via the room's fan-out mechanism.
	_, err := s.client.SendMessage(ctx, httpjson.SendMessageRequest{
		RoomID: s.roomID,
		Text:   message,
	})
	if err != nil {
		return nil, fmt.Errorf("foxprox spawner: ask: send message: %w", err)
	}

	// Fire-and-forget: return immediately. Output comes via push.
	return &AgentAskResult{
		Reply:  "",
		Status: "sent",
	}, nil
}

// Info checks the session status via the readiness endpoint.
// It reports "running" for active sessions, "exited" for gone sessions.
// No screen capture is performed — the summary comes from the push output.
func (s *foxproxAgentSpawner) Info(ctx context.Context, agentID string) (*AgentInfoResult, error) {
	readiness, err := s.client.SessionReadiness(ctx, agentID, client.SessionReadinessOptions{})
	if err != nil {
		// Session likely exited or was deleted.
		return &AgentInfoResult{
			Status: "exited",
		}, nil
	}

	if readiness.Idle {
		return &AgentInfoResult{
			Status: "completed",
			Summary: fmt.Sprintf("output idle for %dms", readiness.IdleForMS),
		}, nil
	}

	return &AgentInfoResult{
		Status: "running",
	}, nil
}

// Kill leaves the foxprox room and then deletes the PTY session.
func (s *foxproxAgentSpawner) Kill(ctx context.Context, sessionID string) error {
	// Leave the room first, then delete the session.
	if s.roomID != "" {
		_, _ = s.client.LeaveRoom(ctx, s.roomID, httpjson.LeaveRoomRequest{
			AgentID: sessionID,
		})
	}

	err := s.client.DeleteSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("foxprox spawner: kill: %w", err)
	}
	return nil
}
