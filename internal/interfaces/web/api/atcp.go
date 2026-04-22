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

func findOrCreateATCPRoomForFoxctl(ctx context.Context, client *atcpclient.Client, workspaceID, roomID string) (httpjson.RoomResponse, error) {
	rooms, err := client.ListRooms(ctx)
	if err != nil {
		return httpjson.RoomResponse{}, err
	}
	for _, room := range rooms {
		if strings.TrimSpace(room.Workspace) == workspaceID &&
			strings.TrimSpace(room.Title) == roomID &&
			room.ArchivedAt.IsZero() {
			return room, nil
		}
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
