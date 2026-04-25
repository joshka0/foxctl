package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	atcpclient "github.com/joshka0/foxctl/internal/atcp/client"
	atcpdaemon "github.com/joshka0/foxctl/internal/atcp/daemon"
	"github.com/joshka0/foxctl/internal/atcp/transport/httpjson"
)

type ATCPSpawnCLIRequest struct {
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

type ATCPSendMessageRequest struct {
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

type atcpRoomSessionSummary struct {
	Room      *httpjson.RoomResponse                `json:"room,omitempty"`
	Members   []httpjson.MemberResponse             `json:"members"`
	Sessions  []httpjson.SessionResponse            `json:"sessions"`
	Readiness map[string]httpjson.ReadinessResponse `json:"readiness,omitempty"`
	Screens   map[string]any                        `json:"screens,omitempty"`
	Count     int                                   `json:"count"`
}

// ATCPHandler exposes a thin HTTP facade over the local ATCP daemon.
func ATCPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/atcp"), "/")
		client := atcpclient.ForSocket(atcpdaemon.DefaultSocketPath())
		switch {
		case path == "health" && r.Method == http.MethodGet:
			if err := client.Health(r.Context()); err != nil {
				writeATCPError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":     true,
				"socket": atcpdaemon.DefaultSocketPath(),
			})
		case path == "sessions" && r.Method == http.MethodGet:
			sessions, err := client.ListSessions(r.Context())
			if err != nil {
				writeATCPError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"sessions": sessions,
				"count":    len(sessions),
			})
		case path == "sessions" && r.Method == http.MethodPost:
			var req httpjson.CreateSessionRequest
			if err := readJSON(w, r, &req); err != nil {
				httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
			session, err := client.CreateSession(r.Context(), req)
			if err != nil {
				writeATCPError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"session": session})
		case path == "rooms" && r.Method == http.MethodGet:
			rooms, err := client.ListRooms(r.Context())
			if err != nil {
				writeATCPError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"rooms": rooms,
				"count": len(rooms),
			})
		case path == "rooms" && r.Method == http.MethodPost:
			var req httpjson.CreateRoomRequest
			if err := readJSON(w, r, &req); err != nil {
				httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
				return
			}
			room, err := client.CreateRoom(r.Context(), req)
			if err != nil {
				writeATCPError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"room": room})
		case isATCPFoxctlRoomSessionPath(path):
			if r.Method != http.MethodDelete {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID, sessionID := splitATCPFoxctlRoomSessionPath(path)
			handleATCPFoxctlRoomDeleteSession(w, r, client, roomID, sessionID)
		case strings.HasPrefix(path, "foxctl-rooms/") && strings.HasSuffix(path, "/sessions"):
			if r.Method != http.MethodGet {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID := strings.TrimSuffix(strings.TrimPrefix(path, "foxctl-rooms/"), "/sessions")
			handleATCPFoxctlRoomSessions(w, r, client, roomID)
		case strings.HasPrefix(path, "foxctl-rooms/") && strings.HasSuffix(path, "/messages"):
			if r.Method != http.MethodPost {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID := strings.TrimSuffix(strings.TrimPrefix(path, "foxctl-rooms/"), "/messages")
			handleATCPFoxctlRoomSendMessage(w, r, client, roomID)
		case strings.HasPrefix(path, "foxctl-rooms/") && strings.HasSuffix(path, "/spawn-cli"):
			if r.Method != http.MethodPost {
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			roomID := strings.TrimSuffix(strings.TrimPrefix(path, "foxctl-rooms/"), "/spawn-cli")
			handleATCPFoxctlRoomSpawnCLI(w, r, client, roomID)
		default:
			httpError(w, http.StatusNotFound, "not found")
		}
	}
}

func handleATCPFoxctlRoomDeleteSession(w http.ResponseWriter, r *http.Request, client *atcpclient.Client, roomID, sessionID string) {
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
	room, found, err := findATCPRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeATCPError(w, err)
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "linked ATCP room not found")
		return
	}
	members, err := client.RoomMembers(r.Context(), room.ID)
	if err != nil {
		writeATCPError(w, err)
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
		httpError(w, http.StatusNotFound, "ATCP session is not attached to room")
		return
	}
	if err := client.DeleteSession(r.Context(), sessionID); err != nil {
		writeATCPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room":       room,
		"session_id": sessionID,
		"agent_id":   agentID,
		"status":     "stopped",
	})
}

func handleATCPFoxctlRoomSendMessage(w http.ResponseWriter, r *http.Request, client *atcpclient.Client, roomID string) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	var req ATCPSendMessageRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		httpError(w, http.StatusBadRequest, "text is required")
		return
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "."
	}
	room, found, err := findATCPRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeATCPError(w, err)
		return
	}
	if !found {
		httpError(w, http.StatusNotFound, "linked ATCP room not found")
		return
	}
	skipAgents := []string{}
	targetAgentID := strings.TrimSpace(req.TargetAgentID)
	if targetAgentID != "" {
		members, err := client.RoomMembers(r.Context(), room.ID)
		if err != nil {
			writeATCPError(w, err)
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
			httpError(w, http.StatusNotFound, "target ATCP agent not found")
			return
		}
	}
	result, err := client.SendMessage(r.Context(), httpjson.SendMessageRequest{
		RoomID:           room.ID,
		Source:           strings.TrimSpace(req.Source),
		CorrelationID:    strings.TrimSpace(req.CorrelationID),
		ReplyToMessageID: strings.TrimSpace(req.ReplyToMessageID),
		Text:             req.Text,
		SubmitKey:        strings.TrimSpace(req.SubmitKey),
		SkipAgents:       skipAgents,
		ReceiptVisible:   req.ReceiptVisible,
		AwaitActivityMS:  req.AwaitActivityMS,
		AwaitReadyMS:     req.AwaitReadyMS,
		TerminalPolicy:   strings.TrimSpace(req.TerminalPolicy),
		PolicyTimeoutMS:  req.PolicyTimeoutMS,
	})
	if err != nil {
		writeATCPError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"room":    room,
		"message": result,
	})
}

func handleATCPFoxctlRoomSessions(w http.ResponseWriter, r *http.Request, client *atcpclient.Client, roomID string) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		workspaceID = "."
	}
	room, found, err := findATCPRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeATCPError(w, err)
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, atcpRoomSessionSummary{
			Members:  []httpjson.MemberResponse{},
			Sessions: []httpjson.SessionResponse{},
			Count:    0,
		})
		return
	}
	members, err := client.RoomMembers(r.Context(), room.ID)
	if err != nil {
		writeATCPError(w, err)
		return
	}
	allSessions, err := client.ListSessions(r.Context())
	if err != nil {
		writeATCPError(w, err)
		return
	}
	sessionsByID := make(map[string]httpjson.SessionResponse, len(allSessions))
	for _, session := range allSessions {
		sessionsByID[session.ID] = session
	}
	sessions := make([]httpjson.SessionResponse, 0, len(members))
	readiness := make(map[string]httpjson.ReadinessResponse, len(members))
	screens := make(map[string]any, len(members))
	for _, member := range members {
		session, ok := sessionsByID[member.SessionID]
		if !ok {
			continue
		}
		sessions = append(sessions, session)
		ready, err := client.SessionReadiness(r.Context(), member.SessionID, atcpclient.SessionReadinessOptions{})
		if err == nil {
			readiness[member.SessionID] = ready
		}
		screen, err := client.SessionScreen(r.Context(), member.SessionID)
		if err == nil {
			screens[member.SessionID] = screen
		}
	}
	writeJSON(w, http.StatusOK, atcpRoomSessionSummary{
		Room:      &room,
		Members:   members,
		Sessions:  sessions,
		Readiness: readiness,
		Screens:   screens,
		Count:     len(sessions),
	})
}

func handleATCPFoxctlRoomSpawnCLI(w http.ResponseWriter, r *http.Request, client *atcpclient.Client, roomID string) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		httpError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	var req ATCPSpawnCLIRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "."
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

	room, err := findOrCreateATCPRoomForFoxctl(r.Context(), client, workspaceID, roomID)
	if err != nil {
		writeATCPError(w, err)
		return
	}
	session, err := client.CreateSession(r.Context(), httpjson.CreateSessionRequest{
		Cmd:            req.Cmd,
		Cwd:            strings.TrimSpace(req.Cwd),
		Env:            req.Env,
		Rows:           req.Rows,
		Cols:           req.Cols,
		Adapter:        strings.TrimSpace(req.Adapter),
		SubmitKey:      strings.TrimSpace(req.SubmitKey),
		EnableRawBytes: req.EnableRawBytes,
	})
	if err != nil {
		writeATCPError(w, err)
		return
	}
	canMutate := true
	if req.CanMutate != nil {
		canMutate = *req.CanMutate
	}
	member, err := client.JoinRoom(r.Context(), room.ID, httpjson.JoinRoomRequest{
		AgentID:   agentID,
		SessionID: session.ID,
		Role:      strings.TrimSpace(req.Role),
		CanMutate: canMutate,
	})
	if err != nil {
		_ = client.DeleteSession(r.Context(), session.ID)
		writeATCPError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"room":    room,
		"session": session,
		"member":  member,
	})
}

func isATCPFoxctlRoomSessionPath(path string) bool {
	roomID, sessionID := splitATCPFoxctlRoomSessionPath(path)
	return roomID != "" && sessionID != ""
}

func splitATCPFoxctlRoomSessionPath(path string) (string, string) {
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

func findATCPRoomForFoxctl(ctx context.Context, client *atcpclient.Client, workspaceID, roomID string) (httpjson.RoomResponse, bool, error) {
	rooms, err := client.ListRooms(ctx)
	if err != nil {
		return httpjson.RoomResponse{}, false, err
	}
	for _, room := range rooms {
		if strings.TrimSpace(room.Workspace) == workspaceID &&
			strings.TrimSpace(room.Title) == roomID &&
			room.ArchivedAt.IsZero() {
			return room, true, nil
		}
	}
	return httpjson.RoomResponse{}, false, nil
}

func findOrCreateATCPRoomForFoxctl(ctx context.Context, client *atcpclient.Client, workspaceID, roomID string) (httpjson.RoomResponse, error) {
	room, found, err := findATCPRoomForFoxctl(ctx, client, workspaceID, roomID)
	if err != nil {
		return httpjson.RoomResponse{}, err
	}
	if found {
		return room, nil
	}
	return client.CreateRoom(ctx, httpjson.CreateRoomRequest{
		Workspace:   workspaceID,
		Title:       roomID,
		Description: "foxctl room " + roomID,
	})
}

func writeATCPError(w http.ResponseWriter, err error) {
	var httpErr *atcpclient.ErrHTTP
	if errors.As(err, &httpErr) {
		status := httpErr.Status
		if status <= 0 {
			status = http.StatusBadGateway
		}
		httpError(w, status, strings.TrimSpace(httpErr.Body))
		return
	}
	httpError(w, http.StatusServiceUnavailable, fmt.Sprintf("atcp daemon unavailable at %s: %v", atcpdaemon.DefaultSocketPath(), err))
}
