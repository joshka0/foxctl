// Package foxproxwire registers the concrete Foxprox implementation with the
// foxproxbridge. Import this package with a blank import in main() to wire
// up Foxprox support:
//
//	import _ "github.com/joshka0/foxctl/internal/interfaces/foxproxbridge/foxproxwire"
//
// When this package is not imported, the bridge returns ErrNotLinked and
// the foxctl binary runs without Foxprox support.
package foxproxwire

import (
	"context"
	"io"
	"log/slog"

	"github.com/joshka/foxprox/foxprox/broker/vtscreen"
	foxproxclient "github.com/joshka/foxprox/foxprox/client"
	foxproxdaemon "github.com/joshka/foxprox/foxprox/daemon"
	"github.com/joshka/foxprox/foxprox/transport/httpjson"
	"github.com/joshka/foxprox/foxprox/transport/unixsocket"
	"github.com/joshka0/foxctl/internal/interfaces/foxproxbridge"
)

func init() {
	foxproxbridge.RegisterDaemon(newDaemon, unixsocket.ErrBrokerAlreadyRunning)
	foxproxbridge.RegisterDefaultSocketPath(foxproxdaemon.DefaultSocketPath)
	foxproxbridge.RegisterClientFactory(newClient)
}

// daemonShim adapts foxproxdaemon.Daemon to foxproxbridge.DaemonLifecycle.
type daemonShim struct {
	d *foxproxdaemon.Daemon
}

func newDaemon(opts foxproxbridge.DaemonOptions) (foxproxbridge.DaemonLifecycle, error) {
	var w io.Writer
	if opts.LogWriter != nil {
		w, _ = opts.LogWriter.(io.Writer)
	}
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
	d := foxproxdaemon.New(foxproxdaemon.Options{
		DataDir:    opts.DataDir,
		SocketPath: opts.SocketPath,
		Logger:     logger,
	})
	return &daemonShim{d: d}, nil
}

func (s *daemonShim) SocketPath() string              { return s.d.SocketPath() }
func (s *daemonShim) Start() error                     { return s.d.Start() }
func (s *daemonShim) Wait(ctx context.Context) error   { return s.d.Wait(ctx) }
func (s *daemonShim) Shutdown(ctx context.Context) error { return s.d.Shutdown(ctx) }

// clientShim adapts foxproxclient.Client to foxproxbridge.HTTPClient.
type clientShim struct {
	c *foxproxclient.Client
}

func newClient(socketPath string) foxproxbridge.HTTPClient {
	return &clientShim{c: foxproxclient.ForSocket(socketPath)}
}

func (cs *clientShim) Health(ctx context.Context) error {
	return cs.c.Health(ctx)
}

func (cs *clientShim) ListSessions(ctx context.Context) ([]foxproxbridge.SessionInfo, error) {
	sessions, err := cs.c.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]foxproxbridge.SessionInfo, len(sessions))
	for i, s := range sessions {
		out[i] = foxproxbridge.SessionInfo{
			ID:               s.ID,
			Status:           s.Status,
			PID:              s.PID,
			CreatedAt:        s.CreatedAt,
			ExitedAt:         s.ExitedAt,
			ExitCode:         s.ExitCode,
			ExitError:        s.ExitError,
			LastSeq:          s.LastSeq,
			Cmd:              s.Cmd,
			Cwd:              s.Cwd,
			Adapter:          s.Adapter,
			SubmitKey:        s.SubmitKey,
			EnableRawBytes:   s.EnableRawBytes,
			OutputBytesTotal: s.OutputBytesTotal,
		}
	}
	return out, nil
}

func (cs *clientShim) CreateSession(ctx context.Context, cmd []string, cwd string, env []string, rows, cols uint16, adapter, submitKey string, enableRawBytes bool) (foxproxbridge.SessionInfo, error) {
	s, err := cs.c.CreateSession(ctx, httpjson.CreateSessionRequest{
		Cmd:            cmd,
		Cwd:            cwd,
		Env:            env,
		Rows:           rows,
		Cols:           cols,
		Adapter:        adapter,
		SubmitKey:      submitKey,
		EnableRawBytes: enableRawBytes,
	})
	if err != nil {
		return foxproxbridge.SessionInfo{}, err
	}
	return foxproxbridge.SessionInfo{
		ID:               s.ID,
		Status:           s.Status,
		PID:              s.PID,
		CreatedAt:        s.CreatedAt,
		Cmd:              s.Cmd,
		Cwd:              s.Cwd,
		Adapter:          s.Adapter,
		SubmitKey:        s.SubmitKey,
		EnableRawBytes:   s.EnableRawBytes,
		OutputBytesTotal: s.OutputBytesTotal,
	}, nil
}

func (cs *clientShim) DeleteSession(ctx context.Context, id string) error {
	return cs.c.DeleteSession(ctx, id)
}

func (cs *clientShim) SessionReadiness(ctx context.Context, id string) (foxproxbridge.ReadinessInfo, error) {
	r, err := cs.c.SessionReadiness(ctx, id, foxproxclient.SessionReadinessOptions{})
	if err != nil {
		return foxproxbridge.ReadinessInfo{}, err
	}
	return foxproxbridge.ReadinessInfo{
		Idle:        r.Idle,
		IdleForMS:   r.IdleForMS,
		ScreenMatch: r.ScreenMatch,
	}, nil
}

func (cs *clientShim) SessionScreen(ctx context.Context, id string) (map[string]any, error) {
	snap, err := cs.c.SessionScreen(ctx, id)
	if err != nil {
		return nil, err
	}
	return screenToMap(snap), nil
}

func (cs *clientShim) ListRooms(ctx context.Context) ([]foxproxbridge.RoomInfo, error) {
	rooms, err := cs.c.ListRooms(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]foxproxbridge.RoomInfo, len(rooms))
	for i, r := range rooms {
		out[i] = foxproxbridge.RoomInfo{
			ID:          r.ID,
			Workspace:   r.Workspace,
			Title:       r.Title,
			Description: r.Description,
			ArchivedAt:  r.ArchivedAt,
			CreatedAt:   r.CreatedAt,
		}
	}
	return out, nil
}

func (cs *clientShim) CreateRoom(ctx context.Context, workspace, title, description string) (foxproxbridge.RoomInfo, error) {
	r, err := cs.c.CreateRoom(ctx, httpjson.CreateRoomRequest{
		Workspace:   workspace,
		Title:       title,
		Description: description,
	})
	if err != nil {
		return foxproxbridge.RoomInfo{}, err
	}
	return foxproxbridge.RoomInfo{
		ID:          r.ID,
		Workspace:   r.Workspace,
		Title:       r.Title,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
	}, nil
}

func (cs *clientShim) RoomMembers(ctx context.Context, roomID string) ([]foxproxbridge.MemberInfo, error) {
	members, err := cs.c.RoomMembers(ctx, roomID)
	if err != nil {
		return nil, err
	}
	out := make([]foxproxbridge.MemberInfo, len(members))
	for i, m := range members {
		out[i] = foxproxbridge.MemberInfo{
			AgentID:   m.AgentID,
			SessionID: m.SessionID,
			Role:      m.Role,
		}
	}
	return out, nil
}

func (cs *clientShim) JoinRoom(ctx context.Context, roomID, agentID, sessionID, role string, canMutate bool) (foxproxbridge.MemberInfo, error) {
	m, err := cs.c.JoinRoom(ctx, roomID, httpjson.JoinRoomRequest{
		AgentID:   agentID,
		SessionID: sessionID,
		Role:      role,
		CanMutate: canMutate,
	})
	if err != nil {
		return foxproxbridge.MemberInfo{}, err
	}
	return foxproxbridge.MemberInfo{
		AgentID:   m.AgentID,
		SessionID: m.SessionID,
		Role:      m.Role,
	}, nil
}

func (cs *clientShim) SendMessage(ctx context.Context, roomID, text, source, submitKey string, skipAgents []string, awaitActivityMS, awaitReadyMS int64) (map[string]any, error) {
	result, err := cs.c.SendMessage(ctx, httpjson.SendMessageRequest{
		RoomID:          roomID,
		Text:            text,
		Source:          source,
		SubmitKey:       submitKey,
		SkipAgents:      skipAgents,
		AwaitActivityMS: awaitActivityMS,
		AwaitReadyMS:    awaitReadyMS,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"delivered":  result.Delivered,
		"failed":     result.Failed,
		"message_id": result.MessageID,
	}, nil
}

func screenToMap(s vtscreen.Snapshot) map[string]any {
	return map[string]any{
		"rows":       s.Rows,
		"cols":       s.Cols,
		"lines":      s.Lines,
		"dirty_rows": s.DirtyRows,
		"cursor":     s.Cursor,
		"alt_screen": s.AltScreen,
	}
}
