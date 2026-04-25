package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/joshka0/foxctl/internal/interfaces/foxproxbridge"
)

type FoxproxSpawnCLIRequest struct {
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	AgentID        string   `json:"agent_id,omitempty"`
	Adapter        string   `json:"adapter,omitempty"`
	Cmd            []string `json:"cmd"`
	Cwd            string   `json:"cwd,omitempty"`
	Env            []string `json:"env,omitempty"`
	Rows           uint16   `json:"rows,omitempty"`
	Cols           uint16   `json:"cols,omitempty"`
	SubmitKey      string   `json:"submit_key,omitempty"`
	Role           string   `json:"role,omitempty"`
	CanMutate      *bool    `json:"can_mutate,omitempty"`
	EnableRawBytes bool     `json:"enable_raw_bytes,omitempty"`
}

type FoxproxSendMessageRequest struct {
	WorkspaceID      string `json:"workspace_id,omitempty"`
	Source           string `json:"source,omitempty"`
	TargetAgentID    string `json:"target_agent_id,omitempty"`
	Text             string `json:"text"`
	SubmitKey        string `json:"submit_key,omitempty"`
	AwaitActivityMS  int64  `json:"await_activity_ms,omitempty"`
	AwaitReadyMS     int64  `json:"await_ready_ms,omitempty"`
	TerminalPolicy   string `json:"terminal_policy,omitempty"`
	PolicyTimeoutMS  int64  `json:"policy_timeout_ms,omitempty"`
	ReceiptVisible   *bool  `json:"receipt_visible,omitempty"`
	CorrelationID    string `json:"correlation_id,omitempty"`
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
}

type foxproxRoomSessionSummary struct {
	Room      *foxproxbridge.RoomInfo                `json:"room,omitempty"`
	Members   []foxproxbridge.MemberInfo             `json:"members"`
	Sessions  []foxproxbridge.SessionInfo            `json:"sessions"`
	Readiness map[string]foxproxbridge.ReadinessInfo `json:"readiness,omitempty"`
	Screens   map[string]any                      `json:"screens,omitempty"`
	Count     int                                 `json:"count"`
}

type spawnSessionDTO struct {
	Cmd            []string `json:"cmd"`
	Cwd            string   `json:"cwd,omitempty"`
	Env            []string `json:"env,omitempty"`
	Rows           uint16   `json:"rows,omitempty"`
	Cols           uint16   `json:"cols,omitempty"`
	Adapter        string   `json:"adapter,omitempty"`
	SubmitKey      string   `json:"submit_key,omitempty"`
	EnableRawBytes bool     `json:"enable_raw_bytes,omitempty"`
}

type createRoomDTO struct {
	Workspace   string `json:"workspace,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// FoxproxHandler exposes a thin HTTP facade over the local Foxprox daemon.
func FoxproxHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/foxprox"), "/")

		// Routes that can validate input before needing a client.
		switch {
		case strings.HasPrefix(path, "foxctl-rooms/") && strings.HasSuffix(path, "/spawn-cli"):
			if r.Method != http.MethodPost {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID := strings.TrimSuffix(strings.TrimPrefix(path, "foxctl-rooms/"), "/spawn-cli")
			client := foxproxbridge.NewClient(foxproxbridge.DefaultSocketPath())
			if client == nil {
				httpError(w, http.StatusServiceUnavailable, "foxprox not linked")
				return
			}
			handleFoxproxFoxctlRoomSpawnCLI(w, r, client, roomID)
			return
		case strings.HasPrefix(path, "foxctl-rooms/") && strings.HasSuffix(path, "/messages"):
			if r.Method != http.MethodPost {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID := strings.TrimSuffix(strings.TrimPrefix(path, "foxctl-rooms/"), "/messages")
			client := foxproxbridge.NewClient(foxproxbridge.DefaultSocketPath())
			if client == nil {
				httpError(w, http.StatusServiceUnavailable, "foxprox not linked")
				return
			}
			handleFoxproxFoxctlRoomSendMessage(w, r, client, roomID)
			return
		}

		// All other routes require a client upfront.
		client := foxproxbridge.NewClient(foxproxbridge.DefaultSocketPath())
		if client == nil {
			httpError(w, http.StatusServiceUnavailable, "foxprox not linked")
			return
		}
		switch {
		case path == "health" && r.Method == http.MethodGet:
			if err := client.Health(r.Context()); err != nil {
				writeFoxproxBridgeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":     true,
				"socket": foxproxbridge.DefaultSocketPath(),
			})
		case path == "sessions" && r.Method == http.MethodGet:
			sessions, err := client.ListSessions(r.Context())
			if err != nil {
				writeFoxproxBridgeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"sessions": sessions,
				"count":    len(sessions),
			})
		case path == "sessions" && r.Method == http.MethodPost:
			var req spawnSessionDTO
			if err := readJSON(w, r, &req); err != nil {
				httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
			session, err := client.CreateSession(r.Context(), req.Cmd, req.Cwd, req.Env, req.Rows, req.Cols, req.Adapter, req.SubmitKey, req.EnableRawBytes)
			if err != nil {
				writeFoxproxBridgeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"session": session})
		case path == "rooms" && r.Method == http.MethodGet:
			rooms, err := client.ListRooms(r.Context())
			if err != nil {
				writeFoxproxBridgeError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"rooms": rooms,
				"count": len(rooms),
			})
		case path == "rooms" && r.Method == http.MethodPost:
			var req createRoomDTO
			if err := readJSON(w, r, &req); err != nil {
				httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
			room, err := client.CreateRoom(r.Context(), req.Workspace, req.Title, req.Description)
			if err != nil {
				writeFoxproxBridgeError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"room": room})
		case isFoxproxFoxctlRoomSessionPath(path):
			if r.Method != http.MethodDelete {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID, sessionID := splitFoxproxFoxctlRoomSessionPath(path)
			handleFoxproxFoxctlRoomDeleteSession(w, r, client, roomID, sessionID)
		case strings.HasPrefix(path, "foxctl-rooms/") && strings.HasSuffix(path, "/sessions"):
			if r.Method != http.MethodGet {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID := strings.TrimSuffix(strings.TrimPrefix(path, "foxctl-rooms/"), "/sessions")
			handleFoxproxFoxctlRoomSessions(w, r, client, roomID)
		default:
			httpError(w, http.StatusNotFound, "not found")
		}
	}
}

func handleFoxproxFoxctlRoomDeleteSession(w http.ResponseWriter, r *http.Request, client foxproxbridge.HTTPClient, roomID, sessionID string) {
	roomID = strings.TrimSpace(roomID)
	sessionID = strings.TrimSpace(sessionID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	if sessionID == "" {
		httpError(w, http.StatusBadRequest, "session_id is required")
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = "."
	}
	room, found, err := findFoxproxRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "linked Foxprox room not found")
		return
	}
	members, err := client.RoomMembers(r.Context(), room.ID)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	agentID := ""
	for _, member := range members {
		if strings.TrimSpace(member.SessionID) == sessionID {
			agentID = member.AgentID
			break
		}
	}
	if agentID == "" {
		httpError(w, http.StatusNotFound, "Foxprox session is not attached to room")
		return
	}
	if err := client.DeleteSession(r.Context(), sessionID); err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room":       room,
		"session_id": sessionID,
		"agent_id":   agentID,
		"status":     "stopped",
	})
}

func handleFoxproxFoxctlRoomSendMessage(w http.ResponseWriter, r *http.Request, client foxproxbridge.HTTPClient, roomID string) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	var req FoxproxSendMessageRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		httpError(w, http.StatusBadRequest, "text is required")
		return
	}
	if client == nil {
		httpError(w, http.StatusServiceUnavailable, "foxprox not linked")
		return
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "."
	}
	room, found, err := findFoxproxRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "linked Foxprox room not found")
		return
	}
	skipAgents := []string{}
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	if targetAgentID != "" {
		members, err := client.RoomMembers(r.Context(), room.ID)
		if err != nil {
			writeFoxproxBridgeError(w, err)
			return
		}
		foundTarget := false
		for _, member := range members {
			if strings.TrimSpace(member.AgentID) == targetAgentID {
				foundTarget = true
				continue
			}
			skipAgents = append(skipAgents, member.AgentID)
		}
		if !foundTarget {
			httpError(w, http.StatusNotFound, "target Foxprox agent not found")
			return
		}
	}
	result, err := client.SendMessage(r.Context(), room.ID, req.Text, strings.TrimSpace(req.Source), strings.TrimSpace(req.SubmitKey), skipAgents, req.AwaitActivityMS, req.AwaitReadyMS)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"room":    room,
		"message": result,
	})
}

func handleFoxproxFoxctlRoomSessions(w http.ResponseWriter, r *http.Request, client foxproxbridge.HTTPClient, roomID string) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = "."
	}
	room, found, err := findFoxproxRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, foxproxRoomSessionSummary{
			Members:  []foxproxbridge.MemberInfo{},
			Sessions: []foxproxbridge.SessionInfo{},
			Count:    0,
		})
		return
	}
	members, err := client.RoomMembers(r.Context(), room.ID)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	allSessions, err := client.ListSessions(r.Context())
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	sessionsByID := make(map[string]foxproxbridge.SessionInfo, len(allSessions))
	for _, session := range allSessions {
		sessionsByID[session.ID] = session
	}
	sessions := make([]foxproxbridge.SessionInfo, 0, len(members))
	readiness := make(map[string]foxproxbridge.ReadinessInfo, len(members))
	screens := make(map[string]any, len(members))
	for _, member := range members {
		session, ok := sessionsByID[member.SessionID]
		if !ok {
			continue
		}
		sessions = append(sessions, session)
		ready, err := client.SessionReadiness(r.Context(), member.SessionID)
		if err == nil {
			readiness[member.SessionID] = ready
		}
		screen, err := client.SessionScreen(r.Context(), member.SessionID)
		if err == nil {
			screens[member.SessionID] = screen
		}
	}
	writeJSON(w, http.StatusOK, foxproxRoomSessionSummary{
		Room:      &room,
		Members:   members,
		Sessions:  sessions,
		Readiness: readiness,
		Screens:   screens,
		Count:     len(sessions),
	})
}

func handleFoxproxFoxctlRoomSpawnCLI(w http.ResponseWriter, r *http.Request, client foxproxbridge.HTTPClient, roomID string) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	var req FoxproxSpawnCLIRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if len(req.Cmd) == 0 || strings.TrimSpace(req.Cmd[0]) == "" {
		httpError(w, http.StatusBadRequest, "cmd is required")
		return
	}
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(req.Adapter)
	}
	if agentID == "" {
		httpError(w, http.StatusBadRequest, "agent_id or adapter is required")
		return
	}
	if client == nil {
		httpError(w, http.StatusServiceUnavailable, "foxprox not linked")
		return
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "."
	}

	room, err := findOrCreateFoxproxRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	session, err := client.CreateSession(r.Context(), req.Cmd, strings.TrimSpace(req.Cwd), req.Env, req.Rows, req.Cols, strings.TrimSpace(req.Adapter), strings.TrimSpace(req.SubmitKey), req.EnableRawBytes)
	if err != nil {
		writeFoxproxBridgeError(w, err)
		return
	}
	canMutate := true
	if req.CanMutate != nil {
		canMutate = *req.CanMutate
	}
	member, err := client.JoinRoom(r.Context(), room.ID, agentID, session.ID, strings.TrimSpace(req.Role), canMutate)
	if err != nil {
		_ = client.DeleteSession(r.Context(), session.ID)
		writeFoxproxBridgeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"room":    room,
		"session": session,
		"member":  member,
	})
}

func isFoxproxFoxctlRoomSessionPath(path string) bool {
	roomID, sessionID := splitFoxproxFoxctlRoomSessionPath(path)
	return roomID != "" && sessionID != ""
}

func splitFoxproxFoxctlRoomSessionPath(path string) (string, string) {
	const prefix = "foxctl-rooms/"
	const marker = "/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return "", ""
	}
	rest := strings.TrimPrefix(path, prefix)
	index := strings.Index(rest, marker)
	if index < 0 {
		return "", ""
	}
	roomID := rest[:index]
	sessionID := rest[index+len(marker):]
	if strings.Contains(sessionID, "/") {
		return "", ""
	}
	return roomID, sessionID
}

func findFoxproxRoomForFoxctl(ctx context.Context, client foxproxbridge.HTTPClient, workspaceID, roomID string) (foxproxbridge.RoomInfo, bool, error) {
	rooms, err := client.ListRooms(ctx)
	if err != nil {
		return foxproxbridge.RoomInfo{}, false, err
	}
	for _, room := range rooms {
		if strings.TrimSpace(room.Workspace) == workspaceID &&
			strings.TrimSpace(room.Title) == roomID &&
			room.ArchivedAt.IsZero() {
			return room, true, nil
		}
	}
	return foxproxbridge.RoomInfo{}, false, nil
}

func findOrCreateFoxproxRoomForFoxctl(ctx context.Context, client foxproxbridge.HTTPClient, workspaceID, roomID string) (foxproxbridge.RoomInfo, error) {
	room, found, err := findFoxproxRoomForFoxctl(ctx, client, workspaceID, roomID)
	if err != nil {
		return foxproxbridge.RoomInfo{}, err
	}
	if found {
		return room, nil
	}
	return client.CreateRoom(ctx, workspaceID, roomID, "foxctl room "+roomID)
}

func writeFoxproxBridgeError(w http.ResponseWriter, err error) {
	if errors.Is(err, foxproxbridge.ErrNotLinked) {
		httpError(w, http.StatusServiceUnavailable, "foxprox not linked")
		return
	}
	httpError(w, http.StatusServiceUnavailable, fmt.Sprintf("foxprox daemon unavailable: %v", err))
}
