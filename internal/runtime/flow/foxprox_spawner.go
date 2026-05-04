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
// the foxprox daemon.
//
// Two modes of operation:
//
// Room-based (non-push): Creates a foxprox room for the flow run, joins the
// agent's PTY session as a member, and sends tasks via room messages. Output
// comes back through the flow engine's push API.
//
// Exec-based (push mode): When OutputMode is "push" in the spawn options,
// uses `droid exec --auto medium "<prompt>"` instead of interactive `droid`.
// This avoids droid's permission dialog blocking issue. The prompt (including
// push instructions already injected by AgentExecutor) is passed as the exec
// argument. No room creation, join, or messages needed.
type foxproxAgentSpawner struct {
	client   FoxproxClient
	config   FoxproxSpawnerConfig
	roomID   string // set by Spawn in room mode, empty in exec mode
	execMode bool   // true when last Spawn used push mode
}

// NewFoxproxAgentSpawner creates a new AgentSpawner backed by foxprox.
func NewFoxproxAgentSpawner(client FoxproxClient, config FoxproxSpawnerConfig) *foxproxAgentSpawner {
	config.defaults()
	return &foxproxAgentSpawner{
		client: client,
		config: config,
	}
}

// Spawn creates a foxprox PTY session. The behavior depends on whether push
// mode is active (opts.OutputMode == "push"):
//
// Push mode: Uses `droid exec --skip-permissions-unsafe "<prompt>"` to run droid
// non-interactively with all permissions bypassed. The flow engine runs agents
// in foxprox PTY sessions which are isolated environments. The prompt (including
// push instructions already injected by AgentExecutor) is passed as the exec
// argument. No room creation or join — droid exec doesn't need rooms.
//
// Non-push mode: Creates a foxprox room, joins the session as a member. The
// session ID serves as both agent_id and session_id. Tasks are sent via room
// messages (Ask).
func (s *foxproxAgentSpawner) Spawn(ctx context.Context, role, prompt string, opts AgentSpawnOptions) (*AgentSpawnResult, error) {
	// Determine the CLI command: per-node override > spawner default.
	cliCmd := s.config.CLICmd
	if opts.CLICmd != "" {
		cliCmd = opts.CLICmd
	}

	// Determine the working directory.
	cwd := opts.Workspace

	// Push mode: when OutputMode is "push", use droid exec --skip-permissions-unsafe
	// to bypass all permission dialogs. The AgentExecutor already injects push
	// instructions into the prompt, so we don't add our own.
	isPushMode := opts.OutputMode == "push"
	s.execMode = isPushMode

	var cmd []string
	if isPushMode {
		// In push mode, pass the full prompt as the droid exec argument.
		cmd = []string{cliCmd, "exec", "--skip-permissions-unsafe", prompt}
	} else {
		cmd = []string{cliCmd}
	}

	// Create the session with readiness profile for output-idle detection.
	req := httpjson.CreateSessionRequest{
		Cmd: cmd,
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

	// In push mode, skip room creation and join entirely.
	// droid exec runs non-interactively — no room coordination needed.
	if isPushMode {
		return &AgentSpawnResult{
			AgentID:   resp.ID,
			SessionID: resp.ID,
		}, nil
	}

	// Non-push mode: create or reuse a foxprox room for this agent.
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

// Ask sends the task to the agent. The behavior depends on the spawn mode:
//
// Exec mode (push): Returns immediately with status "exec". The prompt was
// already passed as the droid exec argument during Spawn — no additional
// message needed.
//
// Room mode (non-push): Sends the task as a room message via foxprox
// client.SendMessage. Foxprox's router delivers the message to the agent's
// PTY through the room fan-out. This is fire-and-forget: the method returns
// immediately after the message is sent. Output comes back via the flow
// engine's push API (`foxctl flow output`), not via screen capture.
func (s *foxproxAgentSpawner) Ask(ctx context.Context, agentID string, message string, timeoutMS int) (*AgentAskResult, error) {
	// In exec mode, the prompt was already passed as the exec argument.
	// Return immediately — no room message needed.
	if s.execMode {
		return &AgentAskResult{
			Reply:  "",
			Status: "exec",
		}, nil
	}

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
			Status:  "completed",
			Summary: fmt.Sprintf("output idle for %dms", readiness.IdleForMS),
		}, nil
	}

	return &AgentInfoResult{
		Status: "running",
	}, nil
}

// Kill terminates the agent session. In exec mode (push), it skips LeaveRoom
// since no room was created. In room mode, it leaves the room first, then
// deletes the PTY session.
func (s *foxproxAgentSpawner) Kill(ctx context.Context, sessionID string) error {
	// In exec mode, no room was created — skip LeaveRoom.
	if !s.execMode && s.roomID != "" {
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
